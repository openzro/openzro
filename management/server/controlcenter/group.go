package controlcenter

import (
	"context"
	"fmt"
	"sort"

	"github.com/openzro/openzro/management/server/types"
)

// buildGroupFocus is ADR-0017 D3 group focus: the UNION of each
// member's per-member reach (never the intersection — that would hide
// access some members have, the wrong answer for an audit tool). Each
// aggregated edge is tagged "k of n members" and posture is evaluated
// per member, so a posture-blocked member yields a distinct
// posture_blocked aggregate edge instead of being dropped.
//
// Clean-room (BSD-3): reuses the per-peer passes (C2–C4) member by
// member; no upstream NetBird code.
func buildGroupFocus(ctx context.Context, acc *types.Account, focus Focus, validatedPeers map[string]struct{}) (*GraphDTO, error) {
	grp := acc.Groups[focus.ID]
	if grp == nil {
		return nil, fmt.Errorf("focus group %q: %w", focus.ID, ErrFocusNotFound)
	}

	g := &GraphDTO{Focus: focus}
	groupNodeID := "group:" + focus.ID
	n := len(grp.Peers)

	type agg struct {
		edge    Edge
		members map[string]struct{}
	}
	aggs := map[string]*agg{}
	toNodes := map[string]Node{}

	// Route/nr firewall indices are account-invariant — build ONCE and
	// share across every member instead of O(members × peers)
	// recomputation (#50-r2 F3).
	idx := newRouteIndex(ctx, acc, validatedPeers)

	for _, mID := range grp.Peers {
		m := acc.GetPeer(mID)
		if m == nil {
			continue
		}
		// An unvalidated member reaches nothing in the dataplane
		// (#50-r2 F1); it contributes zero reach but still counts in
		// the "k of n" denominator (n = declared membership).
		if _, ok := validatedPeers[mID]; !ok {
			continue
		}
		tmp := &GraphDTO{}
		tb := newGraphBuilder(tmp)
		reach, fw := acc.GetPeerConnectionResources(ctx, m, validatedPeers)
		tb.addPeerReach(acc, mID, reach, fw)
		tb.addPostureBlocked(ctx, acc, mID, validatedPeers)
		tb.addRouteReach(ctx, acc, mID, reach, validatedPeers, idx)
		tb.finalize()

		for _, nd := range tmp.Nodes {
			if nd.ID == mID {
				continue // the member's own focus node — not a target
			}
			kind := nd.Kind
			if kind == NodeFocus {
				kind = NodePeer
			}
			toNodes[nd.ID] = Node{ID: nd.ID, Kind: kind, Label: nd.Label}
		}

		for _, e := range tmp.Edges {
			// Posture-blocked edges are split by the failing-check
			// signature so members blocked by DIFFERENT checks do not
			// collapse into one edge with an arbitrary first member's
			// reason (Finding 4 — per-member posture must survive
			// aggregation).
			denialSig := ""
			if e.State == EdgePostureBlocked {
				denialSig = e.Meta["postureCheckId"] + "/" + e.Meta["postureCheckType"]
			}
			key := e.To + "|" + e.PolicyID + "|" + e.Protocol + "|" + string(e.State) + "|" + denialSig
			a := aggs[key]
			if a == nil {
				ne := e
				ne.From = groupNodeID
				a = &agg{edge: ne, members: map[string]struct{}{}}
				aggs[key] = a
			}
			a.members[mID] = struct{}{}
			mergeDirectionValue(&a.edge, e.Direction)
			for _, p := range e.Ports {
				if !contains(a.edge.Ports, p) {
					a.edge.Ports = append(a.edge.Ports, p)
				}
			}
		}
	}

	g.Nodes = append(g.Nodes, Node{ID: groupNodeID, Kind: NodeFocus, Label: grp.Name})
	for _, nd := range toNodes {
		g.Nodes = append(g.Nodes, nd)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })

	for _, a := range aggs {
		e := a.edge
		sort.Strings(e.Ports)
		if e.Meta == nil {
			e.Meta = map[string]string{}
		}
		e.Meta["reachedBy"] = fmt.Sprintf("%d of %d members", len(a.members), n)
		g.Edges = append(g.Edges, e)
	}
	sort.Slice(g.Edges, func(i, j int) bool {
		x, y := g.Edges[i], g.Edges[j]
		if x.To != y.To {
			return x.To < y.To
		}
		if x.PolicyID != y.PolicyID {
			return x.PolicyID < y.PolicyID
		}
		return x.State < y.State
	})
	return g, nil
}
