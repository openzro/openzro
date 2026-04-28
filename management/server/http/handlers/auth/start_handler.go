package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
)

// start initiates the PKCE flow. Inputs:
//   - ?provider=<id>  — required; the AuthenticationProvider row ID.
//   - ?return_to=<p>  — optional; where to land after callback.
//
// Side effects:
//   - sets the sealed state cookie (HttpOnly, SameSite=Lax,
//     scoped to /auth, 5-minute lifetime).
//   - 302s the user to the upstream IdP's authorization endpoint.
func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	providerID, err := strconv.ParseUint(r.URL.Query().Get("provider"), 10, 64)
	if err != nil {
		http.Error(w, "missing or invalid provider", http.StatusBadRequest)
		return
	}

	live, ok := h.providers.GetByID(providerID)
	if !ok {
		http.Error(w, "unknown provider", http.StatusNotFound)
		return
	}
	if live.OAuth2 == nil {
		http.Error(w, "provider misconfigured", http.StatusServiceUnavailable)
		return
	}

	// PKCE verifier (43+ chars, RFC 7636 §4.1) and S256 challenge.
	verifier, err := randomURLString(32)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	challengeRaw := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeRaw[:])

	urlState, err := randomURLString(24)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nonce, err := randomURLString(16)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	returnTo := r.URL.Query().Get("return_to")
	if !isSafeReturnTo(returnTo) {
		returnTo = h.defaultReturnTo
	}

	sealed, err := h.sealer.Seal(stateCookie{
		ProviderID:   providerID,
		CodeVerifier: verifier,
		URLState:     urlState,
		Nonce:        nonce,
		ReturnTo:     returnTo,
		IssuedAt:     time.Now().Unix(),
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     StateCookieName,
		Value:    sealed,
		Path:     "/auth",
		MaxAge:   int(StateCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	authURL := live.OAuth2.AuthCodeURL(
		urlState,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}
