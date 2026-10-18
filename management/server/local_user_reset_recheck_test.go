package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

func dueRechecks(t *testing.T, m *DefaultAccountManager, now time.Time) []store.CredentialsRecheck {
	t.Helper()
	due, err := m.Store.ListDueCredentialsRechecks(context.Background(), now, nil, 100)
	require.NoError(t, err)
	return due
}

// ADR-0027 D6: every reset enqueues its own sweep, one Dex
// authorization-code lifetime (plus margin) later.
func TestLocalPasswordReset_EnqueuesSweep(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")

	before := time.Now().Truncate(time.Second)
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	after := time.Now()

	due := dueRechecks(t, m, after.Add(credentialsRecheckDelay))
	require.Len(t, due, 1)
	assert.Equal(t, u.Id, due[0].UserID)
	assert.False(t, due[0].Fence)
	assert.WithinRange(t, due[0].DueAt.UTC(), before.Add(credentialsRecheckDelay).UTC(), after.Add(credentialsRecheckDelay).UTC())
	assert.Empty(t, dueRechecks(t, m, before.Add(credentialsRecheckDelay-time.Second)), "not due before the deadline")
}

// ADR-0027 D6: the sweep cuts what a login in flight during the reset
// minted, and the fence cuts what Dex was issuing during the sweep itself.
// The user stays active and devices must sign in again.
func TestCredentialsRecheck_SweepThenFence(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	peerID, _ := connectPeer(t, m, u)
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	reset := storedUser(t, m, u.Id)
	sweepAt := dueRechecks(t, m, time.Now().Add(credentialsRecheckDelay+time.Minute))[0].DueAt

	// A login in flight during the reset finishes after its cutoff.
	inFlightReset := reset.CredentialsValidAfter.Add(5 * time.Second)
	require.False(t, reset.JWTRevoked(inFlightReset))
	revokedBefore := len(creds.revoked)

	m.runDueCredentialsRechecks(ctx, sweepAt)
	swept := storedUser(t, m, u.Id)
	assert.True(t, swept.JWTRevoked(inFlightReset), "the sweep cuts it")
	assert.Greater(t, len(creds.revoked), revokedBefore, "and revokes refresh tokens again")
	assert.False(t, swept.Blocked)
	peer, err := m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	assert.True(t, peer.Status.LoginExpired)

	// Dex was issuing a token while the sweep ran; it lands after the cutoff.
	fences := dueRechecks(t, m, swept.CredentialsValidAfter.Add(credentialsFenceDelay+time.Minute))
	require.Len(t, fences, 1, "the sweep enqueued exactly one fence")
	require.True(t, fences[0].Fence)
	inFlightSweep := swept.CredentialsValidAfter.Add(time.Second)
	require.False(t, swept.JWTRevoked(inFlightSweep))

	m.runDueCredentialsRechecks(ctx, fences[0].DueAt)
	fenced := storedUser(t, m, u.Id)
	assert.True(t, fenced.JWTRevoked(inFlightSweep), "the fence cuts it")
	assert.False(t, fenced.JWTRevoked(fences[0].DueAt.Add(2*time.Second)), "later sign-ins are fine")
	assert.Empty(t, dueRechecks(t, m, fences[0].DueAt.Add(time.Hour)), "a fence enqueues nothing more")
}

// #220 review: overlapping resets keep both deadlines. A code C1 from before
// reset R1 is redeemed after R2's cutoff; R1's own sweep must still cut it
// at R1+31m, and R2's sweep stays due at R2+31m.
func TestCredentialsRecheck_OverlappingResetsKeepEachDeadline(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	r1 := *storedUser(t, m, u.Id).CredentialsValidAfter
	r1Sweep := dueRechecks(t, m, r1.Add(credentialsRecheckDelay+time.Minute))[0].DueAt

	// R2, twenty minutes later, as the reset would record it.
	r2 := r1.Add(20 * time.Minute)
	require.NoError(t, m.Store.SetUserCredentialsValidAfter(ctx, accountID, u.Id, r2))
	require.NoError(t, m.Store.EnqueueCredentialsRecheck(ctx, accountID, u.Id, r2.Add(credentialsRecheckDelay), false))
	c1 := r1.Add(29 * time.Minute)
	require.False(t, storedUser(t, m, u.Id).JWTRevoked(c1), "C1 is newer than R2's cutoff")

	m.runDueCredentialsRechecks(ctx, r1Sweep)
	assert.True(t, storedUser(t, m, u.Id).JWTRevoked(c1), "R1's sweep runs on time and cuts C1")
	var sweeps []store.CredentialsRecheck
	for _, r := range dueRechecks(t, m, r2.Add(credentialsRecheckDelay)) {
		if !r.Fence {
			sweeps = append(sweeps, r)
		}
	}
	require.Len(t, sweeps, 1, "R2's sweep is still pending")
	assert.True(t, r2.Add(credentialsRecheckDelay).Equal(sweeps[0].DueAt.UTC()))
}

// #220 review: replicas that loaded the same due pass run it once. The one
// that takes the identity lock second finds the pass gone and does nothing.
func TestCredentialsRecheck_DuplicateWorkersRunOnce(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	row := dueRechecks(t, m, time.Now().Add(credentialsRecheckDelay+time.Minute))[0]
	revokedBefore := len(creds.revoked)

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, m.runCredentialsRecheck(ctx, row, row.DueAt))
		}()
	}
	wg.Wait()
	assert.Len(t, creds.revoked, revokedBefore+1, "one revocation, not one per worker")
	var fences int
	for _, r := range dueRechecks(t, m, row.DueAt.Add(time.Hour)) {
		if r.Fence {
			fences++
		}
	}
	assert.Equal(t, 1, fences, "one fence, not one per worker")
}

// A user deactivated before its passes are due was cut by that transition;
// the passes are only discarded.
func TestCredentialsRecheck_TerminatedUserOnlyDiscards(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)
	_, err = m.DeactivateLocalUser(ctx, accountID, userID, u.Id)
	require.NoError(t, err)
	revokedBefore := len(creds.revoked)

	m.runDueCredentialsRechecks(ctx, time.Now().Add(time.Hour))
	assert.Empty(t, dueRechecks(t, m, time.Now().Add(2*time.Hour)))
	assert.Len(t, creds.revoked, revokedBefore)
}

// listCountingStore records the size of every due-list page.
type listCountingStore struct {
	store.Store
	mu    sync.Mutex
	pages []int
}

func (s *listCountingStore) ListDueCredentialsRechecks(ctx context.Context, now time.Time,
	after *store.CredentialsRecheck, limit int) ([]store.CredentialsRecheck, error) {
	due, err := s.Store.ListDueCredentialsRechecks(ctx, now, after, limit)
	s.mu.Lock()
	s.pages = append(s.pages, len(due))
	s.mu.Unlock()
	return due, err
}

// #220 review: a backlog is worked through in bounded pages until nothing
// is due, so no pass is left for a later tick and no page loads the table.
func TestCredentialsRecheck_WorksThroughBacklogInPages(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	now := time.Now()
	for i := range 5 {
		// Passes of users that no longer exist are discarded: cheap to run.
		require.NoError(t, m.Store.EnqueueCredentialsRecheck(ctx, accountID, "gone-"+string(rune('a'+i)), now, false))
	}
	counting := &listCountingStore{Store: m.Store}
	m.Store = counting

	m.runDueCredentialsRechecksInPages(ctx, now, 2)
	m.Store = counting.Store
	assert.Empty(t, dueRechecks(t, m, now), "every pass ran")
	for _, n := range counting.pages {
		assert.LessOrEqual(t, n, 2, "pages are bounded")
	}
	assert.GreaterOrEqual(t, len(counting.pages), 3)
}

// failingUserStore fails every user read for the given ids, as a persistent
// inconsistency would.
type failingUserStore struct {
	store.Store
	broken map[string]bool
}

func (s *failingUserStore) GetUserByUserID(ctx context.Context, lockStrength store.LockingStrength,
	userID string) (*types.User, error) {
	if s.broken[userID] {
		return nil, status.Errorf(status.Internal, "persistent failure")
	}
	return s.Store.GetUserByUserID(ctx, lockStrength, userID)
}

// #220 review: a full page of passes that keep failing must not starve the
// passes behind it. The round moves past them; they stay for the next tick.
func TestCredentialsRecheck_FailingPageDoesNotBlockTheQueue(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	now := time.Now()
	broken := map[string]bool{}
	for i := range 3 {
		id := "broken-" + string(rune('a'+i))
		broken[id] = true
		require.NoError(t, m.Store.EnqueueCredentialsRecheck(ctx, accountID, id, now.Add(-time.Hour), false))
	}
	// Due after the failing ones: a user that no longer exists, discarded.
	require.NoError(t, m.Store.EnqueueCredentialsRecheck(ctx, accountID, "gone-z", now.Add(-time.Minute), false))
	plain := m.Store
	m.Store = &failingUserStore{Store: plain, broken: broken}

	m.runDueCredentialsRechecksInPages(ctx, now, 2)
	m.Store = plain
	left := dueRechecks(t, m, now)
	require.Len(t, left, 3, "the failing passes stay for the next tick")
	for _, r := range left {
		assert.True(t, broken[r.UserID], "the pass behind them ran in the same round")
	}
}
