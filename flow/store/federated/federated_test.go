package federated

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// fakeStore is a programmable store.Store stub. Records every call
// the federated wrapper makes so tests can assert on routing
// behaviour without standing up a real backend.
type fakeStore struct {
	name        string
	queryCalls  []store.Filter
	saveCalls   [][]*store.Event
	purgeCalls  []time.Time
	closeCalls  int
	queryResult []*store.Event
	queryErr    error
	saveErr     error
	purgeErr    error
}

func (s *fakeStore) Save(_ context.Context, events []*store.Event) error {
	s.saveCalls = append(s.saveCalls, events)
	return s.saveErr
}
func (s *fakeStore) Query(_ context.Context, f store.Filter) ([]*store.Event, error) {
	s.queryCalls = append(s.queryCalls, f)
	return s.queryResult, s.queryErr
}
func (s *fakeStore) Purge(_ context.Context, t time.Time) (int64, error) {
	s.purgeCalls = append(s.purgeCalls, t)
	return 0, s.purgeErr
}
func (s *fakeStore) Close() error {
	s.closeCalls++
	return nil
}

// fixedNow returns a deterministic now() for splitByBoundary tests.
// 2026-05-03T12:00:00Z gives a 30-day boundary at 2026-04-03T12:00:00Z.
func fixedNow() time.Time {
	return time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
}

func newFed(t *testing.T, retention time.Duration, archive *fakeStore) (*Federated, *fakeStore, *fakeStore) {
	hot := &fakeStore{name: "hot"}
	var archStore store.Store
	if archive != nil {
		archStore = archive
	}
	f, err := New(hot, archStore, retention)
	require.NoError(t, err)
	f.now = fixedNow
	return f, hot, archive
}

// TestNew_RequiresHot guards the cross-cut: federated without a hot
// store would have nowhere to send freshly arrived events. The
// archive can be nil but hot can't.
func TestNew_RequiresHot(t *testing.T) {
	_, err := New(nil, nil, time.Hour)
	assert.Error(t, err)
}

// TestQuery_NoArchive_RoutesToHot is the path a binary without
// `archive_duckdb` ends up on (or any operator that doesn't
// configure a bucket). Federated must behave exactly like the hot
// store — no boundary slicing, no extra filter mangling.
func TestQuery_NoArchive_RoutesToHot(t *testing.T) {
	f, hot, _ := newFed(t, 30*24*time.Hour, nil)
	filter := store.Filter{
		AccountID: "a",
		Since:     fixedNow().Add(-90 * 24 * time.Hour),
	}
	_, err := f.Query(context.Background(), filter)
	require.NoError(t, err)
	require.Len(t, hot.queryCalls, 1)
	// Filter passes through unchanged.
	assert.True(t, hot.queryCalls[0].Since.Equal(filter.Since))
}

// TestQuery_FullyInsideHotRetention exercises the common case: the
// dashboard's default date range (last few hours / day) lands well
// inside hot retention so federated should skip the archive
// entirely.
func TestQuery_FullyInsideHotRetention(t *testing.T) {
	arch := &fakeStore{name: "arch"}
	f, hot, _ := newFed(t, 30*24*time.Hour, arch)
	filter := store.Filter{
		AccountID: "a",
		Since:     fixedNow().Add(-1 * time.Hour),
		Until:     fixedNow(),
	}
	_, err := f.Query(context.Background(), filter)
	require.NoError(t, err)
	assert.Len(t, hot.queryCalls, 1)
	assert.Empty(t, arch.queryCalls, "archive must not be queried for in-retention windows")
}

// TestQuery_FullyOutsideHotRetention is the forensics path: someone
// looking at events from 60 days ago. Hot has nothing; only archive
// is queried.
func TestQuery_FullyOutsideHotRetention(t *testing.T) {
	arch := &fakeStore{name: "arch"}
	f, hot, _ := newFed(t, 30*24*time.Hour, arch)
	filter := store.Filter{
		AccountID: "a",
		Since:     fixedNow().Add(-90 * 24 * time.Hour),
		Until:     fixedNow().Add(-60 * 24 * time.Hour),
	}
	_, err := f.Query(context.Background(), filter)
	require.NoError(t, err)
	assert.Empty(t, hot.queryCalls, "hot must not be queried for past-retention windows")
	require.Len(t, arch.queryCalls, 1)
}

// TestQuery_StraddlesBoundary triggers the both-sides path. Each
// side's window must be trimmed to its half so the merged result
// has no duplicates: hot gets [boundary, until], archive gets
// [since, boundary].
func TestQuery_StraddlesBoundary(t *testing.T) {
	arch := &fakeStore{name: "arch"}
	f, hot, _ := newFed(t, 30*24*time.Hour, arch)
	since := fixedNow().Add(-60 * 24 * time.Hour)
	until := fixedNow().Add(-1 * 24 * time.Hour)
	boundary := fixedNow().Add(-30 * 24 * time.Hour)

	filter := store.Filter{AccountID: "a", Since: since, Until: until}
	_, err := f.Query(context.Background(), filter)
	require.NoError(t, err)
	require.Len(t, hot.queryCalls, 1)
	require.Len(t, arch.queryCalls, 1)
	assert.True(t, hot.queryCalls[0].Since.Equal(boundary),
		"hot Since should clamp to boundary")
	assert.True(t, hot.queryCalls[0].Until.Equal(until))
	assert.True(t, arch.queryCalls[0].Since.Equal(since))
	assert.True(t, arch.queryCalls[0].Until.Equal(boundary),
		"archive Until should clamp to boundary")
}

// TestQuery_OpenEndedSinceBeforeBoundary covers the dashboard's
// "all time" filter where Since is zero. Both sides must still be
// queried so archive returns historical events and hot returns
// recent ones.
func TestQuery_OpenEndedSinceBeforeBoundary(t *testing.T) {
	arch := &fakeStore{name: "arch"}
	f, hot, _ := newFed(t, 30*24*time.Hour, arch)
	filter := store.Filter{AccountID: "a"} // no Since, no Until
	_, err := f.Query(context.Background(), filter)
	require.NoError(t, err)
	assert.Len(t, hot.queryCalls, 1, "hot must be queried for open-ended window")
	assert.Len(t, arch.queryCalls, 1, "archive must be queried for open-ended window")
}

// TestQuery_MergesByReceivedAtDesc verifies the cross-side merge:
// federated must interleave hot + archive results so the dashboard
// renders a single chronologically-ordered stream.
func TestQuery_MergesByReceivedAtDesc(t *testing.T) {
	t1 := fixedNow().Add(-25 * 24 * time.Hour) // in hot retention (28 days < 30)
	t2 := fixedNow().Add(-31 * 24 * time.Hour) // in archive
	t3 := fixedNow().Add(-20 * 24 * time.Hour) // in hot retention
	t4 := fixedNow().Add(-40 * 24 * time.Hour) // in archive

	arch := &fakeStore{
		name:        "arch",
		queryResult: []*store.Event{{ReceivedAt: t2}, {ReceivedAt: t4}},
	}
	hot := &fakeStore{
		name:        "hot",
		queryResult: []*store.Event{{ReceivedAt: t3}, {ReceivedAt: t1}},
	}
	f, err := New(hot, arch, 30*24*time.Hour)
	require.NoError(t, err)
	f.now = fixedNow

	got, err := f.Query(context.Background(), store.Filter{
		AccountID: "a",
		Since:     fixedNow().Add(-50 * 24 * time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, got, 4)
	// Newest first: t3 (-20d) > t1 (-25d) > t2 (-31d) > t4 (-40d)
	assert.True(t, got[0].ReceivedAt.Equal(t3))
	assert.True(t, got[1].ReceivedAt.Equal(t1))
	assert.True(t, got[2].ReceivedAt.Equal(t2))
	assert.True(t, got[3].ReceivedAt.Equal(t4))
}

// TestQuery_AppliesPagingAfterMerge documents that Limit / Offset
// apply to the merged result, not to each side independently. A
// query for "last 10 events across hot + archive" should give 10
// total, not 10 from each side.
func TestQuery_AppliesPagingAfterMerge(t *testing.T) {
	now := fixedNow()
	arch := &fakeStore{
		name: "arch",
		queryResult: []*store.Event{
			{ReceivedAt: now.Add(-31 * 24 * time.Hour)},
			{ReceivedAt: now.Add(-32 * 24 * time.Hour)},
			{ReceivedAt: now.Add(-33 * 24 * time.Hour)},
		},
	}
	hot := &fakeStore{
		name: "hot",
		queryResult: []*store.Event{
			{ReceivedAt: now.Add(-1 * time.Hour)},
			{ReceivedAt: now.Add(-2 * time.Hour)},
			{ReceivedAt: now.Add(-3 * time.Hour)},
		},
	}
	f, err := New(hot, arch, 30*24*time.Hour)
	require.NoError(t, err)
	f.now = fixedNow

	got, err := f.Query(context.Background(), store.Filter{
		AccountID: "a",
		Since:     now.Add(-50 * 24 * time.Hour),
		Limit:     2,
		Offset:    1,
	})
	require.NoError(t, err)
	assert.Len(t, got, 2, "paging should apply across merged result")
}

// TestQuery_PartialFailure_HotOnly captures the resilience contract:
// when the archive bucket is briefly unreachable, federated still
// returns the hot half rather than failing the whole call. The
// dashboard surfaces "incomplete" via the ProbeAvailability hook;
// this test pins the behaviour.
func TestQuery_PartialFailure_HotOnly(t *testing.T) {
	arch := &fakeStore{name: "arch", queryErr: errors.New("bucket unreachable")}
	hot := &fakeStore{
		name:        "hot",
		queryResult: []*store.Event{{ReceivedAt: fixedNow().Add(-1 * time.Hour)}},
	}
	f, err := New(hot, arch, 30*24*time.Hour)
	require.NoError(t, err)
	f.now = fixedNow

	got, err := f.Query(context.Background(), store.Filter{
		AccountID: "a",
		Since:     fixedNow().Add(-50 * 24 * time.Hour),
	})
	require.NoError(t, err, "hot success should mask archive failure")
	assert.Len(t, got, 1)
}

// TestQuery_BothFailed_PropagatesError ensures we never silently
// swallow a total backend outage — caller must see the error.
func TestQuery_BothFailed_PropagatesError(t *testing.T) {
	arch := &fakeStore{queryErr: errors.New("arch down")}
	hot := &fakeStore{queryErr: errors.New("hot down")}
	f, err := New(hot, arch, 30*24*time.Hour)
	require.NoError(t, err)
	f.now = fixedNow

	_, err = f.Query(context.Background(), store.Filter{
		AccountID: "a",
		Since:     fixedNow().Add(-50 * 24 * time.Hour),
	})
	assert.Error(t, err)
}

// TestSave_RoutesToHotOnly pins the cross-side write contract.
// Archive must never receive a Save() because the FlowService sink
// fan-out already covers it; double-writing would dupe rows in the
// bucket.
func TestSave_RoutesToHotOnly(t *testing.T) {
	arch := &fakeStore{}
	f, hot, _ := newFed(t, time.Hour, arch)
	require.NoError(t, f.Save(context.Background(), []*store.Event{{}}))
	assert.Len(t, hot.saveCalls, 1)
	assert.Empty(t, arch.saveCalls, "archive must not see Save calls")
}

// TestPurge_RoutesToHotOnly is the symmetric write-side contract:
// archive lifecycle is the bucket's job, not federated's.
func TestPurge_RoutesToHotOnly(t *testing.T) {
	arch := &fakeStore{}
	f, hot, _ := newFed(t, time.Hour, arch)
	_, err := f.Purge(context.Background(), fixedNow())
	require.NoError(t, err)
	assert.Len(t, hot.purgeCalls, 1)
	assert.Empty(t, arch.purgeCalls)
}

// TestClose_ClosesBoth verifies both sides close so a graceful
// management shutdown does not leak goroutines from either backend.
func TestClose_ClosesBoth(t *testing.T) {
	arch := &fakeStore{}
	f, hot, _ := newFed(t, time.Hour, arch)
	require.NoError(t, f.Close())
	assert.Equal(t, 1, hot.closeCalls)
	assert.Equal(t, 1, arch.closeCalls)
}
