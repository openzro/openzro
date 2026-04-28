package auth

import "net/http"

// logout clears the openZro session cookie. Idempotent: the
// browser drops the cookie regardless of whether it had a valid
// session. Upstream end-session (RP-initiated logout) is not
// triggered here — V2 work; see ADR-0005.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}
