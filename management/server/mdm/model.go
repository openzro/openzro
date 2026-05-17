package mdm

import (
	"time"
)

// defaultRefreshIntervalMinutes is the fallback for rows that
// predate the field (zero on read) and the seed value at create
// time when the operator leaves the form blank. 5 matches the
// hard-coded TTL the cache shipped with before this knob existed —
// existing tenants see no behavior change on first boot.
const defaultRefreshIntervalMinutes = 5

// MDMProvider is the GORM-managed row holding one vendor's
// credentials, encrypted at rest. PublicConfig carries the
// non-sensitive subset (tenant ID, base URL) so the dashboard can
// render the configuration without holding the secret.
type MDMProvider struct {
	ID      uint64       `gorm:"primaryKey;autoIncrement"`
	Name    string       `gorm:"size:128;not null"`
	Type    ProviderType `gorm:"size:32;not null;index"`
	Enabled bool         `gorm:"not null;default:true"`

	// RefreshIntervalMinutes is how often (in minutes) the cache for
	// this provider expires and the background refresh worker
	// re-queries the vendor for every device it tracks. The same
	// value drives both: lazy cache invalidation (next Sync after
	// expiry forces a fresh lookup) and the proactive worker that
	// keeps the cache warm so a peer Sync after a long idle window
	// doesn't pay the cold-start latency. Bounded 1-60 at the API
	// boundary; 0 on read means "row predates this field" and the
	// resolver in ResolvedRefreshInterval() falls back to 5 minutes
	// — same as the pre-knob hard-coded TTL, so existing tenants see
	// no behavior change on upgrade.
	RefreshIntervalMinutes uint16 `gorm:"not null;default:5"`

	PublicConfig []byte `gorm:"type:bytea"`
	ConfigCipher []byte `gorm:"type:bytea;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ResolvedRefreshInterval returns the configured interval as a
// time.Duration, falling back to the default for legacy rows that
// stored 0 (pre-knob) or values that bypassed the API layer's
// validation. Always non-zero.
func (p MDMProvider) ResolvedRefreshInterval() time.Duration {
	m := p.RefreshIntervalMinutes
	if m == 0 {
		m = defaultRefreshIntervalMinutes
	}
	return time.Duration(m) * time.Minute
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

// SentinelOneCompliance is the operator-tunable gate set evaluated
// against a SentinelOne agent record. It mirrors the configurable
// conditions an operator expects from an EDR posture integration
// (threat tolerance, disk encryption, firewall, console
// connectivity, agent version floor, check-in recency).
//
// The zero value is the legacy-safe baseline: NONE of these
// additive conditions is enforced. The three always-on signals
// (decommissioned / infected / inactive) are NOT represented here
// because they are not operator-tunable — they always block, and
// they are exactly the enforcement an existing SentinelOne provider
// row already has. Keeping them implicit means upgrading a provider
// row that predates this struct changes nothing (no field present
// in its stored JSON => zero value => same behavior).
type SentinelOneCompliance struct {
	// MaxActiveThreats, when non-nil, blocks a device whose active
	// threat count exceeds the value. nil = no threshold (the
	// always-on infected gate still blocks a confirmed infection).
	// A pointer so "0 allowed" (zero tolerance) is distinguishable
	// from "not configured".
	MaxActiveThreats *int `json:"max_active_threats,omitempty"`

	// RequireDiskEncryption blocks a device that does not report
	// disk encryption enabled (S1 encryptedApplications).
	RequireDiskEncryption bool `json:"require_disk_encryption,omitempty"`

	// RequireFirewall blocks a device whose host firewall is not
	// enabled (S1 firewallEnabled).
	RequireFirewall bool `json:"require_firewall,omitempty"`

	// RequireNetworkConnected blocks an agent not connected to the
	// SentinelOne console (S1 networkStatus != "connected").
	// Distinct from "active": an agent can be locally active yet
	// cut off from the console, meaning its posture is stale.
	RequireNetworkConnected bool `json:"require_network_connected,omitempty"`

	// MinAgentVersion is a semver floor; an agent older than this is
	// blocked. Empty = no version floor.
	MinAgentVersion string `json:"min_agent_version,omitempty"`

	// SyncWindowMinutes blocks an agent whose last check-in is older
	// than this many minutes. 0 = no recency requirement.
	SyncWindowMinutes int `json:"sync_window_minutes,omitempty"`
}

// SentinelOneConfig holds SentinelOne API credentials plus the
// operator-tunable compliance gates.
type SentinelOneConfig struct {
	ManagementURL string                `json:"management_url"` // https://<tenant>.sentinelone.net
	APIToken      string                `json:"api_token,omitempty"`
	Compliance    SentinelOneCompliance `json:"compliance,omitempty"`
}

func (c SentinelOneConfig) PublicView() SentinelOnePublicConfig {
	return SentinelOnePublicConfig{
		ManagementURL: c.ManagementURL,
		HasAPIToken:   c.APIToken != "",
		// Compliance carries no secret — surface it verbatim so the
		// dashboard can render the current toggle state.
		Compliance: c.Compliance,
	}
}

type SentinelOnePublicConfig struct {
	ManagementURL string                `json:"management_url"`
	HasAPIToken   bool                  `json:"has_api_token"`
	Compliance    SentinelOneCompliance `json:"compliance,omitempty"`
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
