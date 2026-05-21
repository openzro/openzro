// Package activity_exporters exposes the per-account CRUD API for
// audit log streamers. Mirrors mdm_providers — same encrypted-at-rest
// envelope, same write-only secret semantics, same admin-only gate.
//
// Scope: every request is implicitly scoped to the caller's account
// via the JWT (no path account ID parameter). The Store enforces the
// same scoping on Delete to defend against ID-guessing across tenants.
package activity_exporters

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/activity/exporter"
	exporters "github.com/openzro/openzro/management/server/activity_exporters"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
)

// Handler holds the persistent store and the live Manager. Saves
// and deletes call Manager.Refresh so the new configuration takes
// effect without a process restart.
type Handler struct {
	permissions permissions.Manager
	store       *exporters.Store
	manager     *exporters.Manager
}

func AddEndpoints(perms permissions.Manager, store *exporters.Store, manager *exporters.Manager, router *mux.Router) {
	if store == nil || manager == nil {
		return
	}
	h := &Handler{permissions: perms, store: store, manager: manager}
	router.HandleFunc("/admin/activity-exporters", h.list).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/admin/activity-exporters", h.create).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/admin/activity-exporters/{id}", h.get).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/admin/activity-exporters/{id}", h.update).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/admin/activity-exporters/{id}", h.delete).Methods(http.MethodDelete, http.MethodOptions)
	// Validate-template is a small POST endpoint that just runs
	// exporter.ValidateTemplate against the body and returns the
	// rendered output for a sample event. Lets the dashboard preview
	// what a receiver will see before saving.
	router.HandleFunc("/admin/activity-exporters/validate-template", h.validateTemplate).Methods(http.MethodPost, http.MethodOptions)
}

type requestBody struct {
	Name     string                       `json:"name"`
	Type     exporters.ExporterType       `json:"type"`
	Enabled  *bool                        `json:"enabled,omitempty"`
	Template string                       `json:"template,omitempty"`
	HTTP     *exporters.HTTPDestConfig    `json:"http,omitempty"`
	Datadog  *exporters.DatadogDestConfig `json:"datadog,omitempty"`
	Elastic  *exporters.ElasticDestConfig `json:"elastic,omitempty"`
}

type responseBody struct {
	ID        uint64                 `json:"id"`
	Name      string                 `json:"name"`
	Type      exporters.ExporterType `json:"type"`
	Enabled   bool                   `json:"enabled"`
	Template  string                 `json:"template,omitempty"`
	Config    json.RawMessage        `json:"config,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

func toResponse(row *exporters.ActivityExporter) responseBody {
	return responseBody{
		ID:        row.ID,
		Name:      row.Name,
		Type:      row.Type,
		Enabled:   row.Enabled,
		Template:  row.Template,
		Config:    row.PublicConfig,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func (h *Handler) auth(r *http.Request) (string, error) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		return "", err
	}
	allowed, err := h.permissions.ValidateUserPermissions(
		r.Context(), userAuth.AccountId, userAuth.UserId,
		modules.Settings, operations.Update)
	if err != nil {
		return "", status.NewPermissionValidationError(err)
	}
	if !allowed {
		return "", status.NewPermissionDeniedError()
	}
	return userAuth.AccountId, nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	accountID, err := h.auth(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	rows, err := h.store.List(r.Context(), accountID)
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
	accountID, err := h.auth(r)
	if err != nil {
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
	if row.AccountID != accountID {
		// Don't leak existence across tenants.
		util.WriteErrorResponse("not found", http.StatusNotFound, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toResponse(row))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	accountID, err := h.auth(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		util.WriteErrorResponse("invalid JSON body", http.StatusBadRequest, w)
		return
	}
	row, err := h.store.Save(r.Context(), bodyToInput(body, accountID, 0))
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
	accountID, err := h.auth(r)
	if err != nil {
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
	row, err := h.store.Save(r.Context(), bodyToInput(body, accountID, id))
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
	accountID, err := h.auth(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}
	if err := h.store.Delete(r.Context(), accountID, id); err != nil {
		writeNotFoundOrErr(w, r, err)
		return
	}
	if err := h.manager.Refresh(r.Context()); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) validateTemplate(w http.ResponseWriter, r *http.Request) {
	if _, err := h.auth(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	var body struct {
		Template string `json:"template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		util.WriteErrorResponse("invalid JSON body", http.StatusBadRequest, w)
		return
	}
	if err := exporter.ValidateTemplate(body.Template); err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, map[string]any{"ok": true})
}

func bodyToInput(b requestBody, accountID string, id uint64) exporters.SaveInput {
	enabled := true
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	return exporters.SaveInput{
		ID:        id,
		AccountID: accountID,
		Name:      b.Name,
		Type:      b.Type,
		Enabled:   enabled,
		Template:  b.Template,
		HTTP:      b.HTTP,
		Datadog:   b.Datadog,
		Elastic:   b.Elastic,
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
	if errors.Is(err, exporters.ErrNotFound) {
		util.WriteErrorResponse("not found", http.StatusNotFound, w)
		return
	}
	util.WriteError(r.Context(), err, w)
}
