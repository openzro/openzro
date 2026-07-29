package dns

import (
	"errors"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/client/internal/dns/selection"
	"github.com/openzro/openzro/client/internal/dns/test"
)

const (
	poolZone    = "eu.example.com."
	publicAddr  = "203.0.113.10"
	privateAddr = "10.1.2.3"
	meshAddr    = "100.64.0.7"
)

// testGrace keeps the pool's grace window short enough to keep tests fast while
// staying well above the scheduling jitter of a loaded CI runner.
const testGrace = 300 * time.Millisecond

// fakeGroup is a groupResolver that replies from a canned message after an
// optional delay, without touching the network.
type fakeGroup struct {
	src   string
	resp  *dns.Msg
	err   error
	delay time.Duration

	queried atomic.Int32
	checked atomic.Int32
	probed  atomic.Int32
	stops   atomic.Int32

	mu       sync.Mutex
	seen     []*dns.Msg
	reported []error
}

// reportedErrors returns the errors handed to checkUpstreamFails.
func (f *fakeGroup) reportedErrors() []error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reported)
}

func (f *fakeGroup) queryUpstreams(_ *log.Entry, r *dns.Msg) (*dns.Msg, error) {
	f.queried.Add(1)
	f.mu.Lock()
	f.seen = append(f.seen, r)
	f.mu.Unlock()

	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.resp == nil {
		return nil, f.err
	}

	resp := f.resp.Copy()
	resp.Id = r.Id
	return resp, nil
}

func (f *fakeGroup) checkUpstreamFails(err error) {
	f.checked.Add(1)
	f.mu.Lock()
	f.reported = append(f.reported, err)
	f.mu.Unlock()
}
func (f *fakeGroup) ProbeAvailability() { f.probed.Add(1) }
func (f *fakeGroup) Stop()              { f.stops.Add(1) }
func (f *fakeGroup) stopped() bool      { return f.stops.Load() > 0 }
func (f *fakeGroup) source() string     { return f.src }

func (f *fakeGroup) requests() []*dns.Msg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

// aReply builds an upstream reply carrying the given addresses as A records.
func aReply(addrs ...string) *dns.Msg {
	m := rcodeReply(dns.RcodeSuccess)
	for _, addr := range addrs {
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: poolZone, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.ParseIP(addr),
		})
	}
	return m
}

// rcodeReply builds an answerless upstream reply with the given rcode.
func rcodeReply(rcode int) *dns.Msg {
	return &dns.Msg{
		MsgHdr:   dns.MsgHdr{Response: true, Rcode: rcode},
		Question: []dns.Question{{Name: poolZone, Qtype: dns.TypeA, Qclass: dns.ClassINET}},
	}
}

func newTestPool(t *testing.T, policy string, groups ...*fakeGroup) *resolverPool {
	t.Helper()

	p, ok := selection.Get(policy)
	require.True(t, ok, "unknown policy %q", policy)

	resolvers := make([]groupResolver, 0, len(groups))
	for _, g := range groups {
		resolvers = append(resolvers, g)
	}

	pool := newResolverPool(poolZone, p, resolvers)
	pool.grace = testGrace
	return pool
}

// serveQuery runs one query through the pool and returns the written response.
func serveQuery(t *testing.T, pool *resolverPool, qtype uint16) *dns.Msg {
	t.Helper()

	var got *dns.Msg
	w := &test.MockResponseWriter{WriteMsgFunc: func(m *dns.Msg) error {
		got = m
		return nil
	}}
	pool.ServeDNS(w, new(dns.Msg).SetQuestion(poolZone, qtype))
	require.NotNil(t, got, "pool wrote no response")
	return got
}

// answerAddrs returns the addresses carried by m, for comparison in assertions.
func answerAddrs(m *dns.Msg) []string {
	var addrs []string
	for _, rr := range m.Answer {
		switch v := rr.(type) {
		case *dns.A:
			addrs = append(addrs, v.A.String())
		case *dns.AAAA:
			addrs = append(addrs, v.AAAA.String())
		}
	}
	return addrs
}

func TestResolverPool_ServeDNS(t *testing.T) {
	tests := []struct {
		name        string
		policy      string
		qtype       uint16
		groups      []*fakeGroup
		disable     []int
		wantAddrs   []string
		wantRcode   int
		wantQueried []int32
	}{
		{
			name:   "prefer_private returns the internal answer over the public one",
			policy: "prefer_private",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "a-public", resp: aReply(publicAddr)},
				{src: "b-internal", resp: aReply(privateAddr), delay: 20 * time.Millisecond},
			},
			wantAddrs:   []string{privateAddr},
			wantRcode:   dns.RcodeSuccess,
			wantQueried: []int32{1, 1},
		},
		{
			name:   "prefer_private returns the internal answer regardless of reply order",
			policy: "prefer_private",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "a-internal", resp: aReply(privateAddr)},
				{src: "b-public", resp: aReply(publicAddr), delay: 20 * time.Millisecond},
			},
			wantAddrs:   []string{privateAddr},
			wantRcode:   dns.RcodeSuccess,
			wantQueried: []int32{1, 1},
		},
		{
			name:   "a CGNAT mesh address counts as internal",
			policy: "prefer_private",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "a-public", resp: aReply(publicAddr)},
				{src: "b-mesh", resp: aReply(meshAddr)},
			},
			wantAddrs:   []string{meshAddr},
			wantRcode:   dns.RcodeSuccess,
			wantQueried: []int32{1, 1},
		},
		{
			name:   "two internal answers resolve deterministically by source",
			policy: "prefer_private",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "b-second", resp: aReply(privateAddr), delay: 20 * time.Millisecond},
				{src: "a-first", resp: aReply("10.9.9.9")},
			},
			wantAddrs:   []string{"10.9.9.9"},
			wantRcode:   dns.RcodeSuccess,
			wantQueried: []int32{1, 1},
		},
		{
			name:   "the public answer is kept when no group has an internal one",
			policy: "prefer_private",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "b-second", resp: aReply("198.51.100.7"), delay: 20 * time.Millisecond},
				{src: "a-first", resp: aReply(publicAddr)},
			},
			wantAddrs:   []string{publicAddr},
			wantRcode:   dns.RcodeSuccess,
			wantQueried: []int32{1, 1},
		},
		{
			name:   "an upstream error reply is forwarded rather than synthesized",
			policy: "prefer_private",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "a-first", resp: rcodeReply(dns.RcodeServerFailure)},
				{src: "b-second", resp: rcodeReply(dns.RcodeRefused)},
			},
			wantRcode:   dns.RcodeServerFailure,
			wantQueried: []int32{1, 1},
		},
		{
			name:   "no reply from any group yields SERVFAIL",
			policy: "prefer_private",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "a-first", err: errors.New("timeout")},
				{src: "b-second", err: errors.New("timeout")},
			},
			wantRcode:   dns.RcodeServerFailure,
			wantQueried: []int32{1, 1},
		},
		{
			name:   "a non-address query is served by the first group alone",
			policy: "prefer_private",
			qtype:  dns.TypeMX,
			groups: []*fakeGroup{
				{src: "a-first", resp: rcodeReply(dns.RcodeSuccess)},
				{src: "b-second", resp: rcodeReply(dns.RcodeRefused)},
			},
			wantRcode:   dns.RcodeSuccess,
			wantQueried: []int32{1, 0},
		},
		{
			name:   "a deactivated group is not queried",
			policy: "prefer_private",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "a-internal", resp: aReply(privateAddr)},
				{src: "b-public", resp: aReply(publicAddr)},
			},
			disable:     []int{0},
			wantAddrs:   []string{publicAddr},
			wantRcode:   dns.RcodeSuccess,
			wantQueried: []int32{0, 1},
		},
		{
			name:   "SERVFAIL is returned when every group is deactivated",
			policy: "prefer_private",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "a-internal", resp: aReply(privateAddr)},
				{src: "b-public", resp: aReply(publicAddr)},
			},
			disable:     []int{0, 1},
			wantRcode:   dns.RcodeServerFailure,
			wantQueried: []int32{0, 0},
		},
		{
			// Not reachable in production — a non-ranking policy is never pooled
			// (selection.Ranking). Kept so the pool stays sane with any policy.
			name:   "a non-ranking policy forwards the reply that arrives first",
			policy: "first_success",
			qtype:  dns.TypeA,
			groups: []*fakeGroup{
				{src: "a-first", resp: aReply(publicAddr)},
				{src: "b-second", resp: aReply(privateAddr), delay: 50 * time.Millisecond},
			},
			wantAddrs:   []string{publicAddr},
			wantRcode:   dns.RcodeSuccess,
			wantQueried: []int32{1, 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool := newTestPool(t, tc.policy, tc.groups...)
			for _, i := range tc.disable {
				pool.setMember(i, false)
			}

			got := serveQuery(t, pool, tc.qtype)

			assert.Equal(t, dns.RcodeToString[tc.wantRcode], dns.RcodeToString[got.Rcode])
			assert.Equal(t, tc.wantAddrs, answerAddrs(got))
			for i, want := range tc.wantQueried {
				assert.Equalf(t, want, tc.groups[i].queried.Load(),
					"group %s query count", tc.groups[i].src)
			}
		})
	}
}

// TestResolverPool_ReportsFailureToEveryGroup asserts the per-group health
// bookkeeping still runs for each group the pool queried, so a group that stops
// answering is still deactivated and probed back to life.
func TestResolverPool_ReportsFailureToEveryGroup(t *testing.T) {
	groups := []*fakeGroup{
		{src: "a-first", err: errors.New("timeout")},
		{src: "b-second", err: errors.New("timeout")},
	}
	pool := newTestPool(t, "prefer_private", groups...)

	serveQuery(t, pool, dns.TypeA)

	for _, g := range groups {
		assert.Equal(t, int32(1), g.checked.Load(), "group %s health check", g.src)
	}
}

// TestResolverPool_GraceWindowCapsTheWait asserts a straggling group cannot hold
// the answer back for longer than the grace window: the clock starts with the
// first reply, so the pool is never slower than a single group plus the window.
func TestResolverPool_GraceWindowCapsTheWait(t *testing.T) {
	fast := &fakeGroup{src: "a-public", resp: aReply(publicAddr)}
	slow := &fakeGroup{src: "b-internal", resp: aReply(privateAddr), delay: 2 * time.Second}
	pool := newTestPool(t, "prefer_private", fast, slow)
	pool.grace = 50 * time.Millisecond

	start := time.Now()
	got := serveQuery(t, pool, dns.TypeA)
	elapsed := time.Since(start)

	assert.Equal(t, []string{publicAddr}, answerAddrs(got), "should not wait for the straggler")
	assert.Less(t, elapsed, time.Second, "grace window did not cap the wait")
}

// TestResolverPool_ClonesTheRequestPerGroup guards against the groups sharing one
// *dns.Msg: they pack it concurrently, which would be a data race.
func TestResolverPool_ClonesTheRequestPerGroup(t *testing.T) {
	first := &fakeGroup{src: "a-first", resp: aReply(publicAddr)}
	second := &fakeGroup{src: "b-second", resp: aReply(privateAddr)}
	pool := newTestPool(t, "prefer_private", first, second)

	req := new(dns.Msg).SetQuestion(poolZone, dns.TypeA)
	w := &test.MockResponseWriter{}
	pool.ServeDNS(w, req)

	sent := append(first.requests(), second.requests()...)
	require.Len(t, sent, 2)
	assert.NotSame(t, sent[0], sent[1], "groups must not share one request")
	for _, m := range sent {
		assert.NotSame(t, req, m, "each group gets its own copy of the request")
		assert.Equal(t, req.Id, m.Id, "the copy keeps the request ID")
		assert.Equal(t, req.Question, m.Question, "the copy keeps the question")
	}
}

// TestResolverPool_FormErrOnQuestionlessRequest pins the one place the pool is
// stricter than a single resolver, which would index Question[0] and panic.
func TestResolverPool_FormErrOnQuestionlessRequest(t *testing.T) {
	group := &fakeGroup{src: "a-first", resp: aReply(privateAddr)}
	pool := newTestPool(t, "prefer_private", group)

	var got *dns.Msg
	w := &test.MockResponseWriter{WriteMsgFunc: func(m *dns.Msg) error {
		got = m
		return nil
	}}
	pool.ServeDNS(w, new(dns.Msg))

	require.NotNil(t, got, "a question-less request must still be answered")
	assert.Equal(t, dns.RcodeToString[dns.RcodeFormatError], dns.RcodeToString[got.Rcode])
	assert.Equal(t, int32(0), group.queried.Load(), "nothing to ask an upstream about")
}

// TestResolverPool_WriteErrorIsNotTheUpstreamsFault pins the other declared
// difference: a failed local write no longer counts towards a group's health, as
// it did through the unpooled path's deferred check.
func TestResolverPool_WriteErrorIsNotTheUpstreamsFault(t *testing.T) {
	group := &fakeGroup{src: "a-first", resp: aReply(privateAddr)}
	pool := newTestPool(t, "prefer_private", group)

	w := &test.MockResponseWriter{WriteMsgFunc: func(*dns.Msg) error {
		return errors.New("write failed")
	}}
	pool.ServeDNS(w, new(dns.Msg).SetQuestion(poolZone, dns.TypeA))

	require.Equal(t, []error{nil}, group.reportedErrors(),
		"the group answered fine; the write error is ours, not its")
}

// TestResolverPool_WaitsForAStragglerAfterAFailure pins that a group failing
// outright does not start the grace window: the pool would otherwise cut the wait
// short after a fast failure and throw away the answer it exists to fetch.
func TestResolverPool_WaitsForAStragglerAfterAFailure(t *testing.T) {
	broken := &fakeGroup{src: "a-broken", err: errors.New("connection refused")}
	slow := &fakeGroup{src: "b-slow", resp: aReply(privateAddr), delay: 2 * testGrace}
	pool := newTestPool(t, "prefer_private", broken, slow)

	got := serveQuery(t, pool, dns.TypeA)

	assert.Equal(t, []string{privateAddr}, answerAddrs(got),
		"the straggler's answer must not be cut off by the window")
}

// TestResolverPool_WarnsOnlyOnRealDisagreement checks the end-to-end path of the
// ambiguity signal: the selector flags it, the pool consumes one warning slot.
func TestResolverPool_WarnsOnlyOnRealDisagreement(t *testing.T) {
	t.Run("identical private answers are normal HA", func(t *testing.T) {
		pool := newTestPool(t, "prefer_private",
			&fakeGroup{src: "a-first", resp: aReply(privateAddr)},
			&fakeGroup{src: "b-second", resp: aReply(privateAddr)},
		)

		serveQuery(t, pool, dns.TypeA)
		assert.True(t, pool.lastAmbiguity.IsZero(), "redundant resolvers must not warn")
	})

	t.Run("different private answers warn once", func(t *testing.T) {
		pool := newTestPool(t, "prefer_private",
			&fakeGroup{src: "a-first", resp: aReply(privateAddr)},
			&fakeGroup{src: "b-second", resp: aReply("10.9.9.9")},
		)

		serveQuery(t, pool, dns.TypeA)
		first := pool.lastAmbiguity
		require.False(t, first.IsZero(), "a genuine conflict must warn")

		serveQuery(t, pool, dns.TypeA)
		assert.Equal(t, first, pool.lastAmbiguity, "the second conflict is throttled")
	})
}

// TestResolverPool_AmbiguityWarningIsThrottled keeps a misconfigured zone from
// flooding the log: the warning is emitted at most once per interval (ADR-0023 D4).
func TestResolverPool_AmbiguityWarningIsThrottled(t *testing.T) {
	pool := newTestPool(t, "prefer_private", &fakeGroup{src: "a-first", resp: aReply(privateAddr)})
	pool.warnInterval = time.Minute

	now := time.Now()
	assert.True(t, pool.shouldWarn(now), "the first disagreement warns")
	assert.False(t, pool.shouldWarn(now.Add(30*time.Second)), "within the interval it stays quiet")
	assert.True(t, pool.shouldWarn(now.Add(2*time.Minute)), "after the interval it warns again")
}

// TestResolverPool_StoppedPoolStaysQuiet covers a query that raced a
// reconfiguration: a stopped pool must not fan out to resolvers whose context is
// already canceled, which would cost a failing exchange and a log line per
// upstream server. Like a stopped single resolver, it writes nothing.
func TestResolverPool_StoppedPoolStaysQuiet(t *testing.T) {
	groups := []*fakeGroup{
		{src: "a-first", resp: aReply(privateAddr)},
		{src: "b-second", resp: aReply(publicAddr)},
	}
	pool := newTestPool(t, "prefer_private", groups...)
	pool.Stop()

	var written *dns.Msg
	w := &test.MockResponseWriter{WriteMsgFunc: func(m *dns.Msg) error {
		written = m
		return nil
	}}
	pool.ServeDNS(w, new(dns.Msg).SetQuestion(poolZone, dns.TypeA))

	assert.Nil(t, written, "a stopped pool must not answer")
	for _, g := range groups {
		assert.Equal(t, int32(0), g.queried.Load(), "group %s must not be queried", g.src)
	}
}

func TestResolverPool_StopAndProbeReachEveryGroup(t *testing.T) {
	groups := []*fakeGroup{
		{src: "a-first", resp: aReply(privateAddr)},
		{src: "b-second", resp: aReply(publicAddr)},
	}
	pool := newTestPool(t, "prefer_private", groups...)
	pool.setMember(1, false)

	pool.ProbeAvailability()
	pool.Stop()

	for _, g := range groups {
		assert.Equal(t, int32(1), g.probed.Load(), "group %s probed", g.src)
		assert.Equal(t, int32(1), g.stops.Load(), "group %s stopped", g.src)
	}
}

func TestResolverPool_IDIsStableAndDistinct(t *testing.T) {
	groups := func() []*fakeGroup {
		return []*fakeGroup{{src: "1.1.1.1:53"}, {src: "10.0.0.1:53"}}
	}
	pool := newTestPool(t, "prefer_private", groups()...)

	assert.Equal(t, pool.ID(), newTestPool(t, "prefer_private", groups()...).ID(),
		"the same zone and groups keep the same handler ID")
	assert.NotEqual(t, pool.ID(), pool.ID()[:len(pool.ID())-1], "sanity: IDs are compared whole")

	other := newResolverPool("other.example.com.", pool.policy, []groupResolver{groups()[0], groups()[1]})
	assert.NotEqual(t, pool.ID(), other.ID(), "a different zone gets a different handler ID")

	fewer := newTestPool(t, "prefer_private", groups()[0])
	assert.NotEqual(t, pool.ID(), fewer.ID(), "a different group set gets a different handler ID")
	assert.True(t, pool.MatchSubdomains(), "a pooled zone matches its subdomains, like a single group")
}
