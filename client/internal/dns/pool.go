package dns

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/client/internal/dns/selection"
	"github.com/openzro/openzro/client/internal/dns/types"
)

const (
	// selectionGrace bounds how much longer the pool waits for the remaining
	// nameserver groups once the first one has replied. The clock starts with that
	// first reply, so it caps the latency the pool ADDS, not the query as a whole
	// (ADR-0023 D5).
	selectionGrace = 500 * time.Millisecond

	// ambiguityWarnInterval throttles the warning about nameserver groups that
	// disagree on a name: a configuration smell for an operator to fix, not a
	// per-query event (ADR-0023 D4).
	ambiguityWarnInterval = 5 * time.Minute
)

// groupResolver is one nameserver group's resolver as the pool uses it: query it,
// tell it how the query went, and manage its lifecycle. *upstreamResolver
// satisfies this through the embedded upstreamResolverBase.
type groupResolver interface {
	queryUpstreams(logger *log.Entry, r *dns.Msg) (*dns.Msg, error)
	checkUpstreamFails(err error)
	ProbeAvailability()
	Stop()
	stopped() bool
	source() string
}

// upstreamGroupHandler is one nameserver group's resolver as the server builds
// it: a handler it can register on its own, plus the surface a pool needs.
type upstreamGroupHandler interface {
	handlerWithStop
	groupResolver
	setCallbacks(deactivate func(error), reactivate func())
}

// poolMember is one nameserver group inside a pool. enabled is false while the
// group's health hooks have it deactivated; the pool then skips it.
type poolMember struct {
	resolver groupResolver
	enabled  bool
}

// resolverPool serves one zone from every nameserver group configured for it.
// Without a pool the highest-priority group answers and the others are never
// asked, which is what makes a split-horizon zone return the public view of a
// name that also has an internal one. The pool queries all groups at once and
// hands their replies to a selection.Policy, which decides what the client gets.
//
// Locking, in order: hookMu → mu → the server lock. The members slice is fixed at
// construction; only the enabled flags and the warning timestamp change, both
// guarded by mu, and mu is never held while calling into a member or the server —
// a member's health hook takes the server lock, so holding mu across it would
// invert that order. hookMu is the outer lock a whole zone-level transition runs
// under, membership change plus the zone hook that follows from it; nothing the
// server calls while holding its own lock takes hookMu. See applyZoneState.
type resolverPool struct {
	domain string
	policy selection.Policy
	// grace and warnInterval carry the constants above; they are fields so tests
	// can shorten them.
	grace        time.Duration
	warnInterval time.Duration

	mu            sync.Mutex
	members       []*poolMember
	lastAmbiguity time.Time

	hookMu sync.Mutex
	zoneUp bool
}

func newResolverPool(domain string, policy selection.Policy, resolvers []groupResolver) *resolverPool {
	members := make([]*poolMember, 0, len(resolvers))
	for _, resolver := range resolvers {
		members = append(members, &poolMember{resolver: resolver, enabled: true})
	}

	return &resolverPool{
		domain:       domain,
		policy:       policy,
		grace:        selectionGrace,
		warnInterval: ambiguityWarnInterval,
		members:      members,
		// The pool is registered for its zone as it is built.
		zoneUp: true,
	}
}

// String returns a string representation of the pool.
func (p *resolverPool) String() string {
	return fmt.Sprintf("pool for %s over %d nameserver groups", p.domain, len(p.members))
}

// ID returns the unique handler ID. It covers the zone and the pooled groups, so
// re-applying an unchanged configuration re-registers the same handler, and is
// distinct from the ID of any single group's resolver.
func (p *resolverPool) ID() types.HandlerID {
	sources := make([]string, 0, len(p.members))
	for _, m := range p.members {
		sources = append(sources, m.resolver.source())
	}
	slices.Sort(sources)

	hash := sha256.New()
	hash.Write([]byte(p.domain + ":"))
	hash.Write([]byte(strings.Join(sources, "|")))
	return types.HandlerID("upstream-pool-" + hex.EncodeToString(hash.Sum(nil)[:8]))
}

// MatchSubdomains reports that the pool serves the zone's subdomains too, as a
// single upstream group does.
func (p *resolverPool) MatchSubdomains() bool {
	return true
}

// Stop stops every pooled group, deactivated ones included.
func (p *resolverPool) Stop() {
	log.Debugf("stopping %s", p)
	for _, m := range p.members {
		m.resolver.Stop()
	}
}

// ProbeAvailability probes every pooled group, deactivated ones included, and
// leaves it to each group what to make of the result. Note that probing never
// brings a group back by itself: a deactivated group returns through its own
// backoff loop (waitUntilResponse), which then calls the reactivate hook.
func (p *resolverPool) ProbeAvailability() {
	var wg sync.WaitGroup
	for _, m := range p.members {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.resolver.ProbeAvailability()
		}()
	}
	wg.Wait()
}

// ServeDNS answers one query for the pooled zone.
func (p *resolverPool) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	logger := log.WithField("request_id", GenerateRequestID())

	if len(r.Question) == 0 {
		logger.Warnf("%s received a request without a question", p)
		p.writeRcode(w, r, dns.RcodeFormatError, logger)
		return
	}

	q := r.Question[0]
	logger.Tracef("received pooled question: domain=%s type=%v class=%v", q.Name, q.Qtype, q.Qclass)

	if r.Extra == nil {
		r.MsgHdr.AuthenticatedData = true
	}

	enabled := p.enabledMembers()
	members := runningMembers(enabled)
	switch {
	case len(enabled) > 0 && len(members) == 0:
		// Stopped, not deactivated: a query that raced a reconfiguration. Say
		// nothing and write nothing, as a stopped single resolver does.
		logger.Tracef("%s has been stopped", p)
	case len(members) == 0:
		logger.Errorf("every nameserver group of %s is deactivated, question domain=%s", p, q.Name)
		p.writeRcode(w, r, dns.RcodeServerFailure, logger)
	case len(members) == 1 || !isAddressQuery(q.Qtype):
		// Only address queries carry the signal a policy ranks on (ADR-0023 D5),
		// so anything else is served by the highest-priority group alone —
		// exactly as it is without a pool.
		p.serveSingleGroup(w, r, members[0], logger)
	default:
		p.serveSelected(w, r, q, members, logger)
	}
}

// serveSingleGroup forwards one group's reply, or SERVFAIL when it did not
// answer: the behavior of a zone served by a single nameserver group.
func (p *resolverPool) serveSingleGroup(w dns.ResponseWriter, r *dns.Msg, m *poolMember, logger *log.Entry) {
	resp, err := m.resolver.queryUpstreams(logger, r)
	m.resolver.checkUpstreamFails(err)

	if resp == nil {
		logger.Errorf("nameserver group %s of %s failed for question domain=%s: %v",
			m.resolver.source(), p, r.Question[0].Name, err)
		p.writeRcode(w, r, dns.RcodeServerFailure, logger)
		return
	}
	p.write(w, resp, logger)
}

// serveSelected gathers every group's reply and writes the one the policy picks.
func (p *resolverPool) serveSelected(
	w dns.ResponseWriter,
	r *dns.Msg,
	q dns.Question,
	members []*poolMember,
	logger *log.Entry,
) {
	cands := p.gather(r, members, logger)

	res, ok := p.policy.Select(q, cands)
	if !ok {
		logger.Errorf("no nameserver group of %s answered question domain=%s", p, q.Name)
		p.writeRcode(w, r, dns.RcodeServerFailure, logger)
		return
	}

	// Debug, not trace: when an operator asks why a name resolved to the public
	// address although prefer_private is on, the source that won is the first thing
	// to look at, and a policy can decline to prefer an answer for reasons the
	// answer alone does not show (a mixed address set, an alias-only reply).
	logger.Debugf("policy=%s selected the response from %s for question domain=%s",
		p.policy.Name(), res.Source, q.Name)
	if res.Ambiguous {
		p.warnAmbiguous(q, res, cands, logger)
	}
	p.write(w, res.Resp, logger)
}

// gather queries every given group concurrently and collects the replies. It
// returns once all of them have answered, or once the grace window that starts
// with the first reply has elapsed — a straggler must not hold the answer back
// (ADR-0023 D5). Groups still in flight are left behind; their result lands in the
// buffered channel, so nothing blocks or leaks. There is deliberately no early
// exit on an already-preferred reply: waiting out the window is what keeps the
// choice independent of arrival order (ADR-0023 D4).
func (p *resolverPool) gather(r *dns.Msg, members []*poolMember, logger *log.Entry) []selection.Candidate {
	replies := make(chan selection.Candidate, len(members))
	for _, m := range members {
		go func() {
			// Each group gets its own copy of the request: they pack it
			// concurrently, and a shared message would be a data race.
			resp, err := m.resolver.queryUpstreams(logger, r.Copy())
			m.resolver.checkUpstreamFails(err)
			replies <- selection.Candidate{Resp: resp, Source: m.resolver.source()}
		}()
	}

	var timer *time.Timer
	var grace <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	cands := make([]selection.Candidate, 0, len(members))
	for range members {
		select {
		case c := <-replies:
			cands = append(cands, c)
			// Armed by the first candidate a policy could actually rank, so a
			// group that failed outright does not start the clock: cutting the
			// window short after a fast failure would throw away the very answer
			// the pool exists to fetch. The wait is then bounded by each group's
			// own upstreamTimeout — see the ADR's implementation notes.
			if timer == nil && selection.Responded(c.Resp) {
				timer = time.NewTimer(p.grace)
				grace = timer.C
			}
		case <-grace:
			// Drain what is already in the channel before giving up. Go picks a
			// ready case at random, so a reply that landed in the same instant
			// the window elapsed would otherwise be kept or dropped by chance —
			// exactly the arrival-order dependence D4 rules out.
			cands = drainCandidates(replies, cands)
			logger.Debugf("grace window of %s elapsed for question domain=%s with %d of %d replies",
				p, r.Question[0].Name, len(cands), len(members))
			return cands
		}
	}
	return cands
}

// drainCandidates appends every candidate already buffered in replies, without waiting.
func drainCandidates(replies <-chan selection.Candidate, cands []selection.Candidate) []selection.Candidate {
	for {
		select {
		case c := <-replies:
			cands = append(cands, c)
		default:
			return cands
		}
	}
}

// runningMembers returns the members that have not been stopped. It is applied outside
// enabledMembers so that no member is called while p.mu is held.
func runningMembers(members []*poolMember) []*poolMember {
	active := make([]*poolMember, 0, len(members))
	for _, m := range members {
		if !m.resolver.stopped() {
			active = append(active, m)
		}
	}
	return active
}

// enabledMembers returns the groups the pool may query, in configuration order.
func (p *resolverPool) enabledMembers() []*poolMember {
	p.mu.Lock()
	defer p.mu.Unlock()

	active := make([]*poolMember, 0, len(p.members))
	for _, m := range p.members {
		if m.enabled {
			active = append(active, m)
		}
	}
	return active
}

// setMember adds a group to the fan-out or drops it, and reports whether that
// changed anything. An out-of-range group is ignored.
func (p *resolverPool) setMember(member int, enabled bool) (changed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if member < 0 || member >= len(p.members) || p.members[member].enabled == enabled {
		return false
	}
	p.members[member].enabled = enabled
	return true
}

// serving reports whether the pool has a group left to ask.
func (p *resolverPool) serving() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, m := range p.members {
		if m.enabled {
			return true
		}
	}
	return false
}

// applyZoneState brings the zone-level registration in line with the membership:
// a zone with no group left is deactivated, one that has a group again is
// reactivated, and anything else is left alone.
//
// Callers hold hookMu, which is what makes this safe. Two groups can flip at the
// same moment — one deactivates under its own lock while another reactivates from
// its backoff goroutine — so edge-triggering off "was that the last one?" would let
// the two zone hooks run in either order, leaving the zone deregistered with a
// healthy group in the pool. Deciding from the membership converges whatever the
// order, and reactivate can no longer precede deactivate, which its contract
// forbids.
func (p *resolverPool) applyZoneState(err error, deactivateZone func(error), reactivateZone func()) {
	serving := p.serving()
	if serving == p.zoneUp {
		return
	}

	p.zoneUp = serving
	if serving {
		reactivateZone()
		return
	}
	deactivateZone(err)
}

// warnAmbiguous logs, at most once per warnInterval, that the zone's nameserver
// groups return different internal addresses for a name. The selection itself
// stays deterministic; this is a configuration smell for the operator to fix.
func (p *resolverPool) warnAmbiguous(
	q dns.Question,
	res selection.Result,
	cands []selection.Candidate,
	logger *log.Entry,
) {
	if !p.shouldWarn(time.Now()) {
		return
	}

	answered := make([]string, 0, len(cands))
	for _, c := range cands {
		if c.Resp != nil {
			answered = append(answered, c.Source)
		}
	}
	logger.Warnf("nameserver groups of %s disagree on the internal address for domain=%s "+
		"(answered: %s), serving the answer from %s. Check the zone's nameserver configuration.",
		p, q.Name, strings.Join(answered, ", "), res.Source)
}

// shouldWarn reports whether an ambiguity warning may be emitted at now, and
// records it when it may. Time is a parameter so the throttle stays testable.
func (p *resolverPool) shouldWarn(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.lastAmbiguity.IsZero() && now.Sub(p.lastAmbiguity) < p.warnInterval {
		return false
	}
	p.lastAmbiguity = now
	return true
}

// write sends resp to the client.
func (p *resolverPool) write(w dns.ResponseWriter, resp *dns.Msg, logger *log.Entry) {
	if err := w.WriteMsg(resp); err != nil {
		logger.Errorf("failed to write DNS response for %s: %s", p, err)
	}
}

// writeRcode answers r with a bare rcode.
func (p *resolverPool) writeRcode(w dns.ResponseWriter, r *dns.Msg, rcode int, logger *log.Entry) {
	m := new(dns.Msg)
	m.SetRcode(r, rcode)
	p.write(w, m, logger)
}

// isAddressQuery reports whether qtype asks for an address — the only kind of
// answer a selection policy can rank (ADR-0023 D5).
func isAddressQuery(qtype uint16) bool {
	return qtype == dns.TypeA || qtype == dns.TypeAAAA
}
