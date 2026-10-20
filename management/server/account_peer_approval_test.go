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
