// Package selection chooses which of several valid DNS answers — gathered from
// multiple upstream resolvers that all serve the same zone — should be returned
// to the client. A Policy is a pure function of the question and the gathered
// candidates: no network I/O, no logging (ambiguity is surfaced in Result for the
// caller), which keeps it exhaustively table-testable and keeps the resolver dumb.
//
// baseTier classifies every response onto a policy-independent quality ladder and
// pickBest picks the highest-scoring candidate with a deterministic tiebreak; a
// ranking policy refines only the top "answer" tier, so a future one (e.g.
// prefer_routed) needs no change below it. Both grade a response as an answer to
// the question asked: a reply is only an answer about a name it actually mentions.
//
// Ordering is all this package does. It never suppresses, rewrites or repairs a
// response — the chosen *dns.Msg is returned unmodified, and a lone candidate is
// returned however it looks, exactly as an unpooled resolver would.
//
// Policy is ADR-0023's SelectionPolicy and Candidate its CandidateResponse; the
// shorter names avoid stuttering with the package name. The ladder's ordering is
// reasoned through in docs/adr/0023-policy-based-multi-resolver-dns.md.
package selection

import (
	"net/netip"
	"sort"
	"strings"

	"github.com/miekg/dns"
)

// DefaultPolicy is the policy used when none is configured. It reproduces the
// pre-existing behavior: the first resolver to return a response wins.
const DefaultPolicy = "first_success"

// Candidate is one resolver's answer to a query, as gathered by the pool. It
// carries only what selection needs today; a future policy's signals (ADR-0023
// D11) can be added without changing this interface.
type Candidate struct {
	// Resp is the resolver's reply, or nil if it did not answer. It must be an
	// actual reply — the QR bit set (Resp.Response), as every DNS exchange
	// yields; a message without it counts as "no reply". The pool sets Resp = nil
	// for a transport error or timeout.
	Resp *dns.Msg
	// Source identifies the resolver (host:port) and is the stable, deterministic
	// tiebreak key. An identifier, never a secret.
	Source string
}

// Result is the outcome of a selection.
type Result struct {
	// Resp is the chosen response.
	Resp *dns.Msg
	// Source is the resolver that produced Resp (for the caller's logging).
	Source string
	// Ambiguous is true only when two or more resolvers returned DIFFERENT
	// private addresses for the name. Identical private answers from redundant
	// resolvers are normal HA, not a conflict. The choice is deterministic either
	// way; the flag lets the pool warn, throttled (ADR-0023 D4).
	Ambiguous bool
}

// Policy ranks candidates and returns the winning response.
type Policy interface {
	// Name is the stable config identifier, e.g. "prefer_private".
	Name() string
	// Select returns the chosen Result and true, or (zero, false) when no
	// candidate is usable — in which case the caller returns SERVFAIL.
	Select(q dns.Question, cands []Candidate) (Result, bool)
}

// Base response tiers: the shared, policy-independent quality ladder, low to
// high. baseTier maps every response onto one of these; a ranking policy refines
// only tierAnswer, so the lower ordering is decided once for every policy. Why it
// is ordered this way is reasoned through in ADR-0023's notes (D3).
//
// All tiers but one grade the response's content. tierOffTopic grades its
// relevance: a reply can be well formed and still say nothing about the name that
// was asked for, in which case it must not be read as an answer to it.
const (
	tierNone     = iota // no reply at all (nil): transport error / timeout — nothing to return
	tierErrored         // an error-rcode reply (SERVFAIL/REFUSED/…); may carry an RFC 8914 EDE diagnostic
	tierNXDOMAIN        // the name does not exist (definitive)
	tierNODATA          // the name exists but has no record of the queried type
	tierOffTopic        // carries records, but none of them is about the queried name
	tierReferral        // a NOERROR CNAME/DNAME alias with no resolved address — a referral to follow
	tierAnswer          // carries an A/AAAA of the queried type — a ranking policy refines this
)

// scorePrivate is prefer_private's sole refinement of the base ladder: a
// private/CGNAT answer ranks one step above a public one (which keeps tierAnswer).
const scorePrivate = tierAnswer + 1

type firstSuccess struct{}

// Name implements Policy.
func (firstSuccess) Name() string { return "first_success" }

// Select returns the first candidate that produced a response, in the order
// given — the resolver's pre-existing sequential behavior 1:1: any response wins
// (including SERVFAIL), only a non-answering upstream (nil) is skipped.
func (firstSuccess) Select(_ dns.Question, cands []Candidate) (Result, bool) {
	for _, c := range cands {
		if responded(c.Resp) {
			return Result{Resp: c.Resp, Source: c.Source}, true
		}
	}
	return Result{}, false
}

type preferPrivate struct{}

// Name implements Policy.
func (preferPrivate) Name() string { return "prefer_private" }

// Select ranks A/AAAA answers by refining the shared base ladder: a private/CGNAT
// address beats a public one, and within a tier the lowest Source wins, so the
// outcome does not depend on arrival order (ADR-0023 D3/D4). It returns false only
// when no candidate replied; the caller then synthesizes SERVFAIL. A non-address
// query has no address notion — the pool does not fan those out (D5) — and yields
// a responded answer by lowest Source.
func (preferPrivate) Select(q dns.Question, cands []Candidate) (Result, bool) {
	if atype, isAddr := addrType(q.Qtype); isAddr {
		best, top := pickBest(cands, func(c Candidate) int {
			t := baseTier(c.Resp, q, atype)
			if t == tierAnswer && allAddrsPrivate(c.Resp, q, atype) {
				return scorePrivate
			}
			return t
		})
		if top == tierNone {
			return Result{}, false
		}
		return Result{
			Resp:      best.Resp,
			Source:    best.Source,
			Ambiguous: top == scorePrivate && privateDisagreement(cands, q, atype),
		}, true
	}

	// Non-address query: no address notion — return a responded answer
	// deterministically by lowest Source.
	best, top := pickBest(cands, func(c Candidate) int {
		if responded(c.Resp) {
			return 1
		}
		return 0
	})
	if top == 0 {
		return Result{}, false
	}
	return Result{Resp: best.Resp, Source: best.Source}, true
}

// pickBest returns the candidate with the highest positive score and that score.
// Ties are broken by the lowest Source, so the result is deterministic
// regardless of arrival order. topScore is 0 when no candidate scores > 0. This
// is the shared ranking spine; policies differ only in the score they supply.
func pickBest(cands []Candidate, score func(Candidate) int) (best Candidate, topScore int) {
	for _, c := range cands {
		s := score(c)
		if s <= 0 {
			continue
		}
		switch {
		case s > topScore:
			best, topScore = c, s
		case s == topScore && c.Source < best.Source:
			best = c
		}
	}
	return best, topScore
}

// baseTier classifies a response onto the shared, policy-independent quality
// ladder (tierNone … tierAnswer), as an answer to q. tierAnswer means "gives an
// A/AAAA of the queried type for the queried name"; a ranking policy refines that.
// A CNAME accompanying an NXDOMAIN is not a referral (the alias resolves to
// nothing), so it stays NXDOMAIN.
func baseTier(resp *dns.Msg, q dns.Question, atype uint16) int {
	if !responded(resp) {
		return tierNone
	}
	// An EDNS extended rcode (≥16, RFC 6891) lives in the OPT record, not in the
	// 4-bit header field resp.Rcode holds. Without this guard BADVERS (16, nibble
	// 0) would look like NOERROR and BADMODE (19, nibble 3) like NXDOMAIN.
	if opt := resp.IsEdns0(); opt != nil && opt.ExtendedRcode() != 0 {
		return tierErrored
	}
	if resp.Rcode == dns.RcodeNameError {
		return tierNXDOMAIN
	}
	if resp.Rcode != dns.RcodeSuccess {
		return tierErrored // SERVFAIL, REFUSED, NOTIMP, …
	}
	// An empty answer section is the ordinary NODATA reply, handled below — there
	// is nothing in it that could be about a name. A section that carries records
	// but never mentions the queried one says nothing about it, so whatever else
	// those records hold must not be read as this name's answer.
	if len(resp.Answer) > 0 && !mentions(resp, q) {
		return tierOffTopic
	}

	switch {
	case len(addrsOf(resp, q, atype)) > 0:
		return tierAnswer
	case aliasesQuestion(resp, q):
		return tierReferral
	default:
		return tierNODATA
	}
}

// mentions reports whether any record in the answer section is about the queried
// name — of any type, because an MX or TXT for it still makes the reply an answer
// about that name, just not an address one. Names are compared in canonical form:
// DNS 0x20 case randomization makes that a requirement, not a nicety. A record in
// another class says nothing about this question, whatever name it carries.
func mentions(resp *dns.Msg, q dns.Question) bool {
	name := dns.CanonicalName(q.Name)
	for _, rr := range resp.Answer {
		if rr.Header().Class == q.Qclass && dns.CanonicalName(rr.Header().Name) == name {
			return true
		}
	}
	return false
}

// aliasSet returns the names this reply presents as aliases of the queried name:
// the question's own name plus every CNAME target reachable from it, in the
// question's class and in canonical form.
//
// It is a reachability set, not a resolved chain, which is why it needs none of
// the rulings a chain walk would: a cycle is harmless because adding to a set is
// idempotent, a name aliased twice simply contributes both targets, and there is
// no hop limit to choose — every round either adds at least one name or ends the
// loop, and the only candidates are CNAME targets of this answer section, so there
// are at most len(resp.Answer) rounds.
//
// Only CNAME extends the set. A DNAME aliases a subtree rather than a name: its
// owner is a proper ancestor of the queried name, and it reaches this answer
// through the synthesized CNAME that RFC 6672 requires alongside it. Without that
// CNAME the set does not grow and the ladder settles on a referral or NODATA,
// which is the safe direction.
func aliasSet(resp *dns.Msg, q dns.Question) map[string]bool {
	set := map[string]bool{dns.CanonicalName(q.Name): true}
	for changed := true; changed; {
		changed = false
		for _, rr := range resp.Answer {
			cname, ok := rr.(*dns.CNAME)
			if !ok || cname.Hdr.Class != q.Qclass {
				continue
			}
			if !set[dns.CanonicalName(cname.Hdr.Name)] {
				continue
			}
			if target := dns.CanonicalName(cname.Target); !set[target] {
				set[target] = true
				changed = true
			}
		}
	}
	return set
}

// aliased reports whether name carries a CNAME of its own in this answer section.
// An address at such a name is not this reply's answer: RFC 1034 §3.6.2 forbids a
// CNAME owner from holding other data, so a reply doing both is malformed, and a
// client that follows the alias ends up somewhere other than the address suggests.
// Only CNAME counts — a DNAME owner may legitimately carry other types.
func aliased(resp *dns.Msg, name string, qclass uint16) bool {
	for _, rr := range resp.Answer {
		cname, ok := rr.(*dns.CNAME)
		if ok && cname.Hdr.Class == qclass && dns.CanonicalName(cname.Hdr.Name) == name {
			return true
		}
	}
	return false
}

// aliasesQuestion reports whether the reply carries an alias for a name the
// question resolves through — a referral to follow, as opposed to an alias for
// some unrelated name, which says nothing about this question.
func aliasesQuestion(resp *dns.Msg, q dns.Question) bool {
	set := aliasSet(resp, q)
	for _, rr := range resp.Answer {
		cname, ok := rr.(*dns.CNAME)
		if ok && cname.Hdr.Class == q.Qclass && set[dns.CanonicalName(cname.Hdr.Name)] {
			return true
		}
	}
	return false
}

// responded reports whether the upstream returned a DNS reply at all (any
// rcode). A nil Resp means it did not answer (transport error / timeout).
func responded(resp *dns.Msg) bool {
	return resp != nil && resp.Response
}

// addrType maps a query type to the record type that carries its address.
// Only A and AAAA carry addresses; the bool is false for anything else.
func addrType(qtype uint16) (uint16, bool) {
	switch qtype {
	case dns.TypeA, dns.TypeAAAA:
		return qtype, true
	default:
		return 0, false
	}
}

// allAddrsPrivate reports whether every address of type atype in resp is internal,
// and that there is at least one. The whole set decides, not a single member: an
// answer that also hands out a public address does not resolve the name into the
// internal network, and unlike "carries one", a property of the whole set cannot be
// won by adding records to the message.
//
// Which of the addresses belong to the queried name is deliberately not determined.
// That would mean following the answer's alias chain, with the cycles, forks and hop
// limits such a walk has to rule on. Judging the set as a whole needs none of that
// and still lets a chain ending in an internal address count — at the price of a
// reply that pads its answer with unrelated public records losing its preference
// (ADR-0023 implementation notes).
func allAddrsPrivate(resp *dns.Msg, q dns.Question, atype uint16) bool {
	addrs := addrsOf(resp, q, atype)
	if len(addrs) == 0 {
		return false
	}
	for _, a := range addrs {
		if !isPrivateAddr(a) {
			return false
		}
	}
	return true
}

// privateDisagreement reports whether two or more candidates are internal answers
// that do not name the same addresses — i.e. the resolvers disagree on which
// internal endpoint the name maps to. Identical answers from redundant resolvers
// are not a disagreement, and a candidate that is not an internal answer at all
// cannot disagree about one. See ADR-0023 D4.
func privateDisagreement(cands []Candidate, q dns.Question, atype uint16) bool {
	first, seen := "", false
	for _, c := range cands {
		if !allAddrsPrivate(c.Resp, q, atype) {
			continue
		}
		sig := addrSig(c.Resp, q, atype)
		if !seen {
			first, seen = sig, true
			continue
		}
		if sig != first {
			return true
		}
	}
	return false
}

// addrSig returns a canonical signature of the addresses resp gives for q (sorted,
// comma-joined), so two answers can be compared for naming the same set. It reads
// the same restricted set the ranking does, so the two cannot drift apart.
func addrSig(resp *dns.Msg, q dns.Question, atype uint16) string {
	addrs := addrsOf(resp, q, atype)
	ss := make([]string, 0, len(addrs))
	for _, a := range addrs {
		ss = append(ss, a.String())
	}
	sort.Strings(ss)
	return strings.Join(ss, ",")
}

// addrsOf returns the (unmapped) addresses of type atype that resp gives as the
// answer to q: in the question's class, owned by a name the reply aliases the
// question to, and not sitting at a name that is itself aliased. Everything else
// in the answer section belongs to some other question and must not decide this
// one. The record's Go type — not its header Rrtype — decides A vs AAAA, so
// hand-built records classify correctly too.
func addrsOf(resp *dns.Msg, q dns.Question, atype uint16) []netip.Addr {
	if resp == nil {
		return nil
	}
	set := aliasSet(resp, q)
	var addrs []netip.Addr
	for _, rr := range resp.Answer {
		var addr netip.Addr
		var ok bool
		switch v := rr.(type) {
		case *dns.A:
			if atype != dns.TypeA {
				continue
			}
			addr, ok = netip.AddrFromSlice(v.A)
		case *dns.AAAA:
			if atype != dns.TypeAAAA {
				continue
			}
			addr, ok = netip.AddrFromSlice(v.AAAA)
		default:
			continue
		}
		if !ok || rr.Header().Class != q.Qclass {
			continue
		}
		owner := dns.CanonicalName(rr.Header().Name)
		if !set[owner] || aliased(resp, owner, q.Qclass) {
			continue
		}
		addrs = append(addrs, addr.Unmap())
	}
	return addrs
}

// cgnatPrefix is the shared address space (RFC 6598) openZro peers use.
// netip.Addr.IsPrivate excludes it, so we test it explicitly, with our own prefix:
// the range is only checked inline elsewhere (e.g. isWellKnown in
// client/anonymize), never through an exported helper.
var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

// isPrivateAddr reports whether addr is internal/mesh-reachable: RFC1918 or
// IPv6-ULA (via IsPrivate) plus the CGNAT range. Best-effort by design
// (ADR-0023 D3); a routing-aware signal is future work (D11).
func isPrivateAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsPrivate() || cgnatPrefix.Contains(addr)
}

// Get returns the policy for name. An empty name resolves to DefaultPolicy; the
// bool is false for an unknown name. A switch rather than a package-level map
// keeps this free of global mutable state.
func Get(name string) (Policy, bool) {
	if name == "" {
		name = DefaultPolicy
	}
	switch name {
	case "first_success":
		return firstSuccess{}, true
	case "prefer_private":
		return preferPrivate{}, true
	default:
		return nil, false
	}
}
