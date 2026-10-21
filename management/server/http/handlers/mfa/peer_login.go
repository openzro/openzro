package mfa

import (
	"encoding/json"
	"errors"
	"io"
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

// maxApproveBody bounds the approve body, which holds one short code. The
// route is public, so the body is limited before it is read.
const maxApproveBody = 1 << 10

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
	code, httpStatus, err := readApproveCode(w, r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": err.Error(), "code": httpStatus})
		return
	}
	outcome, err := h.am.ApprovePeerLoginMFA(r.Context(), token, code)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	resp := api.PeerLoginApproveResponse{Approved: outcome.Approved, LockedUntil: outcome.LockedUntil}
	respStatus := http.StatusOK
	if !outcome.Approved {
		reason := api.PeerLoginApproveResponseReasonWrongCode
		respStatus = http.StatusUnprocessableEntity
		switch {
		case outcome.Locked:
			reason, respStatus = api.PeerLoginApproveResponseReasonLocked, http.StatusLocked
		case outcome.CodeReused:
			reason = api.PeerLoginApproveResponseReasonCodeReused
		}
		resp.Reason = &reason
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(respStatus)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		util.WriteError(r.Context(), err, w)
	}
}

// readApproveCode reads the optional code from a body of at most
// maxApproveBody bytes holding one JSON object and nothing after it. An
// empty body or an absent code is the retry of an approval that already
// happened; the manager refuses it as a wrong code otherwise.
func readApproveCode(w http.ResponseWriter, r *http.Request) (string, int, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxApproveBody))
	var req api.PeerLoginApproveRequest
	err := dec.Decode(&req)
	if errors.Is(err, io.EOF) {
		return "", http.StatusOK, nil
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return "", http.StatusRequestEntityTooLarge, errors.New("request body too large")
	}
	if err != nil {
		return "", http.StatusBadRequest, errors.New("body must be a JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if errors.As(err, &tooLarge) {
			return "", http.StatusRequestEntityTooLarge, errors.New("request body too large")
		}
		return "", http.StatusBadRequest, errors.New("unexpected data after the JSON object")
	}
	if req.Code == nil {
		return "", http.StatusOK, nil
	}
	return strings.TrimSpace(*req.Code), http.StatusOK, nil
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
