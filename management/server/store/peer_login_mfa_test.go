package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/types"
)

const (
	plmAccount = "bf1c8084-ba50-4ce7-9439-34653001fc3b"
	plmUser    = "edafee4e-63fb-11ec-90d6-0242ac120003"
)

func newApproval(id, key string, created time.Time) *types.PeerLoginMFAApproval {
	return &types.PeerLoginMFAApproval{
		ID: id, AccountID: plmAccount, UserID: plmUser, WGPubKey: key,
		Hostname: "laptop", OS: "linux", IP: "203.0.113.7",
		CreatedAt: created, ExpiresAt: created.Add(types.PeerLoginMFAWindow),
		Deadline: created.Add(types.PeerLoginMFADeadline),
	}
}

// The approval record's transitions are conditional updates: each succeeds
// once, from its source state only, and never on an expired record.
func TestSqlStore_PeerLoginMFAApprovalTransitions(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("a1", "key-1", t0)))
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("a2", "key-2", t0)))

		got, err := s.GetPeerLoginMFAApprovalByID(ctx, LockingStrengthNone, "a1")
		require.NoError(t, err)
		assert.Equal(t, "laptop", got.Hostname)
		assert.True(t, got.Pending(t0))

		_, err = s.GetPeerLoginMFAApprovalByID(ctx, LockingStrengthNone, "missing")
		require.ErrorIs(t, err, ErrPeerLoginMFAApprovalNotFound)

		// Approve a1 at t0+1m, extending it by the grace.
		at := t0.Add(time.Minute)
		ok, err := s.ApprovePeerLoginMFAApproval(ctx, "a1", at, at.Add(types.PeerLoginMFAApprovedGrace))
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = s.ApprovePeerLoginMFAApproval(ctx, "a1", at, at.Add(types.PeerLoginMFAApprovedGrace))
		require.NoError(t, err)
		assert.False(t, ok, "approve is pending → approved only")
		ok, err = s.DenyPeerLoginMFAApproval(ctx, "a1", at)
		require.NoError(t, err)
		assert.False(t, ok, "an approved record cannot be denied")

		// Deny a2; it can no longer be approved.
		ok, err = s.DenyPeerLoginMFAApproval(ctx, "a2", at)
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = s.ApprovePeerLoginMFAApproval(ctx, "a2", at, at.Add(time.Minute))
		require.NoError(t, err)
		assert.False(t, ok, "a denied record cannot be approved")

		// Consume: only the approved, unconsumed, unexpired record of that key.
		ok, err = s.ConsumePeerLoginMFAApproval(ctx, plmAccount, plmUser, "key-2", at)
		require.NoError(t, err)
		assert.False(t, ok, "a denied record is not consumable")
		ok, err = s.ConsumePeerLoginMFAApproval(ctx, plmAccount, "someone-else", "key-1", at)
		require.NoError(t, err)
		assert.False(t, ok, "another user's record is not consumable")
		ok, err = s.ConsumePeerLoginMFAApproval(ctx, plmAccount, plmUser, "key-1", at)
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = s.ConsumePeerLoginMFAApproval(ctx, plmAccount, plmUser, "key-1", at)
		require.NoError(t, err)
		assert.False(t, ok, "a record is consumed once")

		// An expired pending record can neither be approved nor denied.
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("a3", "key-3", t0)))
		late := t0.Add(types.PeerLoginMFAWindow)
		ok, err = s.ApprovePeerLoginMFAApproval(ctx, "a3", late, late.Add(time.Minute))
		require.NoError(t, err)
		assert.False(t, ok)
		ok, err = s.DenyPeerLoginMFAApproval(ctx, "a3", late)
		require.NoError(t, err)
		assert.False(t, ok)

		// An approved record past its expiry is not consumable.
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("a4", "key-4", t0)))
		ok, err = s.ApprovePeerLoginMFAApproval(ctx, "a4", at, at.Add(types.PeerLoginMFAApprovedGrace))
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = s.ConsumePeerLoginMFAApproval(ctx, plmAccount, plmUser, "key-4", at.Add(types.PeerLoginMFAApprovedGrace))
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

// The per-user listing returns unexpired records only; the expiry sweep
// finds expired ones in batches and deletes each once.
func TestSqlStore_PeerLoginMFAApprovalListingAndSweep(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("old", "key-1", t0.Add(-10*time.Minute))))
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("new1", "key-2", t0)))
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("new2", "key-3", t0)))

		live, err := s.GetUserPeerLoginMFAApprovals(ctx, LockingStrengthNone, plmAccount, plmUser, t0)
		require.NoError(t, err)
		ids := []string{}
		for _, a := range live {
			ids = append(ids, a.ID)
		}
		assert.ElementsMatch(t, []string{"new1", "new2"}, ids)

		expired, err := s.GetExpiredPeerLoginMFAApprovals(ctx, t0, 10)
		require.NoError(t, err)
		require.Len(t, expired, 1)
		assert.Equal(t, "old", expired[0].ID)

		ok, err := s.DeleteExpiredPeerLoginMFAApproval(ctx, "old", t0)
		require.NoError(t, err)
		assert.True(t, ok)
		ok, err = s.DeleteExpiredPeerLoginMFAApproval(ctx, "old", t0)
		require.NoError(t, err)
		assert.False(t, ok, "a record is swept once")
		ok, err = s.DeleteExpiredPeerLoginMFAApproval(ctx, "new1", t0)
		require.NoError(t, err)
		assert.False(t, ok, "an unexpired record is not swept")

		n, err := s.DeleteUserPeerLoginMFAApprovals(ctx, plmAccount, plmUser)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		live, err = s.GetUserPeerLoginMFAApprovals(ctx, LockingStrengthNone, plmAccount, plmUser, t0)
		require.NoError(t, err)
		assert.Empty(t, live)
	})
}

// Deleting a user or an account deletes their approval records.
func TestSqlStore_DeletionClearsPeerLoginMFAApprovals(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Now().UTC()
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("u1", "key-1", t0)))
		other := newApproval("o1", "key-2", t0)
		other.UserID = "f4f6d672-63fb-11ec-90d6-0242ac120003"
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, other))

		require.NoError(t, s.DeleteUser(ctx, LockingStrengthUpdate, plmAccount, plmUser))
		_, err := s.GetPeerLoginMFAApprovalByID(ctx, LockingStrengthNone, "u1")
		require.ErrorIs(t, err, ErrPeerLoginMFAApprovalNotFound)
		_, err = s.GetPeerLoginMFAApprovalByID(ctx, LockingStrengthNone, "o1")
		require.NoError(t, err, "another user's record stays")

		account, err := s.GetAccount(ctx, plmAccount)
		require.NoError(t, err)
		require.NoError(t, s.DeleteAccount(ctx, account))
		_, err = s.GetPeerLoginMFAApprovalByID(ctx, LockingStrengthNone, "o1")
		require.ErrorIs(t, err, ErrPeerLoginMFAApprovalNotFound)
	})
}

// Only a record nobody acted on is swept as untouched; a denied, approved
// or consumed one is left for the plain sweep.
func TestSqlStore_DeleteUntouchedExpiredPeerLoginMFAApproval(t *testing.T) {
	runTestForAllEngines(t, "../testdata/store.sql", func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("untouched", "key-1", t0)))
		require.NoError(t, s.CreatePeerLoginMFAApproval(ctx, newApproval("denied", "key-2", t0)))
		ok, err := s.DenyPeerLoginMFAApproval(ctx, "denied", t0)
		require.NoError(t, err)
		require.True(t, ok)
		after := t0.Add(types.PeerLoginMFAWindow)

		ok, err = s.DeleteUntouchedExpiredPeerLoginMFAApproval(ctx, "denied", after)
		require.NoError(t, err)
		assert.False(t, ok, "a denied record is not untouched")
		ok, err = s.DeleteUntouchedExpiredPeerLoginMFAApproval(ctx, "untouched", t0)
		require.NoError(t, err)
		assert.False(t, ok, "an unexpired record is not swept")
		ok, err = s.DeleteUntouchedExpiredPeerLoginMFAApproval(ctx, "untouched", after)
		require.NoError(t, err)
		assert.True(t, ok)
		ok, err = s.DeleteExpiredPeerLoginMFAApproval(ctx, "denied", after)
		require.NoError(t, err)
		assert.True(t, ok)
	})
}
