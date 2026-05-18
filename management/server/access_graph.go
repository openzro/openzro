package server

import (
	"context"

	"github.com/openzro/openzro/management/server/controlcenter"
)

// GetAccessGraph builds the read-only Control Center v2 topology for a
// focus node. It loads the account and delegates to the controlcenter
// adapter — it makes no access decisions of its own. v2 is a
// policy-topology projection (ADR-0017 2026-05-18c), so it does NOT
// compute a validated-peers set: posture is evaluated inside the
// projection; live peer validation is intentionally not a gate.
// RBAC (admin-only) is enforced at the HTTP boundary, consistent with
// the flow-exports handler.
//
// Clean-room (BSD-3): wiring designed against openZro's own manager
// helpers and ADR-0017; no upstream NetBird management/ code consulted
// or ported.
func (am *DefaultAccountManager) GetAccessGraph(ctx context.Context, accountID, view, focusID string) (*controlcenter.GraphDTO, error) {
	account, err := am.Store.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	return controlcenter.BuildGraph(ctx, account, controlcenter.Focus{
		Type: controlcenter.FocusType(view),
		ID:   focusID,
	})
}
