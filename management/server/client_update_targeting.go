package server

import (
	"hash/crc32"

	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/types"
)

// Client self-update SUBSET TARGETING (openZro #5 Q2).
//
// The operator-set directive (target version + force) is scoped to a
// subset of the fleet, decided SERVER-SIDE per peer at Login/Sync
// time. The targeting fields never leave the server — the client only
// ever receives the resolved proto.UpdateConfig (or nothing).
//
// Precedence (owner-signed-off spec v1):
//   1. ExcludeGroups beats EVERYTHING, including an explicit
//      TargetPeers entry — routing/gateway/server peers must never
//      silently self-update even if fat-fingered into the peer list.
//   2. An explicit TargetPeers entry is in scope and PIERCES the
//      rollout ring (canary / break-glass).
//   3. TargetGroups membership is in scope but SUBJECT to the ring.
//   4. Empty TargetGroups AND empty TargetPeers => the whole fleet
//      (the pre-Q2 behaviour), still subject to the ring.
//
// Fail-closed: when the decision depends on group membership and that
// membership could not be resolved (groupsKnown == false), the peer
// is NOT targeted — never risk auto-updating a peer we cannot prove
// is outside an ExcludeGroup.
//
// Clean-room (AGPL management/): modeled on openZro's own in-tree
// group-overlap precedents (applyFlowGroupFilter,
// admission.HasGroupOverlap) and the public NetBird behaviour of an
// operator-set fleet update target. No upstream AGPL management/
// source consulted.

// clientUpdateNeedsGroups reports whether evaluating the directive
// for a peer requires that peer's group membership. Only the group
// dimensions need it; TargetPeers and the percentage ring do not, so
// the hot path skips the (potentially failing) group lookup whenever
// the operator scopes by peer/percent alone.
func clientUpdateNeedsGroups(s *types.Settings) bool {
	return s != nil && s.ClientUpdateTargetVersion != "" &&
		(len(s.ClientUpdateTargetGroups) > 0 || len(s.ClientUpdateExcludeGroups) > 0)
}

// groupOverlap is true iff a and b share at least one element. Map is
// built from the smaller slice (peers are in 1–3 groups, scope a
// small handful) to keep the constant down. Pure.
func groupOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	idx := make(map[string]struct{}, len(small))
	for _, g := range small {
		idx[g] = struct{}{}
	}
	for _, g := range large {
		if _, ok := idx[g]; ok {
			return true
		}
	}
	return false
}

// updateBucket maps a peer's stable WireGuard public key to a
// deterministic 0..99 slot for the server-side staged ring. It is a
// PURE, cluster-deterministic function of the key alone (no time, no
// rand, no per-replica state) so every management replica agrees on
// who is in the ring, and a peer stays consistently in/out across
// Syncs. The algorithm (crc32-IEEE % 100) is a behavioral contract:
// changing it re-buckets the entire fleet mid-rollout, so it must not
// be altered. Deliberately independent of the client-side
// selfupdate.bucketOf — under Q2 the client skips its own bucket
// (Authoritative, C1), so the server ring is the single source of
// rollout truth.
func updateBucket(peerKey string) int {
	return int(crc32.ChecksumIEEE([]byte(peerKey)) % 100)
}

// peerTargetedForUpdate decides, server-side, whether this peer is in
// the operator's update subset. Pure + deterministic (see
// updateBucket) so it is cluster-safe and fully unit-testable.
func peerTargetedForUpdate(s *types.Settings, peerID, peerKey string, peerGroups []string, groupsKnown bool) bool {
	if s == nil || s.ClientUpdateTargetVersion == "" {
		return false
	}

	// Fail-closed: a group-dependent decision with unknown membership
	// must not target the peer (it could be in an ExcludeGroup).
	if clientUpdateNeedsGroups(s) && !groupsKnown {
		return false
	}

	// 1. Exclude beats everything, including explicit peers.
	if groupOverlap(peerGroups, s.ClientUpdateExcludeGroups) {
		return false
	}

	// 2. Explicit peer: in scope, pierces the ring.
	for _, id := range s.ClientUpdateTargetPeers {
		if id == peerID {
			return true
		}
	}

	// 3/4. Group membership or no-constraint(whole fleet), then ring.
	inTargetGroups := len(s.ClientUpdateTargetGroups) > 0 && groupOverlap(peerGroups, s.ClientUpdateTargetGroups)
	noConstraint := len(s.ClientUpdateTargetGroups) == 0 && len(s.ClientUpdateTargetPeers) == 0
	if !inTargetGroups && !noConstraint {
		return false
	}

	return passesUpdateRing(s.ClientUpdateRolloutPercent, peerKey)
}

// passesUpdateRing applies the server staged ring. nil => no ring
// (everyone). Explicit 0 => nobody (fail-closed, same nil-vs-0
// discipline as the manifest StagedRollout). >=100 => everyone.
func passesUpdateRing(percent *int, peerKey string) bool {
	if percent == nil {
		return true
	}
	r := *percent
	switch {
	case r <= 0:
		return false
	case r >= 100:
		return true
	default:
		return updateBucket(peerKey) < r
	}
}

// buildUpdateConfig maps the operator-set account Settings into the
// per-peer client self-update directive (openZro #5). nil when no
// target is set OR this peer is not in the operator's subset (the
// client then does nothing). Targeting fields are never put on the
// wire. Clean-room: see file header.
func buildUpdateConfig(s *types.Settings, peerID, peerKey string, peerGroups []string, groupsKnown bool) *proto.UpdateConfig {
	if !peerTargetedForUpdate(s, peerID, peerKey, peerGroups, groupsKnown) {
		return nil
	}
	return &proto.UpdateConfig{
		TargetVersion: s.ClientUpdateTargetVersion,
		Force:         s.ClientUpdateForce,
	}
}
