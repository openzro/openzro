package mdm

import (
	"time"
)

// MDMProvider is the GORM-managed row holding one vendor's
// credentials, encrypted at rest. PublicConfig carries the
// non-sensitive subset (tenant ID, base URL) so the dashboard can
// render the configuration without holding the secret.
type MDMProvider struct {
	ID           uint64       `gorm:"primaryKey;autoIncrement"`
	Name         string       `gorm:"size:128;not null"`
	Type         ProviderType `gorm:"size:32;not null;index"`
	Enabled      bool         `gorm:"not null;default:true"`
	PublicConfig []byte       `gorm:"type:bytea"`
	ConfigCipher []byte       `gorm:"type:bytea;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (MDMProvider) TableName() string { return "mdm_providers" }

// IntuneConfig is the full Intune configuration as stored
// (encrypted). Public projection omits ClientSecret.
type IntuneConfig struct {
	// TenantID is the Microsoft Entra (Azure AD) tenant ID — the
	// directory in which the openZro app registration lives.
	TenantID string `json:"tenant_id"`

	// ClientID is the openZro app registration's Application (client)
	// ID. Public; safe to display.
	ClientID string `json:"client_id"`

	// ClientSecret is the registered application secret. Sensitive.
	ClientSecret string `json:"client_secret,omitempty"`

	// Authority overrides the default login.microsoftonline.com.
	// Only useful for Azure China / Government clouds.
	Authority string `json:"authority,omitempty"`

	// StrictCompliance controls whether `complianceState=inGracePeriod`
	// is accepted as compliant. Default false (= permissive: peers in
	// the grace window keep network access while their config drifts
	// back into policy). Operators with strict regulatory requirements
	// flip it true to treat the grace period as non-compliant — peers
	// drop off the network the moment Intune flags them, even before
	// the grace window expires.
	StrictCompliance bool `json:"strict_compliance,omitempty"`
}

func (c IntuneConfig) PublicView() IntunePublicConfig {
	return IntunePublicConfig{
		TenantID:         c.TenantID,
		ClientID:         c.ClientID,
		Authority:        c.Authority,
		StrictCompliance: c.StrictCompliance,
		HasClientSecret:  c.ClientSecret != "",
	}
}

type IntunePublicConfig struct {
	TenantID         string `json:"tenant_id"`
	ClientID         string `json:"client_id"`
	Authority        string `json:"authority,omitempty"`
	StrictCompliance bool   `json:"strict_compliance,omitempty"`
	HasClientSecret  bool   `json:"has_client_secret"`
}

// SentinelOneConfig holds SentinelOne API credentials.
type SentinelOneConfig struct {
	ManagementURL string `json:"management_url"` // https://<tenant>.sentinelone.net
	APIToken      string `json:"api_token,omitempty"`
}

func (c SentinelOneConfig) PublicView() SentinelOnePublicConfig {
	return SentinelOnePublicConfig{
		ManagementURL: c.ManagementURL,
		HasAPIToken:   c.APIToken != "",
	}
}

type SentinelOnePublicConfig struct {
	ManagementURL string `json:"management_url"`
	HasAPIToken   bool   `json:"has_api_token"`
}

// HuntressConfig holds Huntress API credentials. Huntress uses
// HTTP Basic with a key + secret pair.
type HuntressConfig struct {
	APIKey    string `json:"api_key,omitempty"`
	APISecret string `json:"api_secret,omitempty"`
}

func (c HuntressConfig) PublicView() HuntressPublicConfig {
	return HuntressPublicConfig{
		HasCredentials: c.APIKey != "" && c.APISecret != "",
	}
}

type HuntressPublicConfig struct {
	HasCredentials bool `json:"has_credentials"`
}

// CrowdStrikeConfig holds Falcon API credentials. CrowdStrike uses
// OAuth2 client_credentials minted from a Falcon API client (Console
// → Support → API Clients and Keys), and a regional cloud bucket
// determines the base URL.
type CrowdStrikeConfig struct {
	// Cloud is the Falcon cloud region the tenant lives in. Empty
	// defaults to us-1 (the original public cloud). The full set of
	// recognized values is in cloudBaseURL().
	Cloud string `json:"cloud"`

	// ClientID is the Falcon API client ID. Public; safe to display.
	ClientID string `json:"client_id"`

	// ClientSecret is the Falcon API client secret. Sensitive.
	ClientSecret string `json:"client_secret,omitempty"`
}

func (c CrowdStrikeConfig) PublicView() CrowdStrikePublicConfig {
	return CrowdStrikePublicConfig{
		Cloud:           c.Cloud,
		ClientID:        c.ClientID,
		HasClientSecret: c.ClientSecret != "",
	}
}

type CrowdStrikePublicConfig struct {
	Cloud           string `json:"cloud"`
	ClientID        string `json:"client_id"`
	HasClientSecret bool   `json:"has_client_secret"`
}
