package integrations

import (
	"context"

	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/activity"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/types"
)

// integratedValidatorStub satisfies integrated_validator.IntegratedValidator
// and implements the openzro peer-approval policy.
//
// Policy (clean-room from public docs and the surface that already
// existed in the BSD-3 portion of the codebase):
//
//   - When ExtraSettings.PeerApprovalEnabled is false (or settings
//     are nil), every peer is considered valid — no gating.
//   - When PeerApprovalEnabled is true, peers must have
//     Status.RequiresApproval == false to be valid. New peers default
//     to RequiresApproval=true and stay pending until an admin
//     approves them via the management API.
//   - Membership in any group listed in
//     ExtraSettings.IntegratedValidatorGroups exempts a peer from the
//     approval requirement (useful for posture-checked or
//     service-account groups). Exempt peers are auto-approved on
//     prepare and reported valid on every check.
type integratedValidatorStub struct {
	invalidate func(accountID string)
}

func NewIntegratedValidator(_ context.Context, _ activity.Store) (*IntegratedValidatorImpl, error) {
	return &IntegratedValidatorImpl{}, nil
}

type IntegratedValidatorImpl = integratedValidatorStub

// approvalEnabled returns true iff the operator has opted into
// peer approval gating. Nil settings are treated as gating off — that
// matches the implicit default and keeps backward-compat with accounts
// that never set the flag.
func approvalEnabled(extra *types.ExtraSettings) bool {
	return extra != nil && extra.PeerApprovalEnabled
}

// peerExempt returns true when the peer belongs to a group that is
// listed in IntegratedValidatorGroups. The peerGroupIDs slice is the
// resolved set for the peer at evaluation time; the matching is by
// group ID equality (not name) because group names are mutable.
func peerExempt(peerGroupIDs []string, extra *types.ExtraSettings) bool {
	if extra == nil || len(extra.IntegratedValidatorGroups) == 0 || len(peerGroupIDs) == 0 {
		return false
	}
	exempt := make(map[string]struct{}, len(extra.IntegratedValidatorGroups))
	for _, id := range extra.IntegratedValidatorGroups {
		exempt[id] = struct{}{}
	}
	for _, id := range peerGroupIDs {
		if _, ok := exempt[id]; ok {
			return true
		}
	}
	return false
}

// peersByGroup builds an exempt-peer set from groups when the call
// site only has *types.Group, not pre-resolved peer→group memberships.
func exemptPeersFromGroups(groups []*types.Group, extra *types.ExtraSettings) map[string]struct{} {
	if extra == nil || len(extra.IntegratedValidatorGroups) == 0 || len(groups) == 0 {
		return nil
	}
	want := make(map[string]struct{}, len(extra.IntegratedValidatorGroups))
	for _, id := range extra.IntegratedValidatorGroups {
		want[id] = struct{}{}
	}
	exempt := make(map[string]struct{})
	for _, g := range groups {
		if _, ok := want[g.ID]; !ok {
			continue
		}
		for _, peerID := range g.Peers {
			exempt[peerID] = struct{}{}
		}
	}
	return exempt
}

func (integratedValidatorStub) ValidateExtraSettings(_ context.Context, _ *types.ExtraSettings, _ *types.ExtraSettings, _ map[string]*nbpeer.Peer, _ string, _ string) error {
	// Settings transitions are accepted unconditionally; recomputing
	// per-peer status when the toggle flips is the responsibility of
	// the caller (it has the SavePeer transaction).
	return nil
}

// requiresGating returns whether the peer would be subject to approval
// gating given the current settings and the peer's group membership.
// Independent of the peer's stored RequiresApproval flag.
func requiresGating(peerGroupIDs []string, extra *types.ExtraSettings) bool {
	return approvalEnabled(extra) && !peerExempt(peerGroupIDs, extra)
}

// PreparePeer is called for newly registering peers. New peers under
// gating start pending; otherwise they are auto-approved.
func (integratedValidatorStub) PreparePeer(_ context.Context, _ string, peer *nbpeer.Peer, peersGroup []string, extra *types.ExtraSettings) *nbpeer.Peer {
	out := peer.Copy()
	if out.Status == nil {
		out.Status = &nbpeer.PeerStatus{}
	}
	out.Status.RequiresApproval = requiresGating(peersGroup, extra)
	return out
}

// ValidatePeer is called from UpdatePeer to reconcile an admin-driven
// update against current settings. The handler at the HTTP layer has
// already enforced "user has Update permission on Peers" before this
// runs, so an explicit RequiresApproval value in the update IS the
// admin's intent and must be honored.
//
// Behavior:
//   - Gating off (or peer exempt): RequiresApproval forced to false —
//     ungated peers can never be pending.
//   - Gating on, update.Status set: use the admin's value (this is how
//     "approve" / "revoke approval" actions land here).
//   - Gating on, update.Status nil: preserve the persisted state — the
//     caller didn't touch the approval field, so don't either.
func (integratedValidatorStub) ValidatePeer(_ context.Context, update *nbpeer.Peer, peer *nbpeer.Peer, _ string, _ string, _ string, peersGroup []string, extra *types.ExtraSettings) (*nbpeer.Peer, bool, error) {
	if update == nil {
		return update, false, nil
	}
	out := update.Copy()

	var current bool
	if peer != nil && peer.Status != nil {
		current = peer.Status.RequiresApproval
	}

	var desired bool
	switch {
	case !requiresGating(peersGroup, extra):
		desired = false
	case update.Status != nil:
		desired = update.Status.RequiresApproval
	default:
		desired = current
	}

	if out.Status == nil {
		out.Status = &nbpeer.PeerStatus{}
	}
	out.Status.RequiresApproval = desired
	return out, current != desired, nil
}

func (integratedValidatorStub) IsNotValidPeer(_ context.Context, _ string, peer *nbpeer.Peer, peersGroup []string, extra *types.ExtraSettings) (bool, bool, error) {
	if peer == nil {
		return false, false, nil
	}
	if !requiresGating(peersGroup, extra) {
		return false, false, nil
	}
	pending := peer.Status != nil && peer.Status.RequiresApproval
	return pending, false, nil
}

func (integratedValidatorStub) GetValidatedPeers(_ context.Context, _ string, groups []*types.Group, peers []*nbpeer.Peer, extra *types.ExtraSettings) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(peers))
	if !approvalEnabled(extra) {
		for _, p := range peers {
			out[p.ID] = struct{}{}
		}
		return out, nil
	}
	exempt := exemptPeersFromGroups(groups, extra)
	for _, p := range peers {
		if _, ok := exempt[p.ID]; ok {
			out[p.ID] = struct{}{}
			continue
		}
		if p.Status != nil && p.Status.RequiresApproval {
			continue
		}
		out[p.ID] = struct{}{}
	}
	return out, nil
}

func (integratedValidatorStub) PeerDeleted(_ context.Context, _, _ string, _ *types.ExtraSettings) error {
	return nil
}

func (s *integratedValidatorStub) SetPeerInvalidationListener(fn func(accountID string)) {
	s.invalidate = fn
}

func (integratedValidatorStub) Stop(_ context.Context) {
	// no-op
}

func (integratedValidatorStub) ValidateFlowResponse(_ context.Context, _ string, flowResponse *proto.PKCEAuthorizationFlow) *proto.PKCEAuthorizationFlow {
	return flowResponse
}
