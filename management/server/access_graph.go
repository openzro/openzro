package server

import (
	"context"

	"github.com/openzro/openzro/management/server/controlcenter"
)

// GetAccessGraph builds the read-only Control Center access graph for a
// focus node (ADR-0017 Phase 1). It loads the account and assembles
// the validated-peers set from the store the same way the network-map
// path does, then delegates to the controlcenter adapter — it makes no
// access decisions of its own. RBAC (admin-only) is enforced at the
// HTTP boundary, consistent with the flow-exports handler.
//
// Clean-room (BSD-3): wiring designed against openZro's own manager
// helpers and ADR-0017; no upstream NetBird management/ code consulted
// or ported.
func (am *DefaultAccountManager) GetAccessGraph(ctx context.Context, accountID, view, focusID string) (*controlcenter.GraphDTO, error) {
	account, err := am.Store.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	validatedPeers, err := am.GetValidatedPeers(ctx, accountID)
	if err != nil {
		return nil, err
	}

	return controlcenter.BuildGraph(ctx, account, controlcenter.Focus{
		Type: controlcenter.FocusType(view),
		ID:   focusID,
	}, validatedPeers)
}
