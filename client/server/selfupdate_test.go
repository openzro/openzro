package server

import (
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/openzro/openzro/client/proto"
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

// waitPreflightDone polls buildUpdateState until the async R3c
// preflight worker has published a verdict (decision is no longer the
// transient "checking…"), or fails the test on timeout.
func waitPreflightDone(t *testing.T, s *Server) *proto.UpdateState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := s.buildUpdateState()
		if st != nil && !strings.Contains(st.GetLastDecision(), "checking") {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("preflight did not complete within deadline")
	return nil
}

// TestBuildUpdateState pins the openZro #5 R3a→R3c daemon→UI surface:
// nil before any Sync directive (post-restart staleness fix), and an
// async, fail-closed verdict once a directive is recorded.
func TestBuildUpdateState(t *testing.T) {
	// Keep the management-driven manifest path unconfigured so the
	// preflight is deterministic and never hits the network.
	t.Setenv("OPENZRO_UPDATE_MANIFEST_TEMPLATE", "")

	t.Run("nil until a directive has been seen", func(t *testing.T) {
		s := &Server{}
		if got := s.buildUpdateState(); got != nil {
			t.Fatalf("expected nil before any Sync directive, got %+v", got)
		}
	})

	t.Run("cleared directive resolves to not-available everywhere", func(t *testing.T) {
		s := &Server{}
		s.onUpdateDirective("", false)
		st := waitPreflightDone(t, s)
		if st.GetTargetVersion() != "" || st.GetForce() {
			t.Fatalf("round-trip mismatch: %q force=%v", st.GetTargetVersion(), st.GetForce())
		}
		if st.GetAvailable() {
			t.Fatal("a cleared directive must never be available")
		}
		if !strings.Contains(st.GetLastDecision(), "no directive") {
			t.Fatalf("decision %q must explain the cleared directive", st.GetLastDecision())
		}
	})

	t.Run("directed target is fail-closed when no manifest path is configured", func(t *testing.T) {
		s := &Server{}
		s.onUpdateDirective("9.9.9", true)
		st := waitPreflightDone(t, s)
		if st.GetTargetVersion() != "9.9.9" || !st.GetForce() {
			t.Fatalf("round-trip mismatch: %q force=%v", st.GetTargetVersion(), st.GetForce())
		}
		// macOS-only in this phase OR template not configured — either
		// way the safe default is NOT available, never an accidental
		// "yes" off an unconfigured path.
		if st.GetAvailable() {
			t.Fatalf("must fail closed, got available with decision %q", st.GetLastDecision())
		}
		if st.GetLastDecision() == "" {
			t.Fatal("a not-available verdict must still carry a reason")
		}
	})

	t.Run("a newer directive supersedes a stale verdict", func(t *testing.T) {
		s := &Server{}
		s.onUpdateDirective("1.2.3", false)
		_ = waitPreflightDone(t, s)
		s.onUpdateDirective("4.5.6", true)
		st := waitPreflightDone(t, s)
		if st.GetTargetVersion() != "4.5.6" || !st.GetForce() {
			t.Fatalf("expected the latest directive to win, got %q force=%v",
				st.GetTargetVersion(), st.GetForce())
		}
	})
}
