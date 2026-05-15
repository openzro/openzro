package sql

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/openzro/openzro/flow/store"
)

// newTestStore opens a per-test on-disk SQLite under t.TempDir() and
// constructs a Store on top of it. We do not use `:memory:` with
// cache=shared because that leaks rows across tests; per-test temp
// files are isolated by Go's test framework and cleaned up
// automatically.
//
// We register a cleanup that closes the underlying *sql.DB before
// the t.TempDir cleanup runs (cleanups fire LIFO, and t.TempDir
// registered its cleanup before this function was called). Without
// this, Windows holds the .db file handle until the process exits
// and t.TempDir's RemoveAll fails with "the process cannot access
// the file because it is being used by another process".
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/test.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	s, err := New(db)
	require.NoError(t, err)
	return s
}

func sampleEvent(accountID, peerID string, occurred time.Time) *store.Event {
	return &store.Event{
		EventID:       []byte("ev-" + peerID),
		FlowID:        []byte("flow-" + peerID),
		PeerPublicKey: []byte{0xde, 0xad, 0xbe, 0xef},
		IsInitiator:   true,
		AccountID:     accountID,
		PeerID:        peerID,
		OccurredAt:    occurred,
		ReceivedAt:    occurred.Add(time.Millisecond),
		Type:          store.EventTypeStart,
		Direction:     store.DirectionEgress,
		Protocol:      6, // TCP
		SourceIP:      "10.0.0.1",
		DestIP:        "10.0.0.2",
		SourcePort:    49152,
		DestPort:      443,
		RxBytes:       100,
		TxBytes:       200,
		RuleID:        []byte("rule-allow"),
	}
}

func TestSQL_SaveAndQueryRoundtrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	in := sampleEvent("acct-1", "peer-1", now)
	require.NoError(t, s.Save(ctx, []*store.Event{in}))

	got, err := s.Query(ctx, store.Filter{AccountID: "acct-1"})
	require.NoError(t, err)
	require.Len(t, got, 1)

	out := got[0]
	assert.Equal(t, in.AccountID, out.AccountID)
	assert.Equal(t, in.PeerID, out.PeerID)
	assert.Equal(t, in.SourceIP, out.SourceIP)
	assert.Equal(t, in.DestIP, out.DestIP)
	assert.Equal(t, in.Protocol, out.Protocol)
	assert.Equal(t, in.Type, out.Type)
	assert.Equal(t, in.Direction, out.Direction)
	assert.Equal(t, in.RxBytes, out.RxBytes)
	assert.Equal(t, in.TxBytes, out.TxBytes)
	assert.Equal(t, in.RuleID, out.RuleID)
	assert.WithinDuration(t, in.OccurredAt, out.OccurredAt, time.Millisecond)
}

func TestSQL_QueryFiltersByAccount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	require.NoError(t, s.Save(ctx, []*store.Event{
		sampleEvent("acct-A", "peer-1", now),
		sampleEvent("acct-B", "peer-2", now),
		sampleEvent("acct-A", "peer-3", now),
	}))

	a, err := s.Query(ctx, store.Filter{AccountID: "acct-A"})
	require.NoError(t, err)
	assert.Len(t, a, 2, "must return only acct-A rows; account isolation is non-negotiable")

	for _, ev := range a {
		assert.Equal(t, "acct-A", ev.AccountID)
	}
}

func TestSQL_QueryAccountIDRequired(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Query(context.Background(), store.Filter{})
	assert.Error(t, err, "missing AccountID must error so we never accidentally return cross-account data")
}

func TestSQL_QueryByPeerAndTimeRange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	require.NoError(t, s.Save(ctx, []*store.Event{
		sampleEvent("acct-1", "peer-A", now.Add(-2*time.Hour)),
		sampleEvent("acct-1", "peer-A", now.Add(-30*time.Minute)),
		sampleEvent("acct-1", "peer-B", now.Add(-30*time.Minute)),
	}))

	got, err := s.Query(ctx, store.Filter{
		AccountID: "acct-1",
		PeerID:    "peer-A",
		Since:     now.Add(-time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "peer-A", got[0].PeerID)
}

func TestSQL_QueryOrdersByReceivedAtDesc(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)

	for i := 0; i < 5; i++ {
		ev := sampleEvent("acct-1", "peer-X", base)
		ev.ReceivedAt = base.Add(time.Duration(i) * time.Minute)
		require.NoError(t, s.Save(ctx, []*store.Event{ev}))
	}

	got, err := s.Query(ctx, store.Filter{AccountID: "acct-1"})
	require.NoError(t, err)
	require.Len(t, got, 5)
	for i := 1; i < len(got); i++ {
		assert.True(t, !got[i].ReceivedAt.After(got[i-1].ReceivedAt),
			"results must be DESC by received_at; index %d not in order", i)
	}
}

func TestSQL_QueryRespectsLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 10; i++ {
		require.NoError(t, s.Save(ctx, []*store.Event{sampleEvent("acct-1", "peer-X", now)}))
	}

	got, err := s.Query(ctx, store.Filter{AccountID: "acct-1", Limit: 3})
	require.NoError(t, err)
	assert.Len(t, got, 3)
}

func TestSQL_PurgeOnlyDeletesOldEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	old := sampleEvent("acct-1", "peer-old", now.Add(-30*24*time.Hour))
	old.ReceivedAt = old.OccurredAt
	recent := sampleEvent("acct-1", "peer-new", now)
	recent.ReceivedAt = now
	require.NoError(t, s.Save(ctx, []*store.Event{old, recent}))

	deleted, err := s.Purge(ctx, now.Add(-7*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted, "exactly one event predates the cutoff")

	got, err := s.Query(ctx, store.Filter{AccountID: "acct-1"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "peer-new", got[0].PeerID)
}

func TestSQL_BulkInsertHandlesLargeBatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	const n = 1500
	events := make([]*store.Event, n)
	for i := 0; i < n; i++ {
		ev := sampleEvent("acct-1", "peer-bulk", now)
		ev.ReceivedAt = now.Add(time.Duration(i) * time.Microsecond)
		events[i] = ev
	}
	require.NoError(t, s.Save(ctx, events))

	got, err := s.Query(ctx, store.Filter{AccountID: "acct-1", Limit: n + 100})
	require.NoError(t, err)
	assert.Len(t, got, n)
}

func TestSQL_ResolvedAddressesForResources_GroupsByResource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mk := func(peerID, destIP, resourceID string, t time.Time) *store.Event {
		e := sampleEvent("acct-1", peerID, t)
		e.DestIP = destIP
		e.DestResource = []byte(resourceID)
		return e
	}

	// 3 peers resolve "domain-A" to two IPs total, 1 peer resolves
	// "domain-B" to a third IP. "domain-C" gets no traffic — must NOT
	// appear in the result.
	events := []*store.Event{
		mk("peer-1", "10.0.0.10", "res-domain-A", now.Add(-1*time.Hour)),
		mk("peer-2", "10.0.0.10", "res-domain-A", now.Add(-30*time.Minute)), // dupe IP, must collapse via DISTINCT
		mk("peer-3", "10.0.0.11", "res-domain-A", now.Add(-15*time.Minute)),
		mk("peer-1", "10.0.0.20", "res-domain-B", now.Add(-2*time.Hour)),
	}
	require.NoError(t, s.Save(ctx, events))

	out, err := s.ResolvedAddressesForResources(ctx, "acct-1",
		[]string{"res-domain-A", "res-domain-B", "res-domain-C"},
		now.Add(-24*time.Hour),
	)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"10.0.0.10", "10.0.0.11"}, out["res-domain-A"])
	assert.ElementsMatch(t, []string{"10.0.0.20"}, out["res-domain-B"])
	_, hasC := out["res-domain-C"]
	assert.False(t, hasC, "resources with no flow events must be absent from the result map")
}

func TestSQL_ResolvedAddressesForResources_RespectsWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mk := func(destIP string, t time.Time) *store.Event {
		e := sampleEvent("acct-1", "peer-1", t)
		e.DestIP = destIP
		e.DestResource = []byte("res-A")
		return e
	}
	require.NoError(t, s.Save(ctx, []*store.Event{
		mk("10.0.0.1", now.Add(-48*time.Hour)), // outside window
		mk("10.0.0.2", now.Add(-2*time.Hour)),  // inside window
	}))

	out, err := s.ResolvedAddressesForResources(ctx, "acct-1",
		[]string{"res-A"}, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"10.0.0.2"}, out["res-A"],
		"events older than the window must be excluded")
}

func TestSQL_ResolvedAddressesForResources_ScopedByAccount(t *testing.T) {
	// Cross-tenant safety: an event in account B with the same
	// resource ID must NOT leak into account A's response. The
	// resource-ID namespace is per-account by design but the query
	// must still hard-filter at the row level.
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mk := func(accountID, destIP string) *store.Event {
		e := sampleEvent(accountID, "peer-1", now.Add(-1*time.Hour))
		e.DestIP = destIP
		e.DestResource = []byte("res-shared-id")
		return e
	}
	require.NoError(t, s.Save(ctx, []*store.Event{
		mk("acct-A", "10.0.0.1"),
		mk("acct-B", "10.0.0.2"),
	}))

	out, err := s.ResolvedAddressesForResources(ctx, "acct-A",
		[]string{"res-shared-id"}, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"10.0.0.1"}, out["res-shared-id"])
}

func TestSQL_ResolvedAddressesForResources_EmptyInputsAreNoOps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Empty resource ID slice → empty map, no error, no query needed.
	out, err := s.ResolvedAddressesForResources(ctx, "acct-1", nil, time.Time{})
	require.NoError(t, err)
	assert.Empty(t, out)

	// All-empty-string IDs → same.
	out, err = s.ResolvedAddressesForResources(ctx, "acct-1", []string{"", ""}, time.Time{})
	require.NoError(t, err)
	assert.Empty(t, out)

	// Missing account ID is a programming error → returns an error,
	// never an accidental cross-tenant scan.
	_, err = s.ResolvedAddressesForResources(ctx, "", []string{"r1"}, time.Time{})
	assert.Error(t, err)
}
