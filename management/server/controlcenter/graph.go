// Package controlcenter builds the read-only access-graph DTO that
// answers, for a focus node, what it reaches right now, through which
// policy (or route default-permit), on which protocols/ports, and what
// is policy-permitted but posture-blocked.
//
// Clean-room (BSD-3 tree, openZro): every type and the adapter logic
// here are designed against openZro's own enforcement engine
// (Account.GetPeerConnectionResources / GetRoutesToSync /
// GetPeerRoutesFirewallRules) and the decisions recorded in
// docs/adr/0017-control-center-access-graph.md. No upstream NetBird
// management/ code was consulted or ported.
package controlcenter

import "errors"

// Typed errors so the HTTP layer can map precisely instead of
// treating every non-status error as 404 (#50 Finding 5). Wrap with
// %w and check with errors.Is.
var (
	// ErrFocusNotFound — the requested focus peer/group does not
	// exist in the account (→ 404).
	ErrFocusNotFound = errors.New("focus not found")
	// ErrUnsupportedFocus — the view is not peer/user/group/network
	// (→ 400).
	ErrUnsupportedFocus = errors.New("unsupported focus type")
)

// FocusType is the focus tab of the v2 topology view. Policy is always
// the middle pivot column; only User adds a Peers column; Network is
// the inverse fan-in (ADR-0017 2026-05-18b).
type FocusType string

const (
	FocusPeer    FocusType = "peer"
	FocusUser    FocusType = "user"
	FocusGroup   FocusType = "group"
	FocusNetwork FocusType = "network"
)

// Focus identifies the node the graph is built around.
type Focus struct {
	Type FocusType `json:"type"`
	ID   string    `json:"id"`
}

// NodeKind tags a graph node. focus is the centred node; the rest are
// the columns it fans out into. The v2 topology projection emits
// columns of these kinds left→right (ADR-0017 2026-05-18b):
//
//	peer  : focus(peer)  → policy → {group|network_resource|route}
//	user  : focus(user)  → peer   → policy → {group|network_resource|route}
//	group : focus(group) → policy → {group|network_resource|route}
//	network: group       → policy → focus(network_resource)  (inverse fan-in)
type NodeKind string

const (
	NodeFocus           NodeKind = "focus"
	NodePolicy          NodeKind = "policy"
	NodeGroup           NodeKind = "group"
	NodePeer            NodeKind = "peer"
	NodeUser            NodeKind = "user"
	NodeRoute           NodeKind = "route"
	NodeNetworkResource NodeKind = "network_resource"
	NodeNetwork         NodeKind = "network"
)

// EdgeState distinguishes enforced reach from reach a policy permits
// but posture blocks (ADR-0017 D1.2). It is never collapsed into
// "no edge" — the absence of an edge means "no policy", which is a
// different audit answer.
type EdgeState string

const (
	EdgeEnforced       EdgeState = "enforced"
	EdgePostureBlocked EdgeState = "posture_blocked"
)

// PermitSource records why an edge is permitted. Not every route edge
// is backed by an editable policy: a route with no AccessControlGroups
// falls to a route default-permit, which has no PolicyID (ADR-0017
// D1.1 / Point 3).
type PermitSource string

const (
	PermitPolicy       PermitSource = "policy"
	PermitRouteDefault PermitSource = "route_default_permit"
	// PermitRouterLocal — the focus IS the router serving this route,
	// so it reaches the routed network as its own gateway. This is
	// NOT route_default_permit (the route may carry
	// AccessControlGroups that gate OTHER clients); it is honestly
	// labelled as infrastructure-local reach (#50-r2 semantic note,
	// owner-decided 2026-05-17).
	PermitRouterLocal PermitSource = "router_local"
	// PermitIdentity — a structural identity/ownership edge, NOT a
	// policy permit: the v2 User→Peer edge ("these are the user's
	// machines"). It carries no PolicyID. Formalised so the wire
	// contract enumerates it instead of an empty-string sentinel the
	// frontend matched by accident (#39 v2 review, finding 3).
	PermitIdentity PermitSource = "identity"
)

// EdgeDirection is the traffic direction the permitting rule grants.
type EdgeDirection string

const (
	DirectionIn            EdgeDirection = "in"
	DirectionOut           EdgeDirection = "out"
	DirectionBidirectional EdgeDirection = "bidirectional"
)

// Node is one vertex of the access graph.
type Node struct {
	ID    string            `json:"id"`
	Kind  NodeKind          `json:"kind"`
	Label string            `json:"label"`
	Meta  map[string]string `json:"meta,omitempty"`
}

// Edge is a permitted (or posture-blocked) reach relation. Ports and
// SourceRanges are string slices so port ranges and CIDRs round-trip
// without lossy flattening (ADR-0017 minimum envelope).
type Edge struct {
	From         string            `json:"from"`
	To           string            `json:"to"`
	PermitSource PermitSource      `json:"permitSource"`
	PolicyID     string            `json:"policyId,omitempty"`
	PolicyName   string            `json:"policyName,omitempty"`
	Protocol     string            `json:"protocol"`
	Ports        []string          `json:"ports,omitempty"`
	SourceRanges []string          `json:"sourceRanges,omitempty"`
	Direction    EdgeDirection     `json:"direction"`
	State        EdgeState         `json:"state"`
	Meta         map[string]string `json:"meta,omitempty"`
}

// GraphDTO is the read-only access-graph response for one focus node.
type GraphDTO struct {
	Focus Focus  `json:"focus"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}
