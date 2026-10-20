package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

func requireEventually(t *testing.T, m *DefaultAccountManager, accountID string, want activity.Activity) {
	t.Helper()
	assert.Eventually(t, func() bool {
		events, err := m.eventStore.Get(context.Background(), accountID, 0, 200, false)
		if err != nil {
			return false
		}
		for _, e := range events {
			if e.Activity == want {
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "missing audit event %s", want.StringCode())
}

// Peer approval for third parties: the exempt list must name existing
// groups, and both the approval toggle and the exempt list are audited.
func TestUpdateAccountSettings_PeerApproval(t *testing.T) {
	m, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := m.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)
	require.NoError(t, m.SaveGroup(ctx, accountID, userID, &types.Group{ID: "g-staff", Name: "staff", Issued: types.GroupIssuedAPI}, true))

	settings := func(enabled bool, groups ...string) *types.Settings {
		return &types.Settings{
			PeerLoginExpiration: time.Hour,
			Extra:               &types.ExtraSettings{PeerApprovalEnabled: enabled, IntegratedValidatorGroups: groups},
		}
	}

	_, err = m.UpdateAccountSettings(ctx, accountID, userID, settings(true, "g-staff"))
	require.NoError(t, err)
	stored, err := m.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	assert.True(t, stored.Extra.PeerApprovalEnabled)
	assert.Equal(t, []string{"g-staff"}, stored.Extra.IntegratedValidatorGroups)
	requireEventually(t, m, accountID, activity.AccountPeerApprovalEnabled)
	requireEventually(t, m, accountID, activity.AccountPeerApprovalExemptGroupsUpdated)

	_, err = m.UpdateAccountSettings(ctx, accountID, userID, settings(true, "g-staff", "no-such-group"))
	sErr, ok := status.FromError(err)
	require.True(t, ok && err != nil, "expected a status error, got %v", err)
	assert.Equal(t, status.InvalidArgument, sErr.Type())
	stored, err = m.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	assert.Equal(t, []string{"g-staff"}, stored.Extra.IntegratedValidatorGroups, "a rejected update changes nothing")

	_, err = m.UpdateAccountSettings(ctx, accountID, userID, settings(false, "g-staff"))
	require.NoError(t, err)
	requireEventually(t, m, accountID, activity.AccountPeerApprovalDisabled)
}

// Changing the toggle or the exempt groups changes which peers are
// validated, so connected peers must get a new network map.
func TestUpdateAccountSettings_PeerApprovalReachesConnectedPeers(t *testing.T) {
	m, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := m.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)
	require.NoError(t, m.SaveGroup(ctx, accountID, userID, &types.Group{ID: "g-staff", Name: "staff", Issued: types.GroupIssuedAPI}, true))
	owner, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	require.NoError(t, err)
	_, updates := connectPeer(t, m, owner)

	expectUpdate := func(why string) {
		t.Helper()
		select {
		case <-updates:
		case <-time.After(3 * time.Second):
			t.Fatalf("connected peer got no update after %s", why)
		}
		for len(updates) > 0 {
			<-updates
		}
	}
	for len(updates) > 0 {
		<-updates
	}

	base := &types.Settings{PeerLoginExpiration: time.Hour, Extra: &types.ExtraSettings{PeerApprovalEnabled: true}}
	_, err = m.UpdateAccountSettings(ctx, accountID, userID, base)
	require.NoError(t, err)
	expectUpdate("enabling peer approval")

	withGroup := &types.Settings{PeerLoginExpiration: time.Hour,
		Extra: &types.ExtraSettings{PeerApprovalEnabled: true, IntegratedValidatorGroups: []string{"g-staff"}}}
	_, err = m.UpdateAccountSettings(ctx, accountID, userID, withGroup)
	require.NoError(t, err)
	expectUpdate("changing the exempt groups")
}

// A nil exempt list means the API client omitted the field: the stored list is
// kept, merged inside the settings transaction. An empty list clears it.
func TestUpdateAccountSettings_PeerApprovalExemptGroupsOmitted(t *testing.T) {
	m, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := m.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)
	require.NoError(t, m.SaveGroup(ctx, accountID, userID, &types.Group{ID: "g-staff", Name: "staff", Issued: types.GroupIssuedAPI}, true))

	update := func(groups []string) []string {
		t.Helper()
		_, err := m.UpdateAccountSettings(ctx, accountID, userID, &types.Settings{
			PeerLoginExpiration: time.Hour,
			Extra:               &types.ExtraSettings{PeerApprovalEnabled: true, IntegratedValidatorGroups: groups},
		})
		require.NoError(t, err)
		stored, err := m.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
		require.NoError(t, err)
		return stored.Extra.IntegratedValidatorGroups
	}

	assert.Equal(t, []string{"g-staff"}, update([]string{"g-staff"}))
	assert.Equal(t, []string{"g-staff"}, update(nil), "an omitted list keeps the stored one")
	assert.Empty(t, update([]string{}), "an explicit empty list clears it")
}

// The exempt list is a set. Duplicates are dropped and the order is canonical,
// so reordering it is not a change: no audit event and no network serial bump.
func TestUpdateAccountSettings_PeerApprovalExemptGroupsAreASet(t *testing.T) {
	m, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := m.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)
	for _, id := range []string{"g-a", "g-b"} {
		require.NoError(t, m.SaveGroup(ctx, accountID, userID, &types.Group{ID: id, Name: id, Issued: types.GroupIssuedAPI}, true))
	}

	update := func(groups ...string) {
		t.Helper()
		_, err := m.UpdateAccountSettings(ctx, accountID, userID, &types.Settings{
			PeerLoginExpiration: time.Hour,
			Extra:               &types.ExtraSettings{PeerApprovalEnabled: true, IntegratedValidatorGroups: groups},
		})
		require.NoError(t, err)
	}
	serial := func() uint64 {
		t.Helper()
		network, err := m.Store.GetAccountNetwork(ctx, store.LockingStrengthShare, accountID)
		require.NoError(t, err)
		return network.CurrentSerial()
	}
	exemptEvents := func() int {
		t.Helper()
		events, err := m.eventStore.Get(ctx, accountID, 0, 200, false)
		require.NoError(t, err)
		n := 0
		for _, e := range events {
			if e.Activity == activity.AccountPeerApprovalExemptGroupsUpdated {
				n++
			}
		}
		return n
	}

	update("g-b", "g-a", "g-a")
	stored, err := m.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	assert.Equal(t, []string{"g-a", "g-b"}, stored.Extra.IntegratedValidatorGroups)
	requireEventually(t, m, accountID, activity.AccountPeerApprovalExemptGroupsUpdated)

	events, before := exemptEvents(), serial()
	update("g-b", "g-a")
	assert.Equal(t, before, serial(), "reordering the set must not bump the network serial")
	assert.Never(t, func() bool { return exemptEvents() != events }, 300*time.Millisecond, 20*time.Millisecond,
		"reordering the set must not be audited")
}

// An API client that omits the whole extra object changes nothing in it:
// the toggle and the exempt groups stay, with no audit event, no network
// serial bump and no update to connected peers.
func TestUpdateAccountSettings_OmittedExtraKeepsPeerApproval(t *testing.T) {
	m, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := m.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)
	require.NoError(t, m.SaveGroup(ctx, accountID, userID, &types.Group{ID: "g-staff", Name: "staff", Issued: types.GroupIssuedAPI}, true))

	_, err = m.UpdateAccountSettings(ctx, accountID, userID, &types.Settings{
		PeerLoginExpiration: time.Hour,
		Extra:               &types.ExtraSettings{PeerApprovalEnabled: true, IntegratedValidatorGroups: []string{"g-staff"}},
	})
	require.NoError(t, err)
	requireEventually(t, m, accountID, activity.AccountPeerApprovalExemptGroupsUpdated)

	owner, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, userID)
	require.NoError(t, err)
	_, updates := connectPeer(t, m, owner)
	// Let the connect-time updates settle before measuring.
	time.Sleep(300 * time.Millisecond)
	for len(updates) > 0 {
		<-updates
	}

	approvalEvents := func() int {
		t.Helper()
		events, err := m.eventStore.Get(ctx, accountID, 0, 200, false)
		require.NoError(t, err)
		n := 0
		for _, e := range events {
			switch e.Activity {
			case activity.AccountPeerApprovalEnabled, activity.AccountPeerApprovalDisabled,
				activity.AccountPeerApprovalExemptGroupsUpdated:
				n++
			}
		}
		return n
	}
	network, err := m.Store.GetAccountNetwork(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	serialBefore, eventsBefore := network.CurrentSerial(), approvalEvents()

	_, err = m.UpdateAccountSettings(ctx, accountID, userID, &types.Settings{PeerLoginExpiration: time.Hour})
	require.NoError(t, err)

	stored, err := m.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	require.NotNil(t, stored.Extra)
	assert.True(t, stored.Extra.PeerApprovalEnabled, "an omitted extra keeps the toggle")
	assert.Equal(t, []string{"g-staff"}, stored.Extra.IntegratedValidatorGroups, "an omitted extra keeps the groups")

	network, err = m.Store.GetAccountNetwork(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	assert.Equal(t, serialBefore, network.CurrentSerial(), "an omitted extra must not bump the network serial")
	select {
	case <-updates:
		t.Fatal("an omitted extra must not fan out to connected peers")
	case <-time.After(500 * time.Millisecond):
	}
	assert.Equal(t, eventsBefore, approvalEvents(), "an omitted extra must not be audited")
}
