// Package control_center exposes the admin-only, read-only Control
// Center access-graph API (openZro #39, ADR-0017 Phase 1):
//
//	GET /api/control-center/{view}/{id}   view ∈ {peer, user, group, network}
//
// The graph is derived server-side from the enforcement engine
// (never re-derived client-side). The endpoint is gated to full
// account admins — RBAC tightened from the ADR's "Admin /
// Network-Admin" to admin-only (modules.Settings + operations.Update,
// same posture as the flow-exports handler) per owner decision
// 2026-05-17: the access graph is a sensitive audit surface.
package control_center

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/controlcenter"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
)

// Handler is a thin adapter: it authenticates, enforces admin RBAC,
// validates the view, and delegates to the manager. It makes no
// access decisions of its own.
type Handler struct {
	accountManager account.Manager
	permissions    permissions.Manager
}

func AddEndpoints(accountManager account.Manager, permissionsManager permissions.Manager, router *mux.Router) {
	h := &Handler{accountManager: accountManager, permissions: permissionsManager}
	router.HandleFunc("/control-center/{view}/{id}", h.getGraph).Methods(http.MethodGet, http.MethodOptions)
}

// requireAdmin blocks anyone without Update on the Settings module —
// the same admin gate the flow-exports handler uses. The access graph
// exposes the whole tenant's reachability and is admin-only.
func (h *Handler) requireAdmin(r *http.Request, accountID, userID string) error {
	allowed, err := h.permissions.ValidateUserPermissions(
		r.Context(), accountID, userID, modules.Settings, operations.Update)
	if err != nil {
		return status.NewPermissionValidationError(err)
	}
	if !allowed {
		return status.NewPermissionDeniedError()
	}
	return nil
}

func (h *Handler) getGraph(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if err := h.requireAdmin(r, userAuth.AccountId, userAuth.UserId); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	vars := mux.Vars(r)
	view := vars["view"]
	switch controlcenter.FocusType(view) {
	case controlcenter.FocusPeer, controlcenter.FocusUser,
		controlcenter.FocusGroup, controlcenter.FocusNetwork:
	default:
		util.WriteErrorResponse(
			"unsupported view: must be 'peer', 'user', 'group' or 'network'",
			http.StatusBadRequest, w)
		return
	}

	// accountID is the caller's authenticated account (NOT a path
	// param), so a tenant can only ever query its own graph. A focus
	// id outside that account simply does not resolve.
	graph, err := h.accountManager.GetAccessGraph(r.Context(), userAuth.AccountId, view, vars["id"])
	if err != nil {
		switch {
		case errors.Is(err, controlcenter.ErrFocusNotFound):
			util.WriteErrorResponse(err.Error(), http.StatusNotFound, w)
		case errors.Is(err, controlcenter.ErrUnsupportedFocus):
			util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		default:
			// status errors map themselves; anything else is a real
			// failure (500), NOT silently a 404 (Finding 5).
			util.WriteError(r.Context(), err, w)
		}
		return
	}

	util.WriteJSONObject(r.Context(), w, graph)
}
