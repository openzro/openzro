package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/controlcenter"
)

// C6: the manager seam loads the account + assembles validatedPeers
// from the store exactly like the network-map path, then delegates to
// the exhaustively-unit-tested adapter. This e2e proves the wiring
// (account load, validated-peers assembly, focus mapping, error
// propagation) against a real store-backed manager.
func TestGetAccessGraph_ManagerSeam(t *testing.T) {
	manager, account, peer1, _, _ := setupNetworkMapTest(t)
	ctx := context.Background()

	g, err := manager.GetAccessGraph(ctx, account.Id, string(controlcenter.FocusPeer), peer1.ID)
	require.NoError(t, err)
	require.Equal(t, controlcenter.Focus{Type: controlcenter.FocusPeer, ID: peer1.ID}, g.Focus)

	var focusNode *controlcenter.Node
	for i := range g.Nodes {
		if g.Nodes[i].ID == peer1.ID {
			focusNode = &g.Nodes[i]
		}
	}
	require.NotNil(t, focusNode, "the focus peer must be a node")
	require.Equal(t, controlcenter.NodeFocus, focusNode.Kind)

	// group focus is also wired (use a real group id from the account).
	var anyGroupID string
	for id := range account.Groups {
		anyGroupID = id
		break
	}
	require.NotEmpty(t, anyGroupID)
	_, err = manager.GetAccessGraph(ctx, account.Id, string(controlcenter.FocusGroup), anyGroupID)
	require.NoError(t, err)

	// unknown focus type propagates the adapter error.
	_, err = manager.GetAccessGraph(ctx, account.Id, "bogus", peer1.ID)
	require.Error(t, err)
}
