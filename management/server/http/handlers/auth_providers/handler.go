// Package auth_providers exposes the admin-only CRUD API for
// federated identity providers (the Dex connectors visible at
// /dex/auth as "Sign in with X" buttons).
//
// This handler is a THIN PROXY over Dex's gRPC management API
// — see management/server/dex_proxy. The dashboard's Settings
// → Authentication Providers tab POSTs / GETs / PUTs / DELETEs
// here; we forward into Dex and Dex persists to its storage
// backend (sqlite/postgres). No parallel openZro storage, no
// custom encryption: Dex owns the source of truth.
//
// See ADR-0006.
package auth_providers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/dex_proxy"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
)

// Handler holds the live gRPC client to Dex. nil = feature off
// (Dex isn't wired in this deployment); routes return 503 in
// that case so the dashboard can render an "IdP not available"
// state.
type Handler struct {
	permissions permissions.Manager
	dex         *dex_proxy.Client
}

// AddEndpoints registers /admin/auth-providers under router. dex
// may be nil — the routes are still registered so the dashboard
// gets a clean 503 with a JSON error rather than a 404.
func AddEndpoints(perms permissions.Manager, dex *dex_proxy.Client, router *mux.Router) {
	h := &Handler{permissions: perms, dex: dex}
	router.HandleFunc("/admin/auth-providers", h.list).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/admin/auth-providers", h.create).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/admin/auth-providers/{id}", h.update).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/admin/auth-providers/{id}", h.delete).Methods(http.MethodDelete, http.MethodOptions)
}

// requestBody is the JSON the dashboard posts. Type carries the
// connector kind ("google" / "github" / "microsoft" / "oidc" /
// "ldap" / etc.); Config is the per-type JSON blob (clientID +
// clientSecret + redirectURI for OAuth-style; bindDN/userSearch
// for LDAP). The dashboard form composes Config from a per-type
// component so the operator never types raw JSON.
type requestBody struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

type responseBody struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config,omitempty"`
}

func toResponse(c dex_proxy.Connector) responseBody {
	r := responseBody{
		ID:   c.ID,
		Type: c.Type,
		Name: c.Name,
	}
	if len(c.Config) > 0 {
		r.Config = json.RawMessage(c.Config)
	}
	return r
}

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

func (h *Handler) auth(r *http.Request) (string, string, error) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		return "", "", err
	}
	if err := h.requireAdmin(r, userAuth.AccountId, userAuth.UserId); err != nil {
		return "", "", err
	}
	return userAuth.AccountId, userAuth.UserId, nil
}

func (h *Handler) requireDex(w http.ResponseWriter, r *http.Request) bool {
	if h.dex == nil {
		util.WriteErrorResponse(
			"identity provider service not configured (Dex gRPC unavailable)",
			http.StatusServiceUnavailable, w)
		return false
	}
	return true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if !h.requireDex(w, r) {
		return
	}
	rows, err := h.dex.ListConnectors(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	out := make([]responseBody, len(rows))
	for i := range rows {
		out[i] = toResponse(rows[i])
	}
	util.WriteJSONObject(r.Context(), w, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if !h.requireDex(w, r) {
		return
	}
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		util.WriteErrorResponse("invalid JSON body", http.StatusBadRequest, w)
		return
	}
	if msg := validateBody(body, true); msg != "" {
		util.WriteErrorResponse(msg, http.StatusBadRequest, w)
		return
	}
	in := dex_proxy.Connector{
		ID:     body.ID,
		Type:   body.Type,
		Name:   body.Name,
		Config: []byte(body.Config),
	}
	if err := h.dex.CreateConnector(r.Context(), in); err != nil {
		writeMappedErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	util.WriteJSONObject(r.Context(), w, toResponse(in))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if !h.requireDex(w, r) {
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		util.WriteErrorResponse("missing id", http.StatusBadRequest, w)
		return
	}
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		util.WriteErrorResponse("invalid JSON body", http.StatusBadRequest, w)
		return
	}
	body.ID = id
	if msg := validateBody(body, false); msg != "" {
		util.WriteErrorResponse(msg, http.StatusBadRequest, w)
		return
	}
	in := dex_proxy.Connector{
		ID:     id,
		Type:   body.Type,
		Name:   body.Name,
		Config: []byte(body.Config),
	}
	if err := h.dex.UpdateConnector(r.Context(), in); err != nil {
		writeMappedErr(w, r, err)
		return
	}
	util.WriteJSONObject(r.Context(), w, toResponse(in))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if !h.requireDex(w, r) {
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		util.WriteErrorResponse("missing id", http.StatusBadRequest, w)
		return
	}
	if err := h.dex.DeleteConnector(r.Context(), id); err != nil {
		writeMappedErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateBody(b requestBody, requireID bool) string {
	if requireID && b.ID == "" {
		return "id is required"
	}
	if b.Type == "" {
		return "type is required"
	}
	// `len == 0` catches a missing field; `"null"` catches an
	// explicit JSON null (RawMessage preserves the literal four
	// bytes). Either way the operator left config blank.
	if len(b.Config) == 0 || string(b.Config) == "null" {
		return "config is required"
	}
	// Outer JSON Decode already validated the wire shape, so
	// b.Config is a syntactically-valid JSON value at this
	// point. Dex rejects per-connector-type misconfiguration
	// itself; we don't second-guess it here.
	return ""
}

func writeMappedErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, dex_proxy.ErrAlreadyExists):
		util.WriteErrorResponse("connector with this id already exists", http.StatusConflict, w)
	case errors.Is(err, dex_proxy.ErrNotFound):
		util.WriteErrorResponse("connector not found", http.StatusNotFound, w)
	default:
		util.WriteError(r.Context(), err, w)
	}
}
