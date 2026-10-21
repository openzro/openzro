package mfa

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/status"
)

// The device-login approval page's endpoints (ADR-0028 D4). They skip the
// JWT middleware: the page token from the approval link, sent as a bearer,
// is their only credential, and the manager checks it.

// peerLoginHeaders keeps responses that carry the page's data out of caches
// and keeps the link out of Referer headers.
func peerLoginHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// peerLoginToken reads the page token from the Authorization header.
func peerLoginToken(r *http.Request) string {
	parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (h *handler) peerLoginGet(w http.ResponseWriter, r *http.Request) {
	peerLoginHeaders(w)
	token := peerLoginToken(r)
	if token == "" {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "invalid approval link"), w)
		return
	}
	view, err := h.am.GetPeerLoginMFAApproval(r.Context(), token)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toPeerLoginApproval(view))
}

func (h *handler) peerLoginApprove(w http.ResponseWriter, r *http.Request) {
	peerLoginHeaders(w)
	token := peerLoginToken(r)
	if token == "" {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "invalid approval link"), w)
		return
	}
	var req api.PeerLoginApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "decode body"), w)
		return
	}
	outcome, err := h.am.ApprovePeerLoginMFA(r.Context(), token, strings.TrimSpace(req.Code))
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	resp := api.PeerLoginApproveResponse{Approved: outcome.Approved, LockedUntil: outcome.LockedUntil}
	code := http.StatusOK
	if !outcome.Approved {
		reason := api.PeerLoginApproveResponseReasonWrongCode
		code = http.StatusUnprocessableEntity
		switch {
		case outcome.Locked:
			reason, code = api.PeerLoginApproveResponseReasonLocked, http.StatusLocked
		case outcome.CodeReused:
			reason = api.PeerLoginApproveResponseReasonCodeReused
		}
		resp.Reason = &reason
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		util.WriteError(r.Context(), err, w)
	}
}

func (h *handler) peerLoginDeny(w http.ResponseWriter, r *http.Request) {
	peerLoginHeaders(w)
	token := peerLoginToken(r)
	if token == "" {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "invalid approval link"), w)
		return
	}
	view, err := h.am.DenyPeerLoginMFA(r.Context(), token)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toPeerLoginApproval(view))
}

func toPeerLoginApproval(v *account.PeerLoginMFAView) api.PeerLoginApproval {
	out := api.PeerLoginApproval{
		State:          api.PeerLoginApprovalState(v.State),
		Hostname:       v.Hostname,
		Os:             v.OS,
		Ip:             v.IP,
		NewPeer:        v.NewPeer,
		KeyFingerprint: v.KeyFingerprint,
		MaskedEmail:    v.MaskedEmail,
		AccountDomain:  v.AccountDomain,
		CreatedAt:      v.CreatedAt.UTC(),
		ExpiresAt:      v.ExpiresAt.UTC(),
	}
	if v.GeoCountry != "" {
		out.GeoCountry = &v.GeoCountry
	}
	if v.GeoCity != "" {
		out.GeoCity = &v.GeoCity
	}
	return out
}
