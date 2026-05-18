package controlcenter

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/types"
	"github.com/openzro/openzro/route"
)

// addRouteReach is ADR-0017 D1.1 for route edges: a route edge is the
// COMPOSITION of distribution (GetRoutesToSync — does the route reach
// the focus at all) and permission (is the focus an allowed source on
// that route, or does the route default-permit). A route synced to the
// focus but not permitted by any access-control policy is NOT drawn as
// reachable (the route_test.go:1934 invariant) — it produces neither
// an edge nor an orphan node.
//
// Clean-room (BSD-3): reuses openZro's own GetRoutesToSync /
// GetAllRoutePoliciesFromGroups / GetNetworkResourcesRoutesToSync;
// no parallel policy walker, no upstream NetBird code.
func (b *graphBuilder) addRouteReach(ctx context.Context, acc *types.Account, focusID string, reachable []*nbpeer.Peer, validatedPeers map[string]struct{}) {
	focusPeer := acc.GetPeer(focusID)
	if focusPeer == nil {
		return
	}

	// Permission is materialised at the ROUTER, not re-derived here:
	// index every routing peer's real RouteFirewallRules by RouteID.
	// A rule's SourceRanges are the client IPs it actually permits
	// (or 0.0.0.0/0 for a route default-permit, PolicyID == "").
	ruleIdx := map[route.ID][]*types.RouteFirewallRule{}
	for _, p := range acc.Peers {
		if p == nil {
			continue
		}
		for _, fr := range acc.GetPeerRoutesFirewallRules(ctx, p.ID, validatedPeers) {
			ruleIdx[fr.RouteID] = append(ruleIdx[fr.RouteID], fr)
		}
	}

	// Distribution ∩ permission: of the routes the focus receives, an
	// edge exists only where a real route firewall rule permits the
	// focus's IP. A synced-but-unpermitted route yields no edge/node.
	for _, r := range acc.GetRoutesToSync(ctx, focusID, reachable) {
		if r == nil || !r.Enabled {
			continue
		}
		nodeID := "route:" + string(r.ID)

		// The focus itself serves this route — as the router it reaches
		// the routed network by definition (its own gateway).
		if r.Peer == focusPeer.Key {
			b.addNode(nodeID, NodeRoute, routeLabel(r))
			b.addRouteEdge(focusID, nodeID, &Edge{
				PermitSource: PermitRouteDefault,
				Protocol:     string(types.PolicyRuleProtocolALL),
				Direction:    DirectionOut,
				State:        EdgeEnforced,
			})
			continue
		}

		permitted := false
		for _, fr := range ruleIdx[r.ID] {
			if !ipInAnyRange(focusPeer.IP, fr.SourceRanges) {
				continue
			}
			permitted = true
			e := &Edge{
				Protocol:     fr.Protocol,
				Ports:        routeFirewallPorts(fr),
				SourceRanges: fr.SourceRanges,
				Direction:    DirectionOut,
				State:        EdgeEnforced,
			}
			if fr.PolicyID == "" {
				e.PermitSource = PermitRouteDefault
			} else {
				e.PermitSource = PermitPolicy
				e.PolicyID = fr.PolicyID
				if pol := policyByID(acc, fr.PolicyID); pol != nil {
					e.PolicyName = pol.Name
				}
			}
			b.addNode(nodeID, NodeRoute, routeLabel(r))
			b.addRouteEdge(focusID, nodeID, e)
		}

		// Explanatory (D1.2 for routes): the focus is absent from every
		// real rule's SourceRanges. If it WOULD be a permitted source
		// but for posture, surface a distinct posture_blocked edge
		// instead of silently dropping it. This annotates the engine's
		// decision (reuses the same posture eval) — it does not
		// re-decide enforced reach, which stays strictly real-rule.
		if !permitted && len(r.AccessControlGroups) > 0 {
			b.addRoutePostureBlocked(ctx, acc, focusID, r, nodeID)
		}
	}

	// Network-resource routes are already permission-resolved by the
	// producer (it only returns resources the focus is entitled to), so
	// no distribution∩permission step is needed — label the permit
	// source from the resource's policy when there is one.
	resourcePolicies := acc.GetResourcePoliciesMap()
	_, nrRoutes, _ := acc.GetNetworkResourcesRoutesToSync(ctx, focusID, resourcePolicies, acc.GetResourceRoutersMap())
	for _, r := range nrRoutes {
		if r == nil {
			continue
		}
		nodeID := "nr:" + string(r.ID)
		b.addNode(nodeID, NodeNetworkResource, routeLabel(r))
		e := &Edge{
			PermitSource: PermitRouteDefault,
			Protocol:     string(types.PolicyRuleProtocolALL),
			Direction:    DirectionOut,
			State:        EdgeEnforced,
		}
		if pols := resourcePolicies[string(r.GetResourceID())]; len(pols) > 0 && pols[0] != nil {
			e.PermitSource = PermitPolicy
			e.PolicyID = pols[0].ID
			e.PolicyName = pols[0].Name
		}
		b.addRouteEdge(focusID, nodeID, e)
	}
}

// addRouteEdge inserts a focus→route edge keyed by
// from|to|policyID|protocol. When the key already exists it MERGES
// ports and source ranges instead of dropping the new rule (two
// firewall rules of the same policy/protocol differing only by port
// — e.g. TCP 80 and TCP 443 — must both survive, Finding 2). An
// enforced edge is never downgraded to posture_blocked; a
// posture_blocked edge is upgraded if real reach is found.
func (b *graphBuilder) addRouteEdge(from, to string, e *Edge) {
	e.From = from
	e.To = to
	key := from + "|" + to + "|" + e.PolicyID + "|" + e.Protocol
	existing, ok := b.edgeAgg[key]
	if !ok {
		b.edgeAgg[key] = e
		return
	}
	if existing.State == EdgeEnforced && e.State != EdgeEnforced {
		return // never downgrade
	}
	if existing.State != EdgeEnforced && e.State == EdgeEnforced {
		// upgrade to the enforced edge, then carry merged ports below.
		e.Ports = unionStrings(e.Ports, existing.Ports)
		e.SourceRanges = unionStrings(e.SourceRanges, existing.SourceRanges)
		b.edgeAgg[key] = e
		return
	}
	existing.Ports = unionStrings(existing.Ports, e.Ports)
	existing.SourceRanges = unionStrings(existing.SourceRanges, e.SourceRanges)
}

func unionStrings(a, b []string) []string {
	for _, v := range b {
		if !contains(a, v) {
			a = append(a, v)
		}
	}
	return a
}

func routeLabel(r *route.Route) string {
	if r.Network.IsValid() {
		return r.Network.String()
	}
	if len(r.Domains) > 0 {
		ds := make([]string, 0, len(r.Domains))
		for _, d := range r.Domains {
			ds = append(ds, string(d))
		}
		return strings.Join(ds, ",")
	}
	return string(r.NetID)
}

// addRoutePostureBlocked emits a posture_blocked edge for a route the
// focus would be a permitted source on but for posture. Annotator
// only: reuses the same posture evaluation, makes no enforced-reach
// decision (that stays strictly real-rule based).
func (b *graphBuilder) addRoutePostureBlocked(ctx context.Context, acc *types.Account, focusID string, r *route.Route, nodeID string) {
	postureMap := postureChecksMap(acc)
	for _, pol := range types.GetAllRoutePoliciesFromGroups(acc, r.AccessControlGroups) {
		if !pol.Enabled || len(pol.SourcePostureChecks) == 0 {
			continue
		}
		for _, rule := range pol.Rules {
			if rule == nil || !rule.Enabled {
				continue
			}
			if _, ok := groupMembers(acc, rule.Sources)[focusID]; !ok {
				continue
			}
			d := admissionDenial(ctx, acc, focusID, pol.SourcePostureChecks, postureMap)
			if d == nil {
				continue
			}
			b.addNode(nodeID, NodeRoute, routeLabel(r))
			b.addRouteEdge(focusID, nodeID, &Edge{
				PermitSource: PermitPolicy,
				PolicyID:     pol.ID,
				PolicyName:   pol.Name,
				Protocol:     string(rule.Protocol),
				Ports:        rulePorts(rule),
				Direction:    DirectionOut,
				State:        EdgePostureBlocked,
				Meta: map[string]string{
					"postureCheck":     d.PostureCheckName,
					"postureCheckId":   d.PostureCheckID,
					"postureCheckType": d.CheckType,
					"postureReason":    d.Reason,
				},
			})
		}
	}
}

func ipInAnyRange(ip net.IP, ranges []string) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	for _, cidr := range ranges {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			continue
		}
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func routeFirewallPorts(fr *types.RouteFirewallRule) []string {
	if fr.Port != 0 {
		return []string{fmt.Sprintf("%d", fr.Port)}
	}
	if fr.PortRange.Start != 0 || fr.PortRange.End != 0 {
		return []string{fmt.Sprintf("%d-%d", fr.PortRange.Start, fr.PortRange.End)}
	}
	return nil
}

func policyByID(acc *types.Account, id string) *types.Policy {
	for _, p := range acc.Policies {
		if p != nil && p.ID == id {
			return p
		}
	}
	return nil
}

func postureChecksMap(acc *types.Account) map[string]*posture.Checks {
	m := map[string]*posture.Checks{}
	for _, pc := range acc.PostureChecks {
		if pc != nil {
			m[pc.ID] = pc
		}
	}
	return m
}
