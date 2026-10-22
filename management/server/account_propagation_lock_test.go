package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// Switching groups propagation on through replica A while replica B
// changes a group does not deadlock (openzro#224): A holds the account
// row, B waits for what A took before it, and both commit. Group writers
// lock groups before the account row, so the settings save must too.
func TestUpdateAccountSettings_GroupsPropagationLockOrder(t *testing.T) {
	tests := []struct {
		name string
		// change is B's group write; applied checks it landed.
		change  func(ctx context.Context, am *DefaultAccountManager, accountID, peerID string) error
		applied func(t *testing.T, s store.Store, accountID, peerID string)
	}{
		{"add a peer to a group", func(ctx context.Context, am *DefaultAccountManager, accountID, peerID string) error {
			return am.GroupAddPeer(ctx, accountID, "g-other", peerID)
		}, func(t *testing.T, s store.Store, accountID, peerID string) {
			g, err := s.GetGroupByID(context.Background(), store.LockingStrengthNone, accountID, "g-other")
			require.NoError(t, err)
			require.Contains(t, g.Peers, peerID)
		}},
		{"save a group", func(ctx context.Context, am *DefaultAccountManager, accountID, _ string) error {
			return am.SaveGroup(ctx, accountID, "owner", &types.Group{
				ID: "g-other", Name: "other-renamed", Issued: types.GroupIssuedAPI,
			}, false)
		}, func(t *testing.T, s store.Store, accountID, _ string) {
			g, err := s.GetGroupByID(context.Background(), store.LockingStrengthNone, accountID, "g-other")
			require.NoError(t, err)
			require.Equal(t, "other-renamed", g.Name)
		}},
		{"delete a group", func(ctx context.Context, am *DefaultAccountManager, accountID, _ string) error {
			return am.DeleteGroup(ctx, accountID, "owner", "g-unused")
		}, func(t *testing.T, s store.Store, accountID, _ string) {
			_, err := s.GetGroupByID(context.Background(), store.LockingStrengthNone, accountID, "g-unused")
			require.Error(t, err)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			replicas := newTwoReplicas(t)
			ctx := context.Background()
			acc, err := replicas.A.GetOrCreateAccountByUser(ctx, "owner", "example.com")
			require.NoError(t, err)
			for _, g := range []*types.Group{
				{ID: "g-staff", Name: "staff", Issued: types.GroupIssuedAPI},
				{ID: "g-other", Name: "other", Issued: types.GroupIssuedAPI},
				{ID: "g-unused", Name: "unused", Issued: types.GroupIssuedAPI},
			} {
				require.NoError(t, replicas.A.SaveGroup(ctx, acc.Id, "owner", g, true))
			}
			owner, err := replicas.A.Store.GetUserByUserID(ctx, store.LockingStrengthNone, "owner")
			require.NoError(t, err)
			owner.AutoGroups = []string{"g-staff"}
			require.NoError(t, replicas.A.Store.SaveUser(ctx, store.LockingStrengthUpdate, owner))
			peer, _, _, err := replicas.A.AddPeer(ctx, "", "owner", &nbpeer.Peer{
				Key: wgKeyA, Meta: nbpeer.PeerSystemMeta{Hostname: "laptop", GoOS: "linux"},
			})
			require.NoError(t, err)

			off, err := replicas.A.Store.GetAccountSettings(ctx, store.LockingStrengthNone, acc.Id)
			require.NoError(t, err)
			if off.Extra == nil {
				off.Extra = &types.ExtraSettings{}
			}
			off.GroupsPropagationEnabled = false
			require.NoError(t, replicas.A.Store.SaveAccountSettings(ctx, store.LockingStrengthUpdate, acc.Id, off))
			enable := off.Copy()
			enable.GroupsPropagationEnabled = true

			pause, holder := holdOnA(t, replicas, "account", func() error {
				_, err := replicas.A.UpdateAccountSettings(ctx, acc.Id, "owner", enable)
				return err
			})
			changed := make(chan error, 1)
			go func() { changed <- tc.change(ctx, replicas.B, acc.Id, peer.ID) }()
			require.NoError(t, requireBlockedThenResult(t, replicas.B.Store, changed, pause.release, holder))

			got, err := replicas.B.Store.GetAccountSettings(ctx, store.LockingStrengthNone, acc.Id)
			require.NoError(t, err)
			require.True(t, got.GroupsPropagationEnabled)
			tc.applied(t, replicas.B.Store, acc.Id, peer.ID)
		})
	}
}
