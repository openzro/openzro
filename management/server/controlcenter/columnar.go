// Package controlcenter — v2 columnar topology projection.
//
// The v2 Control Center is a single topology view with four focus
// tabs (ADR-0017 2026-05-18b). Policy is always the middle pivot
// column. The projection emits nodes tagged with meta["column"] and
// edges connecting adjacent columns left→right:
//
//	peer    : focus(peer)  → policy → resource
//	user    : focus(user)  → peer   → policy → resource
//	group   : focus(group) → policy → resource
//	network : group        → policy → focus(network_resource)   [inverse fan-in]
//
// State is the engine-truth posture distinction (ADR-0017 D1.2):
// "enforced" when the source passes the policy's source posture
// checks, "posture_blocked" when a policy permits the source but a
// posture check denies it. v2 is a policy-topology projection — it
// surfaces what policies grant, posture-aware; it does NOT gate on
// live peer validation the way v1 reach did (a deliberate, documented
// re-scope: ADR-0017 2026-05-18b — v2 replaces v1).
//
// Clean-room (BSD-3): every projection here is designed against
// openZro's own Account/Policy/Group/NetworkResource model and
// types.EvaluateAdmission; no upstream NetBird management/ code was
// consulted or ported.
package controlcenter

import (
	"context"
	"fmt"
	"sort"
	"strings"

	resourceTypes "github.com/openzro/openzro/management/server/networks/resources/types"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/types"
)

// Column tags (meta["column"]). The dashboard maps these to the X
// lanes per active tab; the backend only names them semantically.
const (
	colFocus     = "focus"
	colPeers     = "peers"
	colPolicies  = "policies"
	colResources = "resources"
	colGroups    = "groups"
)

// BuildGraph derives the v2 columnar topology for a focus node from
// openZro's own policy model. v2 is a policy-topology projection, not
// a live-dataplane reach walk, so it deliberately does NOT take a
// validated-peers set (ADR-0017 2026-05-18b / 2026-05-18c).
//
// Clean-room (BSD-3): ADR-0017; no upstream NetBird management/ code
// consulted or ported.
func BuildGraph(ctx context.Context, acc *types.Account, focus Focus) (*GraphDTO, error) {
	switch focus.Type {
	case FocusPeer:
		return buildPeerFocus(ctx, acc, focus)
	case FocusUser:
		return buildUserFocus(ctx, acc, focus)
	case FocusGroup:
		return buildGroupFocus(ctx, acc, focus)
	case FocusNetwork:
		return buildNetworkFocus(ctx, acc, focus)
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupportedFocus, focus.Type)
	}
}

// colBuilder accumulates nodes/edges and de-dups + sorts on finalize
// so the DTO is deterministic (stable tests, stable wire).
type colBuilder struct {
	g       *GraphDTO
	nodes   map[string]Node
	edgeAgg map[string]*Edge
	posture map[string]*posture.Checks
	// resByGroup is groupID → the network resources that group
	// contains, resolved the engine way (Group.Resources) and built
	// ONCE per graph. NetworkResource.GroupIDs is gorm:"-" and empty
	// on the graph-loaded account, so a source view that keyed off it
	// silently dropped resources a policy really reaches (#39 v2
	// review, finding 1).
	resByGroup map[string][]*resourceTypes.NetworkResource
	// resByID is the request-scoped resource lookup (replaces the old
	// per-call linear scan of acc.NetworkResources — #39 v2 review R5
	// finding 3).
	resByID map[string]*resourceTypes.NetworkResource
	// polByGroup is sourceGroupID → the (policy,rule) pairs whose
	// Sources include that group, built ONCE per graph. Turns
	// projectSource from O(all policies × rules) per anchor into
	// O(anchor groups × matching rules) (R5 finding 1).
	polByGroup map[string][]polRule
	// gatherCache memoises the deduped (policy,rule) set for a source
	// group-set signature so user focus does not re-walk the index
	// for peers that share the same group membership (R5 finding 4).
	// The posture STATE stays per-peer (peerState) — irreducible and
	// correct; only the structural match set is shared.
	gatherCache map[string][]polRule
}

// polRule is one (policy, rule) pair in the source-group index.
type polRule struct {
	pol *types.Policy
	r   *types.PolicyRule
}

func newColBuilder(acc *types.Account, focus Focus) *colBuilder {
	byID := map[string]*resourceTypes.NetworkResource{}
	for _, res := range acc.NetworkResources {
		if res != nil && res.Enabled {
			byID[res.ID] = res
		}
	}
	resByGroup := map[string][]*resourceTypes.NetworkResource{}
	for gid, g := range acc.Groups {
		if g == nil {
			continue
		}
		for _, r := range g.Resources {
			if res := byID[r.ID]; res != nil {
				resByGroup[gid] = append(resByGroup[gid], res)
			}
		}
	}
	// Source-group → (policy,rule) index, built once in deterministic
	// account/rule order. finalize() re-sorts nodes/edges, so the
	// emission order this index produces never affects the DTO — the
	// contract/enum tests still pin the wire shape.
	polByGroup := map[string][]polRule{}
	for _, pol := range acc.Policies {
		if pol == nil || !pol.Enabled {
			continue
		}
		for _, r := range pol.Rules {
			if r == nil || !r.Enabled {
				continue
			}
			for _, gid := range r.Sources {
				polByGroup[gid] = append(polByGroup[gid], polRule{pol: pol, r: r})
			}
		}
	}

	return &colBuilder{
		g:           &GraphDTO{Focus: focus},
		nodes:       map[string]Node{},
		edgeAgg:     map[string]*Edge{},
		posture:     postureChecksMap(acc),
		resByGroup:  resByGroup,
		resByID:     byID,
		polByGroup:  polByGroup,
		gatherCache: map[string][]polRule{},
	}
}

// gatherPolRules returns the deduped (policy,rule) pairs whose
// Sources include any of srcGroups, memoised by the sorted group-set
// signature so user focus shares the walk across peers with the same
// group membership (R5 finding 4).
func (b *colBuilder) gatherPolRules(srcGroups []string) []polRule {
	sorted := append([]string(nil), srcGroups...)
	sort.Strings(sorted)
	sig := strings.Join(sorted, "\x00")
	if cached, ok := b.gatherCache[sig]; ok {
		return cached
	}
	var out []polRule
	seen := map[*types.PolicyRule]struct{}{}
	for _, gid := range sorted {
		for _, pr := range b.polByGroup[gid] {
			if _, dup := seen[pr.r]; dup {
				continue
			}
			seen[pr.r] = struct{}{}
			out = append(out, pr)
		}
	}
	b.gatherCache[sig] = out
	return out
}

// node is a first-write-wins insert (a node reachable via two rules is
// one vertex). column is always recorded so the dashboard can lane it.
func (b *colBuilder) node(id string, kind NodeKind, label, column string, meta map[string]string) {
	if _, ok := b.nodes[id]; ok {
		return
	}
	if meta == nil {
		meta = map[string]string{}
	}
	for k, v := range meta {
		if v == "" {
			delete(meta, k)
		}
	}
	meta["column"] = column
	b.nodes[id] = Node{ID: id, Kind: kind, Label: label, Meta: meta}
}

// policyNode upserts the policy vertex and unions the per-rule port
// label, so a policy matched by several rules shows one combined tag.
func (b *colBuilder) policyNode(pol *types.Policy, r *types.PolicyRule) {
	id := "policy:" + pol.ID
	pl := portLabel(r)
	if n, ok := b.nodes[id]; ok {
		n.Meta["port"] = mergePortLabel(n.Meta["port"], pl)
		b.nodes[id] = n
		return
	}
	b.nodes[id] = Node{
		ID: id, Kind: NodePolicy, Label: pol.Name,
		Meta: map[string]string{"column": colPolicies, "port": pl},
	}
}

// edge merges rules that differ only by direction and unions ports,
// keyed by from|to|policy|protocol|state. An enforced and a
// posture_blocked edge for the same pair stay distinct (ADR-0017
// D1.2: posture_blocked is never collapsed into enforced).
func (b *colBuilder) edge(e *Edge) {
	key := e.From + "|" + e.To + "|" + e.PolicyID + "|" + e.Protocol + "|" + string(e.State)
	cur, ok := b.edgeAgg[key]
	if !ok {
		b.edgeAgg[key] = e
		return
	}
	mergeDirectionValue(cur, e.Direction)
	for _, p := range e.Ports {
		if !contains(cur.Ports, p) {
			cur.Ports = append(cur.Ports, p)
		}
	}
}

func (b *colBuilder) finalize() *GraphDTO {
	// Always emit non-nil slices: a nil Go slice marshals as JSON
	// `null`, and the dashboard does `graph.edges.length` directly. A
	// focus with no matching policy (e.g. a user whose peers are in no
	// policy source) legitimately has zero edges — that must be `[]`,
	// never `null`, so the contract stays an array (#39 v2 review).
	b.g.Nodes = make([]Node, 0, len(b.nodes))
	for _, n := range b.nodes {
		b.g.Nodes = append(b.g.Nodes, n)
	}
	sort.Slice(b.g.Nodes, func(i, j int) bool { return b.g.Nodes[i].ID < b.g.Nodes[j].ID })

	b.g.Edges = make([]Edge, 0, len(b.edgeAgg))
	for _, e := range b.edgeAgg {
		sort.Strings(e.Ports)
		b.g.Edges = append(b.g.Edges, *e)
	}
	sort.Slice(b.g.Edges, func(i, j int) bool { return edgeLess(b.g.Edges[i], b.g.Edges[j]) })
	return b.g
}

// stateFn answers, for one policy, the source→policy edge state, the
// meta to stamp (posture-denial fields and/or "k of n members"), and
// whether the edge should be emitted at all. emit is false for a
// source group with zero real members: an empty group is configured
// but reaches nothing, so a green "0 of 0" edge would lie to the
// auditor (#39 v2 review, finding 2).
type stateFn func(pol *types.Policy) (state EdgeState, meta map[string]string, emit bool)

// projectSource is the shared peer/user/group fan-out: for every
// enabled policy/rule whose Sources include one of srcGroups, emit
// the policy node, the anchor→policy edge (state from st), and the
// resource column with policy→resource edges. It uses the
// pre-built source-group index instead of scanning every policy, and
// memoises st per policy within this anchor (st depends only on
// (anchor, policy), never the rule — so it is safe to compute once
// per policy even when several of its rules match) (R5 findings
// 1, 2, 4).
func (b *colBuilder) projectSource(acc *types.Account, anchorID string, srcGroups []string, st stateFn) {
	type stRes struct {
		state EdgeState
		meta  map[string]string
		emit  bool
	}
	stMemo := map[string]stRes{}
	for _, pr := range b.gatherPolRules(srcGroups) {
		pol, r := pr.pol, pr.r
		s, ok := stMemo[pol.ID]
		if !ok {
			s.state, s.meta, s.emit = st(pol)
			stMemo[pol.ID] = s
		}
		if !s.emit {
			continue // empty source group — nothing to audit as permitted
		}
		b.policyNode(pol, r)
		b.edge(&Edge{
			From: anchorID, To: "policy:" + pol.ID,
			PermitSource: PermitPolicy,
			PolicyID:     pol.ID, PolicyName: pol.Name,
			Protocol: string(r.Protocol), Ports: rulePorts(r),
			Direction: ruleDir(r), State: s.state, Meta: s.meta,
		})
		b.fanResources(acc, pol, r)
	}
}

// fanResources emits the resource column for one matched rule: every
// destination group, the rule's DestinationResource, and any network
// resource whose backing groups intersect the rule destinations. The
// policy→resource edge is structural (always enforced) — posture is a
// source-side property and lives on the anchor→policy edge.
func (b *colBuilder) fanResources(acc *types.Account, pol *types.Policy, r *types.PolicyRule) {
	add := func(nodeID string, kind NodeKind, label, sub, rkind string) {
		b.node(nodeID, kind, label, colResources, map[string]string{"sub": sub, "resourceKind": rkind})
		b.edge(&Edge{
			From: "policy:" + pol.ID, To: nodeID,
			PermitSource: PermitPolicy,
			PolicyID:     pol.ID, PolicyName: pol.Name,
			Protocol: string(r.Protocol), Ports: rulePorts(r),
			Direction: ruleDir(r), State: EdgeEnforced,
		})
	}
	for _, gid := range r.Destinations {
		if g := acc.Groups[gid]; g != nil {
			add("group:"+gid, NodeGroup, g.Name, fmt.Sprintf("%d peer(s)", len(g.Peers)), "peer")
		}
		// network resources the destination group CONTAINS (engine
		// truth via Group.Resources, pre-indexed) — not res.GroupIDs.
		for _, res := range b.resByGroup[gid] {
			add("nr:"+res.ID, NodeNetworkResource, resourceLabel(res), resourceSub(res), "net")
		}
	}
	if r.DestinationResource.ID != "" {
		if res := b.resByID[r.DestinationResource.ID]; res != nil {
			add("nr:"+res.ID, NodeNetworkResource, resourceLabel(res), resourceSub(res), "net")
		}
	}
}

func buildPeerFocus(ctx context.Context, acc *types.Account, focus Focus) (*GraphDTO, error) {
	p := acc.GetPeer(focus.ID)
	if p == nil {
		return nil, fmt.Errorf("focus peer %q: %w", focus.ID, ErrFocusNotFound)
	}
	b := newColBuilder(acc, focus)
	b.node(p.ID, NodeFocus, peerLabel(p), colFocus, peerMeta(p))
	b.projectSource(acc, p.ID, acc.GetPeerGroupsList(p.ID),
		b.peerState(ctx, acc, p.ID))
	return b.finalize(), nil
}

func buildUserFocus(ctx context.Context, acc *types.Account, focus Focus) (*GraphDTO, error) {
	u := acc.Users[focus.ID]
	if u == nil {
		return nil, fmt.Errorf("focus user %q: %w", focus.ID, ErrFocusNotFound)
	}
	b := newColBuilder(acc, focus)
	b.node(u.Id, NodeFocus, userLabel(u), colFocus, map[string]string{"email": u.Email})

	var peers []*nbpeer.Peer
	for _, p := range acc.Peers {
		if p != nil && p.UserID == u.Id {
			peers = append(peers, p)
		}
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	for _, p := range peers {
		b.node(p.ID, NodePeer, peerLabel(p), colPeers, peerMeta(p))
		// User→Peer is identity ownership, not a policy permit.
		b.edge(&Edge{
			From: u.Id, To: p.ID,
			PermitSource: PermitIdentity,
			Direction:    DirectionOut, State: EdgeEnforced,
		})
		b.projectSource(acc, p.ID, acc.GetPeerGroupsList(p.ID),
			b.peerState(ctx, acc, p.ID))
	}
	return b.finalize(), nil
}

func buildGroupFocus(ctx context.Context, acc *types.Account, focus Focus) (*GraphDTO, error) {
	g := acc.Groups[focus.ID]
	if g == nil {
		return nil, fmt.Errorf("focus group %q: %w", focus.ID, ErrFocusNotFound)
	}
	b := newColBuilder(acc, focus)
	anchor := "group:" + focus.ID
	b.node(anchor, NodeFocus, g.Name, colFocus,
		map[string]string{"sub": fmt.Sprintf("%d peer(s)", len(g.Peers))})
	b.projectSource(acc, anchor, []string{focus.ID},
		b.groupState(ctx, acc, g.Peers))
	return b.finalize(), nil
}

// buildNetworkFocus is the inverse fan-in: the focus is a network
// resource (focus.ID = resource ID); the columns are Groups → Policy
// → focus(resource). It answers "who can reach THIS resource, through
// which policy" — the audit mirror of the source-anchored views.
func buildNetworkFocus(ctx context.Context, acc *types.Account, focus Focus) (*GraphDTO, error) {
	res := resourceByID(acc, focus.ID)
	if res == nil {
		return nil, fmt.Errorf("focus network resource %q: %w", focus.ID, ErrFocusNotFound)
	}
	b := newColBuilder(acc, focus)
	focusID := "nr:" + res.ID
	b.node(focusID, NodeFocus, resourceLabel(res), colFocus,
		map[string]string{"sub": resourceSub(res), "resourceKind": "net"})

	// Use openZro's OWN resolver for which policies apply to a network
	// resource (same logic the dataplane uses) and resolve the
	// resource's groups the way the engine does — from
	// acc.Groups[].Resources, NOT res.GroupIDs (that field is
	// gorm:"-" and is empty on the account loaded for the graph, so
	// the old hand-rolled match found nothing and the network focus
	// looked like "nobody reaches it") (#39 v2 review).
	resGroups := networkResourceGroupSet(acc, res.ID)
	for _, pol := range acc.GetPoliciesForNetworkResource(res.ID) {
		if pol == nil || !pol.Enabled {
			continue
		}
		for _, r := range pol.Rules {
			if r == nil || !r.Enabled || !ruleTargetsResource(r, res, resGroups) {
				continue
			}
			// Resolve the source groups that actually EMIT (≥1 real
			// member) BEFORE materializing the policy. If none emit,
			// nobody can reach the resource through this rule — under
			// strict-green that path must not be drawn at all, so we
			// skip the policy node and the policy→resource edge too
			// (no orphan green flow) (#39 v2 review, finding R2-net).
			type srcEdge struct {
				gid, name string
				peers     int
				state     EdgeState
				meta      map[string]string
			}
			var srcs []srcEdge
			for _, sgid := range r.Sources {
				sg := acc.Groups[sgid]
				if sg == nil {
					continue
				}
				state, meta, emit := b.groupState(ctx, acc, sg.Peers)(pol)
				if !emit {
					continue // empty / all-stale source group
				}
				srcs = append(srcs, srcEdge{
					gid: sgid, name: sg.Name, peers: len(sg.Peers),
					state: state, meta: meta,
				})
			}
			if len(srcs) == 0 {
				continue // no real actor → no policy path at all
			}

			b.policyNode(pol, r)
			b.edge(&Edge{
				From: "policy:" + pol.ID, To: focusID,
				PermitSource: PermitPolicy, PolicyID: pol.ID, PolicyName: pol.Name,
				Protocol: string(r.Protocol), Ports: rulePorts(r),
				Direction: ruleDir(r), State: EdgeEnforced,
			})
			for _, s := range srcs {
				b.node("group:"+s.gid, NodeGroup, s.name, colGroups,
					map[string]string{"sub": fmt.Sprintf("%d peer(s)", s.peers)})
				b.edge(&Edge{
					From: "group:" + s.gid, To: "policy:" + pol.ID,
					PermitSource: PermitPolicy, PolicyID: pol.ID, PolicyName: pol.Name,
					Protocol: string(r.Protocol), Ports: rulePorts(r),
					Direction: ruleDir(r), State: s.state, Meta: s.meta,
				})
			}
		}
	}
	return b.finalize(), nil
}

// networkResourceGroupSet is the IDs of the groups that contain the
// network resource, resolved the way openZro's own
// getNetworkResourceGroups does — by scanning Group.Resources — so it
// works on the graph-loaded account where NetworkResource.GroupIDs
// (gorm:"-") is not populated.
func networkResourceGroupSet(acc *types.Account, resID string) map[string]struct{} {
	out := map[string]struct{}{}
	for gid, g := range acc.Groups {
		if g == nil {
			continue
		}
		for _, r := range g.Resources {
			if r.ID == resID {
				out[gid] = struct{}{}
				break
			}
		}
	}
	return out
}

func ruleTargetsResource(r *types.PolicyRule, res *resourceTypes.NetworkResource, resGroups map[string]struct{}) bool {
	if r.DestinationResource.ID == res.ID {
		return true
	}
	for _, gid := range r.Destinations {
		if _, ok := resGroups[gid]; ok {
			return true
		}
	}
	return false
}
