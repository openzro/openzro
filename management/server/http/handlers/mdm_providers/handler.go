// Package mdm_providers exposes the admin-only CRUD API for MDM/EDR
// vendor credentials (Intune, SentinelOne, Huntress, CrowdStrike).
// Mirrors the shape of flow_exports/handler.go — same encrypted-at-rest
// envelope, same write-only semantics for credentials, same admin-only
// gate.
package mdm_providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/mdm"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
)

// Handler holds the persistent store and the live Manager. Saves
// trigger Manager.Refresh so the new configuration takes effect
// without a process restart.
type Handler struct {
	permissions permissions.Manager
	store       *mdm.Store
	manager     *mdm.Manager
}

func AddEndpoints(perms permissions.Manager, store *mdm.Store, manager *mdm.Manager, router *mux.Router) {
	if store == nil || manager == nil {
		return
	}
	h := &Handler{permissions: perms, store: store, manager: manager}
	router.HandleFunc("/admin/mdm-providers", h.list).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/admin/mdm-providers", h.create).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/admin/mdm-providers/{id}", h.get).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/admin/mdm-providers/{id}", h.update).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/admin/mdm-providers/{id}", h.delete).Methods(http.MethodDelete, http.MethodOptions)
}

type requestBody struct {
	Name    string           `json:"name"`
	Type    mdm.ProviderType `json:"type"`
	Enabled *bool            `json:"enabled,omitempty"`
	// RefreshIntervalMinutes pins how often the cache for this
	// provider expires and the background worker refreshes it.
	// Validated 1-60 in mdm.SaveInput.Validate; 0 / omitted means
	// "use server default" (5 minutes).
	RefreshIntervalMinutes uint16                 `json:"refresh_interval_minutes,omitempty"`
	Intune                 *mdm.IntuneConfig      `json:"intune,omitempty"`
	SentinelOne            *mdm.SentinelOneConfig `json:"sentinelone,omitempty"`
	Huntress               *mdm.HuntressConfig    `json:"huntress,omitempty"`
	CrowdStrike            *mdm.CrowdStrikeConfig `json:"crowdstrike,omitempty"`
}

type responseBody struct {
	ID                     uint64           `json:"id"`
	Name                   string           `json:"name"`
	Type                   mdm.ProviderType `json:"type"`
	Enabled                bool             `json:"enabled"`
	RefreshIntervalMinutes uint16           `json:"refresh_interval_minutes"`
	Config                 json.RawMessage  `json:"config,omitempty"`
	CreatedAt              time.Time        `json:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at"`
}

func toResponse(row *mdm.ProviderRow) responseBody {
	return responseBody{
		ID:                     row.ID,
		Name:                   row.Name,
		Type:                   row.Type,
		Enabled:                row.Enabled,
		RefreshIntervalMinutes: row.RefreshIntervalMinutes,
		Config:                 row.PublicConfig,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
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
	if err := h.manager.Refresh(r.Context()); err != nil {
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
	if err := h.manager.Refresh(r.Context()); err != nil {
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
	if err := h.manager.Refresh(r.Context()); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func bodyToInput(b requestBody, id uint64) mdm.SaveInput {
	enabled := true
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	return mdm.SaveInput{
		ID:                     id,
		Name:                   b.Name,
		Type:                   b.Type,
		Enabled:                enabled,
		RefreshIntervalMinutes: b.RefreshIntervalMinutes,
		Intune:                 b.Intune,
		SentinelOne:            b.SentinelOne,
		Huntress:               b.Huntress,
		CrowdStrike:            b.CrowdStrike,
	}
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
	if errors.Is(err, mdm.ErrNotFound) {
		util.WriteErrorResponse("not found", http.StatusNotFound, w)
		return
	}
	util.WriteError(r.Context(), err, w)
}
