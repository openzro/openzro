package posture

import (
	"context"
	"errors"
	"fmt"

	nbpeer "github.com/openzro/openzro/management/server/peer"
)

// EndpointSecurityCheck delegates the per-peer compliance question to
// a vendor MDM/EDR (Intune / SentinelOne / Huntress / …) configured
// through the management's mdm package. The check itself is purely
// declarative; the runtime lookup goes through the MDMResolver
// supplied at evaluation time.
//
// Conceptually:
//
//	posture.EndpointSecurityCheck (this struct, persisted) stores
//	"which provider should answer + what counts as compliant".
//	The Manager wired into the validator resolves the provider and
//	returns a boolean.
//
// The Resolver is a function rather than a concrete dependency so
// the posture package stays free of import cycles with mdm and so
// tests can inject deterministic outcomes.
type EndpointSecurityCheck struct {
	// ProviderID is the row ID of an mdm.ProviderRow stored in the
	// management's primary DB. Operators pick from the configured
	// list when defining the check.
	ProviderID uint64 `json:"provider_id"`

	// FailOpen controls behavior when the vendor lookup itself fails
	// (network error, vendor outage, "device not found"). Default
	// false → the peer is treated as non-compliant on lookup failure.
	// Operators with strict compliance posture leave it false; those
	// who care more about availability than fail-closed flip it true.
	FailOpen bool `json:"fail_open,omitempty"`
}

// MDMResolver is the runtime hook the validator wires into the
// EndpointSecurityCheck. Posture package never imports mdm — the
// resolver function is the only boundary.
//
// The resolver receives the raw peer because some vendors (notably
// Intune via userPrincipalName) need attributes beyond the hostname
// to disambiguate the device. The closure is responsible for looking
// up auxiliary fields (e.g. translating peer.UserID → user email)
// before calling the underlying provider.
type MDMResolver func(ctx context.Context, providerID uint64, peer nbpeer.Peer) (bool, string, error)

// resolverContextKey is the context key used to carry an MDMResolver
// into the Check method. Tests use this; production wires through
// the package-level default below to avoid threading the resolver
// through every Account method.
type resolverContextKey struct{}

// WithMDMResolver returns a derived context carrying r so the
// EndpointSecurityCheck.Check method can use it.
func WithMDMResolver(ctx context.Context, r MDMResolver) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, resolverContextKey{}, r)
}

// defaultMDMResolver is the process-wide fallback. cmd/management.go
// sets this once at startup after constructing the mdm.Manager.
// Reads can race writes during boot; tests should use
// WithMDMResolver instead of touching this global.
var defaultMDMResolver MDMResolver

// SetDefaultMDMResolver installs the process-wide MDMResolver. Called
// once at startup. Subsequent calls replace the previous value;
// no concurrency protection beyond the caller's own ordering.
func SetDefaultMDMResolver(r MDMResolver) {
	defaultMDMResolver = r
}

// Name implements posture.Check.
func (c *EndpointSecurityCheck) Name() string { return "EndpointSecurityCheck" }

// Validate implements posture.Check.
func (c *EndpointSecurityCheck) Validate() error {
	if c.ProviderID == 0 {
		return errors.New("posture: endpoint security check requires a provider_id")
	}
	return nil
}

// Check implements posture.Check. Returns true when the peer is in
// good security standing per the configured provider, false on
// non-compliance or lookup failure (subject to FailOpen).
func (c *EndpointSecurityCheck) Check(ctx context.Context, peer nbpeer.Peer) (bool, error) {
	resolver, _ := ctx.Value(resolverContextKey{}).(MDMResolver)
	if resolver == nil {
		resolver = defaultMDMResolver
	}
	if resolver == nil {
		// No MDM resolver wired (manager not constructed or feature
		// disabled). Fail-closed by default; FailOpen lets operators
		// keep the network functional during a vendor outage.
		return c.FailOpen, fmt.Errorf("endpoint-security: MDM resolver not configured")
	}

	if deviceIdentifierFromPeer(peer) == "" {
		return c.FailOpen, fmt.Errorf("endpoint-security: peer has no usable device identifier (hostname missing)")
	}

	compliant, reason, err := resolver(ctx, c.ProviderID, peer)
	if err != nil {
		if c.FailOpen {
			return true, nil
		}
		return false, fmt.Errorf("endpoint-security: lookup failed: %w", err)
	}
	if !compliant {
		return false, fmt.Errorf("endpoint-security: %s", reason)
	}
	return true, nil
}

// deviceIdentifierFromPeer picks the best peer field to look up the
// device by. Vendors that key off hostname (Intune, SentinelOne,
// Huntress) use peer.Name; operators are encouraged to set the peer
// hostname to match the value the vendor's agent reports.
//
// A future enhancement: peer reports its serial number / MAC and the
// vendor query falls back to those. For now, hostname-based matching
// covers the common case.
func deviceIdentifierFromPeer(peer nbpeer.Peer) string {
	if peer.Meta.Hostname != "" {
		return peer.Meta.Hostname
	}
	return peer.Name
}
