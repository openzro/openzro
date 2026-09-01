package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// Removing clause.OnConflict{UpdateAll: true} from SaveAccount trades silent
// absorption for a reported conflict, and that trade has to be measured where
// it lands rather than argued: two replicas calling SaveAccount with no
// manager lock between them. Both shapes below were absorbed before, and only
// one of them should have been.
func TestSaveAccount_ConcurrentAcrossReplicas(t *testing.T) {
	ctx := context.Background()

	// Same account, two writers, different payloads. Nothing here violates an
	// invariant — it is the ordinary "two replicas saved the same account"
	// case, and it must survive: one account, intact, and any error has to be
	// one a caller can understand rather than a mangled half-write.
	t.Run("same account id, different payloads", func(t *testing.T) {
		const accountID = "concurrent-save-acc"

		r := newTwoReplicas(t)

		seed := newAccountWithId(ctx, accountID, "owner-user", "tenant.example", false)
		require.NoError(t, r.A.Store.SaveAccount(ctx, seed))

		// The barrier goes after the read and the mutation, not before them.
		// What has to collide is the two writes, each carrying a payload built
		// from a pre-commit snapshot; aligning the reads instead would let a
		// near-serial run have the second writer read what the first already
		// committed, and the test would pass without ever racing.
		align := newBarrier()
		save := func(store interface {
			SaveAccount(context.Context, *types.Account) error
		}, groupID string) <-chan error {
			errCh := make(chan error, 1)
			go func() {
				account, err := r.A.Store.GetAccount(ctx, accountID)
				if err != nil {
					errCh <- err
					return
				}
				account.Groups[groupID] = &types.Group{ID: groupID, Name: groupID, AccountID: accountID}
				align.wait(t)
				errCh <- store.SaveAccount(ctx, account)
			}()
			return errCh
		}

		errA, errB := save(r.A.Store, "group-from-a"), save(r.B.Store, "group-from-b")

		join := func(name string, ch <-chan error) error {
			t.Helper()
			select {
			case err := <-ch:
				return err
			case <-time.After(30 * time.Second):
				t.Fatalf("replica %s never returned from SaveAccount", name)
				return nil
			}
		}
		resultA, resultB := join("A", errA), join("B", errB)

		// Losing this one is acceptable — the two writers genuinely raced on
		// the same row — but it must not be the poisonous shape: a caller told
		// nothing while the row went somewhere else.
		require.False(t, resultA != nil && resultB != nil,
			"both writers failed; at least one save of the same account must land (A=%v, B=%v)", resultA, resultB)

		// Whatever happened, the account has to be readable and whole from the
		// other replica's store.
		stored, err := r.B.Store.GetAccount(ctx, accountID)
		require.NoError(t, err, "the account must survive two concurrent saves")
		require.Equal(t, accountID, stored.Id, "the account read back is not the one that was saved")
		require.Contains(t, stored.Users, "owner-user", "the account lost its owner")
		require.NotEmpty(t, stored.Network, "the account lost its network")
		require.Len(t, r.B.Store.GetAllAccounts(ctx), 1, "a concurrent save duplicated the account")

		// The edges are in the children, not in the account row: SaveAccount
		// deletes the tree and recreates it, so a concurrent writer can leave
		// a child orphaned from the payload that won, or leave two of it. The
		// association rows are read directly and compared against what the
		// account carries, because GetAccount reads them through the same
		// preload that would hide a mismatch behind its own result.
		groups, err := r.B.Store.GetAccountGroups(ctx, store.LockingStrengthNone, accountID)
		require.NoError(t, err)
		seen := map[string]int{}
		for _, g := range groups {
			require.Equal(t, accountID, g.AccountID, "group %s is attached to another account", g.ID)
			seen[g.ID]++
		}
		for id, n := range seen {
			require.Equal(t, 1, n, "group %s exists %d times after the race", id, n)
			require.Contains(t, stored.Groups, id, "group %s is an orphan: stored on its own, absent from the account", id)
		}
		require.Len(t, groups, len(stored.Groups), "the account and the group rows disagree on how many groups it has")

		// Exactly one of the two concurrent additions may survive: both
		// writers read the account before either committed, so the payload
		// that lands last carries its own group and not the other's.
		_, hasA := stored.Groups["group-from-a"]
		_, hasB := stored.Groups["group-from-b"]
		require.True(t, hasA != hasB,
			"expected exactly one of the concurrently added groups, got a=%v b=%v", hasA, hasB)

		users, err := r.B.Store.GetAccountUsers(ctx, store.LockingStrengthNone, accountID)
		require.NoError(t, err)
		require.Len(t, users, len(stored.Users), "the account and the user rows disagree")

		peers, err := r.B.Store.GetAccountPeers(ctx, store.LockingStrengthNone, accountID, "", "")
		require.NoError(t, err)
		require.Len(t, peers, len(stored.Peers), "the account and the peer rows disagree")
	})

	// Different accounts, colliding on idx_accounts_primary_private_domain.
	// This one has to be refused, and refused legibly — it is #161's shape,
	// where MySQL's untargeted upsert either reported success and wrote
	// nothing or redirected the write onto the winner's row.
	t.Run("different account ids colliding on the primary domain", func(t *testing.T) {
		const domain = "collide.example"

		r := newTwoReplicas(t)

		primary := func(id, userID string) *types.Account {
			a := newAccountWithId(ctx, id, userID, domain, false)
			a.DomainCategory = types.PrivateCategory
			a.IsDomainPrimaryAccount = true
			return a
		}

		align := newBarrier()
		save := func(am *DefaultAccountManager, account *types.Account) <-chan error {
			errCh := make(chan error, 1)
			go func() {
				align.wait(t)
				errCh <- am.Store.SaveAccount(ctx, account)
			}()
			return errCh
		}

		errA := save(r.A, primary("collide-acc-a", "user-a"))
		errB := save(r.B, primary("collide-acc-b", "user-b"))

		join := func(name string, ch <-chan error) error {
			t.Helper()
			select {
			case err := <-ch:
				return err
			case <-time.After(30 * time.Second):
				t.Fatalf("replica %s never returned from SaveAccount", name)
				return nil
			}
		}
		resultA, resultB := join("A", errA), join("B", errB)

		primaries := 0
		for _, acc := range r.B.Store.GetAllAccounts(ctx) {
			if acc.Domain == domain && acc.IsDomainPrimaryAccount && acc.DomainCategory == types.PrivateCategory {
				primaries++
			}
		}
		require.Equal(t, 1, primaries,
			"the domain is primary on %d accounts (A=%v, B=%v)", primaries, resultA, resultB)

		losers := 0
		for name, err := range map[string]error{"A": resultA, "B": resultB} {
			if err == nil {
				continue
			}
			losers++

			// How the refusal arrives differs by engine, and the difference is
			// #157, not this change. MySQL under REPEATABLE READ takes a gap
			// lock on the index range, so the two writers deadlock before
			// either insert is refused: the invariant holds, the error is one
			// the caller cannot act on. Measured identical with and without
			// the upsert clause, so removing it neither causes nor cures this.
			//
			// Pinned to that mechanism rather than to "an error happened",
			// which would pass on a permission failure or a broken fixture.
			if isRepeatableReadEngine() {
				require.ErrorContains(t, err, "Error 1213",
					"replica %s must lose to the gap-lock deadlock, which is what holds this on MySQL today; got %v", name, err)
				continue
			}
			s, ok := status.FromError(err)
			require.True(t, ok && s.Type() == status.AlreadyExists,
				"replica %s must be refused with a typed conflict, not a raw driver error; got %v", name, err)
		}
		require.Equal(t, 1, losers, "exactly one save must be refused (A=%v, B=%v)", resultA, resultB)
	})
}
