package selfupdate

import "testing"

func mf(version, minv string, rollout int) *Manifest {
	r := rollout
	return &Manifest{
		Version:       version,
		MinVersion:    minv,
		StagedRollout: &r,
		Artifacts:     map[string]Artifact{"darwin/arm64": {URL: "u", SHA256: "s"}},
	}
}

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name         string
		in           GateInput
		wantEligible bool
		wantCritical bool
	}{
		{
			name:         "newer + auto-install on + 100% rollout -> eligible",
			in:           GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "", 100), AutoInstallEnabled: true, ClientID: "c1"},
			wantEligible: true,
		},
		{
			name:         "already current -> not eligible",
			in:           GateInput{Current: "1.1.0", Manifest: mf("1.1.0", "", 100), AutoInstallEnabled: true},
			wantEligible: false,
		},
		{
			name:         "older than manifest but auto-install OFF -> not eligible (manual only)",
			in:           GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "", 100), AutoInstallEnabled: false},
			wantEligible: false,
		},
		{
			name:         "unparseable current -> not eligible (fail-safe)",
			in:           GateInput{Current: "garbage!!", Manifest: mf("1.1.0", "", 100), AutoInstallEnabled: true},
			wantEligible: false,
		},
		{
			name:         "pinned to a different version -> blocked even though newer",
			in:           GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "", 100), AutoInstallEnabled: true, PinnedVersion: "1.0.5"},
			wantEligible: false,
		},
		{
			name:         "pinned to the manifest version -> allowed",
			in:           GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "", 100), AutoInstallEnabled: true, PinnedVersion: "1.1.0", ClientID: "c1"},
			wantEligible: true,
		},
		{
			name:         "staged_rollout 0% -> not eligible (nobody, fail-closed)",
			in:           GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "", 0), AutoInstallEnabled: true, ClientID: "c1"},
			wantEligible: false,
		},
		{
			name:         "partial rollout + empty ClientID -> not eligible (fail-closed)",
			in:           GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "", 50), AutoInstallEnabled: true, ClientID: ""},
			wantEligible: false,
		},
		{
			name:         "below min_version -> critical, bypasses staged even at 0%",
			in:           GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "1.0.5", 0), AutoInstallEnabled: true, ClientID: "c1"},
			wantEligible: true,
			wantCritical: true,
		},
		{
			name:         "critical but pinned elsewhere -> still blocked (explicit pin wins)",
			in:           GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "1.0.5", 100), AutoInstallEnabled: true, PinnedVersion: "1.0.2"},
			wantEligible: false,
			wantCritical: true,
		},
		{
			name:         "critical but auto-install OFF -> not auto-eligible (explicit choice wins)",
			in:           GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "1.0.5", 100), AutoInstallEnabled: false},
			wantEligible: false,
			wantCritical: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Evaluate(tc.in)
			if d.Eligible != tc.wantEligible {
				t.Fatalf("Eligible=%v want %v (reason=%q)", d.Eligible, tc.wantEligible, d.Reason)
			}
			if d.Critical != tc.wantCritical {
				t.Fatalf("Critical=%v want %v", d.Critical, tc.wantCritical)
			}
			if d.Reason == "" {
				t.Fatalf("Reason must always be set for observability")
			}
		})
	}
}

// TestEvaluate_NilStagedRollout: a manifest reaching the gate without
// a declared rollout must fail closed, never become a full rollout.
func TestEvaluate_NilStagedRollout(t *testing.T) {
	m := &Manifest{Version: "1.1.0", Artifacts: map[string]Artifact{"darwin/arm64": {URL: "u", SHA256: "s"}}}
	d := Evaluate(GateInput{Current: "1.0.0", Manifest: m, AutoInstallEnabled: true, ClientID: "c1"})
	if d.Eligible {
		t.Fatalf("nil StagedRollout must refuse, got eligible (reason=%q)", d.Reason)
	}
}

// TestEvaluate_StagedRollout: a stable per-client bucket means the
// same client is consistently in/out of a partial rollout, and a
// critical update bypasses staging entirely.
func TestEvaluate_StagedRollout(t *testing.T) {
	var inID, outID string
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		if bucketOf(id) < 50 {
			inID = id
		} else {
			outID = id
		}
	}
	if inID == "" || outID == "" {
		t.Skip("no split sample; bucketing still deterministic")
	}

	mIn := GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "", 50), AutoInstallEnabled: true, ClientID: inID}
	mOut := GateInput{Current: "1.0.0", Manifest: mf("1.1.0", "", 50), AutoInstallEnabled: true, ClientID: outID}

	if !Evaluate(mIn).Eligible {
		t.Fatalf("client %q (bucket %d) should be inside a 50%% rollout", inID, bucketOf(inID))
	}
	if Evaluate(mOut).Eligible {
		t.Fatalf("client %q (bucket %d) should be outside a 50%% rollout", outID, bucketOf(outID))
	}
	if Evaluate(mIn).Eligible != Evaluate(mIn).Eligible {
		t.Fatal("bucketing must be deterministic")
	}
	crit := mOut
	crit.Manifest = mf("1.1.0", "1.0.5", 50)
	d := Evaluate(crit)
	if !d.Eligible || !d.Critical {
		t.Fatalf("critical must bypass staged rollout: eligible=%v critical=%v", d.Eligible, d.Critical)
	}
}
