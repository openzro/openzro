package controlcenter

import (
	"context"
	"fmt"
	"sort"
	"strings"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/types"
)

// buildUserFocus is the Control Center v2 user-centric columnar
// projection (ADR-0017 topology amendment): User → their Peers →
// Policies they are a source of → Resources those policies target.
// Structure is the configured policy wiring; the per-edge State is
// the EFFECTIVE enforcement status (EdgeEnforced vs EdgePostureBlocked
// from the same posture engine — D1: never re-derive in TS). Nodes
// carry a meta.column so the dashboard lays them in 4 columns.
//
// Clean-room (BSD-3): reuses openZro's own account/posture helpers;
// no parallel policy walker, no upstream NetBird code.
func buildUserFocus(ctx context.Context, acc *types.Account, focus Focus, validatedPeers map[string]struct{}) (*GraphDTO, error) {
	user := acc.Users[focus.ID]
	if user == nil {
		return nil, fmt.Errorf("focus user %q: %w", focus.ID, ErrFocusNotFound)
	}

	g := &GraphDTO{Focus: focus}
	nodes := map[string]Node{}
	edgeAgg := map[string]*Edge{}
	postureMap := postureChecksMap(acc)

	addN := func(id string, kind NodeKind, label, column string, extra map[string]string) {
		if _, ok := nodes[id]; ok {
			return
		}
		m := map[string]string{"column": column}
		for k, v := range extra {
			if v != "" {
				m[k] = v
			}
		}
		nodes[id] = Node{ID: id, Kind: kind, Label: label, Meta: m}
	}
	addE := func(from, to string, st EdgeState, polID, polName, proto string, ports []string, meta map[string]string) {
		key := from + "|" + to + "|" + polID + "|" + proto + "|" + string(st)
		if _, ok := edgeAgg[key]; ok {
			return
		}
		e := &Edge{
			From: from, To: to,
			PermitSource: PermitPolicy,
			PolicyID:     polID, PolicyName: polName,
			Protocol:  proto,
			Ports:     ports,
			Direction: DirectionOut,
			State:     st,
		}
		if polID == "" {
			e.PermitSource = "" // structural ownership edge (User→Peer)
		}
		if len(meta) > 0 {
			e.Meta = meta
		}
		edgeAgg[key] = e
	}

	uname := user.Name
	if uname == "" {
		uname = user.Email
	}
	if uname == "" {
		uname = user.Id
	}
	addN(user.Id, NodeUser, uname, "user", map[string]string{"email": user.Email})

	var userPeers []*nbpeer.Peer
	for _, p := range acc.Peers {
		if p != nil && p.UserID == user.Id {
			userPeers = append(userPeers, p)
		}
	}
	sort.Slice(userPeers, func(i, j int) bool { return userPeers[i].ID < userPeers[j].ID })
	for _, p := range userPeers {
		addN(p.ID, NodePeer, peerLabel(p), "peers", map[string]string{"ip": p.IP.String()})
		addE(user.Id, p.ID, EdgeEnforced, "", "", "", nil, nil)
	}

	for _, pol := range acc.Policies {
		if !pol.Enabled {
			continue
		}
		for _, rule := range pol.Rules {
			if rule == nil || !rule.Enabled {
				continue
			}
			srcMembers := groupMembers(acc, rule.Sources)
			for _, p := range userPeers {
				if _, ok := srcMembers[p.ID]; !ok {
					continue
				}
				st := EdgeEnforced
				var emeta map[string]string
				if len(pol.SourcePostureChecks) > 0 {
					if d := admissionDenial(ctx, acc, p.ID, pol.SourcePostureChecks, postureMap); d != nil {
						st = EdgePostureBlocked
						emeta = map[string]string{
							"postureCheck":     d.PostureCheckName,
							"postureCheckId":   d.PostureCheckID,
							"postureCheckType": d.CheckType,
							"postureReason":    d.Reason,
						}
					}
				}
				polNode := "policy:" + pol.ID
				addN(polNode, NodePolicy, pol.Name, "policies",
					map[string]string{"port": portLabel(rule)})
				addE(p.ID, polNode, st, pol.ID, pol.Name, string(rule.Protocol), rulePorts(rule), emeta)

				for to := range groupMembers(acc, rule.Destinations) {
					if to == p.ID {
						continue
					}
					tp := acc.GetPeer(to)
					if tp == nil {
						continue
					}
					addN(tp.ID, NodePeer, peerLabel(tp), "resources",
						map[string]string{"ip": tp.IP.String()})
					addE(polNode, tp.ID, EdgeEnforced, pol.ID, pol.Name, string(rule.Protocol), rulePorts(rule), nil)
				}
				if rid := rule.DestinationResource.ID; rid != "" {
					label := rid
					for _, res := range acc.NetworkResources {
						if res != nil && res.ID == rid {
							if res.Name != "" {
								label = res.Name
							} else if res.Address != "" {
								label = res.Address
							}
							break
						}
					}
					rnode := "nr:" + rid
					addN(rnode, NodeNetworkResource, label, "resources", nil)
					addE(polNode, rnode, EdgeEnforced, pol.ID, pol.Name, string(rule.Protocol), rulePorts(rule), nil)
				}
			}
		}
	}

	for _, n := range nodes {
		g.Nodes = append(g.Nodes, n)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	for _, e := range edgeAgg {
		g.Edges = append(g.Edges, *e)
	}
	sort.Slice(g.Edges, func(i, j int) bool { return edgeLess(g.Edges[i], g.Edges[j]) })
	return g, nil
}

// portLabel renders a policy rule's protocol/ports for the policy
// node card (e.g. "TCP:443", "ALL", "443,8080").
func portLabel(r *types.PolicyRule) string {
	pts := rulePorts(r)
	proto := strings.ToUpper(string(r.Protocol))
	if len(pts) == 0 {
		if proto == "" || proto == "ALL" {
			return "ALL"
		}
		return proto
	}
	joined := strings.Join(pts, ",")
	if proto == "" || proto == "ALL" {
		return joined
	}
	return proto + ":" + joined
}
