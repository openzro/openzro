package server

import (
	"context"
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

// TestRunSelfUpdate_NoDirectiveIsSkip pins openZro #5 R3d: a manual
// "Install now" with no (or a cleared) management directive is a
// normal Skipped result, never an error or an accidental install.
func TestRunSelfUpdate_NoDirectiveIsSkip(t *testing.T) {
	t.Run("never received a directive", func(t *testing.T) {
		s := &Server{}
		resp, err := s.runSelfUpdate(context.Background(), true)
		if err != nil {
			t.Fatalf("no-directive must not error: %v", err)
		}
		if !resp.GetSkipped() || !strings.Contains(resp.GetReason(), "no update directive") {
			t.Fatalf("expected skip w/ reason, got %+v", resp)
		}
	})

	t.Run("directive explicitly cleared", func(t *testing.T) {
		s := &Server{}
		s.onUpdateDirective("", false)
		resp, err := s.runSelfUpdate(context.Background(), true)
		if err != nil {
			t.Fatalf("cleared directive must not error: %v", err)
		}
		if !resp.GetSkipped() {
			t.Fatalf("cleared directive must skip, got %+v", resp)
		}
	})
}

// TestBuildSelfUpdateConfig pins the Codex-#1 fix and I2 binding:
// AutoInstallEnabled = manual || force, ExpectedVersion = target,
// the per-version TEMPLATE manifest, and a fail-closed config error
// when the template is misconfigured.
func TestBuildSelfUpdateConfig(t *testing.T) {
	t.Run("manual or force opens the auto gate; target is bound", func(t *testing.T) {
		t.Setenv("OPENZRO_UPDATE_MANIFEST_TEMPLATE", "https://dl.example.com/{version}/m.json")
		s := &Server{}

		matrix := []struct {
			manual, force, wantAuto bool
		}{
			{false, false, false},
			{true, false, true},
			{false, true, true},
			{true, true, true},
		}
		for _, m := range matrix {
			cfg, err := s.buildSelfUpdateConfig("0.30.0", m.manual, m.force)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.AutoInstallEnabled != m.wantAuto {
				t.Fatalf("manual=%v force=%v: AutoInstallEnabled=%v want %v",
					m.manual, m.force, cfg.AutoInstallEnabled, m.wantAuto)
			}
			if cfg.ExpectedVersion != "0.30.0" {
				t.Fatalf("ExpectedVersion must bind to target, got %q", cfg.ExpectedVersion)
			}
			if cfg.ManifestURL != "https://dl.example.com/0.30.0/m.json" {
				t.Fatalf("manifest must be the per-version template URL, got %q", cfg.ManifestURL)
			}
		}
	})

	t.Run("template missing {version} is a fail-closed config error", func(t *testing.T) {
		t.Setenv("OPENZRO_UPDATE_MANIFEST_TEMPLATE", "https://dl.example.com/m.json")
		s := &Server{}
		if _, err := s.buildSelfUpdateConfig("0.30.0", true, false); err == nil {
			t.Fatal("a template without the {version} token must fail closed")
		}
	})
}

// TestPreflightBaseDelay pins the review-#2 retry policy: no
// actionable directive parks (0), a transient failure backs off
// exponentially and capped, and a settled directive re-checks on the
// steady interval (so a later staged_rollout bump / recovered infra
// flips availability without a new directive).
func TestPreflightBaseDelay(t *testing.T) {
	if d := preflightBaseDelay(false, "", false, 0); d != 0 {
		t.Fatalf("unseen directive must park, got %v", d)
	}
	if d := preflightBaseDelay(true, "", false, 0); d != 0 {
		t.Fatalf("cleared directive must park, got %v", d)
	}
	if d := preflightBaseDelay(true, "1.2.3", false, 5); d != preflightSteadyInterval {
		t.Fatalf("settled directive must use the steady interval, got %v", d)
	}

	if d := preflightBaseDelay(true, "1.2.3", true, 0); d != preflightRetryBase {
		t.Fatalf("first transient retry must be the base, got %v", d)
	}
	if d := preflightBaseDelay(true, "1.2.3", true, 2); d != preflightRetryBase<<2 {
		t.Fatalf("transient retry must double per attempt, got %v", d)
	}
	// Large attempt count must clamp at the cap, never overflow to <=0.
	for _, a := range []int{8, 20, 62, 100} {
		d := preflightBaseDelay(true, "1.2.3", true, a)
		if d != preflightRetryCap {
			t.Fatalf("attempt %d must clamp to cap %v, got %v", a, preflightRetryCap, d)
		}
	}
}

func TestJitterBounds(t *testing.T) {
	if jitter(0) != 0 || jitter(-1) != 0 {
		t.Fatal("non-positive jitter must be 0")
	}
	for i := 0; i < 1000; i++ {
		j := jitter(100 * time.Millisecond)
		if j < 0 || j >= 100*time.Millisecond {
			t.Fatalf("jitter out of [0,d): %v", j)
		}
	}
}

// TestMaybeAutoInstall pins review-#1: force+available installs via
// the secure pipeline exactly once per target, and is an inert no-op
// otherwise. No directive is set, so the shared pipeline returns a
// deterministic Skipped (no network / no macOS needed).
func TestMaybeAutoInstall(t *testing.T) {
	waitIdle := func(t *testing.T, s *Server) autoInstallState {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			s.autoInstallMu.Lock()
			ai := s.autoInstall
			s.autoInstallMu.Unlock()
			if !ai.inFlight {
				return ai
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("auto-install did not settle")
		return autoInstallState{}
	}

	t.Run("no-op unless force && available && target", func(t *testing.T) {
		for _, c := range []struct {
			target       string
			force, avail bool
		}{
			{"", true, true},
			{"9.9.9", false, true},
			{"9.9.9", true, false},
		} {
			s := &Server{}
			s.maybeAutoInstall(c.target, c.force, c.avail)
			s.autoInstallMu.Lock()
			ai := s.autoInstall
			s.autoInstallMu.Unlock()
			if ai != (autoInstallState{}) {
				t.Fatalf("must be inert for %+v, got %+v", c, ai)
			}
		}
	})

	t.Run("force+available attempts once per target", func(t *testing.T) {
		s := &Server{}
		s.maybeAutoInstall("9.9.9", true, true)

		ai := waitIdle(t, s)
		if ai.target != "9.9.9" {
			t.Fatalf("attempt must bind to target, got %q", ai.target)
		}
		// No directive set => the shared pipeline reports Skipped, so
		// the attempt completed (proves it actually ran the pipeline)
		// without an install.
		if ai.lastErr == "" {
			t.Fatal("expected a recorded skip/err from the pipeline")
		}

		// Same target again => guarded no-op (no fresh attempt).
		before := ai
		s.maybeAutoInstall("9.9.9", true, true)
		s.autoInstallMu.Lock()
		after := s.autoInstall
		s.autoInstallMu.Unlock()
		if after != before {
			t.Fatalf("repeat target must be a no-op: before=%+v after=%+v", before, after)
		}

		// A different target IS allowed to attempt again.
		s.maybeAutoInstall("1.0.0", true, true)
		ai2 := waitIdle(t, s)
		if ai2.target != "1.0.0" {
			t.Fatalf("new target must start a fresh attempt, got %q", ai2.target)
		}
	})
}
