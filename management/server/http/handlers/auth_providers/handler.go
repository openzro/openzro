// Package auth_providers exposes the admin-only CRUD API for
// configured OIDC IdPs (the AuthenticationProvider table).
// Mirrors the shape of mdm_providers/handler.go — same encrypted-
// at-rest envelope, same write-only client_secret semantics, same
// admin-only gate (modules.Settings + operations.Update).
//
// Mutations trigger providers.Manager.Refresh so the new
// configuration takes effect for /login and the multi-issuer
// validator without a process restart.
package auth_providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/auth/providers"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
)

// Handler holds the persistent store and the live Manager. Each
// successful mutation triggers Manager.Refresh so the new
// configuration goes live without a server restart.
type Handler struct {
	permissions permissions.Manager
	store       *providers.Store
	manager     *providers.Manager
}

// AddEndpoints registers the CRUD endpoints under /admin/auth-
// providers. Mounted on the same /api router as mdm_providers
// so the same auth middleware enforces the admin gate.
func AddEndpoints(perms permissions.Manager, store *providers.Store, manager *providers.Manager, router *mux.Router) {
	if store == nil || manager == nil {
		return
	}
	h := &Handler{permissions: perms, store: store, manager: manager}
	router.HandleFunc("/admin/auth-providers", h.list).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/admin/auth-providers", h.create).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/admin/auth-providers/{id}", h.get).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/admin/auth-providers/{id}", h.update).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/admin/auth-providers/{id}", h.delete).Methods(http.MethodDelete, http.MethodOptions)
}

// requestBody is the wire shape POSTed by the dashboard. Enabled
// is *bool so an unset value defaults to true on create, but a
// `false` from the wire is honoured (the GORM zero-value gotcha
// fixed in providers/model.go's schema).
type requestBody struct {
	Name            string                 `json:"name"`
	Type            providers.ProviderType `json:"type"`
	Enabled         *bool                  `json:"enabled,omitempty"`
	Config          providers.Config       `json:"config"`
	BrandLabel      string                 `json:"brand_label,omitempty"`
	BrandLogoURL    string                 `json:"brand_logo_url,omitempty"`
	EmailDomainHint string                 `json:"email_domain_hint,omitempty"`
}

// responseBody projects the row to the wire shape. Config carries
// the PublicConfig blob (no client_secret); HasClientSecret tells
// the dashboard whether to render "secret already set" vs "set a
// secret".
type responseBody struct {
	ID              uint64                 `json:"id"`
	Name            string                 `json:"name"`
	Type            providers.ProviderType `json:"type"`
	Enabled         bool                   `json:"enabled"`
	BrandLabel      string                 `json:"brand_label"`
	BrandLogoURL    string                 `json:"brand_logo_url,omitempty"`
	EmailDomainHint string                 `json:"email_domain_hint,omitempty"`
	Config          json.RawMessage        `json:"config,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

func toResponse(row *providers.AuthenticationProvider) responseBody {
	return responseBody{
		ID:              row.ID,
		Name:            row.Name,
		Type:            row.Type,
		Enabled:         row.Enabled,
		BrandLabel:      row.BrandLabel,
		BrandLogoURL:    row.BrandLogoURL,
		EmailDomainHint: row.EmailDomainHint,
		Config:          row.PublicConfig,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func bodyToInput(b requestBody, id uint64) providers.SaveInput {
	enabled := true
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	return providers.SaveInput{
		ID:              id,
		Name:            b.Name,
		Type:            b.Type,
		Enabled:         enabled,
		Config:          b.Config,
		BrandLabel:      b.BrandLabel,
		BrandLogoURL:    b.BrandLogoURL,
		EmailDomainHint: b.EmailDomainHint,
	}
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

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	rows, err := h.store.List(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	out := make([]responseBody, len(rows))
	for i := range rows {
		out[i] = toResponse(&rows[i])
	}
	util.WriteJSONObject(r.Context(), w, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}
	row, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeNotFoundOrErr(w, r, err)
		return
	}
	util.WriteJSONObject(r.Context(), w, toResponse(row))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		util.WriteErrorResponse("invalid JSON body", http.StatusBadRequest, w)
		return
	}
	row, err := h.store.Save(r.Context(), bodyToInput(body, 0))
	if err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}
	if _, err := h.manager.Refresh(r.Context()); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	w.WriteHeader(http.StatusCreated)
	util.WriteJSONObject(r.Context(), w, toResponse(row))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		util.WriteErrorResponse("invalid JSON body", http.StatusBadRequest, w)
		return
	}
	row, err := h.store.Save(r.Context(), bodyToInput(body, id))
	if err != nil {
		writeNotFoundOrErr(w, r, err)
		return
	}
	if _, err := h.manager.Refresh(r.Context()); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toResponse(row))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		writeNotFoundOrErr(w, r, err)
		return
	}
	if _, err := h.manager.Refresh(r.Context()); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(r *http.Request) (uint64, error) {
	raw := mux.Vars(r)["id"]
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}

func writeNotFoundOrErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, providers.ErrNotFound) {
		util.WriteErrorResponse("not found", http.StatusNotFound, w)
		return
	}
	util.WriteError(r.Context(), err, w)
}
