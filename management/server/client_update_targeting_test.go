package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

// TestClientUpdateSettingsChanged pins the predicate that drives BOTH
// the audit event and the connected-peer fanout (review-Q2 #1): every
// directive field must count as a change; nothing else should.
func TestClientUpdateSettingsChanged(t *testing.T) {
	base := func() *types.Settings {
		p := 10
		return &types.Settings{
			ClientUpdateTargetVersion:  "0.40.0",
			ClientUpdateForce:          true,
			ClientUpdateTargetGroups:   []string{"g1"},
			ClientUpdateTargetPeers:    []string{"p1"},
			ClientUpdateExcludeGroups:  []string{"infra"},
			ClientUpdateRolloutPercent: &p,
		}
	}
	if clientUpdateSettingsChanged(base(), base()) {
		t.Fatal("identical settings must not be a change")
	}
	// An unrelated field flip must NOT be seen as a directive change.
	other := base()
	other.DNSDomain = "x.example"
	if clientUpdateSettingsChanged(base(), other) {
		t.Fatal("a non-directive field must not count")
	}

	mut := []func(s *types.Settings){
		func(s *types.Settings) { s.ClientUpdateTargetVersion = "0.41.0" },
		func(s *types.Settings) { s.ClientUpdateTargetVersion = "" }, // clear
		func(s *types.Settings) { s.ClientUpdateForce = false },
		func(s *types.Settings) { s.ClientUpdateTargetGroups = []string{"g1", "g2"} },
		func(s *types.Settings) { s.ClientUpdateTargetPeers = nil },
		func(s *types.Settings) { s.ClientUpdateExcludeGroups = []string{"infra2"} },
		func(s *types.Settings) { s.ClientUpdateRolloutPercent = nil },        // ring -> nil
		func(s *types.Settings) { z := 0; s.ClientUpdateRolloutPercent = &z }, // 10 -> 0
	}
	for i, m := range mut {
		n := base()
		m(n)
		if !clientUpdateSettingsChanged(base(), n) {
			t.Fatalf("mutation %d must be detected as a directive change", i)
		}
	}
}

// TestIsGroupLinkedToClientUpdate pins review-Q2 #2: a group used only
// by client-update must register as peer-affecting — but only while a
// directive is active (no version => membership cannot change any
// delivered directive, keep the unused path cheap).
func TestIsGroupLinkedToClientUpdate(t *testing.T) {
	active := &types.Settings{
		ClientUpdateTargetVersion: "0.40.0",
		ClientUpdateTargetGroups:  []string{"rollout"},
		ClientUpdateExcludeGroups: []string{"infra"},
	}
	if !isGroupLinkedToClientUpdate(active, "rollout") {
		t.Fatal("a target group must be linked")
	}
	if !isGroupLinkedToClientUpdate(active, "infra") {
		t.Fatal("an exclude group must be linked")
	}
	if isGroupLinkedToClientUpdate(active, "unrelated") {
		t.Fatal("an unrelated group must not be linked")
	}
	if isGroupLinkedToClientUpdate(nil, "rollout") {
		t.Fatal("nil settings must not be linked")
	}
	inactive := &types.Settings{
		ClientUpdateTargetGroups:  []string{"rollout"},
		ClientUpdateExcludeGroups: []string{"infra"},
	}
	if isGroupLinkedToClientUpdate(inactive, "rollout") ||
		isGroupLinkedToClientUpdate(inactive, "infra") {
		t.Fatal("no active target version => not linked (cheap unused path)")
	}
}

// TestClientUpdateGroupDeleteFanout is the channel-level regression for
// the Q2 review-2 wiring (#5): a group referenced only by the
// client-update directive is NOT in any delete-prevention chain (so it
// can be deleted), but deleting it MUST still wake peers, because the
// resolved per-peer UpdateConfig may change. This is the deferred E2E
// that issue #45 tracked. The original #45 blocker (in-memory store
// couldn't seed/round-trip account Settings) no longer reproduces:
// ClientUpdate* settings round-trip cleanly through
// manager.UpdateAccountSettings on the same automigrate store
// setupNetworkMapTest uses, so the E2E is writable as-is with no
// store-layer change.
//
// It asserts three things over a real peer update channel:
//  1. activating the directive (UpdateAccountSettings) fans out;
//  2. deleting an unrelated, unlinked group is silent (no false wake);
//  3. deleting the directive-linked group fans out
//     (isGroupLinkedToClientUpdate -> areGroupChangesAffectPeers ->
//     UpdateAccountPeers, reached via DeleteGroups).
func TestClientUpdateGroupDeleteFanout(t *testing.T) {
	manager, account, peer1, _, _ := setupNetworkMapTest(t)

	err := manager.SaveGroups(context.Background(), account.Id, userID, []*types.Group{
		{ID: "cu-group", Name: "CUGroup", Peers: []string{peer1.ID}},
		{ID: "unrelated", Name: "Unrelated", Peers: []string{}},
	}, true)
	require.NoError(t, err)

	updMsg := manager.peersUpdateManager.CreateChannel(context.Background(), peer1.ID)
	t.Cleanup(func() {
		manager.peersUpdateManager.CloseChannel(context.Background(), peer1.ID)
	})

	s, err := manager.GetAccountSettings(context.Background(), account.Id, userID)
	require.NoError(t, err)
	s.ClientUpdateTargetVersion = "1.2.3"
	s.ClientUpdateTargetGroups = []string{"cu-group"}
	_, err = manager.UpdateAccountSettings(context.Background(), account.Id, userID, s)
	require.NoError(t, err)

	t.Run("activating the directive fans out", func(t *testing.T) {
		peerShouldReceiveUpdate(t, updMsg)
	})

	t.Run("unrelated unlinked group delete is silent", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			peerShouldNotReceiveUpdate(t, updMsg)
			close(done)
		}()
		require.NoError(t, manager.DeleteGroups(context.Background(), account.Id, userID, []string{"unrelated"}))
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("timeout waiting for peerShouldNotReceiveUpdate")
		}
	})

	t.Run("directive-linked group delete fans out", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			peerShouldReceiveUpdate(t, updMsg)
			close(done)
		}()
		require.NoError(t, manager.DeleteGroups(context.Background(), account.Id, userID, []string{"cu-group"}))
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("timeout waiting for peerShouldReceiveUpdate")
		}
	})
}
