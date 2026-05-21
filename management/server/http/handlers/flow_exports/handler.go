// Package flow_exports exposes the admin-only CRUD API for
// configuring flow event destinations at runtime.
//
// Endpoints (all require account owner/admin role):
//
//	GET    /api/admin/flow-exports          list configured destinations
//	POST   /api/admin/flow-exports          create a destination
//	GET    /api/admin/flow-exports/{id}     fetch a single destination
//	PUT    /api/admin/flow-exports/{id}     update a destination
//	DELETE /api/admin/flow-exports/{id}     remove a destination
//
// Credentials (Elastic API key, S3 secret key, HTTP Authorization
// headers) are write-only — POST/PUT accept them in the request body
// and they are encrypted before INSERT/UPDATE. GET responses NEVER
// echo them back; only the public projection (URL, bucket, auth_mode)
// goes on the wire. Operators who lose a credential issue a new PUT
// with the new value.
package flow_exports

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	nbcontext "github.com/openzro/openzro/management/server/context"
	flowExports "github.com/openzro/openzro/management/server/flow_exports"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
)

// Handler holds the runtime config store and the manager that
// applies changes to the live FlowService. The handler is a thin
// adapter — every interesting decision lives in the manager.
type Handler struct {
	permissions permissions.Manager
	store       *flowExports.Store
	manager     *flowExports.Manager
}

func AddEndpoints(
	permissionsManager permissions.Manager,
	store *flowExports.Store,
	manager *flowExports.Manager,
	router *mux.Router,
) {
	if store == nil || manager == nil {
		return
	}
	h := &Handler{permissions: permissionsManager, store: store, manager: manager}
	router.HandleFunc("/admin/flow-exports", h.list).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/admin/flow-exports", h.create).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/admin/flow-exports/{id}", h.get).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/admin/flow-exports/{id}", h.update).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/admin/flow-exports/{id}", h.delete).Methods(http.MethodDelete, http.MethodOptions)
}

// requestBody is the wire shape for create/update. Type determines
// which of the per-destination Config blocks is consulted.
type requestBody struct {
	Name    string                         `json:"name"`
	Type    flowExports.ExportType         `json:"type"`
	Enabled *bool                          `json:"enabled,omitempty"`
	Elastic *flowExports.ElasticDestConfig `json:"elastic,omitempty"`
	S3      *flowExports.S3DestConfig      `json:"s3,omitempty"`
	HTTP    *flowExports.HTTPDestConfig    `json:"http,omitempty"`
	Datadog *flowExports.DatadogDestConfig `json:"datadog,omitempty"`
	GCS     *flowExports.GCSDestConfig     `json:"gcs,omitempty"`
}

// responseBody is the wire shape returned by every read endpoint and
// echoed back on create/update. Sensitive fields are stripped.
type responseBody struct {
	ID        uint64                 `json:"id"`
	Name      string                 `json:"name"`
	Type      flowExports.ExportType `json:"type"`
	Enabled   bool                   `json:"enabled"`
	Config    json.RawMessage        `json:"config,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

func toResponse(row *flowExports.FlowExport) responseBody {
	return responseBody{
		ID:        row.ID,
		Name:      row.Name,
		Type:      row.Type,
		Enabled:   row.Enabled,
		Config:    row.PublicConfig,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// requireAdmin blocks anyone without Update on the Settings module.
// We piggyback on Settings rather than introducing a new permission;
// flow exports are an account-level configuration knob.
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

func (h *Handler) authAndAdmin(r *http.Request) (string, string, error) {
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
	if _, _, err := h.authAndAdmin(r); err != nil {
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
	if _, _, err := h.authAndAdmin(r); err != nil {
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
	if _, _, err := h.authAndAdmin(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		util.WriteErrorResponse("invalid JSON body", http.StatusBadRequest, w)
		return
	}
	in := bodyToSaveInput(body, 0)
	row, err := h.store.Save(r.Context(), in)
	if err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}
	if err := h.manager.ApplyAll(r.Context()); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	w.WriteHeader(http.StatusCreated)
	util.WriteJSONObject(r.Context(), w, toResponse(row))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.authAndAdmin(r); err != nil {
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
	in := bodyToSaveInput(body, id)
	row, err := h.store.Save(r.Context(), in)
	if err != nil {
		writeNotFoundOrErr(w, r, err)
		return
	}
	if err := h.manager.ApplyAll(r.Context()); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toResponse(row))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.authAndAdmin(r); err != nil {
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
	if err := h.manager.ApplyAll(r.Context()); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func bodyToSaveInput(b requestBody, id uint64) flowExports.SaveInput {
	enabled := true
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	return flowExports.SaveInput{
		ID:      id,
		Name:    b.Name,
		Type:    b.Type,
		Enabled: enabled,
		Elastic: b.Elastic,
		S3:      b.S3,
		HTTP:    b.HTTP,
		Datadog: b.Datadog,
		GCS:     b.GCS,
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
	if errors.Is(err, flowExports.ErrNotFound) {
		util.WriteErrorResponse("not found", http.StatusNotFound, w)
		return
	}
	util.WriteError(r.Context(), err, w)
}
