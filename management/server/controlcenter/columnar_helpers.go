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

// peerState is the ADR-0017 D1.2 posture distinction for a single
// source peer: enforced when the policy has no source posture checks
// or the peer passes them; posture_blocked (with the failing check
// named in meta) when a check denies it. It reuses the engine's own
// evaluator (types.EvaluateAdmission) — it makes no access decision,
// it labels the one the engine already encodes.
func (b *colBuilder) peerState(ctx context.Context, acc *types.Account, peerID string) stateFn {
	return func(pol *types.Policy) (EdgeState, map[string]string, bool) {
		if len(pol.SourcePostureChecks) == 0 {
			return EdgeEnforced, nil, true
		}
		d := admissionDenial(ctx, acc, peerID, pol.SourcePostureChecks, b.posture)
		if d == nil {
			return EdgeEnforced, nil, true
		}
		return EdgePostureBlocked, denialMeta(d), true
	}
}

// groupState is the union semantics for a group source (ADR-0017 D3:
// a group's reach is the UNION of its members' — never the
// intersection, which would hide access an auditor must see). Strict
// audit semantics (#39 v2 review, finding 2):
//
//   - n counts only REAL members (acc.GetPeer != nil): a stale peer
//     id in the group must never be counted as a posture pass.
//   - n == 0 (empty / all-stale group) → emit=false: an empty group
//     is configured but permits nobody; a green "0 of 0" edge would
//     lie. The caller drops the edge entirely.
//   - enforced iff the policy has no source posture checks OR at
//     least one real member passes; posture_blocked iff every real
//     member is denied (first denial by sorted peer id → stable).
//   - meta always carries "k of n members" so partial reach (k<n)
//     is visible, never silently shown as full.
func (b *colBuilder) groupState(ctx context.Context, acc *types.Account, members []string) stateFn {
	return func(pol *types.Policy) (EdgeState, map[string]string, bool) {
		real := make([]string, 0, len(members))
		for _, mID := range members {
			if acc.GetPeer(mID) != nil {
				real = append(real, mID)
			}
		}
		n := len(real)
		if n == 0 {
			return EdgeEnforced, nil, false
		}
		if len(pol.SourcePostureChecks) == 0 {
			return EdgeEnforced,
				map[string]string{"reachedBy": fmt.Sprintf("%d of %d members", n, n)},
				true
		}
		sort.Strings(real)
		pass := 0
		var firstDenial *types.AdmissionDenial
		for _, mID := range real {
			if d := admissionDenial(ctx, acc, mID, pol.SourcePostureChecks, b.posture); d == nil {
				pass++
			} else if firstDenial == nil {
				firstDenial = d
			}
		}
		if pass > 0 {
			return EdgeEnforced,
				map[string]string{"reachedBy": fmt.Sprintf("%d of %d members", pass, n)},
				true
		}
		m := denialMeta(firstDenial)
		m["reachedBy"] = fmt.Sprintf("0 of %d members", n)
		return EdgePostureBlocked, m, true
	}
}

func admissionDenial(ctx context.Context, acc *types.Account, peerID string, ids []string, postureMap map[string]*posture.Checks) *types.AdmissionDenial {
	p := acc.GetPeer(peerID)
	if p == nil {
		return nil
	}
	return types.EvaluateAdmission(ctx, p, ids, postureMap)
}

func denialMeta(d *types.AdmissionDenial) map[string]string {
	return map[string]string{
		"postureCheck":     d.PostureCheckName,
		"postureCheckId":   d.PostureCheckID,
		"postureCheckType": d.CheckType,
		"postureReason":    d.Reason,
	}
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

// portLabel is the policy card's port tag: "ALL", "TCP", "TCP:443",
// "TCP:80,443", "UDP:53", "ICMP". Ranges round-trip as "start-end".
func portLabel(r *types.PolicyRule) string {
	proto := strings.ToUpper(string(r.Protocol))
	if r.Protocol == types.PolicyRuleProtocolALL || proto == "" {
		return "ALL"
	}
	ports := rulePorts(r)
	if len(ports) == 0 {
		return proto
	}
	return proto + ":" + strings.Join(ports, ",")
}

// mergePortLabel unions two port tags (a policy matched by several
// rules shows one combined, deterministic tag).
func mergePortLabel(a, b string) string {
	if a == b || b == "" {
		return a
	}
	if a == "" {
		return b
	}
	seen := map[string]struct{}{}
	var out []string
	for _, part := range append(strings.Split(a, " · "), strings.Split(b, " · ")...) {
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	sort.Strings(out)
	return strings.Join(out, " · ")
}

func rulePorts(r *types.PolicyRule) []string {
	ports := append([]string(nil), r.Ports...)
	for _, pr := range r.PortRanges {
		ports = append(ports, fmt.Sprintf("%d-%d", pr.Start, pr.End))
	}
	return ports
}

func ruleDir(r *types.PolicyRule) EdgeDirection {
	if r.Bidirectional {
		return DirectionBidirectional
	}
	return DirectionOut
}

func mergeDirectionValue(e *Edge, d EdgeDirection) {
	switch {
	case e.Direction == "":
		e.Direction = d
	case e.Direction != d:
		e.Direction = DirectionBidirectional
	}
}

func peerLabel(p *nbpeer.Peer) string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

func peerMeta(p *nbpeer.Peer) map[string]string {
	m := map[string]string{}
	if ip := p.IP.String(); ip != "" && ip != "<nil>" {
		m["ip"] = ip
	}
	if p.Meta.GoOS != "" {
		m["os"] = p.Meta.GoOS
	}
	return m
}

func userLabel(u *types.User) string {
	switch {
	case u.IsServiceUser && u.ServiceUserName != "":
		return u.ServiceUserName
	case u.Name != "":
		return u.Name
	case u.Email != "":
		return u.Email
	default:
		return u.Id
	}
}

func resourceByID(acc *types.Account, id string) *resourceTypes.NetworkResource {
	for _, res := range acc.NetworkResources {
		if res != nil && res.ID == id {
			return res
		}
	}
	return nil
}

func resourceLabel(res *resourceTypes.NetworkResource) string {
	switch {
	case res.Name != "":
		return res.Name
	case res.Address != "":
		return res.Address
	default:
		return res.Prefix.String()
	}
}

func resourceSub(res *resourceTypes.NetworkResource) string {
	if res.Address != "" {
		return res.Address
	}
	if res.Prefix.IsValid() {
		return res.Prefix.String()
	}
	return string(res.Type)
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// edgeLess is a TOTAL order over edges so the DTO is deterministic
// (no map-iteration jitter on the wire — ADR-0017 minimum envelope).
// The posture-denial signature is the final tie-breaker so distinct
// posture_blocked causes order stably.
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
