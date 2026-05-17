package server

import (
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/openzro/openzro/version"
)

func TestClientUpdateID(t *testing.T) {
	k1, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	id1 := clientUpdateID(k1.String())
	if len(id1) != 64 { // sha256 hex
		t.Fatalf("expected 64-hex id, got %d chars", len(id1))
	}
	if id1 == k1.String() || id1 == k1.PublicKey().String() {
		t.Fatal("id must be a hash, not the raw/public key itself")
	}
	if clientUpdateID(k1.String()) != id1 {
		t.Fatal("id must be stable for the same key")
	}
	if clientUpdateID(k2.String()) == id1 {
		t.Fatal("different keys must yield different ids")
	}

	// Garbage that does not parse as a wg key still yields a stable,
	// opaque id (fallback path), never panics or empties.
	g := clientUpdateID("not-a-wireguard-key")
	if len(g) != 64 || g != clientUpdateID("not-a-wireguard-key") {
		t.Fatal("fallback id must be stable 64-hex")
	}
}

// TestBuildUpdateState pins the openZro #5 R3b daemon→UI surface:
// nil before any Sync directive (post-restart staleness fix), and a
// correct available/decision verdict once a directive is recorded.
func TestBuildUpdateState(t *testing.T) {
	running := version.OpenzroVersion()

	t.Run("nil until a directive has been seen", func(t *testing.T) {
		s := &Server{}
		if got := s.buildUpdateState(); got != nil {
			t.Fatalf("expected nil before any Sync directive, got %+v", got)
		}
	})

	cases := []struct {
		name          string
		target        string
		force         bool
		wantAvailable bool
		wantDecision  string
	}{
		{"operator cleared", "", false, false, "no directive"},
		{"running the directed version", running, false, false, "up to date"},
		{"offered, user opt-in", "9.9.9", false, true, "user opt-in"},
		{"forced, silent install", "9.9.9", true, true, "forced"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			// Drive it through the real R3a entry point so R3a+R3b
			// are exercised together.
			s.onUpdateDirective(tc.target, tc.force)

			st := s.buildUpdateState()
			if st == nil {
				t.Fatal("expected a state after a directive was recorded")
			}
			if st.GetTargetVersion() != tc.target || st.GetForce() != tc.force {
				t.Fatalf("target/force round-trip mismatch: got (%q,%v) want (%q,%v)",
					st.GetTargetVersion(), st.GetForce(), tc.target, tc.force)
			}
			if st.GetAvailable() != tc.wantAvailable {
				t.Fatalf("available: got %v want %v", st.GetAvailable(), tc.wantAvailable)
			}
			if !strings.Contains(st.GetLastDecision(), tc.wantDecision) {
				t.Fatalf("decision %q does not contain %q", st.GetLastDecision(), tc.wantDecision)
			}
		})
	}
}
