package server

import (
	"fmt"
	"testing"

	"github.com/openzro/openzro/management/server/types"
)

func intp(i int) *int { return &i }

// findKeysByBucket returns one peer key whose updateBucket < boundary
// ("in" the ring) and one whose bucket >= boundary ("out"), so ring
// tests are deterministic without hard-coding hashes.
func findKeysByBucket(t *testing.T, boundary int) (inKey, outKey string) {
	t.Helper()
	for i := 0; i < 100000 && (inKey == "" || outKey == ""); i++ {
		k := fmt.Sprintf("wgkey-%d", i)
		b := updateBucket(k)
		if b < boundary && inKey == "" {
			inKey = k
		}
		if b >= boundary && outKey == "" {
			outKey = k
		}
	}
	if inKey == "" || outKey == "" {
		t.Fatalf("could not synthesize in/out keys for boundary %d", boundary)
	}
	return inKey, outKey
}

func TestUpdateBucket(t *testing.T) {
	for i := 0; i < 5000; i++ {
		b := updateBucket(fmt.Sprintf("k-%d", i))
		if b < 0 || b > 99 {
			t.Fatalf("bucket out of 0..99: %d", b)
		}
	}
	if updateBucket("stable") != updateBucket("stable") {
		t.Fatal("updateBucket must be deterministic for the same key")
	}
}

func TestClientUpdateNeedsGroups(t *testing.T) {
	if clientUpdateNeedsGroups(nil) {
		t.Fatal("nil settings must not need groups")
	}
	if clientUpdateNeedsGroups(&types.Settings{ClientUpdateTargetGroups: []string{"g"}}) {
		t.Fatal("no target version => no directive => no group lookup")
	}
	base := &types.Settings{ClientUpdateTargetVersion: "1.0.0"}
	if clientUpdateNeedsGroups(base) {
		t.Fatal("target only (no group fields) must NOT need groups")
	}
	base.ClientUpdateTargetPeers = []string{"p1"}
	base.ClientUpdateRolloutPercent = intp(10)
	if clientUpdateNeedsGroups(base) {
		t.Fatal("peers/percent alone must NOT need groups (keeps hot path cheap)")
	}
	base.ClientUpdateExcludeGroups = []string{"infra"}
	if !clientUpdateNeedsGroups(base) {
		t.Fatal("exclude groups must need group resolution")
	}
}

// TestPeerTargetedForUpdate is the precedence/edge truth table for the
// signed-off spec v1.
func TestPeerTargetedForUpdate(t *testing.T) {
	const ver = "0.40.0"

	t.Run("no directive => never targeted", func(t *testing.T) {
		if peerTargetedForUpdate(nil, "p", "k", nil, true) {
			t.Fatal("nil settings")
		}
		if peerTargetedForUpdate(&types.Settings{}, "p", "k", nil, true) {
			t.Fatal("empty target version")
		}
	})

	t.Run("whole fleet when unconstrained, no ring", func(t *testing.T) {
		s := &types.Settings{ClientUpdateTargetVersion: ver}
		if !peerTargetedForUpdate(s, "p", "k", nil, true) {
			t.Fatal("no constraint + no ring must target every peer (pre-Q2 behaviour)")
		}
	})

	t.Run("ExcludeGroups beats an explicit TargetPeers entry", func(t *testing.T) {
		s := &types.Settings{
			ClientUpdateTargetVersion: ver,
			ClientUpdateTargetPeers:   []string{"p1"},
			ClientUpdateExcludeGroups: []string{"infra"},
		}
		if peerTargetedForUpdate(s, "p1", "k", []string{"infra"}, true) {
			t.Fatal("an excluded peer must never be targeted, even if explicitly listed")
		}
		if !peerTargetedForUpdate(s, "p1", "k", []string{"other"}, true) {
			t.Fatal("explicit peer NOT in an exclude group must be targeted")
		}
	})

	t.Run("explicit peer pierces the ring; others gated by it", func(t *testing.T) {
		s := &types.Settings{
			ClientUpdateTargetVersion:  ver,
			ClientUpdateTargetPeers:    []string{"canary"},
			ClientUpdateRolloutPercent: intp(0), // ring paused
		}
		if !peerTargetedForUpdate(s, "canary", "anykey", nil, true) {
			t.Fatal("explicit peer must pierce a 0% ring")
		}
		if peerTargetedForUpdate(s, "other", "anykey", nil, true) {
			t.Fatal("non-listed peer with a peer-constraint set must not be targeted")
		}
	})

	t.Run("TargetGroups are subject to the ring", func(t *testing.T) {
		inKey, outKey := findKeysByBucket(t, 10)
		s := &types.Settings{
			ClientUpdateTargetVersion:  ver,
			ClientUpdateTargetGroups:   []string{"g1"},
			ClientUpdateRolloutPercent: intp(10),
		}
		if !peerTargetedForUpdate(s, "p", inKey, []string{"g1"}, true) {
			t.Fatal("in-group + in-ring must be targeted")
		}
		if peerTargetedForUpdate(s, "p", outKey, []string{"g1"}, true) {
			t.Fatal("in-group but out-of-ring must NOT be targeted")
		}
		if peerTargetedForUpdate(s, "p", inKey, []string{"other"}, true) {
			t.Fatal("not in any target group must not be targeted")
		}
	})

	t.Run("ring semantics: nil=all, 0=none, 100=all", func(t *testing.T) {
		mk := func(p *int) *types.Settings {
			return &types.Settings{ClientUpdateTargetVersion: ver, ClientUpdateRolloutPercent: p}
		}
		if !peerTargetedForUpdate(mk(nil), "p", "k", nil, true) {
			t.Fatal("nil ring => everyone")
		}
		if peerTargetedForUpdate(mk(intp(0)), "p", "k", nil, true) {
			t.Fatal("0% ring => nobody (fail-closed)")
		}
		if !peerTargetedForUpdate(mk(intp(100)), "p", "k", nil, true) {
			t.Fatal("100% ring => everyone")
		}
	})

	t.Run("fail-closed when group decision needs unknown membership", func(t *testing.T) {
		s := &types.Settings{
			ClientUpdateTargetVersion: ver,
			ClientUpdateExcludeGroups: []string{"infra"},
		}
		if peerTargetedForUpdate(s, "p", "k", nil, false) {
			t.Fatal("unknown groups + group-dependent settings must fail closed")
		}
		// Non-group settings: groupsKnown is irrelevant, evaluate normally.
		sp := &types.Settings{ClientUpdateTargetVersion: ver, ClientUpdateTargetPeers: []string{"p"}}
		if !peerTargetedForUpdate(sp, "p", "k", nil, false) {
			t.Fatal("peer-only targeting must not be blocked by unknown groups")
		}
	})

	t.Run("constraint set but peer in neither => not targeted", func(t *testing.T) {
		s := &types.Settings{
			ClientUpdateTargetVersion: ver,
			ClientUpdateTargetGroups:  []string{"g1"},
			ClientUpdateTargetPeers:   []string{"p1"},
		}
		if peerTargetedForUpdate(s, "p2", "k", []string{"g2"}, true) {
			t.Fatal("peer outside both the target groups and the peer list")
		}
	})
}

func TestBuildUpdateConfig_Q2(t *testing.T) {
	s := &types.Settings{ClientUpdateTargetVersion: "0.40.0", ClientUpdateForce: true}
	if uc := buildUpdateConfig(s, "p", "k", nil, true); uc == nil ||
		uc.GetTargetVersion() != "0.40.0" || !uc.GetForce() {
		t.Fatalf("targeted peer must get the resolved directive, got %+v", uc)
	}
	excluded := &types.Settings{
		ClientUpdateTargetVersion: "0.40.0",
		ClientUpdateExcludeGroups: []string{"infra"},
	}
	if buildUpdateConfig(excluded, "p", "k", []string{"infra"}, true) != nil {
		t.Fatal("excluded peer must get NO directive (nil), never the targeting metadata")
	}
}
