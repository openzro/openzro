package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/openzro/openzro/client/proto"
	"github.com/openzro/openzro/version/selfupdate"
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
	// Hermetic: empty template => buildSelfUpdateConfig resolves to
	// "disabled" and selfupdate.New fails closed on non-macOS BEFORE
	// any network, so the pipeline returns deterministically.
	t.Setenv("OPENZRO_UPDATE_MANIFEST_TEMPLATE", "")

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
	genOf := func(s *Server) uint64 {
		s.updateDirectiveMu.Lock()
		defer s.updateDirectiveMu.Unlock()
		return s.updateDirective.gen
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
			s.maybeAutoInstall(c.target, c.force, c.avail, 1)
			s.autoInstallMu.Lock()
			ai := s.autoInstall
			s.autoInstallMu.Unlock()
			if ai != (autoInstallState{}) {
				t.Fatalf("must be inert for %+v, got %+v", c, ai)
			}
		}
	})

	t.Run("one attempt per generation; a new generation re-enables", func(t *testing.T) {
		s := &Server{}
		s.onUpdateDirective("9.9.9", true)
		g1 := genOf(s)

		s.maybeAutoInstall("9.9.9", true, true, g1)
		ai := waitIdle(t, s)
		if !ai.attempted || ai.gen != g1 || ai.target != "9.9.9" || ai.lastErr == "" {
			t.Fatalf("expected a completed attempt for gen %d, got %+v", g1, ai)
		}

		// Same generation again => guarded no-op.
		before := ai
		s.maybeAutoInstall("9.9.9", true, true, g1)
		s.autoInstallMu.Lock()
		after := s.autoInstall
		s.autoInstallMu.Unlock()
		if after != before {
			t.Fatalf("repeat within a generation must be a no-op: %+v -> %+v", before, after)
		}

		// The exact review-2 #1 bug: SAME version, new directive
		// generation (force toggled off then on) must retry.
		s.onUpdateDirective("9.9.9", false)
		s.onUpdateDirective("9.9.9", true)
		g3 := genOf(s)
		if g3 == g1 {
			t.Fatal("a force flip must bump the directive generation")
		}
		s.maybeAutoInstall("9.9.9", true, true, g3)
		ai3 := waitIdle(t, s)
		if !ai3.attempted || ai3.gen != g3 {
			t.Fatalf("same version under a new generation must retry, got %+v", ai3)
		}
	})

	t.Run("supersede before install releases the latch (not consumed)", func(t *testing.T) {
		s := &Server{}
		s.onUpdateDirective("2.1.0", true)
		stale := genOf(s)
		s.onUpdateDirective("", false) // operator clears it mid-flight

		s.maybeAutoInstall("2.1.0", true, true, stale)
		ai := waitIdle(t, s)
		if ai.attempted {
			t.Fatalf("a superseded directive must NOT consume the attempt, got %+v", ai)
		}
		if !strings.Contains(ai.lastErr, "superseded") {
			t.Fatalf("expected a superseded note, got %q", ai.lastErr)
		}

		// The previously-superseded target must not be permanently
		// blocked: a fresh legitimate directive installs it.
		s.onUpdateDirective("2.1.0", true)
		g := genOf(s)
		s.maybeAutoInstall("2.1.0", true, true, g)
		ai2 := waitIdle(t, s)
		if !ai2.attempted || ai2.gen != g {
			t.Fatalf("a fresh directive for the same target must attempt, got %+v", ai2)
		}
	})
}

// TestIsTransientFetchErr pins review-2 #2: only genuinely
// self-resolving failures get the fast backoff; structural ones
// (4xx, refused scheme, unsafe redirect, parse) are settled. In
// particular a *url.Error must NOT be treated as transient just
// because it satisfies net.Error.
func TestIsTransientFetchErr(t *testing.T) {
	transient := []struct {
		name string
		err  error
	}{
		{"deadline", context.DeadlineExceeded},
		{"dns", &net.DNSError{Err: "no such host"}},
		{"dns timeout", &net.DNSError{IsTimeout: true}},
		{"conn refused", &net.OpError{Op: "dial", Err: errors.New("connection refused")}},
		{"http 503", &selfupdate.HTTPStatusError{StatusCode: 503}},
		{"http 429", &selfupdate.HTTPStatusError{StatusCode: 429}},
		{"wrapped opErr in url.Error", &url.Error{Op: "Get", URL: "https://x",
			Err: &net.OpError{Op: "dial", Err: errors.New("refused")}}},
		{"wrapped http 500", fmt.Errorf("fetch: %w", &selfupdate.HTTPStatusError{StatusCode: 500})},
	}
	for _, c := range transient {
		if !isTransientFetchErr(c.err) {
			t.Errorf("%s must be transient", c.name)
		}
	}

	settled := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"http 404", &selfupdate.HTTPStatusError{StatusCode: 404}},
		{"http 400", &selfupdate.HTTPStatusError{StatusCode: 400}},
		{"refused scheme", errors.New("selfupdate: refused non-HTTPS scheme")},
		{"oversize", errors.New("selfupdate: manifest exceeds cap")},
		{"redirect-guard url.Error", &url.Error{Op: "Get", URL: "https://x",
			Err: errors.New("selfupdate: refusing HTTPS->HTTP downgrade redirect")}},
	}
	for _, c := range settled {
		if isTransientFetchErr(c.err) {
			t.Errorf("%s must be settled (not fast-retried)", c.name)
		}
	}
}

func awaitAutoIdle(t *testing.T, s *Server) autoInstallState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.autoInstallMu.Lock()
		ai := s.autoInstall
		s.autoInstallMu.Unlock()
		if ai.target != "" && !ai.inFlight {
			return ai
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("auto-install did not settle")
	return autoInstallState{}
}

func curGen(s *Server) uint64 {
	s.updateDirectiveMu.Lock()
	defer s.updateDirectiveMu.Unlock()
	return s.updateDirective.gen
}

// TestOnUpdateDirective_SemanticGen pins review-3 #2: the generation
// advances only on a real (target,force) change — a reconnect that
// re-delivers the SAME decision must NOT bump it (else the install
// latch would re-arm with no operator action).
func TestOnUpdateDirective_SemanticGen(t *testing.T) {
	t.Setenv("OPENZRO_UPDATE_MANIFEST_TEMPLATE", "")
	s := &Server{}

	s.onUpdateDirective("2.1.0", true)
	g1 := curGen(s)
	if g1 == 0 {
		t.Fatal("first directive must advance gen past 0")
	}
	// Reconnect re-delivery of the identical decision: gen stays.
	s.onUpdateDirective("2.1.0", true)
	s.onUpdateDirective("2.1.0", true)
	if curGen(s) != g1 {
		t.Fatalf("reconnect re-delivery must NOT bump gen (was %d, now %d)", g1, curGen(s))
	}
	// Real changes each advance it.
	s.onUpdateDirective("2.1.0", false) // force flip
	g2 := curGen(s)
	if g2 == g1 {
		t.Fatal("force flip must bump gen")
	}
	s.onUpdateDirective("2.2.0", false) // target change
	g3 := curGen(s)
	if g3 == g2 {
		t.Fatal("target change must bump gen")
	}
	s.onUpdateDirective("", false) // clear (target change)
	g4 := curGen(s)
	if g4 == g3 {
		t.Fatal("clear must bump gen")
	}
	s.onUpdateDirective("2.2.0", false) // re-issue after clear
	if curGen(s) == g4 {
		t.Fatal("re-issue after clear must bump gen")
	}
}

// TestMaybeAutoInstall_ReconnectDoesNotRearm pins review-3 #2 at the
// behavior level: a failed attempt is NOT retried just because a
// reconnect re-delivered the same directive; only a genuine new
// directive re-arms.
func TestMaybeAutoInstall_ReconnectDoesNotRearm(t *testing.T) {
	t.Setenv("OPENZRO_UPDATE_MANIFEST_TEMPLATE", "")
	s := &Server{}

	s.onUpdateDirective("2.1.0", true)
	g := curGen(s)
	s.maybeAutoInstall("2.1.0", true, true, g)
	ai := awaitAutoIdle(t, s)
	if !ai.attempted || ai.gen != g {
		t.Fatalf("expected an attempt for gen %d, got %+v", g, ai)
	}

	// Reconnect: same decision re-delivered, gen unchanged.
	s.onUpdateDirective("2.1.0", true)
	if curGen(s) != g {
		t.Fatalf("reconnect must keep gen %d, got %d", g, curGen(s))
	}
	before := ai
	s.maybeAutoInstall("2.1.0", true, true, curGen(s))
	s.autoInstallMu.Lock()
	after := s.autoInstall
	s.autoInstallMu.Unlock()
	if after != before {
		t.Fatalf("reconnect must NOT re-arm a consumed attempt: %+v -> %+v", before, after)
	}

	// A genuine operator change DOES re-arm.
	s.onUpdateDirective("2.2.0", true)
	g2 := curGen(s)
	s.maybeAutoInstall("2.2.0", true, true, g2)
	ai2 := awaitAutoIdle(t, s)
	if !ai2.attempted || ai2.gen != g2 {
		t.Fatalf("a real new directive must re-arm, got %+v", ai2)
	}
}

// TestMaybeAutoInstall_RetriggersWhenNewerPending pins review-3 #1
// and review-4: a superseded attempt wakes the scheduler for the
// newer directive (outer defer), and a completed cycle re-checks via
// the gate release — but the latch still prevents a reinstall loop.
func TestMaybeAutoInstall_RetriggersWhenNewerPending(t *testing.T) {
	t.Setenv("OPENZRO_UPDATE_MANIFEST_TEMPLATE", "")

	t.Run("superseded attempt wakes the scheduler", func(t *testing.T) {
		s := &Server{}
		s.preflightKick = make(chan struct{}, 1)
		s.preflightRunning = true // pretend the scheduler is alive

		// A newer directive (gen 2) is already current.
		s.updateDirectiveMu.Lock()
		s.updateDirective = updateDirective{targetVersion: "2.2.0", force: true, seen: true, gen: 2}
		s.updateDirectiveMu.Unlock()

		// An attempt for the OLD gen 1 runs and finds itself superseded
		// (returns before the install gate — no gate-release kick).
		s.maybeAutoInstall("2.1.0", true, true, 1)
		_ = awaitAutoIdle(t, s)

		select {
		case <-s.preflightKick:
		case <-time.After(time.Second):
			t.Fatal("a superseded attempt must wake the scheduler for the newer directive")
		}
	})

	t.Run("completed cycle re-checks via gate release without re-installing", func(t *testing.T) {
		s := &Server{}
		s.preflightKick = make(chan struct{}, 1)
		s.preflightRunning = true

		s.updateDirectiveMu.Lock()
		s.updateDirective = updateDirective{targetVersion: "2.1.0", force: true, seen: true, gen: 1}
		s.updateDirectiveMu.Unlock()

		s.maybeAutoInstall("2.1.0", true, true, 1)
		ai := awaitAutoIdle(t, s)
		// The attempt actually ran (acquired+released the gate), so it
		// is consumed for this generation.
		if !ai.attempted || ai.gen != 1 {
			t.Fatalf("attempt must be consumed for gen 1, got %+v", ai)
		}
		// review-4: gate release re-triggers preflight so a force
		// directive set DURING the cycle is not stranded for ~30m.
		select {
		case <-s.preflightKick:
		case <-time.After(time.Second):
			t.Fatal("gate release must re-check the directive")
		}
		// ...but the consumed latch means the re-check is a no-op:
		// a second attempt for the same generation must not run.
		before := ai
		s.maybeAutoInstall("2.1.0", true, true, 1)
		s.autoInstallMu.Lock()
		after := s.autoInstall
		s.autoInstallMu.Unlock()
		if after != before {
			t.Fatalf("same-gen re-check must not re-install: %+v -> %+v", before, after)
		}
	})
}

// TestSharedInstallGate pins review-4: the privileged install cycle
// is single-flighted across BOTH entry points (manual RPC + forced
// auto-install), so two `installer -pkg -target /` runs can never
// overlap. A loser gets a clear "already in progress" — and the auto
// path does NOT burn its per-generation attempt on a gate loss.
func TestSharedInstallGate(t *testing.T) {
	t.Setenv("OPENZRO_UPDATE_MANIFEST_TEMPLATE", "")

	t.Run("non-blocking try-lock semantics", func(t *testing.T) {
		s := &Server{}
		if !s.acquireInstallGate() {
			t.Fatal("first acquire must succeed")
		}
		if s.acquireInstallGate() {
			t.Fatal("second acquire while held must fail (no concurrent cycle)")
		}
		s.releaseInstallGate()
		if !s.acquireInstallGate() {
			t.Fatal("acquire must succeed again after release")
		}
		s.releaseInstallGate()
	})

	t.Run("manual RPC loses gate -> clean 'already in progress' skip", func(t *testing.T) {
		s := &Server{}
		s.updateDirectiveMu.Lock()
		s.updateDirective = updateDirective{targetVersion: "2.1.0", force: false, seen: true, gen: 1}
		s.updateDirectiveMu.Unlock()

		if !s.acquireInstallGate() { // a cycle (e.g. auto) is "running"
			t.Fatal("setup acquire failed")
		}
		defer s.releaseInstallGate()

		resp, err := s.runSelfUpdate(context.Background(), true)
		if err != nil {
			t.Fatalf("a gate loss must not be a hard error, got %v", err)
		}
		if !resp.GetSkipped() || resp.GetReason() != errSelfUpdateBusy.Error() {
			t.Fatalf("expected a clean busy skip, got %+v", resp)
		}
	})

	t.Run("auto loses gate -> latch NOT consumed (retries when free)", func(t *testing.T) {
		s := &Server{}
		s.preflightKick = make(chan struct{}, 1)
		s.preflightRunning = true
		s.updateDirectiveMu.Lock()
		s.updateDirective = updateDirective{targetVersion: "2.1.0", force: true, seen: true, gen: 7}
		s.updateDirectiveMu.Unlock()

		if !s.acquireInstallGate() { // a manual cycle is "running"
			t.Fatal("setup acquire failed")
		}

		s.maybeAutoInstall("2.1.0", true, true, 7)
		ai := awaitAutoIdle(t, s)
		if ai.attempted {
			t.Fatalf("a gate loss must NOT consume the forced attempt, got %+v", ai)
		}
		if !strings.Contains(ai.lastErr, "in progress") {
			t.Fatalf("expected an 'in progress' note, got %q", ai.lastErr)
		}

		// Once the holder releases, the same generation can still
		// install (latch was not burned) — proven by a fresh attempt.
		s.releaseInstallGate()
		s.maybeAutoInstall("2.1.0", true, true, 7)
		ai2 := awaitAutoIdle(t, s)
		if !ai2.attempted || ai2.gen != 7 {
			t.Fatalf("after the gate frees, the forced gen must install, got %+v", ai2)
		}
	})
}

// TestCriticalFallbackEligible pins the R6 attempt policy (extracted
// pure so it is unit-testable without driving the worker goroutine).
func TestCriticalFallbackEligible(t *testing.T) {
	now := time.Now()
	beyond := now.Add(-criticalFallbackGrace - time.Minute) // down long enough
	recent := now.Add(-time.Minute)                         // down, but not long enough
	noDir := updateDirective{}
	dir := updateDirective{seen: true, targetVersion: "1.2.3"}

	if criticalFallbackEligible(time.Time{}, time.Time{}, now, noDir) {
		t.Fatal("zero downSince must not be eligible")
	}
	if criticalFallbackEligible(recent, time.Time{}, now, noDir) {
		t.Fatal("down < grace must not be eligible")
	}
	if !criticalFallbackEligible(beyond, time.Time{}, now, noDir) {
		t.Fatal("down >= grace, no directive, no prior attempt must be eligible")
	}
	if criticalFallbackEligible(beyond, time.Time{}, now, dir) {
		t.Fatal("a recorded directive must defer to the directive path")
	}
	if criticalFallbackEligible(beyond, now.Add(-time.Minute), now, noDir) {
		t.Fatal("a recent attempt must be rate-limited")
	}
	if !criticalFallbackEligible(beyond, now.Add(-criticalFallbackMinInterval-time.Minute), now, noDir) {
		t.Fatal("an old enough prior attempt must re-enable")
	}
}

// TestBuildCriticalFallbackConfig pins the R6 fallback Config: silent
// (AutoInstallEnabled), non-authoritative, critical-only, no directed
// target; env override honoured; explicit-empty disables.
func TestBuildCriticalFallbackConfig(t *testing.T) {
	s := &Server{}

	t.Run("default (env unset) — silent critical-only static cycle", func(t *testing.T) {
		cfg, err := s.buildCriticalFallbackConfig()
		if err != nil {
			t.Fatalf("default must be configured, got %v", err)
		}
		if !cfg.CriticalOnly || cfg.Authoritative || !cfg.AutoInstallEnabled {
			t.Fatalf("posture wrong: %+v", cfg)
		}
		if cfg.ExpectedVersion != "" {
			t.Fatalf("fallback must not bind a directed version, got %q", cfg.ExpectedVersion)
		}
		if !strings.Contains(cfg.ManifestURL, "update-manifest.json") {
			t.Fatalf("unexpected static manifest url %q", cfg.ManifestURL)
		}
	})

	t.Run("env override honoured", func(t *testing.T) {
		t.Setenv("OPENZRO_UPDATE_MANIFEST_URL", "https://mirror.example/u.json")
		cfg, err := s.buildCriticalFallbackConfig()
		if err != nil || cfg.ManifestURL != "https://mirror.example/u.json" {
			t.Fatalf("env override not applied: %q %v", cfg.ManifestURL, err)
		}
	})

	t.Run("explicit-empty disables the fallback", func(t *testing.T) {
		t.Setenv("OPENZRO_UPDATE_MANIFEST_URL", "  ")
		if _, err := s.buildCriticalFallbackConfig(); err == nil {
			t.Fatal("explicit-empty must disable (error)")
		}
	})
}
