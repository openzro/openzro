// Package mdm holds the framework for MDM/EDR vendor integrations
// that openZro consults during peer posture validation.
//
// Architecture (per ADR-0003 — to be written):
//
//	types.User / types.Peer
//	       │
//	       │ posture eval
//	       ▼
//	posture.EndpointSecurityCheck
//	       │ delegate
//	       ▼
//	mdm.Manager
//	       │ resolve provider by ID
//	       ▼
//	mdm.Provider (Intune | SentinelOne | Huntress | …)
//	       │ HTTP
//	       ▼
//	vendor SaaS
//
// The framework does not import any specific vendor SDK at the
// posture-check level — vendors plug in by satisfying the Provider
// interface. This keeps the posture validation hot path decoupled
// from the specific vendor's quirks.
//
// Credentials live encrypted in the management's primary DB (same
// envelope as flow_exports). The cache layer in cache.go avoids
// hammering the vendor API on every peer sync.
package mdm

import (
	"context"
	"errors"
	"time"
)

// ProviderType identifies the vendor a Provider implements. Stable
// enum; do not renumber. Operators select a value here when wiring
// credentials through the admin API.
type ProviderType string

const (
	TypeIntune      ProviderType = "intune"
	TypeSentinelOne ProviderType = "sentinelone"
	TypeHuntress    ProviderType = "huntress"
	TypeCrowdStrike ProviderType = "crowdstrike"
)

// DeviceStatus is the vendor-agnostic projection of a device's
// security posture — what the posture check reads to decide whether
// the peer is allowed in the network.
type DeviceStatus struct {
	// Found is false when the vendor has no record of this device.
	// Posture checks treat that as "non-compliant" by default but a
	// permissive policy may opt-in to fail-open.
	Found bool

	// Compliant is the vendor's own answer to "is this device in
	// good standing right now?" — what each vendor means by it
	// is documented per-driver.
	Compliant bool

	// Reason is a short free-text human-readable explanation of why
	// the device is non-compliant. Surfaced in the dashboard.
	Reason string

	// LastChecked is the moment we last got a fresh answer from the
	// vendor. The cache layer fills this in from its own clock.
	LastChecked time.Time
}

// DeviceLookup is the set of hints the caller supplies to a Provider
// for the device search. The hostname is the universal key — every
// supported vendor can answer with just that. UserEmail is optional
// and only some vendors (notably Intune, which keys devices by
// userPrincipalName) use it to disambiguate when the same hostname
// appears on multiple users' enrolled devices (renames, hand-me-down
// laptops, shared hardware). Vendors that don't model per-user device
// ownership simply ignore the field.
//
// Future fields (SerialNumber, MACAddress, AzureADDeviceID) can be
// added without breaking existing callers — implementations should
// only read what they need.
type DeviceLookup struct {
	// Hostname is the OS hostname the openZro agent reported, normalised
	// to whatever the local vendor's MDM agent uses. Required.
	Hostname string

	// UserEmail is the address of the openZro user the peer was
	// registered for, when known. Empty for peers registered via setup
	// keys without user attribution, or for vendors that don't keep
	// per-user device ownership.
	UserEmail string

	// OS is the peer's runtime GOOS — "linux", "darwin", "windows", etc.
	// Used by vendors to disambiguate fallback matches (e.g. when the
	// combined hostname+user query misses and the user-only fallback
	// would otherwise return a sibling device owned by the same user
	// but on a different OS — classic cross-device leak). Empty for
	// legacy callers; implementations must treat empty as "skip the
	// OS check" to preserve backwards-compatible behavior.
	OS string
}

// Provider is the per-vendor interface. Implementations live in
// sibling files (intune.go, sentinelone.go, huntress.go) and are
// constructed from credentials persisted in the mdm.Store.
type Provider interface {
	// Type returns the vendor identifier (matches the persisted
	// provider row's Type column).
	Type() ProviderType

	// GetDeviceStatus looks up the device using the hints in lookup.
	// Hostname is always present; UserEmail may be empty. Returning
	// ErrUnsupported is allowed for vendors that match by a different
	// attribute and the caller did not supply that attribute.
	//
	// The caller is responsible for caching; implementations should
	// not introduce their own per-call cache.
	GetDeviceStatus(ctx context.Context, lookup DeviceLookup) (DeviceStatus, error)

	// Close releases resources (HTTP keep-alive pools). Safe to call
	// multiple times.
	Close() error
}

// Sentinel errors. Mapped to user-visible reasons in the posture
// check layer.
var (
	ErrUnsupported   = errors.New("mdm: vendor does not support this lookup mode")
	ErrNotConfigured = errors.New("mdm: provider not configured")
)
