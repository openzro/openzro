package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/auth"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/middleware/bypass"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// mfaSessionHeader is the dashboard-side counterpart to Authorization
// for MFA-verified sessions. Holds an mfa_session_token (HS256 JWT)
// the dashboard received from /api/mfa/challenge or /enroll/finish;
// the auth middleware verifies it against the JWT session id and
// passes it to the MFA gate.
const mfaSessionHeader = "X-MFA-Token"

type EnsureAccountFunc func(ctx context.Context, userAuth nbcontext.UserAuth) (string, string, error)
type SyncUserJWTGroupsFunc func(ctx context.Context, userAuth nbcontext.UserAuth) error

type GetUserFromUserAuthFunc func(ctx context.Context, userAuth nbcontext.UserAuth) (*types.User, error)

// MFAGateFunc decides whether the request continues, must be
// challenged for an existing TOTP secret, or must enroll. Issue #31.
// `connectorID` comes from the JWT's federated_claims.connector_id;
// `isPAT` should always be false for JWT auth (PATs go through the
// separate PAT path which bypasses MFA by design); `jwtSessionID`
// identifies the current JWT session so the gate can bind / verify
// mfa_session_token against it.
//
// Returns (decision, error). On a non-nil decision with
// .Challenge/.Enroll == true, the middleware writes 403 + the token
// in the response body; on .Pass == true the middleware continues.
// A non-nil error means the gate itself failed (e.g. store
// unavailable) — surfaced as 500.
type MFAGateFunc func(ctx context.Context, connectorID string, user *types.User, settings *types.Settings, isPAT bool, jwtSessionID, mfaSessionToken string) (*MFAGateDecision, error)

// MFAGateDecision mirrors server.MFAGateDecision verbatim — the
// middleware can't import management/server (would cycle) so the
// type lives here. server.MFAGateForRequest builds the equivalent
// struct and the dispatch wrapper at the handler.go wiring step
// translates between the two.
type MFAGateDecision struct {
	Pass      bool
	Challenge bool
	Enroll    bool
	Token     string
}

// GetAccountSettingsFunc is the shim used by the middleware to
// fetch the account-level settings needed by MFAGateFunc — kept as
// a function so the middleware package stays free of the heavier
// server / settings imports.
type GetAccountSettingsFunc func(ctx context.Context, accountID string) (*types.Settings, error)

// AuthMiddleware middleware to verify personal access tokens (PAT) and JWT tokens
type AuthMiddleware struct {
	authManager         auth.Manager
	ensureAccount       EnsureAccountFunc
	getUserFromUserAuth GetUserFromUserAuthFunc
	syncUserJWTGroups   SyncUserJWTGroupsFunc
	mfaGate             MFAGateFunc
	getAccountSettings  GetAccountSettingsFunc
}

// NewAuthMiddleware instance constructor. `mfaGate` and
// `getAccountSettings` may both be nil — when either is missing the
// MFA gate is skipped entirely and the middleware behaves as before
// (issue #31 closed-loop wiring is opt-in at handler.go).
func NewAuthMiddleware(
	authManager auth.Manager,
	ensureAccount EnsureAccountFunc,
	syncUserJWTGroups SyncUserJWTGroupsFunc,
	getUserFromUserAuth GetUserFromUserAuthFunc,
	mfaGate MFAGateFunc,
	getAccountSettings GetAccountSettingsFunc,
) *AuthMiddleware {
	return &AuthMiddleware{
		authManager:         authManager,
		ensureAccount:       ensureAccount,
		syncUserJWTGroups:   syncUserJWTGroups,
		getUserFromUserAuth: getUserFromUserAuth,
		mfaGate:             mfaGate,
		getAccountSettings:  getAccountSettings,
	}
}

// Handler method of the middleware which authenticates a user either by JWT claims or by PAT
func (m *AuthMiddleware) Handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bypass.ShouldBypass(r.URL.Path, h, w, r) {
			return
		}

		auth := strings.Split(r.Header.Get("Authorization"), " ")
		authType := strings.ToLower(auth[0])

		// fallback to token when receive pat as bearer
		if len(auth) >= 2 && authType == "bearer" && strings.HasPrefix(auth[1], "nbp_") {
			authType = "token"
			auth[0] = authType
		}

		switch authType {
		case "bearer":
			request, err := m.checkJWTFromRequest(r, auth)
			if err != nil {
				// MFA gate refusal is NOT an auth failure — surface
				// it with the matching response shape so the
				// dashboard can route the user to the challenge or
				// enrollment flow without re-prompting for login.
				var mfaErr *MFAGateRequired
				if errors.As(err, &mfaErr) {
					writeMFAGateResponse(r.Context(), w, mfaErr.Decision)
					return
				}
				log.WithContext(r.Context()).Errorf("Error when validating JWT: %s", err.Error())
				util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "token invalid"), w)
				return
			}

			h.ServeHTTP(w, request)
		case "token":
			request, err := m.checkPATFromRequest(r, auth)
			if err != nil {
				log.WithContext(r.Context()).Debugf("Error when validating PAT: %s", err.Error())
				util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "token invalid"), w)
				return
			}
			h.ServeHTTP(w, request)
		default:
			util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "no valid authentication provided"), w)
			return
		}
	})
}

// CheckJWTFromRequest checks if the JWT is valid
func (m *AuthMiddleware) checkJWTFromRequest(r *http.Request, auth []string) (*http.Request, error) {
	token, err := getTokenFromJWTRequest(auth)

	// If an error occurs, call the error handler and return an error
	if err != nil {
		return r, fmt.Errorf("error extracting token: %w", err)
	}

	ctx := r.Context()

	userAuth, validatedToken, err := m.authManager.ValidateAndParseToken(ctx, token)
	if err != nil {
		return r, err
	}

	// SECURITY: account id is derived only from the validated token.
	// The previous behavior accepted "?account=<id>" as a query parameter
	// and overwrote userAuth.AccountId (and set IsChild=true, which bypasses
	// admin checks downstream — see peers_handler.go and user.go). That made
	// any authenticated user able to read/manipulate any other account's
	// resources by manipulating a request parameter. See CWE-639. Account
	// impersonation, if ever needed, must be re-introduced as an opt-in
	// feature gated by a server-side authorization check, not as a default.

	// we need to call this method because if user is new, we will automatically add it to existing or create a new account
	accountId, _, err := m.ensureAccount(ctx, userAuth)
	if err != nil {
		return r, err
	}

	if userAuth.AccountId != accountId {
		log.WithContext(ctx).Debugf("Auth middleware sets accountId from ensure, before %s, now %s", userAuth.AccountId, accountId)
		userAuth.AccountId = accountId
	}

	userAuth, err = m.authManager.EnsureUserAccessByJWTGroups(ctx, userAuth, validatedToken)
	if err != nil {
		return r, err
	}

	err = m.syncUserJWTGroups(ctx, userAuth)
	if err != nil {
		log.WithContext(ctx).Errorf("HTTP server failed to sync user JWT groups: %s", err)
	}

	user, err := m.getUserFromUserAuth(ctx, userAuth)
	if err != nil {
		log.WithContext(ctx).Errorf("HTTP server failed to update user from user auth: %s", err)
		return r, err
	}

	// MFA gate (issue #31). Skipped when the wiring is incomplete
	// (mfaGate or getAccountSettings nil) so the middleware works
	// in standalone unit tests / non-production deployments without
	// MFA. The bypass list at /api/mfa/* lets the dashboard reach
	// the challenge / enroll endpoints with just the challenge or
	// enrollment token in the Authorization header — the MFA-routed
	// handlers run their own token-purpose verification before
	// acting.
	if m.mfaGate != nil && m.getAccountSettings != nil && !isMFAExemptRequest(r) {
		settings, sErr := m.getAccountSettings(ctx, userAuth.AccountId)
		if sErr != nil {
			log.WithContext(ctx).Errorf("MFA gate: failed to read account settings: %s", sErr)
			return r, sErr
		}
		jwtSessionID := jwtSessionID(token)
		mfaSessionToken := r.Header.Get(mfaSessionHeader)
		decision, gErr := m.mfaGate(ctx, userAuth.ConnectorID, user, settings, userAuth.IsPAT, jwtSessionID, mfaSessionToken)
		if gErr != nil {
			log.WithContext(ctx).Errorf("MFA gate: %s", gErr)
			return r, gErr
		}
		if decision != nil && !decision.Pass {
			return r, &MFAGateRequired{Decision: decision}
		}
	}

	return nbcontext.SetUserAuthInRequest(r, userAuth), nil
}

// MFAGateRequired is returned by checkJWTFromRequest when the MFA
// gate refuses the request. The outer Handler maps it to a 403 with
// a JSON body the dashboard parses to decide between routing to
// /mfa/challenge (challenge_token) or /mfa/enroll (enrollment_token).
type MFAGateRequired struct {
	Decision *MFAGateDecision
}

func (m *MFAGateRequired) Error() string {
	switch {
	case m.Decision.Challenge:
		return "mfa challenge required"
	case m.Decision.Enroll:
		return "mfa enrollment required"
	default:
		return "mfa gate refused"
	}
}

// isMFAExemptRequest returns true for requests that must reach a
// handler with a verified JWT but WITHOUT clearing the MFA gate.
// Method-aware on purpose: previously a path-only exemption let a
// stolen JWT bypass MFA by hitting DELETE /api/users/me/mfa (disable)
// or the re-enroll endpoints — see issue #31 review (high/sec
// finding #1, fixed in this iteration).
//
// Note: the token-bearer paths under /api/mfa/* skip this middleware
// entirely via bypass.ShouldBypass (registered by the MFA handler at
// startup) — they don't carry a Dex JWT, so the JWT-validation step
// above would 401 them. Those routes never reach this check.
//
// Currently exempt:
//
//   - GET /api/users/me/mfa: a read of the user's own enrollment
//     state, harmless even to a stolen JWT (no mutation).
//
// EVERYTHING ELSE goes through the gate. In particular, the
// sensitive ops (DELETE /api/users/me/mfa, DELETE /api/users/{id}/
// mfa, POST .../backup-codes/regenerate, POST .../enroll/*) ride
// the gate AND additionally require an mfa_session_token at the
// handler — so a JWT-only attacker cannot strip or replace the
// second factor.
func isMFAExemptRequest(r *http.Request) bool {
	if r.URL.Path == "/api/users/me/mfa" && r.Method == http.MethodGet {
		return true
	}
	return false
}

// jwtSessionID derives a short, opaque session id from the raw JWT.
// The MFA layer uses this to bind challenge / session tokens to a
// specific browser session: a stolen JWT alone cannot re-use a
// stolen mfa_session_token unless both came from the same session.
//
// We hash the entire token rather than reading the `jti` claim
// because:
//   - Not every IdP emits jti (Dex does on its current main, but
//     legacy/other providers might not), and silently degrading to
//     "no binding" would erase the security gain.
//   - The hash is one-way; storing it in logs / tokens doesn't leak
//     the bearer back. Truncated to 16 base64 chars (≈96 bits) — a
//     collision needs the attacker to find a second valid signed
//     JWT, not feasible without the IdP signing key.
func jwtSessionID(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

// mfaGateResponseBody is the JSON shape returned to the dashboard
// on an MFA gate refusal. The dashboard distinguishes "challenge"
// vs "enrollment" by reading `mfa_required` / `mfa_enrollment_
// required`; the matching token is what the dashboard then sends
// in the Authorization header to /api/mfa/challenge or
// /api/mfa/enroll/*. Keeping the two flags mutually exclusive in
// the wire format lets the dashboard switch on a simple
// `if (body.mfa_required) ... else if (body.mfa_enrollment_required) ...`.
type mfaGateResponseBody struct {
	MFARequired           bool   `json:"mfa_required,omitempty"`
	MFAEnrollmentRequired bool   `json:"mfa_enrollment_required,omitempty"`
	Token                 string `json:"token"`
}

// writeMFAGateResponse writes a 403 with the gate decision body so
// the dashboard can route the user. Using 403 (not 401) is
// deliberate: a 401 would suggest re-authentication is the answer,
// but the user's JWT IS valid — they just need to step up to MFA.
func writeMFAGateResponse(ctx context.Context, w http.ResponseWriter, d *MFAGateDecision) {
	body := mfaGateResponseBody{Token: d.Token}
	switch {
	case d.Challenge:
		body.MFARequired = true
	case d.Enroll:
		body.MFAEnrollmentRequired = true
	default:
		util.WriteError(ctx, status.Errorf(status.PermissionDenied, "mfa gate refused"), w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.WithContext(ctx).Errorf("failed to encode MFA gate response: %s", err)
	}
}

// CheckPATFromRequest checks if the PAT is valid
func (m *AuthMiddleware) checkPATFromRequest(r *http.Request, auth []string) (*http.Request, error) {
	token, err := getTokenFromPATRequest(auth)
	if err != nil {
		return r, fmt.Errorf("error extracting token: %w", err)
	}

	ctx := r.Context()
	user, pat, accDomain, accCategory, err := m.authManager.GetPATInfo(ctx, token)
	if err != nil {
		return r, fmt.Errorf("invalid Token: %w", err)
	}
	if time.Now().After(pat.GetExpirationDate()) {
		return r, fmt.Errorf("token expired")
	}

	err = m.authManager.MarkPATUsed(ctx, pat.ID)
	if err != nil {
		return r, err
	}

	userAuth := nbcontext.UserAuth{
		UserId:         user.Id,
		AccountId:      user.AccountID,
		Domain:         accDomain,
		DomainCategory: accCategory,
		IsPAT:          true,
	}

	// SECURITY: see CWE-639 note on the JWT path above. AccountId is bound
	// to the PAT's owner; the "?account=" query parameter must not be able
	// to override it.

	return nbcontext.SetUserAuthInRequest(r, userAuth), nil
}

// getTokenFromJWTRequest is a "TokenExtractor" that takes auth header parts and extracts
// the JWT token from the Authorization header.
func getTokenFromJWTRequest(authHeaderParts []string) (string, error) {
	if len(authHeaderParts) != 2 || strings.ToLower(authHeaderParts[0]) != "bearer" {
		return "", errors.New("authorization header format must be Bearer {token}")
	}

	return authHeaderParts[1], nil
}

// getTokenFromPATRequest is a "TokenExtractor" that takes auth header parts and extracts
// the PAT token from the Authorization header.
func getTokenFromPATRequest(authHeaderParts []string) (string, error) {
	if len(authHeaderParts) != 2 || strings.ToLower(authHeaderParts[0]) != "token" {
		return "", errors.New("authorization header format must be Token {token}")
	}

	return authHeaderParts[1], nil
}
