package selection

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

// msg builds a response with the given rcode and A/AAAA answers for the queried
// name. Records carry an owner name on purpose: a reply is only an answer about a
// name it mentions, so a helper that left the header zero would make every
// assertion about that rule vacuous.
func msg(rcode int, ips ...string) *dns.Msg {
	m := new(dns.Msg)
	m.Rcode = rcode
	m.Response = true
	for _, s := range ips {
		ip := net.ParseIP(s)
		if v4 := ip.To4(); v4 != nil {
			m.Answer = append(m.Answer, &dns.A{Hdr: hdr(zone, dns.TypeA), A: v4})
			continue
		}
		m.Answer = append(m.Answer, &dns.AAAA{Hdr: hdr(zone, dns.TypeAAAA), AAAA: ip})
	}
	return m
}

// hdr builds a record header owned by owner.
func hdr(owner string, rrtype uint16) dns.RR_Header {
	return dns.RR_Header{Name: dns.Fqdn(owner), Rrtype: rrtype, Class: dns.ClassINET, Ttl: 60}
}

func cand(source string, rcode int, ips ...string) Candidate {
	return Candidate{Source: source, Resp: msg(rcode, ips...)}
}

// candRRCode builds a candidate from raw records with an explicit rcode.
func candRRCode(source string, rcode int, rrs ...dns.RR) Candidate {
	m := new(dns.Msg)
	m.Rcode = rcode
	m.Response = true
	m.Answer = rrs
	return Candidate{Source: source, Resp: m}
}

// candRR builds a NOERROR candidate from raw records (CNAME chains / mixed answers).
func candRR(source string, rrs ...dns.RR) Candidate {
	return candRRCode(source, dns.RcodeSuccess, rrs...)
}

// Records default to the queried name as their owner; the *At helpers place one
// somewhere else, which is what tells an answer about this name apart from an
// answer that merely arrived in the same message.
func rrA(ip string) dns.RR    { return rrAAt(zone, ip) }
func rrCNAME(t string) dns.RR { return rrCNAMEAt(zone, t) }

func rrAAt(owner, ip string) dns.RR {
	return &dns.A{Hdr: hdr(owner, dns.TypeA), A: net.ParseIP(ip)}
}

func rrCNAMEAt(owner, target string) dns.RR {
	return &dns.CNAME{Hdr: hdr(owner, dns.TypeCNAME), Target: dns.Fqdn(target)}
}

func rrMX(pref uint16, h string) dns.RR {
	return &dns.MX{Hdr: hdr(zone, dns.TypeMX), Preference: pref, Mx: dns.Fqdn(h)}
}

func rrTXTAt(owner, text string) dns.RR {
	return &dns.TXT{Hdr: hdr(owner, dns.TypeTXT), Txt: []string{text}}
}

func rrDNAMEAt(owner, target string) dns.RR {
	return &dns.DNAME{Hdr: hdr(owner, dns.TypeDNAME), Target: dns.Fqdn(target)}
}

// rrAClassAt places an address record in a class other than the question's. The
// class is part of a record's identity, so such a record answers a different
// question — the client matches on (name, class, type) and discards it.
func rrAClassAt(owner string, class uint16, ip string) dns.RR {
	h := hdr(owner, dns.TypeA)
	h.Class = class
	return &dns.A{Hdr: h, A: net.ParseIP(ip)}
}

func failCand(source string) Candidate { return Candidate{Source: source, Resp: nil} }
func servfail(source string) Candidate { return candRRCode(source, dns.RcodeServerFailure) }

// extRcodeCand builds a candidate carrying an EDNS extended rcode (≥16): the
// header holds only the low 4 bits, the OPT record the upper 8 (RFC 6891).
func extRcodeCand(source string, extRcode uint16) Candidate {
	m := new(dns.Msg)
	m.Response = true
	m.Rcode = int(extRcode) & 0xF
	opt := &dns.OPT{Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeOPT}}
	opt.SetExtendedRcode(extRcode)
	m.Extra = append(m.Extra, opt)
	return Candidate{Source: source, Resp: m}
}

// zone is the queried name in the tables below. The questions all carry
// Qclass: dns.ClassINET on purpose: classification compares a record's class
// against the question's, so a fixture leaving the question's class at zero would
// match nothing and quietly make every row about it meaningless.
const zone = "myaccount.blob.core.windows.net."

func TestPreferPrivate_Select(t *testing.T) {
	p, _ := Get("prefer_private")

	tests := []struct {
		name      string
		qtype     uint16 // 0 => A
		cands     []Candidate
		wantIdx   int // index of the candidate whose Resp must be returned; -1 = none
		wantOK    bool
		wantAmbig bool
	}{
		{
			name:    "private beats public even when listed last",
			cands:   []Candidate{cand("b", dns.RcodeSuccess, "1.2.3.4"), cand("a", dns.RcodeSuccess, "10.0.0.5")},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "no private falls back to public, lowest source wins",
			cands:   []Candidate{cand("b", dns.RcodeSuccess, "1.2.3.4"), cand("a", dns.RcodeSuccess, "5.6.7.8")},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "private beats public, CNAME, NODATA and NXDOMAIN",
			cands:   []Candidate{cand("a", dns.RcodeNameError), cand("b", dns.RcodeSuccess, "1.2.3.4"), cand("c", dns.RcodeSuccess, "10.1.2.3")},
			wantIdx: 2, wantOK: true,
		},
		{
			name:    "public beats CNAME referral",
			cands:   []Candidate{candRR("a", rrCNAME("x.privatelink.example")), cand("b", dns.RcodeSuccess, "1.2.3.4")},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "CNAME referral beats NODATA (tier over source)",
			cands:   []Candidate{cand("a", dns.RcodeSuccess), candRR("z", rrCNAME("x.privatelink.example"))},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "CNAME referral beats NXDOMAIN",
			cands:   []Candidate{cand("a", dns.RcodeNameError), candRR("z", rrCNAME("x.privatelink.example"))},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "NODATA beats NXDOMAIN (tier over source)",
			cands:   []Candidate{cand("a", dns.RcodeNameError), cand("z", dns.RcodeSuccess)},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "NXDOMAIN carrying a CNAME stays NXDOMAIN, loses to a NOERROR referral",
			cands:   []Candidate{candRRCode("a", dns.RcodeNameError, rrCNAME("x.example")), candRR("z", rrCNAME("y.example"))},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "NXDOMAIN (definitive) beats a SERVFAIL reply",
			cands:   []Candidate{servfail("a"), cand("b", dns.RcodeNameError)},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "extended rcode (BADVERS, header nibble 0) is an error, not NODATA; NXDOMAIN beats it",
			cands:   []Candidate{extRcodeCand("a", 16), cand("b", dns.RcodeNameError)},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "all NXDOMAIN: lowest source wins deterministically",
			cands:   []Candidate{cand("b", dns.RcodeNameError), cand("a", dns.RcodeNameError)},
			wantIdx: 1, wantOK: true,
		},
		{
			// The address sits at the end of the chain, not at the queried name, and
			// still counts: the address set is judged as a whole, so a chain needs no
			// walking to be understood.
			name:    "CNAME chain to a private A is private (real Azure shape)",
			cands:   []Candidate{cand("b", dns.RcodeSuccess, "1.2.3.4"), candRR("a", rrCNAME("x.privatelink.blob.core.windows.net"), rrAAt("x.privatelink.blob.core.windows.net.", "10.0.0.7"))},
			wantIdx: 1, wantOK: true,
		},
		{
			// An answer that also hands out a public address does not resolve the name
			// into the internal network. It ranks as the public answer it also is, so
			// the source decides — it does not win on tier.
			name:    "a mixed public+private answer is not internal",
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "9.9.9.9"), candRR("z", rrA("1.2.3.4"), rrA("10.0.0.9"))},
			wantIdx: 0, wantOK: true,
		},
		{
			// The padding shape: without this rule an attacker wins with an arbitrary
			// public payload plus one unrelated private record.
			name:    "a private record for another name does not make an answer internal",
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "203.0.113.10"), candRR("z", rrA("203.0.113.66"), rrAAt("pad.other.example.", "10.0.0.1"))},
			wantIdx: 0, wantOK: true,
		},
		{
			// Nothing in it is about the name we asked for, so it is no answer to that
			// question — but it is still a reply that was received, and it is served if
			// nothing better exists. Hence above the negatives, not below them.
			name:    "a reply about no name of ours is off-topic, and still beats NXDOMAIN",
			cands:   []Candidate{cand("a", dns.RcodeNameError), candRR("z", rrAAt("pad.other.example.", "10.0.0.1"))},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "off-topic loses to a referral for the queried name, despite the lower source",
			cands:   []Candidate{candRR("a", rrAAt("pad.other.example.", "10.0.0.1")), candRR("z", rrCNAME("x.privatelink.example"))},
			wantIdx: 1, wantOK: true,
		},
		{
			// An empty answer is the ordinary NODATA reply, not an off-topic one: there
			// is nothing in it that could be about a name. Were it treated as
			// off-topic the two would tie here and the lower source would win.
			name:    "off-topic outranks NODATA, which is how an empty answer stays NODATA",
			cands:   []Candidate{cand("a", dns.RcodeSuccess), candRR("z", rrAAt("pad.other.example.", "10.0.0.1"))},
			wantIdx: 1, wantOK: true,
		},
		{
			// DNS 0x20: a resolver may echo the name in randomized case, so folding it
			// is a requirement — otherwise a legitimate answer turns off-topic.
			name:    "an owner name in different case still anchors the answer",
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "203.0.113.10"), candRR("z", rrAAt("MyAccount.BLOB.core.WINDOWS.net.", "10.0.0.5"))},
			wantIdx: 1, wantOK: true,
		},
		{
			// Only internal answers can disagree about an internal address, and a
			// mixed answer is not one — so it must not raise the flag either.
			name:    "a mixed answer does not make the internal answer ambiguous",
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "10.0.0.1"), candRR("z", rrA("203.0.113.9"), rrA("10.0.0.2"))},
			wantIdx: 0, wantOK: true,
		},
		{
			// The alias-anchored padding shape: an alias for the queried name makes the
			// reply relevant, but the private address belongs to an unrelated name and
			// so is not this answer. Were it counted, the reply would outrank the
			// honest one while the client follows the alias to the attacker instead.
			name:    "a private record for another name does not promote an aliased answer",
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "203.0.113.10"), candRR("z", rrCNAME("evil.attacker.example"), rrAAt("pad.other.example.", "10.0.0.1"))},
			wantIdx: 0, wantOK: true,
		},
		{
			// Reachability is directed: an alias of some unrelated name does not make
			// its target an alias of the question, so the address there stays outside.
			name: "an unrelated alias does not pull a name into the answer",
			cands: []Candidate{cand("a", dns.RcodeSuccess, "203.0.113.10"), candRR("z",
				rrCNAME("evil.attacker.example"),
				rrCNAMEAt("junk.example.", "pad.example"),
				rrAAt("pad.example.", "10.0.0.1"))},
			wantIdx: 0, wantOK: true,
		},
		{
			// Records arrive in whatever order the upstream packed them, so the chain
			// is followed by re-scanning, not by reading the section top to bottom.
			name: "a two-step chain promotes regardless of record order",
			cands: []Candidate{cand("b", dns.RcodeSuccess, "203.0.113.10"), candRR("a",
				rrAAt("second.example.", "10.0.0.7"),
				rrCNAMEAt("first.example.", "second.example"),
				rrCNAME("first.example"))},
			wantIdx: 1, wantOK: true,
		},
		{
			// RFC 1034 §3.6.2: a CNAME owner may hold no other data. A reply doing both
			// is malformed, and whichever half a stub believes must not be decided by
			// us — so the address at an aliased name is not counted.
			name:    "an alias and an address at the same name do not promote",
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "203.0.113.10"), candRR("z", rrCNAME("evil.attacker.example"), rrA("10.0.0.1"))},
			wantIdx: 0, wantOK: true,
		},
		{
			// Pins that the closure terminates: a cycle merely stops adding names. The
			// address sits at an aliased name, so it is not counted either.
			name: "an alias cycle terminates and does not promote",
			cands: []Candidate{cand("a", dns.RcodeSuccess, "203.0.113.10"), candRR("z",
				rrCNAME("x.example"),
				rrCNAMEAt("x.example.", "y.example"),
				rrCNAMEAt("y.example.", "x.example"),
				rrAAt("y.example.", "10.0.0.1"))},
			wantIdx: 0, wantOK: true,
		},
		{
			// A DNAME owner may legitimately carry other types (RFC 6672), unlike a
			// CNAME owner — so the terminality rule must ask about CNAMEs only, or this
			// address quietly stops counting.
			name:    "an address next to a DNAME at the queried name still counts",
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "203.0.113.10"), candRR("z", rrA("10.0.0.5"), rrDNAMEAt(zone, "foo.example"))},
			wantIdx: 1, wantOK: true,
		},
		{
			// A DNAME aliases a subtree, not a name, and reaches an answer through the
			// synthesized CNAME. An unrelated one must not turn this into a referral —
			// it would outrank another resolver's NODATA on nothing.
			name:    "an unrelated DNAME does not make a reply a referral",
			cands:   []Candidate{cand("a", dns.RcodeSuccess), candRR("z", rrTXTAt(zone, "x"), rrDNAMEAt("junk.example.", "pad.example"))},
			wantIdx: 0, wantOK: true,
		},
		{
			// Class is part of a record's identity: an address in another class answers
			// a different question, the client discards it, and counting it would let
			// it decide this one.
			name:    "an address in another class does not count",
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "203.0.113.10"), candRR("z", rrCNAME("evil.attacker.example"), rrAClassAt("evil.attacker.example.", dns.ClassCHAOS, "10.0.0.1"))},
			wantIdx: 0, wantOK: true,
		},
		{
			name:    "two private, different addresses: lowest source wins, flagged ambiguous",
			cands:   []Candidate{cand("z", dns.RcodeSuccess, "10.0.0.1"), cand("a", dns.RcodeSuccess, "10.0.0.2")},
			wantIdx: 1, wantOK: true, wantAmbig: true,
		},
		{
			name:    "two private, SAME address: redundant HA, not ambiguous",
			cands:   []Candidate{cand("b", dns.RcodeSuccess, "10.0.0.5"), cand("a", dns.RcodeSuccess, "10.0.0.5")},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "CGNAT 100.64/10 counts as private",
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "1.1.1.1"), cand("b", dns.RcodeSuccess, "100.100.0.5")},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "IPv6 ULA private beats public v6 (AAAA query)",
			qtype:   dns.TypeAAAA,
			cands:   []Candidate{cand("a", dns.RcodeSuccess, "2606:4700::1111"), cand("b", dns.RcodeSuccess, "fd00::1")},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "a SERVFAIL reply beats a no-reply (nil): a real error reply is worth more than nothing",
			cands:   []Candidate{failCand("a"), servfail("b")},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "all SERVFAIL: returns the upstream error reply (lowest source), not a synthesized one",
			cands:   []Candidate{servfail("a"), servfail("b")},
			wantIdx: 0, wantOK: true,
		},
		{
			name:    "all resolvers failed to reply (nil): returns none, caller synthesizes SERVFAIL",
			cands:   []Candidate{failCand("a"), failCand("b")},
			wantIdx: -1, wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qtype := tt.qtype
			if qtype == 0 {
				qtype = dns.TypeA
			}
			got, ok := p.Select(dns.Question{Name: zone, Qtype: qtype, Qclass: dns.ClassINET}, tt.cands)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				if got.Resp != nil {
					t.Fatalf("got non-nil response, want nil")
				}
				return
			}
			want := tt.cands[tt.wantIdx]
			if got.Resp != want.Resp {
				t.Fatalf("wrong candidate: got %v, want candidate[%d] (source %q)", got.Resp.Answer, tt.wantIdx, want.Source)
			}
			if got.Source != want.Source {
				t.Fatalf("Source = %q, want %q", got.Source, want.Source)
			}
			if got.Ambiguous != tt.wantAmbig {
				t.Fatalf("Ambiguous = %v, want %v", got.Ambiguous, tt.wantAmbig)
			}
		})
	}
}

func TestFirstSuccess_Select(t *testing.T) {
	p, _ := Get("first_success")
	q := dns.Question{Name: zone, Qtype: dns.TypeA, Qclass: dns.ClassINET}

	tests := []struct {
		name    string
		cands   []Candidate
		wantIdx int
		wantOK  bool
	}{
		{
			name:    "first response in order wins",
			cands:   []Candidate{cand("b", dns.RcodeSuccess, "1.2.3.4"), cand("a", dns.RcodeSuccess, "10.0.0.5")},
			wantIdx: 0, wantOK: true,
		},
		{
			// Backward-compatible with the resolver's sequential path: a SERVFAIL
			// IS a response and is returned; only a non-answering upstream (nil) is
			// skipped. (Whether SERVFAIL should count is arguably a pre-existing bug,
			// tracked separately — not changed here.)
			name:    "skips non-answers (nil), but a SERVFAIL response is returned",
			cands:   []Candidate{failCand("a"), servfail("b"), cand("c", dns.RcodeNameError)},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "all non-answers (nil) returns none",
			cands:   []Candidate{failCand("a"), failCand("b")},
			wantIdx: -1, wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := p.Select(q, tt.cands)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Resp != tt.cands[tt.wantIdx].Resp {
				t.Fatalf("wrong candidate: want candidate[%d] (source %q)", tt.wantIdx, tt.cands[tt.wantIdx].Source)
			}
		})
	}
}

// prefer_private ranks only A/AAAA addresses. For any other query type it has no
// private/public distinction, so it returns a responded answer chosen
// deterministically by lowest Source — independent of arrival order, and never
// flagged ambiguous. The pool never fans out for non-A/AAAA anyway (ADR-0023 D5).
func TestPreferPrivate_NonAddressPicksLowestSourceDeterministically(t *testing.T) {
	pp, _ := Get("prefer_private")
	qMX := dns.Question{Name: zone, Qtype: dns.TypeMX, Qclass: dns.ClassINET}

	// "z" is first in arrival order, but "a" has the lower Source and must win.
	cands := []Candidate{candRR("z", rrMX(10, "m1.example.com")), candRR("a", rrMX(20, "m2.example.com"))}

	got, ok := pp.Select(qMX, cands)
	if !ok || got.Source != "a" {
		t.Fatalf("non-address query must pick lowest Source deterministically: got %q ok=%v, want \"a\"", got.Source, ok)
	}
	if got.Ambiguous {
		t.Fatalf("Ambiguous must be false for non-address queries")
	}
}

func TestGetPolicy(t *testing.T) {
	if p, ok := Get(""); !ok || p.Name() != "first_success" {
		t.Fatalf("empty name should resolve to first_success, got ok=%v", ok)
	}
	if p, ok := Get("prefer_private"); !ok || p.Name() != "prefer_private" {
		t.Fatalf("prefer_private not resolved, got ok=%v", ok)
	}
	if _, ok := Get("nope"); ok {
		t.Fatalf("unknown policy should return ok=false")
	}
}

// TestRanking pins which policies a caller must gather candidates for.
func TestRanking(t *testing.T) {
	// The nil case carries the whole opt-in: nothing configured must never be
	// gathered for. It is the only one live in production until the config knob
	// lands, and it is why Ranking exists next to Policy.Ranks.
	if Ranking(nil) {
		t.Error("a nil policy must not rank")
	}

	// One row per name Get resolves. A new policy has to answer Ranks() to
	// compile at all; this table is where the answer is stated out loud.
	tests := []struct {
		name string
		want bool
	}{
		{name: "", want: false}, // resolves to DefaultPolicy
		{name: "first_success", want: false},
		{name: "prefer_private", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := Get(tc.name)
			if !ok {
				t.Fatalf("policy %q does not resolve", tc.name)
			}
			if got := p.Ranks(); got != tc.want {
				t.Errorf("%s.Ranks() = %v, want %v", p.Name(), got, tc.want)
			}
			if got := Ranking(p); got != tc.want {
				t.Errorf("Ranking(%s) = %v, want %v", p.Name(), got, tc.want)
			}
		})
	}
}
