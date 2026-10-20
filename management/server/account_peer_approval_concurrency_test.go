package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

const (
	exemptRaceAccount = "exempt-race-account"
	exemptRaceUser    = "exempt-race-user"
	exemptRaceGroup   = "exempt-race-group"
)

func seedExemptRaceAccount(t *testing.T, r *twoReplicas) {
	t.Helper()
	ctx := context.Background()
	account := newAccountWithId(ctx, exemptRaceAccount, exemptRaceUser, "exempt-race.example", false)
	account.Groups[exemptRaceGroup] = &types.Group{
		ID: exemptRaceGroup, AccountID: exemptRaceAccount, Name: "staff", Issued: types.GroupIssuedAPI,
	}
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))
}

func exemptSettings(groups []string) *types.Settings {
	return &types.Settings{
		PeerLoginExpiration: time.Hour,
		Extra:               &types.ExtraSettings{PeerApprovalEnabled: true, IntegratedValidatorGroups: groups},
	}
}

// A settings save that omits the exempt list must keep whatever another
// replica stored, not the value the saving client happened to read earlier.
func TestUpdateAccountSettings_OmittedExemptGroupsKeepOtherReplicasWrite(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	seedExemptRaceAccount(t, r)

	_, err := r.B.UpdateAccountSettings(ctx, exemptRaceAccount, exemptRaceUser, exemptSettings([]string{exemptRaceGroup}))
	require.NoError(t, err)

	_, err = r.A.UpdateAccountSettings(ctx, exemptRaceAccount, exemptRaceUser, exemptSettings(nil))
	require.NoError(t, err)

	stored, err := r.A.Store.GetAccountSettings(ctx, store.LockingStrengthShare, exemptRaceAccount)
	require.NoError(t, err)
	require.Equal(t, []string{exemptRaceGroup}, stored.Extra.IntegratedValidatorGroups)
}

// A client that omits the whole extra object keeps the latest stored peer
// approval settings, including a change another replica just made.
func TestUpdateAccountSettings_OmittedExtraKeepsOtherReplicasWrite(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	seedExemptRaceAccount(t, r)

	_, err := r.B.UpdateAccountSettings(ctx, exemptRaceAccount, exemptRaceUser, exemptSettings([]string{exemptRaceGroup}))
	require.NoError(t, err)

	_, err = r.A.UpdateAccountSettings(ctx, exemptRaceAccount, exemptRaceUser, &types.Settings{PeerLoginExpiration: time.Hour})
	require.NoError(t, err)

	stored, err := r.A.Store.GetAccountSettings(ctx, store.LockingStrengthShare, exemptRaceAccount)
	require.NoError(t, err)
	require.NotNil(t, stored.Extra)
	require.True(t, stored.Extra.PeerApprovalEnabled)
	require.Equal(t, []string{exemptRaceGroup}, stored.Extra.IntegratedValidatorGroups)
}

// pauseAfterFirstLock makes the replica's next locking read of the accounts or
// groups table report itself and then wait for release. Tables are filtered
// because permission checks lock the user row first, outside the transaction
// under test.
func pauseAfterFirstLock(t *testing.T, s store.Store, locked chan<- struct{}, release <-chan struct{}) {
	t.Helper()
	sqlStore, ok := s.(*store.SqlStore)
	require.True(t, ok)
	db := sqlStore.GetDB()

	var once sync.Once
	hook := func(tx *gorm.DB) {
		if _, locking := tx.Statement.Clauses["FOR"]; !locking {
			return
		}
		if tx.Statement.Table != "accounts" && tx.Statement.Table != "groups" {
			return
		}
		once.Do(func() {
			close(locked)
			if release == nil {
				return
			}
			select {
			case <-release:
			case <-time.After(barrierTimeout):
				t.Errorf("pause: never released")
			}
		})
	}
	// Not removed on cleanup: the store belongs to this test alone, and
	// removing a callback races with queries still running in background
	// fanouts (UpdateAccountPeers). After the first pause the hook is inert.
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:pause-after-first-lock", hook))
}

// Group writers lock the group and then the account row. Saving the exempt
// list must take the same order: if it locked the settings (the account row)
// first and the group second, a settings save on one replica and a group
// delete on another would each hold what the other waits for.
//
// The settings save is held right after its first lock until the delete has
// taken its own first lock (or provably cannot, because the save already holds
// the group). With the inverted order that is a guaranteed deadlock.
func TestUpdateAccountSettings_ExemptGroupsLockOrderAcrossReplicas(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	seedExemptRaceAccount(t, r)

	aLocked, bLocked, releaseA := make(chan struct{}), make(chan struct{}), make(chan struct{})
	pauseAfterFirstLock(t, r.A.Store, aLocked, releaseA)
	pauseAfterFirstLock(t, r.B.Store, bLocked, nil)

	saveErr := make(chan error, 1)
	go func() {
		_, err := r.A.UpdateAccountSettings(ctx, exemptRaceAccount, exemptRaceUser,
			exemptSettings([]string{exemptRaceGroup}))
		saveErr <- err
	}()
	select {
	case <-aLocked:
	case <-time.After(barrierTimeout):
		t.Fatal("the settings save never took a lock")
	}

	deleteErr := make(chan error, 1)
	go func() {
		deleteErr <- r.B.DeleteGroups(ctx, exemptRaceAccount, exemptRaceUser, []string{exemptRaceGroup})
	}()
	select {
	case <-bLocked:
	case <-time.After(2 * time.Second):
		// The delete is blocked behind the save: the order is shared.
	}
	close(releaseA)

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("%s never returned", name)
			return nil
		}
	}
	errSave, errDelete := join("UpdateAccountSettings", saveErr), join("DeleteGroups", deleteErr)

	for name, err := range map[string]error{"save": errSave, "delete": errDelete} {
		if err != nil {
			msg := strings.ToLower(err.Error())
			require.NotContains(t, msg, "deadlock", "%s hit a deadlock: %v", name, err)
		}
	}
	require.NoError(t, errSave)

	// The save committed first, so the delete must have seen the link.
	require.Error(t, errDelete)
	_, err := r.A.Store.GetGroupByID(ctx, store.LockingStrengthShare, exemptRaceAccount, exemptRaceGroup)
	require.NoError(t, err, "an exempt group must not be deleted")
	stored, err := r.A.Store.GetAccountSettings(ctx, store.LockingStrengthShare, exemptRaceAccount)
	require.NoError(t, err)
	require.Equal(t, []string{exemptRaceGroup}, stored.Extra.IntegratedValidatorGroups)
}
