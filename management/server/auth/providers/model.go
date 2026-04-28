// Package providers persists openZro's configured authentication
// providers (OIDC IdPs the operator has wired up) and exposes a
// Store with credentials encrypted at rest.
//
// One AuthenticationProvider row corresponds to one IdP the
// operator has connected — Zitadel, Keycloak, Entra ID, Google,
// GitHub, etc. The /login page reads PublicView projections to
// render its provider buttons; the OIDC manager (PR 2) decrypts
// ConfigCipher to instantiate live oidc.Provider objects.
//
// See ADR-0005 for the architectural decision behind this package.
package providers

import "time"

// ProviderType tags the OIDC vendor a provider row represents.
// `oidc-generic` is the catch-all for any OIDC-compliant IdP that
// doesn't ship with a dedicated brand template; the rest are
// well-known vendors the dashboard pre-fills endpoints + scopes
// for.
type ProviderType string

const (
	TypeGeneric   ProviderType = "oidc-generic"
	TypeGoogle    ProviderType = "google"
	TypeGitHub    ProviderType = "github"
	TypeMicrosoft ProviderType = "microsoft"
	TypeEntraID   ProviderType = "entra-id"
	TypeOkta      ProviderType = "okta"
	TypeKeycloak  ProviderType = "keycloak"
	TypeAuthentik ProviderType = "authentik"
	TypeZitadel   ProviderType = "zitadel"
)

// IsKnownType reports whether t is a recognised ProviderType. The
// Store accepts any non-empty type so operators can experiment with
// new vendors without a code change; this helper is for the
// dashboard, which renders a per-type icon for known types and a
// generic OIDC icon otherwise.
func IsKnownType(t ProviderType) bool {
	switch t {
	case TypeGeneric, TypeGoogle, TypeGitHub, TypeMicrosoft,
		TypeEntraID, TypeOkta, TypeKeycloak, TypeAuthentik, TypeZitadel:
		return true
	}
	return false
}

// AuthenticationProvider is the GORM row for one configured IdP.
// ConfigCipher carries the encrypted Config (client_secret +
// sensitive fields); PublicConfig carries the operator-visible
// projection so the dashboard renders configuration without
// holding the encryption key.
type AuthenticationProvider struct {
	ID   uint64       `gorm:"primaryKey;autoIncrement"`
	Name string       `gorm:"size:128;not null"`
	Type ProviderType `gorm:"size:32;not null;index"`
	// Enabled is set by the admin API explicitly on every Save; the
	// schema carries no DB-side default because GORM treats a Go
	// zero-value bool as "use the column default" on INSERT, which
	// would silently override an explicit `Enabled: false`.
	Enabled         bool   `gorm:"not null"`
	PublicConfig    []byte `gorm:"type:bytea"`
	ConfigCipher    []byte `gorm:"type:bytea;not null"`
	BrandLabel      string `gorm:"size:128"`
	BrandLogoURL    string `gorm:"size:512"`
	EmailDomainHint string `gorm:"size:128"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName pins the table name so plural-ization rules don't
// surprise us across GORM versions.
func (AuthenticationProvider) TableName() string {
	return "authentication_providers"
}

// ToPublicView projects the row to the redacted shape the /login
// page and the admin "list providers" endpoint return.
func (p AuthenticationProvider) ToPublicView() PublicView {
	return PublicView{
		ID:           p.ID,
		Name:         p.Name,
		Type:         p.Type,
		Enabled:      p.Enabled,
		BrandLabel:   p.BrandLabel,
		BrandLogoURL: p.BrandLogoURL,
	}
}

// PublicView is the row-level redacted projection — what the
// /login page renders without holding decryption keys.
type PublicView struct {
	ID           uint64       `json:"id"`
	Name         string       `json:"name"`
	Type         ProviderType `json:"type"`
	Enabled      bool         `json:"enabled"`
	BrandLabel   string       `json:"brand_label"`
	BrandLogoURL string       `json:"brand_logo_url,omitempty"`
}

// Config is the full per-provider OIDC configuration as serialized
// into ConfigCipher. The OIDC manager (PR 2) reconstructs an
// oidc.Provider from this struct.
type Config struct {
	IssuerURL    string   `json:"issuer_url"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`

	// Optional explicit endpoints. Used when the provider doesn't
	// ship full OIDC discovery (GitHub OAuth, custom legacy IdPs).
	// When all three are empty the manager falls back to discovery
	// against IssuerURL.
	AuthorizationEndpoint string `json:"authorization_endpoint,omitempty"`
	TokenEndpoint         string `json:"token_endpoint,omitempty"`
	UserInfoEndpoint      string `json:"userinfo_endpoint,omitempty"`
	JWKsURL               string `json:"jwks_url,omitempty"`
}

// PublicView returns Config with ClientSecret replaced by a
// has_client_secret boolean. Safe to log, render, or return from
// the admin API.
func (c Config) PublicView() PublicConfig {
	return PublicConfig{
		IssuerURL:             c.IssuerURL,
		ClientID:              c.ClientID,
		Scopes:                c.Scopes,
		HasClientSecret:       c.ClientSecret != "",
		AuthorizationEndpoint: c.AuthorizationEndpoint,
		TokenEndpoint:         c.TokenEndpoint,
		UserInfoEndpoint:      c.UserInfoEndpoint,
		JWKsURL:               c.JWKsURL,
	}
}

// PublicConfig is the redacted Config — same shape minus
// ClientSecret, plus a HasClientSecret flag for the dashboard.
type PublicConfig struct {
	IssuerURL             string   `json:"issuer_url"`
	ClientID              string   `json:"client_id"`
	Scopes                []string `json:"scopes,omitempty"`
	HasClientSecret       bool     `json:"has_client_secret"`
	AuthorizationEndpoint string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint         string   `json:"token_endpoint,omitempty"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint,omitempty"`
	JWKsURL               string   `json:"jwks_url,omitempty"`
}
