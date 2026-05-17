package types

import (
	"time"
)

// Settings represents Account settings structure that can be modified via API and Dashboard
type Settings struct {
	// PeerLoginExpirationEnabled globally enables or disables peer login expiration
	PeerLoginExpirationEnabled bool

	// PeerLoginExpiration is a setting that indicates when peer login expires.
	// Applies to all peers that have Peer.LoginExpirationEnabled set to true.
	PeerLoginExpiration time.Duration

	// PeerInactivityExpirationEnabled globally enables or disables peer inactivity expiration
	PeerInactivityExpirationEnabled bool

	// PeerInactivityExpiration is a setting that indicates when peer inactivity expires.
	// Applies to all peers that have Peer.PeerInactivityExpirationEnabled set to true.
	PeerInactivityExpiration time.Duration

	// RegularUsersViewBlocked allows to block regular users from viewing even their own peers and some UI elements
	RegularUsersViewBlocked bool

	// GroupsPropagationEnabled allows to propagate auto groups from the user to the peer
	GroupsPropagationEnabled bool

	// JWTGroupsEnabled allows extract groups from JWT claim, which name defined in the JWTGroupsClaimName
	// and add it to account groups.
	JWTGroupsEnabled bool

	// JWTGroupsClaimName from which we extract groups name to add it to account groups
	JWTGroupsClaimName string

	// JWTAllowGroups list of groups to which users are allowed access
	JWTAllowGroups []string `gorm:"serializer:json"`

	// RoutingPeerDNSResolutionEnabled enabled the DNS resolution on the routing peers
	RoutingPeerDNSResolutionEnabled bool

	// DNSDomain is the custom domain for that account
	DNSDomain string

	// ClientUpdateTargetVersion is the desktop client release the
	// operator wants the fleet on (openZro #5, management-driven). Empty
	// => no directive (clients do nothing). Management conveys only this
	// decision over Sync; the client still downloads+verifies the
	// signed package itself.
	ClientUpdateTargetVersion string

	// ClientUpdateForce, when true, installs silently in the background
	// without prompting the user; otherwise the client surfaces a
	// prompt.
	ClientUpdateForce bool

	// ClientUpdate{Target,Exclude}* scope the directive to a SUBSET of
	// the fleet, evaluated per-peer SERVER-SIDE (openZro #5 Q2). They
	// are never sent to the client — the client only ever receives the
	// resolved UpdateConfig (or nothing). Precedence (signed-off
	// spec): ExcludeGroups beats everything (incl. an explicit peer —
	// infra/gateway safety); an explicit TargetPeers entry pierces the
	// percentage ring; TargetGroups membership is subject to the ring.
	// Empty TargetGroups AND empty TargetPeers => whole fleet (the
	// pre-Q2 behaviour; never "nobody").

	// ClientUpdateTargetGroups: member peers of any of these groups are
	// in scope (subject to the rollout ring). Empty = no group
	// constraint.
	ClientUpdateTargetGroups []string `gorm:"serializer:json"`

	// ClientUpdateTargetPeers: explicit peer IDs always in scope
	// (canary / break-glass) — these pierce the rollout ring but NOT
	// ClientUpdateExcludeGroups.
	ClientUpdateTargetPeers []string `gorm:"serializer:json"`

	// ClientUpdateExcludeGroups: member peers NEVER receive the
	// directive, even if explicitly listed in TargetPeers. Motivating
	// case mirrors AdmissionExemptGroups: routing/gateway/server peers
	// that must never silently self-update.
	ClientUpdateExcludeGroups []string `gorm:"serializer:json"`

	// ClientUpdateRolloutPercent is the server-side staged ring,
	// 0..100. Pointer so absent (nil = no ring, everyone in scope)
	// is distinct from an explicit 0 (nobody — fail-closed, same
	// nil-vs-0 discipline as the manifest StagedRollout). Bucketing is
	// a pure, cluster-deterministic hash of the peer's stable key.
	ClientUpdateRolloutPercent *int

	// Extra is a dictionary of Account settings
	Extra *ExtraSettings `gorm:"embedded;embeddedPrefix:extra_"`

	// LazyConnectionEnabled indicates if the experimental feature is enabled or disabled
	LazyConnectionEnabled bool `gorm:"default:false"`

	// AdmissionEnforcementEnabled gates peer login/registration on the
	// AdmissionPostureChecks list. When false, the list is ignored and
	// only per-policy posture checks apply (current behavior). When
	// true, a peer that fails any of the listed posture checks is
	// rejected at the gRPC Login boundary with PermissionDenied — it
	// cannot enter the mesh at all. Required for regulated tenants
	// (Bacen 4.893 / Circular 3.909) that need provable endpoint
	// admission control with an audit trail.
	AdmissionEnforcementEnabled bool `gorm:"default:false"`

	// AdmissionPostureChecks lists posture check IDs evaluated against
	// every peer at Login/Sync time when AdmissionEnforcementEnabled
	// is true. Order is irrelevant; ALL listed checks must pass.
	AdmissionPostureChecks []string `gorm:"serializer:json"`

	// AdmissionExemptGroups lists Group IDs whose member peers skip
	// the admission gate entirely. Motivating case: routing /
	// gateway peers — server-side machines (cloud VMs, K8s pods,
	// on-prem servers) that are part of the mesh but never enrol
	// in MDM/EDR. Without this, an account that turns admission on
	// would lock its own infrastructure out the moment a posture
	// check fires for a peer that has no MDM agent to report from.
	//
	// Membership in ANY exempt group is sufficient (OR semantics).
	// Changes are audited via activity.AdmissionExemptGroupsUpdated.
	AdmissionExemptGroups []string `gorm:"serializer:json"`
}

// Copy copies the Settings struct
func (s *Settings) Copy() *Settings {
	settings := &Settings{
		PeerLoginExpirationEnabled: s.PeerLoginExpirationEnabled,
		PeerLoginExpiration:        s.PeerLoginExpiration,
		JWTGroupsEnabled:           s.JWTGroupsEnabled,
		JWTGroupsClaimName:         s.JWTGroupsClaimName,
		GroupsPropagationEnabled:   s.GroupsPropagationEnabled,
		JWTAllowGroups:             s.JWTAllowGroups,
		RegularUsersViewBlocked:    s.RegularUsersViewBlocked,

		PeerInactivityExpirationEnabled: s.PeerInactivityExpirationEnabled,
		PeerInactivityExpiration:        s.PeerInactivityExpiration,

		RoutingPeerDNSResolutionEnabled: s.RoutingPeerDNSResolutionEnabled,
		LazyConnectionEnabled:           s.LazyConnectionEnabled,
		DNSDomain:                       s.DNSDomain,
		AdmissionEnforcementEnabled:     s.AdmissionEnforcementEnabled,
		AdmissionPostureChecks:          append([]string(nil), s.AdmissionPostureChecks...),
		AdmissionExemptGroups:           append([]string(nil), s.AdmissionExemptGroups...),

		ClientUpdateTargetVersion: s.ClientUpdateTargetVersion,
		ClientUpdateForce:         s.ClientUpdateForce,
		ClientUpdateTargetGroups:  append([]string(nil), s.ClientUpdateTargetGroups...),
		ClientUpdateTargetPeers:   append([]string(nil), s.ClientUpdateTargetPeers...),
		ClientUpdateExcludeGroups: append([]string(nil), s.ClientUpdateExcludeGroups...),
	}
	if s.ClientUpdateRolloutPercent != nil {
		// Deep-copy the pointer so a mutation through the copy cannot
		// race/alias the original (Copy() callers treat it as owned).
		v := *s.ClientUpdateRolloutPercent
		settings.ClientUpdateRolloutPercent = &v
	}
	if s.Extra != nil {
		settings.Extra = s.Extra.Copy()
	}
	return settings
}

type ExtraSettings struct {
	// PeerApprovalEnabled enables or disables the need for peers bo be approved by an administrator
	PeerApprovalEnabled bool

	// IntegratedValidator is the string enum for the integrated validator type
	IntegratedValidator string
	// IntegratedValidatorGroups list of group IDs to be used with integrated approval configurations
	IntegratedValidatorGroups []string `gorm:"serializer:json"`

	// Flow* fields ride on `extra_settings.*` columns now (one bool per
	// flag). Upstream marked them `gorm:"-"` (transient, never persisted)
	// because the open-source NetBird build had no Flow store and the
	// fields existed only to seed the in-memory dispatcher at boot. Our
	// dashboard surfaces them as toggles in NetworkSettingsTab, so they
	// have to round-trip the DB — without persistence the user-flow was
	// "toggle on → notify success → refresh page → toggle reverts to off"
	// (no save button activation either, since the dirty-tracker doesn't
	// see a server-side change). Removing the `gorm:"-"` lets GORM
	// auto-migrate a column per flag and stash the bool there.
	FlowEnabled              bool
	FlowPacketCounterEnabled bool
	FlowENCollectionEnabled  bool
	FlowDnsCollectionEnabled bool

	// FlowEventsGroups optionally restricts traffic event capture to peers
	// whose own groups intersect this list. Empty list (default) means
	// every peer reports flow events while FlowEnabled is true. The
	// filter is enforced peer-side via FlowConfig.groups in the Sync
	// response (see management.proto), so excluded peers never spend
	// CPU on capture, never queue events, and never push them to
	// management — bandwidth saved end-to-end. Slice is JSON-encoded
	// in the extra_settings column rather than a join table because
	// the cardinality is small (operators typically pick 1–5 groups
	// when they care to scope at all).
	FlowEventsGroups []string `gorm:"serializer:json"`

	// FlowDisableDefaultPortFilter turns OFF the client-side built-in
	// skip list of broadcast/discovery ports (SSDP-1900, mDNS-5353,
	// NetBIOS-137/138, LLMNR-5355). Default false: an enterprise VPN
	// almost never wants those events polluting traffic logs — they're
	// high-volume "device A is here" chatter that no audit policy
	// cares about, and on a typical workstation they account for the
	// majority of recorded events. Operators who DO want to track
	// discovery traffic flip this to true and the built-in skip list
	// is bypassed.
	FlowDisableDefaultPortFilter bool

	// FlowExcludedPorts is an operator-defined list of (port, protocol)
	// pairs to drop on the client side, ADDED to the built-in skip
	// list (or replacing it entirely when FlowDisableDefaultPortFilter
	// is true). Useful for environments with extra protocols generating
	// uninteresting flow events — internal heartbeats, custom multicast,
	// app-specific service discovery on a non-IANA port, etc.
	FlowExcludedPorts []FlowPortFilter `gorm:"serializer:json"`

	// FlowTrafficDefaultRange pre-fills the date filter on the Flow
	// Traffic page so a fresh dashboard load doesn't hammer the API
	// for everything that fits in the 10 000-event cap. Stored as a
	// short string (enum). Recognised values:
	//
	//   "1h"   "6h"   "24h"   "7d"   "30d"   "all" (or empty)
	//
	// Empty / unrecognised values keep the pre-setting behaviour
	// (no time filter — bounded only by the hot store retention and
	// the API's 10 000-event ceiling). Operators can still override
	// per-session via the date picker; this is just the initial value.
	FlowTrafficDefaultRange string
}

// FlowPortFilter is a single (port, protocol) pair the client drops at
// the conntrack-event boundary before queueing for the management.
// protocol is "tcp" / "udp" / "any" (case-insensitive on the wire,
// normalised to lowercase before propagation).
type FlowPortFilter struct {
	Port     uint32 `json:"port"`
	Protocol string `json:"protocol"`
}

// Copy copies the ExtraSettings struct
func (e *ExtraSettings) Copy() *ExtraSettings {
	return &ExtraSettings{
		PeerApprovalEnabled:          e.PeerApprovalEnabled,
		IntegratedValidatorGroups:    append([]string(nil), e.IntegratedValidatorGroups...),
		IntegratedValidator:          e.IntegratedValidator,
		FlowEnabled:                  e.FlowEnabled,
		FlowPacketCounterEnabled:     e.FlowPacketCounterEnabled,
		FlowENCollectionEnabled:      e.FlowENCollectionEnabled,
		FlowDnsCollectionEnabled:     e.FlowDnsCollectionEnabled,
		FlowEventsGroups:             append([]string(nil), e.FlowEventsGroups...),
		FlowDisableDefaultPortFilter: e.FlowDisableDefaultPortFilter,
		FlowExcludedPorts:            append([]FlowPortFilter(nil), e.FlowExcludedPorts...),
		FlowTrafficDefaultRange:      e.FlowTrafficDefaultRange,
	}
}
