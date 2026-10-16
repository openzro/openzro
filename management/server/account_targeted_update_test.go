package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// scimStyleTx mimics the SCIM group-membership flow's lock order on replica
// s: lock the account, wait until released, then lock and save a user. It
// returns once the account lock is held.
func scimStyleTx(t *testing.T, s store.Store, accountID, userID string) (release chan struct{}, done <-chan error) {
	t.Helper()
	locked, release := make(chan struct{}), make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ExecuteInTransaction(context.Background(), func(tx store.Store) error {
			if err := tx.LockAccount(context.Background(), store.LockingStrengthUpdate, accountID); err != nil {
				close(locked)
				return err
			}
			close(locked)
			<-release
			u, err := tx.GetUserByUserID(context.Background(), store.LockingStrengthUpdate, userID)
			if err != nil {
				return err
			}
			u.AutoGroups = append(u.AutoGroups, "g-scim")
			return tx.SaveUser(context.Background(), store.LockingStrengthUpdate, u)
		})
	}()
	<-locked
	return release, errCh
}

// requireBothSucceed waits until the runtime path is blocked on the SCIM
// transaction's account lock (as the database reports it), lets the SCIM
// side continue, and requires both to commit.
func requireBothSucceed(t *testing.T, probe store.Store, runtime <-chan error, release chan struct{}, scim <-chan error) {
	t.Helper()
	deadline := time.After(barrierTimeout)
	for lockWaiters(t, probe) == 0 {
		select {
		case err := <-runtime:
			close(release)
			require.NoError(t, err)
			require.NoError(t, <-scim)
			return
		case <-deadline:
			close(release)
			t.Fatal("the runtime path never waited on the account lock")
		case <-time.After(20 * time.Millisecond):
		}
	}
	close(release)
	for name, ch := range map[string]<-chan error{"scim": scim, "runtime": runtime} {
		select {
		case err := <-ch:
			require.NoError(t, err, "%s side failed; a whole-account rewrite deadlocks with the SCIM lock order", name)
		case <-time.After(barrierTimeout):
			t.Fatalf("%s side never returned", name)
		}
	}
}

// #214: the owner-domain update rewrote the whole account through
// SaveAccount to change one column, which deadlocks with the SCIM
// membership flow (accounts -> users). A targeted update only touches the
// account row.
func TestOwnerDomainUpdate_DoesNotDeadlockWithSCIM(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	account, err := r.A.GetOrCreateAccountByUser(ctx, "owner", "")
	require.NoError(t, err)

	release, scim := scimStyleTx(t, r.A.Store, account.Id, "owner")

	runtime := make(chan error, 1)
	go func() {
		_, err := r.B.GetOrCreateAccountByUser(ctx, "owner", "new-domain.example")
		runtime <- err
	}()

	requireBothSucceed(t, r.A.Store, runtime, release, scim)

	stored, err := r.A.Store.GetAccount(ctx, account.Id)
	require.NoError(t, err)
	require.Equal(t, "new-domain.example", stored.Domain)
	require.Contains(t, stored.Users["owner"].AutoGroups, "g-scim", "the SCIM write must not be lost")
}

// #214: same for the integrated-validator settings save.
func TestIntegratedValidatorUpdate_DoesNotDeadlockWithSCIM(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	account, err := r.A.GetOrCreateAccountByUser(ctx, "owner", "")
	require.NoError(t, err)

	release, scim := scimStyleTx(t, r.A.Store, account.Id, "owner")

	runtime := make(chan error, 1)
	go func() { runtime <- r.B.UpdateIntegratedValidator(ctx, account.Id, "owner", "", nil) }()

	requireBothSucceed(t, r.A.Store, runtime, release, scim)

	stored, err := r.A.Store.GetAccount(ctx, account.Id)
	require.NoError(t, err)
	require.Contains(t, stored.Users["owner"].AutoGroups, "g-scim", "the SCIM write must not be lost")
}

// The targeted validator update must still persist the setting.
func TestIntegratedValidatorUpdate_PersistsSettings(t *testing.T) {
	manager, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := manager.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)

	group := &types.Group{ID: "g-validated", Name: "validated", AccountID: accountID}
	require.NoError(t, manager.Store.SaveGroup(ctx, store.LockingStrengthUpdate, group))

	require.NoError(t, manager.UpdateIntegratedValidator(ctx, accountID, userID, "validator-x", []string{"g-validated"}))

	settings, err := manager.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	require.NotNil(t, settings.Extra)
	require.Equal(t, "validator-x", settings.Extra.IntegratedValidator)
	require.Equal(t, []string{"g-validated"}, settings.Extra.IntegratedValidatorGroups)
}
