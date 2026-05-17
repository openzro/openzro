package types

import "testing"

// TestSettings_Copy_ClientUpdateQ2 pins that Settings.Copy() deep-
// copies the openZro #5 Q2 subset-targeting fields. Copy() is an
// explicit field-by-field clone, so a forgotten field silently
// aliases (or drops) shared state — exactly the class of bug this
// guards against.
func TestSettings_Copy_ClientUpdateQ2(t *testing.T) {
	pct := 25
	orig := &Settings{
		ClientUpdateTargetVersion:  "0.40.0",
		ClientUpdateForce:          true,
		ClientUpdateTargetGroups:   []string{"g1", "g2"},
		ClientUpdateTargetPeers:    []string{"p1"},
		ClientUpdateExcludeGroups:  []string{"infra"},
		ClientUpdateRolloutPercent: &pct,
	}

	cp := orig.Copy()

	if cp.ClientUpdateTargetVersion != "0.40.0" || !cp.ClientUpdateForce {
		t.Fatalf("scalar fields not copied: %+v", cp)
	}
	if cp.ClientUpdateRolloutPercent == nil || *cp.ClientUpdateRolloutPercent != 25 {
		t.Fatalf("rollout percent not copied: %v", cp.ClientUpdateRolloutPercent)
	}
	if cp.ClientUpdateRolloutPercent == orig.ClientUpdateRolloutPercent {
		t.Fatal("rollout percent must be a NEW pointer, not aliased")
	}

	// Mutating the copy must not bleed into the original.
	*cp.ClientUpdateRolloutPercent = 99
	cp.ClientUpdateTargetGroups[0] = "mutated"
	cp.ClientUpdateTargetPeers = append(cp.ClientUpdateTargetPeers, "p2")
	cp.ClientUpdateExcludeGroups[0] = "mutated"

	if *orig.ClientUpdateRolloutPercent != 25 {
		t.Fatal("rollout percent aliased: copy mutation changed the original")
	}
	if orig.ClientUpdateTargetGroups[0] != "g1" {
		t.Fatal("target groups aliased")
	}
	if len(orig.ClientUpdateTargetPeers) != 1 {
		t.Fatal("target peers aliased")
	}
	if orig.ClientUpdateExcludeGroups[0] != "infra" {
		t.Fatal("exclude groups aliased")
	}

	// nil rollout pointer must stay nil (no spurious allocation).
	if (&Settings{}).Copy().ClientUpdateRolloutPercent != nil {
		t.Fatal("nil rollout percent must remain nil through Copy()")
	}
}
