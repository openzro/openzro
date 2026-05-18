package controlcenter

import (
	"context"
	"fmt"

	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/types"
)

// addPostureBlocked is the ADR-0017 D1.2 explanatory pass. The
// enforcement engine silently drops a source peer that fails posture
// (getAllPeersFromGroups continues past it), so "no policy" and
// "policy permits but posture blocks" are indistinguishable in the
// enforced output. This pass re-walks the same enabled policies and
// reuses the same posture evaluation (types.EvaluateAdmission — the
// structured form that also NAMES the failing check) to emit a
// distinct posture_blocked edge. It makes no access decisions; it
// annotates the ones the engine already made (it never overwrites an
// enforced edge for the same peer/policy).
func (b *graphBuilder) addPostureBlocked(ctx context.Context, acc *types.Account, focusID string, validatedPeers map[string]struct{}) {
	postureMap := map[string]*posture.Checks{}
	for _, pc := range acc.PostureChecks {
		if pc != nil {
			postureMap[pc.ID] = pc
		}
	}
	if len(postureMap) == 0 {
		return
	}

	for _, pol := range acc.Policies {
		if !pol.Enabled || len(pol.SourcePostureChecks) == 0 {
			continue
		}
		for _, r := range pol.Rules {
			if r == nil || !r.Enabled {
				continue
			}
			src := groupMembers(acc, r.Sources)
			dst := groupMembers(acc, r.Destinations)

			outDir := DirectionOut
			inDir := DirectionIn
			if r.Bidirectional {
				outDir, inDir = DirectionBidirectional, DirectionBidirectional
			}

			// focus is a source: it cannot reach the destinations it
			// would otherwise reach because IT is non-compliant.
			// Edge is focus-anchored (From=focus), traffic OUT.
			if _, ok := src[focusID]; ok {
				if d := admissionDenial(ctx, acc, focusID, pol.SourcePostureChecks, postureMap); d != nil {
					for other := range dst {
						b.addBlockedEdge(acc, focusID, other, outDir, pol, r, validatedPeers, d)
					}
				}
			}
			// focus is a destination: a source peer that would reach
			// it is non-compliant. Edge is focus-anchored (From=focus),
			// traffic IN.
			if _, ok := dst[focusID]; ok {
				for other := range src {
					if other == focusID {
						continue
					}
					if d := admissionDenial(ctx, acc, other, pol.SourcePostureChecks, postureMap); d != nil {
						b.addBlockedEdge(acc, focusID, other, inDir, pol, r, validatedPeers, d)
					}
				}
			}
		}
	}
}

// addBlockedEdge emits (or merges) a focus-anchored posture_blocked
// edge (From=focus, To=other — same convention as the enforced pass),
// gated the same way the engine gates: the non-compliant endpoint must
// still be a validated mesh peer (a non-validated peer is "not in the
// mesh", not "posture-blocked"). Never downgrades an enforced edge.
func (b *graphBuilder) addBlockedEdge(acc *types.Account, focusID, otherID string, dir EdgeDirection, pol *types.Policy, r *types.PolicyRule, validatedPeers map[string]struct{}, d *types.AdmissionDenial) {
	if focusID == otherID {
		return
	}
	// The engine gates BOTH endpoints on validation
	// (getAllPeersFromGroups + the GetPeerNetworkMap entry check). If
	// either endpoint is unvalidated the pair is unreachable
	// regardless of posture, so posture is NOT the sole remaining
	// blocker and a posture_blocked edge here would be a lie
	// (Finding 3).
	if _, ok := validatedPeers[focusID]; !ok {
		return
	}
	if _, ok := validatedPeers[otherID]; !ok {
		return
	}
	if acc.GetPeer(focusID) == nil || acc.GetPeer(otherID) == nil {
		return
	}
	if p := acc.GetPeer(otherID); p != nil {
		b.addNode(p.ID, NodePeer, peerLabel(p))
	}

	from, to := focusID, otherID
	key := from + "|" + to + "|" + pol.ID + "|" + string(r.Protocol)
	if e, ok := b.edgeAgg[key]; ok {
		if e.State == EdgeEnforced {
			return // never downgrade an enforced edge
		}
		mergeDirectionValue(e, dir)
		for _, port := range rulePorts(r) {
			if !contains(e.Ports, port) {
				e.Ports = append(e.Ports, port)
			}
		}
		return
	}
	b.edgeAgg[key] = &Edge{
		From: from, To: to,
		PermitSource: PermitPolicy,
		PolicyID:     pol.ID,
		PolicyName:   pol.Name,
		Protocol:     string(r.Protocol),
		Ports:        rulePorts(r),
		Direction:    dir,
		State:        EdgePostureBlocked,
		Meta: map[string]string{
			"postureCheck":     d.PostureCheckName,
			"postureCheckId":   d.PostureCheckID,
			"postureCheckType": d.CheckType,
			"postureReason":    d.Reason,
		},
	}
}

func admissionDenial(ctx context.Context, acc *types.Account, peerID string, ids []string, postureMap map[string]*posture.Checks) *types.AdmissionDenial {
	p := acc.GetPeer(peerID)
	if p == nil {
		return nil
	}
	return types.EvaluateAdmission(ctx, p, ids, postureMap)
}

// groupMembers is the union of peer IDs across the given group IDs
// (the engine's source/destination membership, posture aside).
func groupMembers(acc *types.Account, groupIDs []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, gid := range groupIDs {
		g := acc.Groups[gid]
		if g == nil {
			continue
		}
		for _, pid := range g.Peers {
			out[pid] = struct{}{}
		}
	}
	return out
}

func rulePorts(r *types.PolicyRule) []string {
	ports := append([]string(nil), r.Ports...)
	for _, pr := range r.PortRanges {
		ports = append(ports, fmt.Sprintf("%d-%d", pr.Start, pr.End))
	}
	return ports
}

func mergeDirectionValue(e *Edge, d EdgeDirection) {
	switch {
	case e.Direction == "":
		e.Direction = d
	case e.Direction != d:
		e.Direction = DirectionBidirectional
	}
}
