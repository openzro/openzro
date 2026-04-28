package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/auth"
	nbcontext "github.com/openzro/openzro/management/server/context"
	authHandler "github.com/openzro/openzro/management/server/http/handlers/auth"
	"github.com/openzro/openzro/management/server/http/middleware/bypass"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

type EnsureAccountFunc func(ctx context.Context, userAuth nbcontext.UserAuth) (string, string, error)
type SyncUserJWTGroupsFunc func(ctx context.Context, userAuth nbcontext.UserAuth) error

type GetUserFromUserAuthFunc func(ctx context.Context, userAuth nbcontext.UserAuth) (*types.User, error)

// AuthMiddleware middleware to verify personal access tokens (PAT)
// and JWT tokens. Optionally accepts the openZro session cookie
// (oz_session) minted by the centralized /auth/callback handler;
// when configured, requests without an Authorization header but
// carrying a valid session cookie are accepted (ADR-0005).
type AuthMiddleware struct {
	authManager         auth.Manager
	sessions            *authHandler.SessionService
	ensureAccount       EnsureAccountFunc
	getUserFromUserAuth GetUserFromUserAuthFunc
	syncUserJWTGroups   SyncUserJWTGroupsFunc
}

// NewAuthMiddleware instance constructor. sessions may be nil —
// when nil, only the legacy Bearer (JWT) and Token (PAT) paths
// are accepted; the cookie-session bridge stays off.
func NewAuthMiddleware(
	authManager auth.Manager,
	sessions *authHandler.SessionService,
	ensureAccount EnsureAccountFunc,
	syncUserJWTGroups SyncUserJWTGroupsFunc,
	getUserFromUserAuth GetUserFromUserAuthFunc,
) *AuthMiddleware {
	return &AuthMiddleware{
		authManager:         authManager,
		sessions:            sessions,
		ensureAccount:       ensureAccount,
		syncUserJWTGroups:   syncUserJWTGroups,
		getUserFromUserAuth: getUserFromUserAuth,
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
			// No Authorization header (or unrecognized scheme):
			// try the session cookie minted by /auth/callback. The
			// dashboard's same-origin fetch automatically sends it,
			// so the operator gets API access right after a /login
			// round-trip without the dashboard having to manage a
			// Bearer token in localStorage.
			if m.sessions != nil {
				if cookie, err := r.Cookie(authHandler.SessionCookieName); err == nil {
					request, sessionErr := m.checkSessionCookie(r, cookie.Value)
					if sessionErr == nil {
						h.ServeHTTP(w, request)
						return
					}
					log.WithContext(r.Context()).Debugf("session cookie rejected: %v", sessionErr)
				}
			}
			util.WriteError(r.Context(), status.Errorf(status.Unauthorized, "no valid authentication provided"), w)
			return
		}
	})
}

// checkSessionCookie verifies an oz_session cookie and synthesizes
// a UserAuth from its claims. The downstream ensureAccount call
// auto-provisions the account / user the same way it does for a
// first-time JWT login — UpstreamSub takes the role normally
// played by the JWT's `sub` claim.
//
// Group sync is skipped: the session JWT we mint doesn't carry
// upstream IdP group claims (the callback didn't extract them).
// Group membership for cookie-authenticated users comes from the
// openZro user record (SCIM provisioning, manual assignment) —
// see ADR-0005 V2 for the upstream-claims passthrough plan.
func (m *AuthMiddleware) checkSessionCookie(r *http.Request, raw string) (*http.Request, error) {
	ctx := r.Context()
	claims, err := m.sessions.Verify(raw)
	if err != nil {
		return r, fmt.Errorf("session verify: %w", err)
	}
	userAuth := nbcontext.UserAuth{
		UserId: claims.UpstreamSub,
	}
	accountID, _, err := m.ensureAccount(ctx, userAuth)
	if err != nil {
		return r, fmt.Errorf("ensure account: %w", err)
	}
	userAuth.AccountId = accountID
	if _, err := m.getUserFromUserAuth(ctx, userAuth); err != nil {
		return r, fmt.Errorf("get user: %w", err)
	}
	return nbcontext.SetUserAuthInRequest(r, userAuth), nil
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

	_, err = m.getUserFromUserAuth(ctx, userAuth)
	if err != nil {
		log.WithContext(ctx).Errorf("HTTP server failed to update user from user auth: %s", err)
		return r, err
	}

	return nbcontext.SetUserAuthInRequest(r, userAuth), nil
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
