package auth

import (
	"errors"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/openzro/openzro/management/server/activity"
)

// callback finalizes the PKCE flow. Inputs (all from the
// upstream's redirect):
//   - state cookie (sealed)
//   - ?state=<s>      — must match cookie's URLState (CSRF check)
//   - ?code=<c>       — authorization code to exchange
//   - ?error=<e>      — upstream-reported failure
//
// Side effects on success:
//   - clears the state cookie.
//   - sets the openZro session cookie.
//   - 302s to the user-supplied return_to (or defaultReturnTo).
func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	if upstreamErr := r.URL.Query().Get("error"); upstreamErr != "" {
		// Surface the upstream's failure verbatim — operators
		// debugging an IdP misconfiguration need to see what
		// the IdP actually said.
		http.Error(w, "upstream rejected sign-in: "+upstreamErr, http.StatusUnauthorized)
		return
	}

	cookie, err := r.Cookie(StateCookieName)
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}
	state, err := h.sealer.Unseal(cookie.Value)
	if err != nil {
		if errors.Is(err, ErrStateExpired) {
			http.Error(w, "state cookie expired — sign-in took too long", http.StatusBadRequest)
			return
		}
		http.Error(w, "invalid state cookie", http.StatusBadRequest)
		return
	}

	// CSRF: the URL state must equal the cookie's URLState.
	if r.URL.Query().Get("state") != state.URLState {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	live, ok := h.providers.GetByID(state.ProviderID)
	if !ok {
		// Provider was deleted between /auth/start and /auth/callback.
		http.Error(w, "provider gone", http.StatusBadRequest)
		return
	}
	if live.OAuth2 == nil {
		http.Error(w, "provider misconfigured", http.StatusServiceUnavailable)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	exchangeCtx := oidc.ClientContext(r.Context(), h.httpClient)
	token, err := live.OAuth2.Exchange(
		exchangeCtx,
		code,
		oauth2.SetAuthURLParam("code_verifier", state.CodeVerifier),
	)
	if err != nil {
		http.Error(w, "code exchange failed", http.StatusBadGateway)
		return
	}

	if live.Verifier == nil {
		// PR 4 ships the OIDC path only. OAuth2-only providers
		// (GitHub) need a userinfo-endpoint round-trip to
		// resolve identity — landing in a follow-up. Today, the
		// admin shouldn't have configured a non-OIDC provider as
		// enabled because the dashboard form doesn't surface the
		// option, but defensively we return 501 rather than
		// minting a session with no upstream subject.
		http.Error(w, "OAuth2-only providers not yet implemented", http.StatusNotImplemented)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "upstream did not return an id_token", http.StatusBadGateway)
		return
	}
	idToken, err := live.Verifier.Verify(exchangeCtx, rawIDToken)
	if err != nil {
		http.Error(w, "id_token verification failed: "+err.Error(), http.StatusUnauthorized)
		return
	}
	if idToken.Nonce != state.Nonce {
		http.Error(w, "nonce mismatch", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "claims parse failed", http.StatusBadGateway)
		return
	}
	if claims.Sub == "" {
		http.Error(w, "id_token missing sub", http.StatusBadGateway)
		return
	}

	sessionToken, err := h.sessions.Issue(SessionClaims{
		Email:       claims.Email,
		Name:        claims.Name,
		ProviderID:  state.ProviderID,
		UpstreamIss: idToken.Issuer,
		UpstreamSub: claims.Sub,
	}, SessionTTL)
	if err != nil {
		http.Error(w, "session issue failed", http.StatusInternalServerError)
		return
	}

	// Drop the now-redundant state cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookieName,
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	if h.emit != nil {
		h.emit(r.Context(), claims.Sub, claims.Sub, "",
			activity.AuthSessionGranted, map[string]any{
				"provider_id":  state.ProviderID,
				"upstream_iss": idToken.Issuer,
				"upstream_sub": claims.Sub,
				"email":        claims.Email,
			})
	}

	returnTo := state.ReturnTo
	if !isSafeReturnTo(returnTo) {
		returnTo = h.defaultReturnTo
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}
