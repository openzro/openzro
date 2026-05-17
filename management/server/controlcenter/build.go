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
		return nil, fmt.Errorf("unsupported focus type %q", focus.Type)
	}
}

func buildPeerFocus(ctx context.Context, acc *types.Account, focus Focus, validatedPeers map[string]struct{}) (*GraphDTO, error) {
	focusPeer := acc.GetPeer(focus.ID)
	if focusPeer == nil {
		return nil, fmt.Errorf("focus peer %q not found", focus.ID)
	}

	g := &GraphDTO{Focus: focus}
	b := newGraphBuilder(g)
	b.addNode(focusPeer.ID, NodeFocus, peerLabel(focusPeer))

	reachable, fwRules := acc.GetPeerConnectionResources(ctx, focusPeer, validatedPeers)
	b.addPeerReach(acc, focusPeer.ID, reachable, fwRules)
	b.addPostureBlocked(ctx, acc, focusPeer.ID, validatedPeers)
	b.addRouteReach(ctx, acc, focusPeer.ID, reachable, validatedPeers)

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

// addPeerReach turns the enforcement output (reachable peers + the
// firewall rules that permit them) into peer nodes + enforced edges.
func (b *graphBuilder) addPeerReach(acc *types.Account, fromID string, reachable []*nbpeer.Peer, fwRules []*types.FirewallRule) {
	ruleToPolicy := indexRuleToPolicy(acc)

	ipToPeer := map[string]*nbpeer.Peer{}
	for _, p := range reachable {
		if p == nil {
			continue
		}
		b.addNode(p.ID, NodePeer, peerLabel(p))
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
		a, c := b.g.Edges[i], b.g.Edges[j]
		if a.From != c.From {
			return a.From < c.From
		}
		if a.To != c.To {
			return a.To < c.To
		}
		return a.PolicyID < c.PolicyID
	})
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
