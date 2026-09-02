package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// API group names are unique within the API issuer. The old protection was
// accidental: two replicas could both prove the name absent, and the loser only
// failed because the transaction later upgraded the account row from shared to
// exclusive while bumping the network serial. That left the database with one
// row, but reported an internal deadlock instead of a conflict the caller can
// act on.
func TestGroups_ConcurrentAPICreateAcrossReplicas(t *testing.T) {
	const (
		accountID = "group-race-account"
		userID    = "group-race-user"
		groupName = "contested-api-group"
	)

	r := newTwoReplicas(t)
	ctx := context.Background()

	account := newAccountWithId(ctx, accountID, userID, "", false)
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))

	// Warm each replica. The barrier aligns calls, not the first statement
	// inside the transaction.
	for _, am := range []*DefaultAccountManager{r.A, r.B} {
		_, err := am.GetAllGroups(ctx, accountID, userID)
		require.NoError(t, err)
	}

	align := newBarrier()
	create := func(am *DefaultAccountManager) <-chan error {
		errCh := make(chan error, 1)
		go func() {
			align.wait(t)
			err := am.SaveGroup(ctx, accountID, userID, &types.Group{
				Name:   groupName,
				Issued: types.GroupIssuedAPI,
				Peers:  []string{},
			}, true)
			errCh <- err
		}()
		return errCh
	}

	errA, errB := create(r.A), create(r.B)

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("replica %s never returned from SaveGroup", name)
			return nil
		}
	}
	resultA, resultB := join("A", errA), join("B", errB)

	groups := accountGroupsByNameAndIssued(t, r.B, accountID, groupName, types.GroupIssuedAPI)
	require.Len(t, groups, 1, "the account must hold exactly one API group named %q (A=%v, B=%v)", groupName, resultA, resultB)

	accepted := 0
	for _, err := range []error{resultA, resultB} {
		if err == nil {
			accepted++
		}
	}
	require.Equal(t, 1, accepted, "exactly one API create must be accepted")

	loser := resultA
	if loser == nil {
		loser = resultB
	}
	require.ErrorContains(t, loser, "already exists")
	s, ok := status.FromError(loser)
	require.True(t, ok && s.Type() == status.AlreadyExists,
		"the losing create must be an actionable conflict, not an internal deadlock: %v", loser)
}

// JWT sync has the same name invariant, but a different user-facing contract:
// losing the create race must not fail login. The second replica should wait
// for the first, re-read the JWT-owned group, and add its user to that group.
// A same-name API group must remain untouched; joining it by display name would
// let a manual group grant access through an IdP claim collision.
func TestGroups_ConcurrentJWTCreateAcrossReplicas(t *testing.T) {
	const (
		accountID    = "jwt-group-race-account"
		firstUserID  = "jwt-user-a"
		secondUserID = "jwt-user-b"
		groupName    = "Engineering"
	)

	r := newTwoReplicas(t)
	ctx := context.Background()

	account := newAccountWithId(ctx, accountID, firstUserID, "", false)
	account.Users[secondUserID] = types.NewUser(secondUserID, types.UserRoleUser, false, false, "", nil, types.UserIssuedAPI)
	account.Users[secondUserID].AccountID = accountID
	account.Settings.JWTGroupsEnabled = true
	account.Settings.JWTGroupsClaimName = "groups"
	account.Groups["api-same-name"] = &types.Group{
		ID:        "api-same-name",
		AccountID: accountID,
		Name:      groupName,
		Issued:    types.GroupIssuedAPI,
		Peers:     []string{},
	}
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))

	for _, am := range []*DefaultAccountManager{r.A, r.B} {
		_, err := am.GetAllGroups(ctx, accountID, firstUserID)
		require.NoError(t, err)
	}

	align := newBarrier()
	sync := func(am *DefaultAccountManager, userID string) <-chan error {
		errCh := make(chan error, 1)
		go func() {
			align.wait(t)
			errCh <- am.SyncUserJWTGroups(ctx, nbcontext.UserAuth{
				AccountId: accountID,
				UserId:    userID,
				Groups:    []string{groupName},
			})
		}()
		return errCh
	}

	errA, errB := sync(r.A, firstUserID), sync(r.B, secondUserID)

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("replica %s never returned from SyncUserJWTGroups", name)
			return nil
		}
	}
	resultA, resultB := join("A", errA), join("B", errB)
	require.NoError(t, resultA, "JWT sync must not fail the first login")
	require.NoError(t, resultB, "JWT sync must not fail the second login")

	apiGroups := accountGroupsByNameAndIssued(t, r.B, accountID, groupName, types.GroupIssuedAPI)
	require.Len(t, apiGroups, 1, "the API group must remain a separate same-name group")

	jwtGroups := accountGroupsByNameAndIssued(t, r.B, accountID, groupName, types.GroupIssuedJWT)
	require.Len(t, jwtGroups, 1, "the account must hold exactly one JWT group named %q", groupName)
	jwtGroupID := jwtGroups[0].ID

	for _, userID := range []string{firstUserID, secondUserID} {
		user, err := r.B.Store.GetUserByUserID(ctx, store.LockingStrengthNone, userID)
		require.NoError(t, err)
		require.Contains(t, user.AutoGroups, jwtGroupID, "%s must join the JWT-owned group", userID)
		require.NotContains(t, user.AutoGroups, apiGroups[0].ID, "%s must not join the API group by name collision", userID)
	}
}

// Group create validates same-source name uniqueness after taking the account
// row. That validation must not take a group lock: update/delete transactions
// hold the group row first and only then serialize on the account row. This
// test pins the create-side direction, which is the inverse of
// TestGroups_UpdateAgainstGroupFirstValidator_NoDeadlock.
func TestGroups_CreateValidationAgainstGroupFirstWriter_NoDeadlock(t *testing.T) {
	const (
		accountID = "group-create-order-account"
		userID    = "group-create-order-user"
		groupID   = "group-create-order-group"
		groupName = "create-order-group"
	)

	r := newTwoReplicas(t)
	ctx := context.Background()

	account := newAccountWithId(ctx, accountID, userID, "", false)
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))
	require.NoError(t, r.A.SaveGroup(ctx, accountID, userID, &types.Group{
		ID:     groupID,
		Name:   groupName,
		Issued: types.GroupIssuedAPI,
		Peers:  []string{},
	}, true))

	groupUpdateHeld := make(chan struct{})
	releaseGroupFirstWriter := make(chan struct{})
	var releaseOnce sync.Once
	releaseWriter := func() {
		releaseOnce.Do(func() {
			close(releaseGroupFirstWriter)
		})
	}
	defer releaseWriter()

	groupFirstErrCh := make(chan error, 1)
	go func() {
		groupFirstErrCh <- r.A.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
			if _, err := tx.GetGroupByID(ctx, store.LockingStrengthUpdate, accountID, groupID); err != nil {
				return err
			}
			close(groupUpdateHeld)
			<-releaseGroupFirstWriter
			return tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID)
		})
	}()

	select {
	case <-groupUpdateHeld:
	case <-time.After(10 * time.Second):
		t.Fatal("group-first writer never took the group update lock")
	}

	accountLockHeld := make(chan struct{})
	createValidationErrCh := make(chan error, 1)
	go func() {
		createValidationErrCh <- r.B.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
			if err := tx.LockAccount(ctx, store.LockingStrengthUpdate, accountID); err != nil {
				return err
			}
			close(accountLockHeld)
			return validateGroupSave(ctx, tx, accountID, &types.Group{
				ID:     "new-api-group",
				Name:   groupName,
				Issued: types.GroupIssuedAPI,
				Peers:  []string{},
			}, true, nil)
		})
	}()

	select {
	case <-accountLockHeld:
	case <-time.After(10 * time.Second):
		t.Fatal("create validation never took the account update lock")
	}
	releaseWriter()

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("%s never returned; it is blocked, which is the failure this test watches for", name)
			return nil
		}
	}

	createErr := join("create validation", createValidationErrCh)
	groupFirstErr := join("group-first writer", groupFirstErrCh)

	require.NoError(t, groupFirstErr, "the group-first writer must not become the deadlock victim")
	require.ErrorContains(t, createErr, "already exists")
	s, ok := status.FromError(createErr)
	require.True(t, ok && s.Type() == status.AlreadyExists,
		"the losing create must be a same-source name conflict, not an internal deadlock: %v", createErr)
}

// Group membership updates must keep the group-before-account lock order.
// Route and DNS validators already read group rows before they serialize on
// the account row, so taking the account row before updating the group would
// create a cycle: route-like writer holds group(Share) and waits for account,
// group writer holds account(Update) and waits for group(Update).
func TestGroups_UpdateAgainstGroupFirstValidator_NoDeadlock(t *testing.T) {
	const (
		accountID = "group-order-account"
		userID    = "group-order-user"
		groupID   = "group-order-group"
	)

	r := newTwoReplicas(t)
	ctx := context.Background()

	account := newAccountWithId(ctx, accountID, userID, "", false)
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))
	require.NoError(t, r.A.SaveGroup(ctx, accountID, userID, &types.Group{
		ID:     groupID,
		Name:   "route-like-group",
		Issued: types.GroupIssuedAPI,
		Peers:  []string{},
	}, true))

	groupShareHeld := make(chan struct{})
	releaseRouteLikeWriter := make(chan struct{})
	routeLikeErrCh := make(chan error, 1)
	go func() {
		routeLikeErrCh <- r.A.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
			if _, err := tx.GetGroupsByIDs(ctx, store.LockingStrengthShare, accountID, []string{groupID}); err != nil {
				return err
			}
			close(groupShareHeld)
			<-releaseRouteLikeWriter
			return tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID)
		})
	}()

	select {
	case <-groupShareHeld:
	case <-time.After(10 * time.Second):
		t.Fatal("route-like writer never took the shared group lock")
	}

	groupErrCh := make(chan error, 1)
	go func() {
		groupErrCh <- r.B.GroupAddPeer(ctx, accountID, groupID, "peer-id")
	}()

	// Give a regressed account-before-group implementation enough room to take
	// the account row before it blocks on the group row. The current order
	// blocks on the group row first, holding no account lock.
	time.Sleep(200 * time.Millisecond)
	close(releaseRouteLikeWriter)

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("%s never returned; it is blocked, which is the failure this test watches for", name)
			return nil
		}
	}

	routeLikeErr := join("route-like writer", routeLikeErrCh)
	groupErr := join("GroupAddPeer", groupErrCh)
	require.NoError(t, routeLikeErr, "the route-like writer must not be the deadlock victim")
	require.NoError(t, groupErr, "GroupAddPeer must queue behind the group reader, not deadlock")
}
