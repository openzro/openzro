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

// FocusType is the kind of node the graph is centred on. v1 ships peer
// and group focus; network/user focus are an ADR-0017 v2 follow-up.
type FocusType string

const (
	FocusPeer  FocusType = "peer"
	FocusGroup FocusType = "group"
)

// Focus identifies the node the graph is built around.
type Focus struct {
	Type FocusType `json:"type"`
	ID   string    `json:"id"`
}

// NodeKind tags a graph node. focus is the centred node; the rest are
// what it relates to.
type NodeKind string

const (
	NodeFocus           NodeKind = "focus"
	NodePolicy          NodeKind = "policy"
	NodeGroup           NodeKind = "group"
	NodePeer            NodeKind = "peer"
	NodeRoute           NodeKind = "route"
	NodeNetworkResource NodeKind = "network_resource"
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
