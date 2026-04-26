package integrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/types"
)

// These tests are the regression line for the peer-approval security
// false-positive: ExtraSettings.PeerApprovalEnabled must actually gate
// peers, not be a no-op cosmetic flag. They are the contract for the
// clean-room re-implementation in validator.go.

const (
	tAccount = "acct-1"
	tDomain  = "openzro.dev"
)

func newPeer(id string, requiresApproval bool, groupIDs ...string) *nbpeer.Peer {
	return &nbpeer.Peer{
		ID:     id,
		Status: &nbpeer.PeerStatus{RequiresApproval: requiresApproval},
	}
}

func newApprovalEnabledSettings(exemptGroups ...string) *types.ExtraSettings {
	return &types.ExtraSettings{
		PeerApprovalEnabled:       true,
		IntegratedValidatorGroups: exemptGroups,
	}
}

func TestGetValidatedPeers_ApprovalDisabled_AllPeersValid(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	pending := newPeer("p1", true)
	ok := newPeer("p2", false)

	got, err := v.GetValidatedPeers(context.Background(), tAccount, nil,
		[]*nbpeer.Peer{pending, ok}, &types.ExtraSettings{PeerApprovalEnabled: false})
	assert.NoError(t, err)

	// Flag off: validator must not gate. Both peers in the set.
	assert.Contains(t, got, "p1")
	assert.Contains(t, got, "p2")
}

func TestGetValidatedPeers_ApprovalEnabled_PendingExcluded(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	pending := newPeer("p1", true)
	approved := newPeer("p2", false)

	got, err := v.GetValidatedPeers(context.Background(), tAccount, nil,
		[]*nbpeer.Peer{pending, approved}, newApprovalEnabledSettings())
	assert.NoError(t, err)

	// THIS is the security regression: a pending peer must not be
	// returned as validated when the toggle is on. The current stub
	// returns it — this assertion fails on main.
	assert.NotContains(t, got, "p1", "pending peer must be excluded when PeerApprovalEnabled=true")
	assert.Contains(t, got, "p2")
}

func TestGetValidatedPeers_ExemptGroupBypassesApproval(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	exemptGroup := &types.Group{ID: "g-exempt", Peers: []string{"p1"}}
	ungated := &types.Group{ID: "g-other", Peers: []string{"p2"}}

	pendingExempt := newPeer("p1", true)
	pendingNotExempt := newPeer("p2", true)

	got, err := v.GetValidatedPeers(context.Background(), tAccount,
		[]*types.Group{exemptGroup, ungated},
		[]*nbpeer.Peer{pendingExempt, pendingNotExempt},
		newApprovalEnabledSettings("g-exempt"))
	assert.NoError(t, err)

	// p1 is in an exempt group → should pass even though
	// RequiresApproval=true. p2 is not exempt → should be excluded.
	assert.Contains(t, got, "p1", "peer in IntegratedValidatorGroups must be exempt from approval")
	assert.NotContains(t, got, "p2")
}

func TestPreparePeer_ApprovalEnabled_NewPeerMarkedPending(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	in := &nbpeer.Peer{ID: "p1", Status: &nbpeer.PeerStatus{}}
	out := v.PreparePeer(context.Background(), tAccount, in, nil, newApprovalEnabledSettings())

	assert.True(t, out.Status.RequiresApproval, "new peer must be flagged pending when approval is on")
}

func TestPreparePeer_ApprovalDisabled_NewPeerApproved(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	in := &nbpeer.Peer{ID: "p1", Status: &nbpeer.PeerStatus{RequiresApproval: true}}
	out := v.PreparePeer(context.Background(), tAccount, in, nil,
		&types.ExtraSettings{PeerApprovalEnabled: false})

	assert.False(t, out.Status.RequiresApproval, "approval flag off must clear RequiresApproval")
}

func TestValidatePeer_AdminApprovesPendingPeer(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	current := newPeer("p1", true)
	update := &nbpeer.Peer{ID: "p1", Status: &nbpeer.PeerStatus{RequiresApproval: false}}

	out, changed, err := v.ValidatePeer(context.Background(), update, current, "u", "acct", "", nil, newApprovalEnabledSettings())
	assert.NoError(t, err)
	assert.False(t, out.Status.RequiresApproval, "admin's approve action must clear RequiresApproval")
	assert.True(t, changed, "approval transition must be reported as a change")
}

func TestValidatePeer_AdminRevokesApproval(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	current := newPeer("p1", false)
	update := &nbpeer.Peer{ID: "p1", Status: &nbpeer.PeerStatus{RequiresApproval: true}}

	out, changed, err := v.ValidatePeer(context.Background(), update, current, "u", "acct", "", nil, newApprovalEnabledSettings())
	assert.NoError(t, err)
	assert.True(t, out.Status.RequiresApproval, "admin's revoke action must set RequiresApproval")
	assert.True(t, changed)
}

func TestValidatePeer_NoStatusUpdate_PreservesCurrent(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	current := newPeer("p1", true)
	update := &nbpeer.Peer{ID: "p1", Name: "renamed"} // Status nil — admin only renamed

	out, changed, err := v.ValidatePeer(context.Background(), update, current, "u", "acct", "", nil, newApprovalEnabledSettings())
	assert.NoError(t, err)
	assert.True(t, out.Status.RequiresApproval, "unrelated update must preserve approval state")
	assert.False(t, changed)
}

func TestValidatePeer_GatingOff_AlwaysClears(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	current := newPeer("p1", true)
	update := &nbpeer.Peer{ID: "p1", Status: &nbpeer.PeerStatus{RequiresApproval: true}}

	out, changed, err := v.ValidatePeer(context.Background(), update, current, "u", "acct", "", nil,
		&types.ExtraSettings{PeerApprovalEnabled: false})
	assert.NoError(t, err)
	assert.False(t, out.Status.RequiresApproval, "gating off must force-clear RequiresApproval regardless of intent")
	assert.True(t, changed)
}

func TestIsNotValidPeer_PendingPeerReportedInvalid(t *testing.T) {
	v, err := NewIntegratedValidator(context.Background(), nil)
	assert.NoError(t, err)

	pending := newPeer("p1", true)
	invalid, _, err := v.IsNotValidPeer(context.Background(), tAccount, pending, nil, newApprovalEnabledSettings())
	assert.NoError(t, err)
	assert.True(t, invalid, "pending peer must be reported invalid")

	approved := newPeer("p2", false)
	invalid, _, err = v.IsNotValidPeer(context.Background(), tAccount, approved, nil, newApprovalEnabledSettings())
	assert.NoError(t, err)
	assert.False(t, invalid, "approved peer must be reported valid")
}
