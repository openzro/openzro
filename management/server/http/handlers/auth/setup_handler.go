package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openzro/openzro/management/server/auth/providers"
)

// setupPageData drives templates/setup.tmpl: rendered on GET
// after token validation and re-rendered on POST validation
// failure with Error populated.
type setupPageData struct {
	Token      string
	Types      []providers.ProviderType
	Error      string
	Name       string
	Type       providers.ProviderType
	IssuerURL  string
	ClientID   string
	BrandLabel string
	Submitted  bool
}

// setup handles both the initial wizard render (GET) and the
// form submission (POST). Path: /setup. Activated only when a
// BootstrapTokenStore is wired and has an active token.
//
// The token MUST appear on every request (URL query for GET,
// form field for POST) and is verified in constant time. The
// first successful POST creates the AuthenticationProvider row,
// invalidates the bootstrap token, and redirects to /login so
// the operator can sign in via the new provider immediately.
func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	if h.bootstrap == nil {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.setupGet(w, r)
	case http.MethodPost:
		h.setupPost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) setupGet(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if err := h.bootstrap.Verify(token); err != nil {
		// Either disabled or mismatch — both render as 404 to
		// avoid leaking which one (defence against operators
		// scanning for live bootstrap surfaces). Operators with
		// the right token always succeed.
		http.NotFound(w, r)
		return
	}

	data := setupPageData{
		Token: token,
		Types: knownTypeList(),
		Type:  providers.TypeGeneric,
	}
	h.renderSetup(w, data)
}

func (h *Handler) setupPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	token := r.FormValue("token")
	if err := h.bootstrap.Verify(token); err != nil {
		http.NotFound(w, r)
		return
	}

	data := setupPageData{
		Token:      token,
		Types:      knownTypeList(),
		Name:       strings.TrimSpace(r.FormValue("name")),
		Type:       providers.ProviderType(strings.TrimSpace(r.FormValue("type"))),
		IssuerURL:  strings.TrimSpace(r.FormValue("issuer_url")),
		ClientID:   strings.TrimSpace(r.FormValue("client_id")),
		BrandLabel: strings.TrimSpace(r.FormValue("brand_label")),
		Submitted:  true,
	}
	clientSecret := r.FormValue("client_secret")
	scopesRaw := strings.TrimSpace(r.FormValue("scopes"))

	if msg := h.validateSetupForm(data, clientSecret); msg != "" {
		data.Error = msg
		h.renderSetup(w, data)
		return
	}

	in := providers.SaveInput{
		Name:       data.Name,
		Type:       data.Type,
		Enabled:    true,
		BrandLabel: data.BrandLabel,
		Config: providers.Config{
			IssuerURL:    data.IssuerURL,
			ClientID:     data.ClientID,
			ClientSecret: clientSecret,
			Scopes:       splitScopes(scopesRaw),
		},
	}
	row, err := h.bootstrapStore.Save(r.Context(), in)
	if err != nil {
		data.Error = "failed to save provider: " + err.Error()
		h.renderSetup(w, data)
		return
	}

	// Refresh the live cache so /login picks up the new row
	// without a restart. Per-row failures (discovery against the
	// just-supplied issuer) are reported back to the operator
	// inline so they can fix the URL on the spot.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	perRow, err := h.providers.Refresh(ctx)
	if err != nil {
		data.Error = "saved but failed to refresh provider cache: " + err.Error()
		h.renderSetup(w, data)
		return
	}
	for _, e := range perRow {
		if e.ID == row.ID {
			data.Error = "saved but discovery failed: " + e.Err.Error() + ". Fix the issuer URL or remove the provider via the API."
			h.renderSetup(w, data)
			return
		}
	}

	// Single-shot: bootstrap is over once a provider exists.
	if err := h.bootstrap.Invalidate(); err != nil {
		// Log via emit if wired; the row is already persisted so
		// don't fail the response. Worst case the file lingers
		// and next boot's EnsureMinted is a no-op.
		_ = err
	}

	http.Redirect(w, r, "/login", http.StatusFound)
}

func (h *Handler) validateSetupForm(d setupPageData, clientSecret string) string {
	if d.Name == "" {
		return "Name is required."
	}
	if d.IssuerURL == "" {
		return "Issuer URL is required."
	}
	if !strings.HasPrefix(strings.ToLower(d.IssuerURL), "https://") &&
		!strings.HasPrefix(strings.ToLower(d.IssuerURL), "http://") {
		return "Issuer URL must include the scheme (https:// recommended)."
	}
	if d.ClientID == "" {
		return "Client ID is required."
	}
	if clientSecret == "" {
		return "Client Secret is required."
	}
	return ""
}

func (h *Handler) renderSetup(w http.ResponseWriter, data setupPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; style-src 'unsafe-inline'; img-src 'self' data: https:; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := h.renderer.tmpl.ExecuteTemplate(w, "setup.tmpl", data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func splitScopes(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func knownTypeList() []providers.ProviderType {
	return []providers.ProviderType{
		providers.TypeGeneric,
		providers.TypeGoogle,
		providers.TypeMicrosoft,
		providers.TypeEntraID,
		providers.TypeOkta,
		providers.TypeKeycloak,
		providers.TypeAuthentik,
		providers.TypeZitadel,
	}
}

// setupURL builds the absolute /setup URL the boot logger emits
// for the operator to copy. base must already include the scheme;
// token must be a freshly minted bootstrap value.
func setupURL(base, token string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base + "/setup?token=" + url.QueryEscape(token)
	}
	u.Path = "/setup"
	u.RawQuery = url.Values{"token": []string{token}}.Encode()
	return u.String()
}

// SetupURL is the exported variant the bootstrap caller uses.
// Returns "" when the store has no active token.
func SetupURL(base string, store *BootstrapTokenStore) string {
	if store == nil {
		return ""
	}
	tok := store.Active()
	if tok == "" {
		return ""
	}
	return setupURL(base, tok)
}
