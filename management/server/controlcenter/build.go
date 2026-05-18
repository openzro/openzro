package controlcenter

import (
	"context"
	"fmt"
	"sort"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/types"
)

// BuildGraph derives the read-only access graph for a focus node from
// openZro's own enforcement engine. v1 supports peer focus (group
// focus lands in a later commit). It never re-decides access: it
// composes the producers GetPeerNetworkMap uses and labels the result.
//
// Clean-room (BSD-3): ADR-0017; no upstream NetBird management/ code
// consulted or ported.
func BuildGraph(ctx context.Context, acc *types.Account, focus Focus, validatedPeers map[string]struct{}) (*GraphDTO, error) {
	switch focus.Type {
	case FocusPeer:
		return buildPeerFocus(ctx, acc, focus, validatedPeers)
	case FocusGroup:
		return buildGroupFocus(ctx, acc, focus, validatedPeers)
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupportedFocus, focus.Type)
	}
}

func buildPeerFocus(ctx context.Context, acc *types.Account, focus Focus, validatedPeers map[string]struct{}) (*GraphDTO, error) {
	focusPeer := acc.GetPeer(focus.ID)
	if focusPeer == nil {
		return nil, fmt.Errorf("focus peer %q: %w", focus.ID, ErrFocusNotFound)
	}

	g := &GraphDTO{Focus: focus}
	b := newGraphBuilder(g)
	b.addPeerNode(focusPeer, NodeFocus)

	// Mirror the dataplane: GetPeerNetworkMap early-returns an empty
	// map when the focus is not validated (account.go:262). An
	// unvalidated peer reaches nothing "right now" — the graph must
	// say the same, not fabricate posture/route edges (#50-r2 F1).
	if _, ok := validatedPeers[focusPeer.ID]; !ok {
		b.finalize()
		return g, nil
	}

	reachable, fwRules := acc.GetPeerConnectionResources(ctx, focusPeer, validatedPeers)
	b.addPeerReach(acc, focusPeer.ID, reachable, fwRules)
	b.addPostureBlocked(ctx, acc, focusPeer.ID, validatedPeers)
	b.addRouteReach(ctx, acc, focusPeer.ID, reachable, validatedPeers,
		newRouteIndex(ctx, acc, validatedPeers))

	b.finalize()
	return g, nil
}

// graphBuilder accumulates nodes/edges and de-dups + sorts on
// finalize so the DTO is deterministic (stable tests, stable wire).
type graphBuilder struct {
	g       *GraphDTO
	nodes   map[string]Node
	edgeAgg map[string]*Edge // key: from|to|policyID|protocol
}

func newGraphBuilder(g *GraphDTO) *graphBuilder {
	return &graphBuilder{g: g, nodes: map[string]Node{}, edgeAgg: map[string]*Edge{}}
}

func (b *graphBuilder) addNode(id string, kind NodeKind, label string) {
	if _, ok := b.nodes[id]; ok {
		return
	}
	b.nodes[id] = Node{ID: id, Kind: kind, Label: label}
}

// addPeerNode is addNode for a peer (focus or reachable): it also
// carries the peer IP in meta so the dashboard can show it as the
// node's secondary line (route/network_resource already expose their
// CIDR via the label). meta is the existing freeform node field — an
// additive, non-breaking contract use.
func (b *graphBuilder) addPeerNode(p *nbpeer.Peer, kind NodeKind) {
	if _, ok := b.nodes[p.ID]; ok {
		return
	}
	n := Node{ID: p.ID, Kind: kind, Label: peerLabel(p)}
	if ip := p.IP.String(); ip != "" && ip != "<nil>" {
		n.Meta = map[string]string{"ip": ip}
	}
	b.nodes[p.ID] = n
}

// addPeerReach turns the enforcement output (reachable peers + the
// firewall rules that permit them) into peer nodes + enforced edges.
func (b *graphBuilder) addPeerReach(acc *types.Account, fromID string, reachable []*nbpeer.Peer, fwRules []*types.FirewallRule) {
	ruleToPolicy := indexRuleToPolicy(acc)

	ipToPeer := map[string]*nbpeer.Peer{}
	for _, p := range reachable {
		if p == nil {
			continue
		}
		b.addPeerNode(p, NodePeer)
		ipToPeer[p.IP.String()] = p
	}

	for _, fr := range fwRules {
		if fr == nil {
			continue
		}
		// "0.0.0.0" is the GetGroupAll shortcut: the rule applies to
		// every reachable peer, so fan it out to all of them.
		var targets []*nbpeer.Peer
		if fr.PeerIP == "0.0.0.0" {
			targets = reachable
		} else if p := ipToPeer[fr.PeerIP]; p != nil {
			targets = []*nbpeer.Peer{p}
		}

		for _, p := range targets {
			if p == nil {
				continue
			}
			b.upsertPolicyEdge(fromID, p.ID, fr, ruleToPolicy[fr.PolicyID])
		}
	}
}

// upsertPolicyEdge merges firewall rules that differ only by direction
// (a bidirectional policy emits separate IN/OUT rules) and unions
// ports, keeping the DTO one-edge-per-(peer,policy,protocol).
func (b *graphBuilder) upsertPolicyEdge(from, to string, fr *types.FirewallRule, pol *types.Policy) {
	key := from + "|" + to + "|" + fr.PolicyID + "|" + fr.Protocol
	e, ok := b.edgeAgg[key]
	if !ok {
		e = &Edge{
			From: from, To: to,
			PermitSource: PermitPolicy,
			Protocol:     fr.Protocol,
			State:        EdgeEnforced,
		}
		if pol != nil {
			e.PolicyID = pol.ID
			e.PolicyName = pol.Name
		}
		b.edgeAgg[key] = e
	}
	mergeDirection(e, fr.Direction)
	for _, port := range firewallPorts(fr) {
		if !contains(e.Ports, port) {
			e.Ports = append(e.Ports, port)
		}
	}
}

func (b *graphBuilder) finalize() {
	b.g.Nodes = b.g.Nodes[:0]
	for _, n := range b.nodes {
		b.g.Nodes = append(b.g.Nodes, n)
	}
	sort.Slice(b.g.Nodes, func(i, j int) bool { return b.g.Nodes[i].ID < b.g.Nodes[j].ID })

	for _, e := range b.edgeAgg {
		sort.Strings(e.Ports)
		b.g.Edges = append(b.g.Edges, *e)
	}
	sort.Slice(b.g.Edges, func(i, j int) bool {
		return edgeLess(b.g.Edges[i], b.g.Edges[j])
	})
}

// edgeLess is a TOTAL order over edges so the DTO is deterministic.
// Sorting only by from/to/policyID left ties (different protocol /
// state / posture-denial cause) in map-iteration order, causing wire
// jitter (#50-r2 F4). The posture-denial signature is the final
// tie-breaker so distinct posture_blocked causes order stably.
func edgeLess(a, b Edge) bool {
	switch {
	case a.From != b.From:
		return a.From < b.From
	case a.To != b.To:
		return a.To < b.To
	case a.PolicyID != b.PolicyID:
		return a.PolicyID < b.PolicyID
	case a.PermitSource != b.PermitSource:
		return a.PermitSource < b.PermitSource
	case a.Protocol != b.Protocol:
		return a.Protocol < b.Protocol
	case a.State != b.State:
		return a.State < b.State
	}
	return denialSig(a) < denialSig(b)
}

func denialSig(e Edge) string {
	return e.Meta["postureCheckId"] + "/" + e.Meta["postureCheckType"]
}

func indexRuleToPolicy(acc *types.Account) map[string]*types.Policy {
	idx := map[string]*types.Policy{}
	for _, pol := range acc.Policies {
		for _, r := range pol.Rules {
			if r != nil {
				idx[r.ID] = pol
			}
		}
	}
	return idx
}

func mergeDirection(e *Edge, frDir int) {
	d := DirectionOut
	if frDir == types.FirewallRuleDirectionIN {
		d = DirectionIn
	}
	switch {
	case e.Direction == "":
		e.Direction = d
	case e.Direction != d:
		e.Direction = DirectionBidirectional
	}
}

func firewallPorts(fr *types.FirewallRule) []string {
	if fr.Port != "" {
		return []string{fr.Port}
	}
	if fr.PortRange.Start != 0 || fr.PortRange.End != 0 {
		return []string{fmt.Sprintf("%d-%d", fr.PortRange.Start, fr.PortRange.End)}
	}
	return nil
}

func peerLabel(p *nbpeer.Peer) string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
