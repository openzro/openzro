// Package admission_bypass exposes the per-peer Device Admission
// bypass CRUD API. Operators grant short-lived overrides on
// non-compliant peers (CEO laptop with a 24h reason while the
// device is being remediated); each grant + revoke + expiry emits
// a durable activity event so the auditor sees the full lifecycle.
//
// See ADR-0004 for the rationale.
package admission_bypass

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/admission"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
)

// EventEmitter mirrors the abstract dependency the expiry worker
// uses; see admission/expiry_worker.go. The handler emits granted /
// revoked events so all three lifecycle codes (granted, revoked,
// expired) share a single audit code-space.
type EventEmitter func(
	ctx context.Context,
	initiatorID, targetID, accountID string,
	activityCode activity.Activity,
	meta map[string]any,
)

// Handler holds the bypass store + audit emitter + permissions
// checker. Mounted under /api/peers/{peerId}/admission-bypass for
// the per-peer actions and /api/admin/admission-bypasses for the
// account-scoped list (auditor view).
type Handler struct {
	permissions    permissions.Manager
	store          *admission.Store
	accountManager account.Manager
	emit           EventEmitter
}

func AddEndpoints(perms permissions.Manager, s *admission.Store, accountManager account.Manager, emit EventEmitter, router *mux.Router) {
	if s == nil {
		return
	}
	h := &Handler{permissions: perms, store: s, accountManager: accountManager, emit: emit}
	router.HandleFunc("/peers/{peerId}/admission-bypass", h.grant).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/peers/{peerId}/admission-bypass", h.get).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/peers/{peerId}/admission-bypass", h.revoke).Methods(http.MethodDelete, http.MethodOptions)
	router.HandleFunc("/admin/admission-bypasses", h.list).Methods(http.MethodGet, http.MethodOptions)
}

// requestBody is what the dashboard POSTs to grant a bypass.
//
// ExpiresAtSeconds is "seconds from now"; the API normalises it to
// an absolute time before persisting. Operators usually want
// "24 hours from now" not "Jan 4 2026 14:32 UTC", and the relative
// form survives clock drift between the client and the management.
type requestBody struct {
	Reason           string `json:"reason"`
	ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
	ExpiresAtRFC3339 string `json:"expires_at,omitempty"`
}

type responseBody struct {
	ID          uint64    `json:"id"`
	PeerID      string    `json:"peer_id"`
	InitiatorID string    `json:"initiator_id"`
	Reason      string    `json:"reason"`
	GrantedAt   time.Time `json:"granted_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Active      bool      `json:"active"`
}

func toResponse(row *admission.PeerAdmissionBypass) responseBody {
	return responseBody{
		ID:          row.ID,
		PeerID:      row.PeerID,
		InitiatorID: row.InitiatorID,
		Reason:      row.Reason,
		GrantedAt:   row.GrantedAt,
		ExpiresAt:   row.ExpiresAt,
		Active:      row.IsActive(time.Now().UTC()),
	}
}

func (h *Handler) auth(r *http.Request) (string, string, error) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		return "", "", err
	}
	allowed, err := h.permissions.ValidateUserPermissions(
		r.Context(), userAuth.AccountId, userAuth.UserId,
		modules.Peers, operations.Update)
	if err != nil {
		return "", "", status.NewPermissionValidationError(err)
	}
	if !allowed {
		return "", "", status.NewPermissionDeniedError()
	}
	return userAuth.AccountId, userAuth.UserId, nil
}

// peerBelongsToAccount defends against ID-guessing across tenants.
// Without this, an authenticated user could grant a bypass on any
// peer ID they could enumerate. accountManager.GetPeer enforces the
// scoping for us — a peer in another account returns NotFound.
func (h *Handler) peerBelongsToAccount(r *http.Request, accountID, userID, peerID string) error {
	if _, err := h.accountManager.GetPeer(r.Context(), accountID, peerID, userID); err != nil {
		return err
	}
	return nil
}

func (h *Handler) grant(w http.ResponseWriter, r *http.Request) {
	accountID, userID, err := h.auth(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	peerID := mux.Vars(r)["peerId"]
	if peerID == "" {
		util.WriteErrorResponse("invalid peer id", http.StatusBadRequest, w)
		return
	}
	if err := h.peerBelongsToAccount(r, accountID, userID, peerID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		util.WriteErrorResponse("invalid JSON body", http.StatusBadRequest, w)
		return
	}

	expiresAt, err := resolveExpiry(body)
	if err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}

	row, err := h.store.Grant(r.Context(), admission.GrantInput{
		AccountID:   accountID,
		PeerID:      peerID,
		InitiatorID: userID,
		Reason:      body.Reason,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}

	if h.emit != nil {
		h.emit(r.Context(), userID, peerID, accountID, activity.PeerAdmissionBypassGranted, map[string]any{
			"reason":     row.Reason,
			"expires_at": row.ExpiresAt.Format(time.RFC3339),
		})
	}

	w.WriteHeader(http.StatusCreated)
	util.WriteJSONObject(r.Context(), w, toResponse(row))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	accountID, userID, err := h.auth(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	peerID := mux.Vars(r)["peerId"]
	if err := h.peerBelongsToAccount(r, accountID, userID, peerID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	_, row, err := h.store.IsActive(r.Context(), accountID, peerID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if row == nil {
		util.WriteErrorResponse("not found", http.StatusNotFound, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toResponse(row))
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	accountID, userID, err := h.auth(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	peerID := mux.Vars(r)["peerId"]
	if err := h.peerBelongsToAccount(r, accountID, userID, peerID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	row, err := h.store.Revoke(r.Context(), accountID, peerID)
	if err != nil {
		if errors.Is(err, admission.ErrNotFound) {
			util.WriteErrorResponse("not found", http.StatusNotFound, w)
			return
		}
		util.WriteError(r.Context(), err, w)
		return
	}
	if h.emit != nil {
		h.emit(r.Context(), userID, peerID, accountID, activity.PeerAdmissionBypassRevoked, map[string]any{
			"reason":              row.Reason,
			"granted_by":          row.InitiatorID,
			"granted_at":          row.GrantedAt.Format(time.RFC3339),
			"original_expires_at": row.ExpiresAt.Format(time.RFC3339),
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	accountID, _, err := h.auth(r)
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

// resolveExpiry validates the input fields and returns an absolute
// UTC time. Either ExpiresInSeconds or ExpiresAt must be supplied;
// when both are present the relative form wins (matches the
// dashboard's UX where the user picks "24h" and we don't want a
// stale absolute timestamp from earlier in the form session).
func resolveExpiry(body requestBody) (time.Time, error) {
	if body.Reason == "" {
		return time.Time{}, errors.New("reason is required")
	}
	now := time.Now().UTC()
	if body.ExpiresInSeconds > 0 {
		return now.Add(time.Duration(body.ExpiresInSeconds) * time.Second), nil
	}
	if body.ExpiresAtRFC3339 != "" {
		t, err := time.Parse(time.RFC3339, body.ExpiresAtRFC3339)
		if err != nil {
			return time.Time{}, fmt.Errorf("expires_at: must be RFC3339, got %q", body.ExpiresAtRFC3339)
		}
		return t, nil
	}
	return time.Time{}, errors.New("either expires_in_seconds or expires_at is required (no-expiry bypasses are not permitted)")
}
