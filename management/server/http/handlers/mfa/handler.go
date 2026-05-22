// Package mfa registers HTTP endpoints for openZro's TOTP MFA layer
// (issue #31).
//
// Endpoint shape:
//
//	Token-bearer paths (NOT covered by the standard auth middleware
//	— see isMFAExemptRequest in middleware/auth_middleware.go):
//	  POST /api/mfa/enroll/start   — enrollment_token  → QR + secret + pending_token
//	  POST /api/mfa/enroll/finish  — enrollment_token + pending_token + code → backup codes + mfa_session_token
//	  POST /api/mfa/challenge      — challenge_token + code → mfa_session_token
//
//	Full-session paths (standard JWT auth + mfa-session gate apply):
//	  GET    /api/users/me/mfa                          — status (exempt from gate, harmless read)
//	  DELETE /api/users/me/mfa                          — self disenroll (requires mfa_session_token)
//	  POST   /api/users/me/mfa/enroll/start             — voluntary enroll start (requires session if already enrolled)
//	  POST   /api/users/me/mfa/enroll/finish            — voluntary enroll finish (requires session if already enrolled)
//	  POST   /api/users/me/mfa/backup-codes/regenerate  — fresh codes (requires mfa_session_token)
//	  DELETE /api/users/{userId}/mfa                    — admin disenroll (requires mfa_session_token + same account + admin)
package mfa

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/middleware/bypass"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
)

// mfaBypassRoutes are the token-bearer endpoints that MUST skip the
// JWT auth middleware: their Authorization header carries an MFA-
// scoped token (challenge_token / enrollment_token), not a Dex JWT.
// The middleware would otherwise try to verify those as OIDC bearers
// and reject with 401 token invalid (review round 2, finding #1).
// path.Match's glob does NOT cross '/', so each depth needs its own
// entry — `/api/mfa/*` would miss `/api/mfa/enroll/start`.
var mfaBypassRoutes = []string{
	"/api/mfa/challenge",
	"/api/mfa/enroll/start",
	"/api/mfa/enroll/finish",
}

// jwtSessionIDFromRequest re-derives the JWT session id from the
// Authorization header, matching middleware/auth_middleware.go's
// jwtSessionID exactly. Duplicated rather than imported to keep
// this handler decoupled from the middleware package.
func jwtSessionIDFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	sum := sha256.Sum256([]byte(parts[1]))
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

// mfaSessionHeader mirrors the constant in the auth middleware.
// Duplicated rather than imported to keep this leaf handler free
// of middleware imports.
const mfaSessionHeader = "X-MFA-Token"

type handler struct {
	am account.Manager
}

// AddEndpoints wires the MFA routes into the given router. Registers
// the token-bearer paths with the auth-middleware bypass list so the
// dashboard can hit them carrying an MFA-scoped token in the
// Authorization header rather than a Dex JWT.
func AddEndpoints(accountManager account.Manager, router *mux.Router) {
	h := &handler{am: accountManager}

	// Token-bearer paths — bypass the JWT auth middleware entirely.
	// Each handler runs its own MFA-token verification before acting.
	for _, p := range mfaBypassRoutes {
		if err := bypass.AddBypassPath(p); err != nil {
			log.Errorf("MFA bypass registration: %s: %v", p, err)
		}
	}
	router.HandleFunc("/mfa/enroll/start", h.enrollStartTokenAuth).Methods("POST", "OPTIONS")
	router.HandleFunc("/mfa/enroll/finish", h.enrollFinishTokenAuth).Methods("POST", "OPTIONS")
	router.HandleFunc("/mfa/challenge", h.challenge).Methods("POST", "OPTIONS")

	// Full-session paths.
	router.HandleFunc("/users/me/mfa", h.status).Methods("GET", "OPTIONS")
	router.HandleFunc("/users/me/mfa", h.disenrollSelf).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/users/me/mfa/enroll/start", h.enrollStartFullSession).Methods("POST", "OPTIONS")
	router.HandleFunc("/users/me/mfa/enroll/finish", h.enrollFinishFullSession).Methods("POST", "OPTIONS")
	router.HandleFunc("/users/me/mfa/backup-codes/regenerate", h.regenerateBackupCodes).Methods("POST", "OPTIONS")
	router.HandleFunc("/users/{userId}/mfa", h.disenroll).Methods("DELETE", "OPTIONS")
}

// extractMFAToken reads the Bearer token and verifies it for the
// requested purpose. Returns the full verify result so the caller
// can also use the JTI binding when minting downstream tokens.
// Returns nil on any failure so the caller writes 401 without
// branching.
func (h *handler) extractMFAToken(r *http.Request, purpose account.MFATokenPurpose) *account.MFATokenVerifyResult {
	auth := r.Header.Get("Authorization")
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil
	}
	res, err := h.am.MFAVerifyToken(parts[1], purpose)
	if err != nil {
		log.WithContext(r.Context()).Debugf("MFA token verify: %v", err)
		return nil
	}
	return res
}

// -- token-bearer endpoints --

func (h *handler) enrollStartTokenAuth(w http.ResponseWriter, r *http.Request) {
	res := h.extractMFAToken(r, account.MFATokenPurposeEnrollment)
	if res == nil {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "invalid enrollment token"), w)
		return
	}
	h.doEnrollStart(w, r, "", res.UserID)
}

func (h *handler) enrollFinishTokenAuth(w http.ResponseWriter, r *http.Request) {
	res := h.extractMFAToken(r, account.MFATokenPurposeEnrollment)
	if res == nil {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "invalid enrollment token"), w)
		return
	}
	h.doEnrollFinish(w, r, "", res.UserID, res.JTIBinding)
}

type challengeRequest struct {
	Code string `json:"code"`
}

type challengeResponse struct {
	OK              bool   `json:"ok"`
	UsedBackupCode  bool   `json:"used_backup_code,omitempty"`
	Locked          bool   `json:"locked,omitempty"`
	LockedUntil     string `json:"locked_until,omitempty"` // RFC3339
	MFASessionToken string `json:"mfa_session_token,omitempty"`
}

func (h *handler) challenge(w http.ResponseWriter, r *http.Request) {
	res := h.extractMFAToken(r, account.MFATokenPurposeChallenge)
	if res == nil {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "invalid challenge token"), w)
		return
	}
	if res.JTIBinding == "" {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "challenge token missing session binding"), w)
		return
	}
	var req challengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "decode body"), w)
		return
	}
	outcome, err := h.am.MFAChallenge(r.Context(), res.UserID, req.Code, res.JTIBinding)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resp := challengeResponse{
		OK:              outcome.OK,
		UsedBackupCode:  outcome.UsedBackupCode,
		Locked:          outcome.Locked,
		MFASessionToken: outcome.SessionToken,
	}
	if outcome.LockedUntil != nil {
		resp.LockedUntil = outcome.LockedUntil.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	util.WriteJSONObject(r.Context(), w, resp)
}

// -- full-session endpoints --

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	userAuth, ok := requireUserAuth(w, r)
	if !ok {
		return
	}
	st, err := h.am.MFAStatus(r.Context(), userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, st)
}

func (h *handler) enrollStartFullSession(w http.ResponseWriter, r *http.Request) {
	userAuth, ok := requireUserAuth(w, r)
	if !ok {
		return
	}
	// If user is already enrolled, demand an mfa_session_token before
	// allowing a re-enroll that would overwrite the existing TOTP
	// secret. Without this, a stolen JWT alone could replace the
	// second factor. The gate-level enrollment-redirect path is
	// covered separately by the token-bearer endpoint above.
	if !h.requireSessionIfEnrolled(w, r, userAuth) {
		return
	}
	h.doEnrollStart(w, r, userAuth.AccountId, userAuth.UserId)
}

type fullSessionFinishRequest struct {
	Code         string `json:"code"`
	PendingToken string `json:"pending_token"`
}

func (h *handler) enrollFinishFullSession(w http.ResponseWriter, r *http.Request) {
	userAuth, ok := requireUserAuth(w, r)
	if !ok {
		return
	}
	if !h.requireSessionIfEnrolled(w, r, userAuth) {
		return
	}
	jwtSessionID := jwtSessionIDFromRequest(r)
	if jwtSessionID == "" {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "missing session binding"), w)
		return
	}
	h.doEnrollFinish(w, r, userAuth.AccountId, userAuth.UserId, jwtSessionID)
}

func (h *handler) regenerateBackupCodes(w http.ResponseWriter, r *http.Request) {
	userAuth, ok := requireUserAuth(w, r)
	if !ok {
		return
	}
	if !h.requireMFASession(w, r, userAuth.UserId) {
		return
	}
	codes, err := h.am.MFARegenerateBackupCodes(r.Context(), userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, map[string]any{"backup_codes": codes})
}

// disenrollSelf handles DELETE /api/users/me/mfa. Convenience over
// the admin-style /api/users/{userId}/mfa: the dashboard doesn't
// have to know the calling user's own id, and the route is always
// safe (self-only). Requires an mfa_session_token so a stolen JWT
// alone cannot strip the user's second factor.
func (h *handler) disenrollSelf(w http.ResponseWriter, r *http.Request) {
	userAuth, ok := requireUserAuth(w, r)
	if !ok {
		return
	}
	if !h.requireMFASession(w, r, userAuth.UserId) {
		return
	}
	if err := h.am.MFADisenroll(r.Context(), userAuth.UserId); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, map[string]any{"disenrolled": true})
}

func (h *handler) disenroll(w http.ResponseWriter, r *http.Request) {
	userAuth, ok := requireUserAuth(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	targetUserID := vars["userId"]
	if targetUserID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "missing userId"), w)
		return
	}

	// Authorization: self-disenroll is always allowed (the user can
	// always clear their own second factor). Cross-user disenrol is
	// admin-only AND requires the target to live in the caller's
	// account — otherwise any authenticated user in any account could
	// strip MFA off any userID they happen to guess. Anti-CWE-639.
	if targetUserID != userAuth.UserId {
		caller, err := h.am.GetUserByID(r.Context(), userAuth.UserId)
		if err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
		if !caller.HasAdminPower() {
			util.WriteError(r.Context(), status.NewPermissionDeniedError(), w)
			return
		}
		target, err := h.am.GetUserByID(r.Context(), targetUserID)
		if err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
		if target.AccountID != caller.AccountID {
			// Don't leak existence — a wrong-account target gets the
			// same response shape as a missing one.
			util.WriteError(r.Context(), status.NewPermissionDeniedError(), w)
			return
		}
	}

	// Always require an MFA-verified session for disenroll, even for
	// admins clearing a different user's row — protects against an
	// admin JWT leak being immediately weaponized to mass-disable MFA.
	if !h.requireMFASession(w, r, userAuth.UserId) {
		return
	}

	if err := h.am.MFADisenroll(r.Context(), targetUserID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, map[string]any{"disenrolled": true})
}

// -- shared helpers --

func (h *handler) doEnrollStart(w http.ResponseWriter, r *http.Request, accountID, userID string) {
	if !h.am.MFAEnabled() {
		util.WriteError(r.Context(), status.Errorf(status.Internal, "mfa: subsystem not initialized"), w)
		return
	}
	if accountID == "" {
		acc, err := h.lookupAccountForUser(r, userID)
		if err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
		accountID = acc
	}
	res, err := h.am.MFAStartEnrollment(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, res)
}

func (h *handler) doEnrollFinish(w http.ResponseWriter, r *http.Request, accountID, userID, jwtSessionID string) {
	if !h.am.MFAEnabled() {
		util.WriteError(r.Context(), status.Errorf(status.Internal, "mfa: subsystem not initialized"), w)
		return
	}
	if accountID == "" {
		acc, err := h.lookupAccountForUser(r, userID)
		if err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
		accountID = acc
	}
	var req fullSessionFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "decode body"), w)
		return
	}
	if req.PendingToken == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "missing pending_token"), w)
		return
	}
	out, err := h.am.MFAFinishEnrollment(r.Context(), accountID, userID, req.PendingToken, req.Code, jwtSessionID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, out)
}

func (h *handler) lookupAccountForUser(r *http.Request, userID string) (string, error) {
	user, err := h.am.GetStore().GetUserByUserID(r.Context(), store.LockingStrengthShare, userID)
	if err != nil {
		return "", err
	}
	return user.AccountID, nil
}

func requireUserAuth(w http.ResponseWriter, r *http.Request) (nbcontext.UserAuth, bool) {
	userAuth, err := nbcontext.GetUserAuthFromRequest(r)
	if err != nil {
		util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "missing user auth"), w)
		return nbcontext.UserAuth{}, false
	}
	return userAuth, true
}

// requireMFASession enforces that the request carries a valid
// mfa_session_token bound to the current JWT session. Used by the
// sensitive ops (disenroll, regenerate, re-enroll over an existing
// TOTP) so a stolen JWT alone cannot mutate the second factor.
// On refusal, mints a fresh challenge_token bound to this JWT
// session so the dashboard's 403 interceptor can route the user to
// /mfa/challenge — without that token the user gets stuck (review
// round 2, finding #2: enforcement-OFF voluntary MFA users hit this
// when sessionStorage is empty after a tab reset). Returns false
// when refused.
func (h *handler) requireMFASession(w http.ResponseWriter, r *http.Request, userID string) bool {
	jwtSession := jwtSessionIDFromRequest(r)
	mfaTok := r.Header.Get(mfaSessionHeader)
	if jwtSession == "" || !h.am.MFASessionValid(userID, mfaTok, jwtSession) {
		token, err := h.am.MFAIssueChallengeToken(userID, jwtSession)
		if err != nil {
			log.WithContext(r.Context()).Errorf("issue challenge token on sensitive-op refusal: %v", err)
		}
		writeMFARequired(r.Context(), w, token)
		return false
	}
	return true
}

// requireSessionIfEnrolled is the variant for the voluntary enroll
// endpoints: when the user has no MFA row yet, the request goes
// through (no session to check); when they already have a row,
// behaves like requireMFASession.
func (h *handler) requireSessionIfEnrolled(w http.ResponseWriter, r *http.Request, userAuth nbcontext.UserAuth) bool {
	st, err := h.am.MFAStatus(r.Context(), userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return false
	}
	if !st.Enrolled {
		return true
	}
	return h.requireMFASession(w, r, userAuth.UserId)
}

// writeMFARequired emits a 403 with the same wire shape the auth
// middleware uses for an unverified session: { mfa_required: true,
// token: <challenge_token> }. The dashboard's api.tsx interceptor
// reads the token and routes the user to /mfa/challenge. The token
// may be empty if the manager failed to mint one (logged upstream);
// the interceptor falls back to a plain error in that case.
func writeMFARequired(ctx context.Context, w http.ResponseWriter, token string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	body := map[string]any{
		"mfa_required": true,
		"message":      "MFA verification required for this action",
	}
	if token != "" {
		body["token"] = token
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.WithContext(ctx).Errorf("write mfa-required: %v", err)
	}
}
