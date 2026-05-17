package controlcenter

import (
	"context"
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
	postureMap := postureChecksMap(acc)

	// Distribution: the classic routes the focus actually receives.
	for _, r := range acc.GetRoutesToSync(ctx, focusID, reachable) {
		if r == nil || !r.Enabled {
			continue
		}
		nodeID := "route:" + string(r.ID)

		if len(r.AccessControlGroups) == 0 {
			b.addNode(nodeID, NodeRoute, routeLabel(r))
			b.addRouteEdge(focusID, nodeID, &Edge{
				PermitSource: PermitRouteDefault,
				Protocol:     string(types.PolicyRuleProtocolALL),
				SourceRanges: []string{defaultSourceRange(r)},
				Direction:    DirectionOut,
				State:        EdgeEnforced,
			})
			continue
		}

		// Permission: a route policy whose rule destination is one of
		// the route's AccessControlGroups and whose source includes the
		// focus. Posture on that policy can downgrade it to blocked.
		for _, pol := range types.GetAllRoutePoliciesFromGroups(acc, r.AccessControlGroups) {
			if !pol.Enabled {
				continue
			}
			for _, rule := range pol.Rules {
				if rule == nil || !rule.Enabled {
					continue
				}
				src := groupMembers(acc, rule.Sources)
				if _, ok := src[focusID]; !ok {
					continue
				}
				e := &Edge{
					PermitSource: PermitPolicy,
					PolicyID:     pol.ID,
					PolicyName:   pol.Name,
					Protocol:     string(rule.Protocol),
					Ports:        rulePorts(rule),
					Direction:    DirectionOut,
					State:        EdgeEnforced,
				}
				if len(pol.SourcePostureChecks) > 0 {
					if d := admissionDenial(ctx, acc, focusID, pol.SourcePostureChecks, postureMap); d != nil {
						e.State = EdgePostureBlocked
						e.Meta = map[string]string{
							"postureCheck":     d.PostureCheckName,
							"postureCheckId":   d.PostureCheckID,
							"postureCheckType": d.CheckType,
							"postureReason":    d.Reason,
						}
					}
				}
				b.addNode(nodeID, NodeRoute, routeLabel(r))
				b.addRouteEdge(focusID, nodeID, e)
			}
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

// addRouteEdge inserts a focus→route edge, de-duped by
// from|to|policyID|protocol, never downgrading an enforced edge.
func (b *graphBuilder) addRouteEdge(from, to string, e *Edge) {
	e.From = from
	e.To = to
	key := from + "|" + to + "|" + e.PolicyID + "|" + e.Protocol
	if existing, ok := b.edgeAgg[key]; ok {
		if existing.State == EdgeEnforced {
			return
		}
		if e.State == EdgeEnforced {
			b.edgeAgg[key] = e
		}
		return
	}
	b.edgeAgg[key] = e
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

func defaultSourceRange(r *route.Route) string {
	if r.Network.IsValid() && r.Network.Addr().Is6() {
		return "::/0"
	}
	return "0.0.0.0/0"
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
