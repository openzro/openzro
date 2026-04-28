package auth

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"

	"github.com/openzro/openzro/management/server/auth/providers"
)

//go:embed templates/login.tmpl templates/setup.tmpl templates/openzro-icon.svg
var loginAssets embed.FS

// providerLink is the per-row data the login template renders.
// Decoupled from providers.LiveProvider so the template doesn't
// reach into runtime fields (oidc.Provider etc.) it has no
// business touching.
type providerLink struct {
	ID      uint64
	Label   string
	LogoURL string
}

type loginPageData struct {
	Providers []providerLink
	ReturnTo  string
	Error     string
}

// loginRenderer caches the parsed template + the inlined SVG. The
// template is parsed once at NewHandler time and reused per
// request — html/template instances are safe for concurrent
// Execute.
type loginRenderer struct {
	tmpl *template.Template
}

func newLoginRenderer() (*loginRenderer, error) {
	icon, err := loginAssets.ReadFile("templates/openzro-icon.svg")
	if err != nil {
		return nil, fmt.Errorf("auth: read embedded icon: %w", err)
	}
	// Pre-bake the icon as template.HTML so it is emitted verbatim
	// rather than entity-encoded.
	iconHTML := template.HTML(icon)

	funcs := template.FuncMap{
		"ozIcon": func() template.HTML { return iconHTML },
		"startURL": func(providerID uint64, returnTo string) template.URL {
			q := url.Values{}
			q.Set("provider", fmt.Sprintf("%d", providerID))
			if isSafeReturnTo(returnTo) {
				q.Set("return_to", returnTo)
			}
			// template.URL is the safe-URL marker so html/template
			// keeps the query string intact instead of percent-
			// re-encoding the already-encoded form.
			return template.URL("/auth/start?" + q.Encode())
		},
	}
	t, err := template.New("login").
		Funcs(funcs).
		ParseFS(loginAssets, "templates/login.tmpl", "templates/setup.tmpl")
	if err != nil {
		return nil, fmt.Errorf("auth: parse templates: %w", err)
	}
	return &loginRenderer{tmpl: t}, nil
}

// providerLinks projects the manager's enabled list onto the
// template's view-model. Order is preserved (manager returns
// stable ID order).
func providerLinks(live []*providers.LiveProvider) []providerLink {
	out := make([]providerLink, 0, len(live))
	for _, p := range live {
		// Only surface providers the callback can complete.
		// Verifier==nil providers (GitHub, OAuth2-only) would
		// reach the 501 branch in callback_handler — surfacing
		// them would be a foot-gun. Once the userinfo path
		// lands, drop this filter.
		if p.Verifier == nil {
			continue
		}
		label := p.Row.BrandLabel
		if label == "" {
			label = p.Row.Name
		}
		out = append(out, providerLink{
			ID:      p.Row.ID,
			Label:   label,
			LogoURL: p.Row.BrandLogoURL,
		})
	}
	return out
}

// login renders the openZro-branded sign-in page. Reads
// providers via Manager.ListEnabled — no DB hit on the request
// path, the manager's cache is the source of truth.
//
// Query parameters:
//   - return_to: passed through to the eventual /auth/start
//     redirects (validated for safety).
//   - error: optional human-readable message rendered above the
//     provider list. Used by the callback to bounce errors back
//     to /login (?error=...) when sign-in fails.
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	data := loginPageData{
		Providers: providerLinks(h.providers.ListEnabled()),
		ReturnTo:  r.URL.Query().Get("return_to"),
		Error:     r.URL.Query().Get("error"),
	}
	if !isSafeReturnTo(data.ReturnTo) {
		data.ReturnTo = ""
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// Conservative CSP: inline styles allowed (we ship the CSS
	// inline in the template), no scripts at all, frames denied.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; style-src 'unsafe-inline'; img-src 'self' data: https:; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := h.renderer.tmpl.ExecuteTemplate(w, "login.tmpl", data); err != nil {
		// Headers may already be flushed; best we can do.
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
