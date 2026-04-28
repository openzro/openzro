package auth

import (
	"net/http"

	"github.com/openzro/openzro/management/server/activity"
)

// logout clears the openZro session cookie. Idempotent: the
// browser drops the cookie regardless of whether it had a valid
// session. Upstream end-session (RP-initiated logout) is not
// triggered here — V2 work; see ADR-0005.
//
// Best-effort audit: if the request carries a verifiable session
// cookie, the session.revoked event records the upstream
// provenance. Logout-without-session still clears the cookie but
// emits no event.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if h.emit != nil {
		if c, err := r.Cookie(SessionCookieName); err == nil {
			if claims, err := h.sessions.Verify(c.Value); err == nil {
				h.emit(r.Context(), claims.UpstreamSub, claims.UpstreamSub, "",
					activity.AuthSessionRevoked, map[string]any{
						"provider_id":  claims.ProviderID,
						"upstream_iss": claims.UpstreamIss,
						"upstream_sub": claims.UpstreamSub,
						"email":        claims.Email,
					})
			}
		}
	}
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
