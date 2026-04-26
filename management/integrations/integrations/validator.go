package integrations

import (
	"context"

	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/activity"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/types"
)

// integratedValidatorStub satisfies integrated_validator.IntegratedValidator
// with permissive defaults: every peer is valid, no extra settings are
// rejected, every peer ID in the input list ends up in the validated set.
//
// This matches the upstream stub semantics: openzro accepts whatever
// the management server already approves; richer policy lives in
// extensions operators wire themselves.
type integratedValidatorStub struct{}

// NewIntegratedValidator constructs the stub. Returns the concrete type
// (not the interface) to mirror the upstream signature so call sites
// that take *IntegratedValidatorImpl still work; the type is exported
// for that reason as IntegratedValidatorImpl below.
func NewIntegratedValidator(_ context.Context, _ activity.Store) (*IntegratedValidatorImpl, error) {
	return &IntegratedValidatorImpl{}, nil
}

// IntegratedValidatorImpl is the public name for the stub so call sites
// that declare it explicitly keep compiling without changes.
type IntegratedValidatorImpl = integratedValidatorStub

func (integratedValidatorStub) ValidateExtraSettings(_ context.Context, _ *types.ExtraSettings, _ *types.ExtraSettings, _ map[string]*nbpeer.Peer, _ string, _ string) error {
	return nil
}

func (integratedValidatorStub) ValidatePeer(_ context.Context, update *nbpeer.Peer, _ *nbpeer.Peer, _ string, _ string, _ string, _ []string, _ *types.ExtraSettings) (*nbpeer.Peer, bool, error) {
	return update, false, nil
}

func (integratedValidatorStub) PreparePeer(_ context.Context, _ string, peer *nbpeer.Peer, _ []string, _ *types.ExtraSettings) *nbpeer.Peer {
	return peer.Copy()
}

func (integratedValidatorStub) IsNotValidPeer(_ context.Context, _ string, _ *nbpeer.Peer, _ []string, _ *types.ExtraSettings) (bool, bool, error) {
	return false, false, nil
}

func (integratedValidatorStub) GetValidatedPeers(_ context.Context, _ string, _ []*types.Group, peers []*nbpeer.Peer, _ *types.ExtraSettings) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(peers))
	for _, p := range peers {
		out[p.ID] = struct{}{}
	}
	return out, nil
}

func (integratedValidatorStub) PeerDeleted(_ context.Context, _, _ string, _ *types.ExtraSettings) error {
	return nil
}

func (integratedValidatorStub) SetPeerInvalidationListener(_ func(accountID string)) {
	// no-op
}

func (integratedValidatorStub) Stop(_ context.Context) {
	// no-op
}

func (integratedValidatorStub) ValidateFlowResponse(_ context.Context, _ string, flowResponse *proto.PKCEAuthorizationFlow) *proto.PKCEAuthorizationFlow {
	return flowResponse
}
