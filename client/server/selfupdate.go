package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/client/proto"
	"github.com/openzro/openzro/version"
	"github.com/openzro/openzro/version/selfupdate"
)

// preflightTimeout bounds one manifest preflight (resolve + fetch +
// gate). It is network-only — no download/verify/install — so it is
// short; a hung endpoint must not pin the scheduler.
const preflightTimeout = 30 * time.Second

const (
	// preflightRetryBase/Cap drive the exponential backoff used ONLY
	// for transient fetch failures, so a brief outage recovers in
	// tens of seconds, not at the next directive change.
	preflightRetryBase = 30 * time.Second
	preflightRetryCap  = 8 * time.Minute

	// preflightSteadyInterval re-checks a settled directive so a
	// later staged_rollout bump (or recovered infra) flips
	// availability without needing a new directive — and so the user
	// is never permanently stuck on a stale verdict.
	preflightSteadyInterval = 30 * time.Minute
)

// jitter returns a random duration in [0, d) — spreads fleet-wide
// re-checks so a synchronized directive change does not become a
// thundering herd on the manifest endpoint.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(d)))
}

// preflightBaseDelay is the pure backoff/refresh decision (jitter is
// added by the caller). 0 means "park until kicked" — there is no
// actionable directive to poll. Extracted so the review-#2 retry
// policy is unit-testable without driving the timing loop.
func preflightBaseDelay(seen bool, target string, transient bool, attempt int) time.Duration {
	if !seen || target == "" {
		return 0
	}
	if !transient {
		return preflightSteadyInterval
	}
	backoff := preflightRetryBase << attempt
	if backoff <= 0 || backoff > preflightRetryCap {
		backoff = preflightRetryCap
	}
	return backoff
}

// clientUpdateID derives a stable, opaque per-install id for staged-
// rollout bucketing from the WireGuard identity (D2: hash the PUBLIC
// key, never the raw key, so it cannot leak into a log line or a
// manifest bucket). Falls back to hashing the key string as-is if it
// does not parse — still stable per install, still opaque.
func clientUpdateID(wgPrivKey string) string {
	src := wgPrivKey
	if k, err := wgtypes.ParseKey(wgPrivKey); err == nil {
		src = k.PublicKey().String()
	}
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:])
}

// buildSelfUpdateConfig assembles a selfupdate.Config bound to the
// management-directed target (openZro #5). The manifest is the
// per-version TEMPLATE one (never the legacy rolling manifest), and
// ExpectedVersion pins the cycle to target so a wrong-version
// manifest is refused (I2). manual reflects an explicit user "Install
// now": per Codex finding #1, a manual click is itself the opt-in, so
// it bypasses ONLY the auto/force gate — AutoInstallEnabled becomes
// manual || force. Everything else (version check, staged rollout,
// min-version, signature/Team-ID verify) still applies.
func (s *Server) buildSelfUpdateConfig(target string, manual, force bool) (selfupdate.Config, error) {
	manifestURL, err := selfupdate.ResolveManifestTemplateURL(target)
	if err != nil {
		return selfupdate.Config{}, err // fail-closed config error
	}

	return selfupdate.Config{
		CurrentVersion:     version.OpenzroVersion(),
		ManifestURL:        manifestURL, // "" => RunOnce reports disabled/skipped
		ExpectedVersion:    target,
		UserAgent:          "openzro-daemon/" + version.OpenzroVersion(),
		ExpectedTeamID:     selfupdate.BuildTeamID(),
		AutoInstallEnabled: manual || force,
		ClientID:           s.clientUpdateBucketID(),
		// Authoritative: this is the management-directive path by
		// construction (the 30m poll was removed in R3a; the only
		// caller is a server-conveyed directive). The server already
		// evaluated the operator's subset/ring for this peer (Q2), so
		// the client must NOT re-roll the manifest staged_rollout —
		// that would double-gate and defeat operator control. The
		// future critical-only static fallback (R6) is a DIFFERENT
		// code path and will build its Config with Authoritative=false
		// so it still honours staged_rollout.
		Authoritative: true,
		// PinnedVersion intentionally empty: in the management-driven
		// model the directive target IS the pin and ExpectedVersion
		// enforces it. There is no client-side pin/auto toggle.
	}, nil
}

// errSelfUpdateBusy is returned by runSelfUpdateDirective when the
// shared install gate is already held. It is a sentinel, not a gRPC
// status, so each caller can map it to its own clean outcome.
var errSelfUpdateBusy = errors.New("self-update already in progress")

// acquireInstallGate is a non-blocking try-lock over the privileged
// install cycle (#5 review-4). false => another cycle (manual or
// auto) is running; the caller must NOT start a second one.
func (s *Server) acquireInstallGate() bool {
	s.installGateMu.Lock()
	defer s.installGateMu.Unlock()
	if s.installActive {
		return false
	}
	s.installActive = true
	return true
}

// releaseInstallGate frees the gate and wakes the scheduler: a force
// directive that lost the race (or arrived during the cycle) must be
// re-evaluated now, not at the ~30m steady refresh.
func (s *Server) releaseInstallGate() {
	s.installGateMu.Lock()
	s.installActive = false
	s.installGateMu.Unlock()
	s.triggerUpdatePreflight()
}

// runSelfUpdate executes one rollout-gated cycle for the CURRENT
// management directive. manual=true is the explicit user "Install
// now" path (the manual RPC always acts on whatever is live now).
// A lost race for the shared install gate is reported as a normal
// "already in progress" Skipped result, not an error.
func (s *Server) runSelfUpdate(ctx context.Context, manual bool) (*proto.UpdateResponse, error) {
	s.updateDirectiveMu.Lock()
	d := s.updateDirective
	s.updateDirectiveMu.Unlock()
	resp, err := s.runSelfUpdateDirective(ctx, d, manual)
	if errors.Is(err, errSelfUpdateBusy) {
		return &proto.UpdateResponse{Skipped: true, Reason: errSelfUpdateBusy.Error()}, nil
	}
	return resp, err
}

// runSelfUpdateDirective executes one rollout-gated cycle bound to an
// explicit directive snapshot. The auto-install path passes the
// directive it already validated as still-current, so the install is
// never silently re-pointed at a directive that changed under it
// (review-2 #1). With no directive (or a cleared one) there is
// nothing to install — a normal Skipped result, not an error.
func (s *Server) runSelfUpdateDirective(ctx context.Context, d updateDirective, manual bool) (*proto.UpdateResponse, error) {
	if !d.seen || d.targetVersion == "" {
		return &proto.UpdateResponse{
			Skipped: true,
			Reason:  "no update directive from management — nothing to install",
		}, nil
	}

	cfg, err := s.buildSelfUpdateConfig(d.targetVersion, manual, d.force)
	if err != nil {
		// Fail-closed config error (e.g. template missing {version}).
		return nil, status.Errorf(codes.FailedPrecondition, "selfupdate config: %v", err)
	}

	// #5128 (openZro side, R4 review #3): flush the user's latest
	// route / exit-node selection AFTER verify and immediately before
	// the privileged install+restart — glued to the restart, not
	// merely before the whole fetch/download/verify cycle, so the
	// residual race (selection changed while the pkg downloaded) is
	// closed. Best-effort: a flush failure is logged and we return
	// nil so the engine never aborts a (potentially security) update
	// on it; no-op when no engine is connected.
	cfg.BeforeInstall = func(context.Context) error {
		if s.connectClient != nil {
			if perr := s.connectClient.PersistState(); perr != nil {
				log.Warnf("client self-update: pre-install state flush failed (continuing): %v", perr)
			}
		}
		return nil
	}

	// Shared single-flight: at most one privileged install cycle at a
	// time across BOTH the manual RPC and the forced auto-install
	// (#5 review-4). A loser returns the sentinel rather than running
	// a second `installer -pkg -target /` concurrently.
	if !s.acquireInstallGate() {
		return nil, errSelfUpdateBusy
	}
	defer s.releaseInstallGate()

	u, err := selfupdate.New(cfg)
	if err == selfupdate.ErrUnsupportedPlatform {
		return nil, status.Error(codes.Unimplemented, "self-update is macOS-only in phase 1")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "selfupdate init: %v", err)
	}

	res, err := u.RunOnce(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "selfupdate: %v", err)
	}
	return &proto.UpdateResponse{
		Installed: res.Installed,
		Skipped:   res.Skipped,
		Version:   res.Version,
		Reason:    res.Reason,
		Critical:  res.Critical,
	}, nil
}

// autoInstallState tracks the single in-flight / last forced
// auto-install (#5 review-2 #1). gen binds the latch to the directive
// GENERATION (not just the version), so a brand-new directive — even
// for the same target, e.g. force toggled off then on — always
// re-enables one fresh attempt. attempted is set only when an install
// actually ran under the triggering directive: a pre-install
// supersede reverts the latch instead of consuming it, so a later
// legitimate force directive is not silently swallowed.
type autoInstallState struct {
	gen       uint64
	target    string
	inFlight  bool
	attempted bool
	lastErr   string
}

// maybeAutoInstall silently installs a force=true directive whose
// preflight is positive, through the SAME rollout-gated secure
// pipeline as the manual RPC (download → Team-ID verify → install).
// This is what makes force actually mean "silent install" rather
// than merely "available" (review-1 #1). One attempt per directive
// generation (review-2 #1): no-op while in flight or once attempted
// for this gen, and the latch is only consumed if the install really
// begins under the SAME still-current force directive that triggered
// it — otherwise it is released so the next directive can retry.
func (s *Server) maybeAutoInstall(target string, force, available bool, gen uint64) {
	if !force || !available || target == "" {
		return
	}

	s.autoInstallMu.Lock()
	if s.autoInstall.inFlight || (s.autoInstall.attempted && s.autoInstall.gen == gen) {
		s.autoInstallMu.Unlock()
		return
	}
	s.autoInstall = autoInstallState{gen: gen, target: target, inFlight: true}
	s.autoInstallMu.Unlock()

	go func() {
		// Declared first => runs LAST (after the recover defer has
		// cleared inFlight). If a NEWER directive arrived while this
		// attempt was in flight (it was blocked by the global inFlight
		// guard), wake the scheduler now instead of letting it sit
		// until the ~30m steady refresh (review-3 #1). gen advances
		// only on a real change (review-3 #2), so a bare reconnect
		// does NOT spuriously re-trigger here.
		defer func() {
			s.updateDirectiveMu.Lock()
			live := s.updateDirective
			s.updateDirectiveMu.Unlock()
			if live.seen && live.gen != gen {
				s.triggerUpdatePreflight()
			}
		}()
		defer func() {
			if r := recover(); r != nil {
				s.autoInstallMu.Lock()
				s.autoInstall.inFlight = false
				s.autoInstall.attempted = true
				s.autoInstall.lastErr = "panic during auto-install"
				s.autoInstallMu.Unlock()
				log.Errorf("client self-update: auto-install recovered from panic: %v", r)
			}
		}()

		// Re-read the LIVE directive: only install if the exact
		// directive that triggered this (same gen, still force, same
		// target) is still current. If it changed under us, RELEASE
		// the latch (attempted stays false) so a later legitimate
		// force directive is not lost.
		s.updateDirectiveMu.Lock()
		live := s.updateDirective
		s.updateDirectiveMu.Unlock()

		if !live.seen || live.gen != gen || !live.force || live.targetVersion != target {
			s.autoInstallMu.Lock()
			s.autoInstall.inFlight = false
			s.autoInstall.lastErr = "superseded before install — will retry on the next directive"
			s.autoInstallMu.Unlock()
			log.Infof("client self-update: auto-install %s superseded before start (gen %d) — latch released",
				target, gen)
			return
		}

		base := s.rootCtx
		if base == nil {
			base = context.Background()
		}
		log.Infof("client self-update: force directive — auto-installing %s (gen %d)", target, gen)

		// manual=false: the operator's force IS the opt-in, so
		// buildSelfUpdateConfig sets AutoInstallEnabled = false||force
		// = true. Bound to the validated snapshot so it cannot be
		// re-pointed mid-flight. Every other safety (version check,
		// staged rollout, min-version, Team-ID verify, I2) still holds.
		resp, err := s.runSelfUpdateDirective(base, live, false)

		// Lost the shared gate to an in-flight cycle (manual or auto):
		// DO NOT consume the attempt — releasing it (attempted=false)
		// lets this same directive be retried once the gate frees
		// (releaseInstallGate re-triggers the scheduler). Without
		// this, a concurrent manual install would silently burn the
		// forced directive's only attempt.
		if errors.Is(err, errSelfUpdateBusy) {
			s.autoInstallMu.Lock()
			s.autoInstall.inFlight = false
			s.autoInstall.lastErr = "another self-update cycle in progress — will retry"
			s.autoInstallMu.Unlock()
			log.Infof("client self-update: auto-install %s deferred — install gate busy", target)
			return
		}

		s.autoInstallMu.Lock()
		s.autoInstall.inFlight = false
		s.autoInstall.attempted = true
		switch {
		case err != nil:
			s.autoInstall.lastErr = err.Error()
			log.Errorf("client self-update: auto-install %s failed: %v", target, err)
		case resp.GetInstalled():
			s.autoInstall.lastErr = ""
			log.Infof("client self-update: auto-installed %s — service will restart", resp.GetVersion())
		default:
			s.autoInstall.lastErr = "skipped: " + resp.GetReason()
			log.Infof("client self-update: auto-install %s skipped: %s", target, resp.GetReason())
		}
		s.autoInstallMu.Unlock()
	}()
}

// Update is DaemonService.Update: the unprivileged Fyne UI asks the
// privileged daemon to run the cycle (C1 — only the daemon can run
// `installer -target /` as root) for whatever version management has
// directed. Clicking it IS the explicit opt-in (manual=true), so it
// proceeds even when the directive is force=false; it still refuses
// if there is no directive or the manifest/version checks fail.
// "skipped" (not eligible) is a normal response; a genuine failure is
// a gRPC status error.
func (s *Server) Update(ctx context.Context, _ *proto.UpdateRequest) (*proto.UpdateResponse, error) {
	return s.runSelfUpdate(ctx, true)
}

// updateDirective is the latest management-conveyed client
// self-update decision (openZro #5). targetVersion == "" means the
// operator has no active directive — clients do nothing. seen
// distinguishes "never received a Sync directive" from "operator
// explicitly cleared the target", which R3b/R3c need to surface
// Available correctly.
type updateDirective struct {
	targetVersion string
	force         bool
	seen          bool
	// gen is a monotonic id advanced only on a SEMANTIC change of
	// (target,force) — see onUpdateDirective. The engine's per-Engine
	// dedupe is not sufficient on its own (a reconnect rebuilds the
	// Engine and re-delivers the same decision), so the daemon
	// dedupes too. The auto-install latch keys on gen, so a genuine
	// new directive (incl. a force flip / clear, or the same version
	// re-issued after a different one) re-enables one fresh attempt,
	// while a bare reconnect does not. gen==0 means "no directive
	// ever"; a daemon restart starts empty and correctly re-arms.
	gen uint64
}

// updatePreflight is the async verdict for the recorded directive
// (openZro #5 R3c). forTarget binds the verdict to the target it was
// computed for, so buildUpdateState can detect (and not surface) a
// verdict that a newer directive has already superseded. done=false
// means "still checking". No signature verification happens here —
// that is deferred to the install path (R3d); preflight is
// network-cheap and read-only.
type updatePreflight struct {
	forTarget string
	available bool
	reason    string
	done      bool
	// transient is true only when the verdict is "not available
	// because the manifest fetch failed" (network/DNS/5xx/timeout) —
	// i.e. it may flip on its own. The scheduler retries these with
	// backoff; settled outcomes (config error, I2 mismatch, gate
	// decision, macOS-only, disabled) are not retried fast. Internal
	// only — never crosses the proto boundary.
	transient bool
}

// onUpdateDirective is the daemon side of the management-driven
// self-update seam (openZro #5). The engine invokes it — deduped by
// (target,force), while holding syncMsgMux — whenever the operator's
// fleet decision changes on the Sync stream. It MUST stay cheap and
// non-blocking: it only records the directive, marks the preflight
// pending for the new target, and kicks the single-flight preflight
// worker. The rollout-gated secure pipeline runs on the daemon's own
// goroutines and reads s.updateDirective — never from this callback.
func (s *Server) onUpdateDirective(targetVersion string, force bool) {
	s.updateDirectiveMu.Lock()
	cur := s.updateDirective
	// Semantic dedupe in the DAEMON (review-3 #2): the engine's
	// (target,force) dedupe lives per-Engine, and a management
	// reconnect builds a fresh Engine that re-delivers the SAME
	// decision. Bumping gen on every delivery would re-arm the
	// install latch on a mere reconnect — defeating "a failed
	// auto-install waits for a NEW operator decision". So only a
	// genuine change (or the first-ever delivery) advances the
	// generation. A daemon RESTART still re-arms correctly because
	// its state starts empty (cur.seen == false).
	gen := cur.gen
	if !(cur.seen && cur.targetVersion == targetVersion && cur.force == force) {
		gen++
	}
	s.updateDirective = updateDirective{
		targetVersion: targetVersion,
		force:         force,
		seen:          true,
		gen:           gen,
	}
	// Drop any prior verdict immediately: until the worker re-checks,
	// the honest answer for the NEW target is "checking", never the
	// stale verdict of the previous target.
	s.updatePreflight = updatePreflight{forTarget: targetVersion}
	s.updateDirectiveMu.Unlock()

	if targetVersion == "" {
		log.Infof("client self-update: management cleared the update directive")
	} else {
		log.Infof("client self-update: management directive target=%s force=%t (recorded; preflight queued)",
			targetVersion, force)
	}

	s.triggerUpdatePreflight()
}

// triggerUpdatePreflight ensures the scheduler goroutine is running
// and wakes it immediately (non-blocking) so a directive change is
// acted on at once instead of at the next steady tick.
func (s *Server) triggerUpdatePreflight() {
	s.preflightMu.Lock()
	if s.preflightKick == nil {
		s.preflightKick = make(chan struct{}, 1)
	}
	kick := s.preflightKick
	if !s.preflightRunning {
		s.preflightRunning = true
		s.preflightMu.Unlock()
		// A freshly started scheduler runs its first iteration
		// immediately; kicking too would just cause a redundant
		// back-to-back preflight (review-2 #3).
		go s.updatePreflightScheduler(kick)
		return
	}
	s.preflightMu.Unlock()

	select {
	case kick <- struct{}{}:
	default: // a wake is already pending — coalesce
	}
}

// updatePreflightScheduler is the single long-lived preflight loop.
// It recomputes the active directive's verdict, then sleeps:
//   - transient fetch failure  -> exponential backoff (fast recovery)
//   - settled, has a target    -> steady interval + jitter (lets a
//     later staged_rollout bump / recovered infra flip availability,
//     and guarantees the user is never permanently stuck)
//   - no/blank directive       -> park until kicked (nothing to poll)
//
// A directive change kicks the loop awake and resets the backoff.
// Bounded to one goroutine; ctx is process-lifetime, each pass has
// preflightTimeout so a hung endpoint cannot wedge it.
func (s *Server) updatePreflightScheduler(kick <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("client self-update: preflight scheduler recovered from panic: %v", r)
		}
		// Allow a later trigger to restart the loop.
		s.preflightMu.Lock()
		s.preflightRunning = false
		s.preflightMu.Unlock()
	}()

	base := s.rootCtx
	if base == nil {
		// Only nil for a unit-constructed &Server{}; real New() always
		// sets it. Keeps the scheduler panic-free in tests.
		base = context.Background()
	}

	attempt := 0
	lastTarget := "\x00not-a-target" // force a reset on first pass

	for {
		s.updateDirectiveMu.Lock()
		d := s.updateDirective
		s.updateDirectiveMu.Unlock()

		if d.targetVersion != lastTarget {
			attempt = 0 // a new directive: start backoff fresh
			lastTarget = d.targetVersion
		}

		ctx, cancel := context.WithTimeout(base, preflightTimeout)
		pf := s.computeUpdatePreflight(ctx, d.targetVersion, d.force)
		cancel()

		s.updateDirectiveMu.Lock()
		// Publish only if the directive has not moved on under us, so
		// a slow compute for an old target never clobbers a newer one.
		live := s.updateDirective
		published := live.targetVersion == pf.forTarget
		if published {
			s.updatePreflight = pf
		}
		s.updateDirectiveMu.Unlock()

		log.Infof("client self-update: preflight target=%q available=%t transient=%t reason=%q",
			pf.forTarget, pf.available, pf.transient, pf.reason)

		// Force directive + positive preflight => install silently
		// now (review-1 #1), latched per directive generation
		// (review-2 #1). No-op otherwise; self-guards repeat/concurrent
		// attempts and releases the latch if superseded before start.
		if published {
			s.maybeAutoInstall(pf.forTarget, live.force, pf.available, live.gen)
		}

		baseDelay := preflightBaseDelay(d.seen, d.targetVersion, pf.transient, attempt)
		var delay time.Duration
		switch {
		case baseDelay == 0:
			// No actionable directive: park until kicked.
			delay = 0
		case pf.transient:
			delay = baseDelay + jitter(baseDelay/3)
			attempt++
		default:
			attempt = 0
			delay = baseDelay + jitter(baseDelay/6)
		}

		if delay == 0 {
			select {
			case <-base.Done():
				return
			case <-kick:
				continue
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-base.Done():
			timer.Stop()
			return
		case <-kick:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// isTransientFetchErr decides whether a FetchManifest failure is
// worth the fast 30s→8m backoff (review-2 #2). Only genuinely
// self-resolving conditions qualify: a deadline, a timeout, a DNS
// failure (network not up at boot), a refused/unreachable socket, or
// an HTTP 429/5xx. Everything else — a 4xx, a malformed/oversize
// manifest, a refused scheme, an unsafe redirect — is structural and
// will NOT fix itself on a 30s retry; it is left to the steady
// refresh so we neither hammer the endpoint nor lie in the logs.
// (net.Error is intentionally NOT matched broadly: *url.Error
// satisfies it even for a redirect-guard rejection, so we match the
// concrete transient types instead.)
func isTransientFetchErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) { // connection refused / reset / unreachable
		return true
	}
	var httpErr *selfupdate.HTTPStatusError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 429 || httpErr.StatusCode >= 500
	}
	return false
}

// computeUpdatePreflight resolves the per-version manifest for target
// and runs the rollout gate WITHOUT downloading or verifying — that
// is the install path's job (R3d). force is intentionally NOT a gate
// input here: "is an update available to this client" is independent
// of whether the operator wants it installed silently; force only
// decides silent-vs-prompt at install time. AutoInstallEnabled is
// pinned true so the gate reports pure version+pin+staged
// eligibility rather than the surface-only short-circuit.
func (s *Server) computeUpdatePreflight(ctx context.Context, target string, _ bool) updatePreflight {
	pf := updatePreflight{forTarget: target, done: true}

	// A cleared directive is the same truth on every platform — answer
	// it before the macOS-only gate so the UI shows the real reason.
	if target == "" {
		pf.reason = "no directive (operator cleared the target)"
		return pf
	}
	if runtime.GOOS != "darwin" {
		pf.reason = "self-update is macOS-only in phase 1"
		return pf
	}

	manifestURL, err := selfupdate.ResolveManifestTemplateURL(target)
	if err != nil {
		// Fail-closed config error (e.g. template missing {version}).
		pf.reason = err.Error()
		return pf
	}
	if manifestURL == "" {
		// Only reachable when the operator explicitly emptied the env
		// (the disable escape hatch); unset uses the built-in default.
		pf.reason = "self-update disabled by operator (OPENZRO_UPDATE_MANIFEST_TEMPLATE set empty)"
		return pf
	}

	ua := "openzro-daemon/" + version.OpenzroVersion()
	hc := &http.Client{Timeout: preflightTimeout}
	m, err := selfupdate.FetchManifest(ctx, hc, manifestURL, ua)
	if err != nil {
		if isTransientFetchErr(err) {
			// Network may be momentarily down, DNS not up at boot, a
			// 5xx/429 blip during a rollout: fast backoff so a single
			// blip does not pin the client "unavailable".
			pf.transient = true
			pf.reason = "manifest temporarily unreachable, retrying: " + err.Error()
			return pf
		}
		// Structural/permanent (4xx, bad/oversize manifest, refused
		// scheme, unsafe redirect): NOT fast-retried — the steady
		// refresh still covers a later fix.
		pf.reason = "manifest fetch failed: " + err.Error()
		return pf
	}

	// I2 — strict target binding. A per-version manifest endpoint that
	// serves a manifest for a different version than we asked for is
	// either misconfigured or hostile; refuse rather than risk
	// installing an unrequested version.
	if m.Version != target {
		pf.reason = "served manifest version " + m.Version + " != directed target " + target + " — refusing"
		return pf
	}

	d := selfupdate.Evaluate(selfupdate.GateInput{
		Current:            version.OpenzroVersion(),
		Manifest:           m,
		AutoInstallEnabled: true,
		ClientID:           s.clientUpdateBucketID(),
	})
	pf.available = d.Eligible
	pf.reason = d.Reason
	return pf
}

// clientUpdateBucketID snapshots the stable staged-rollout bucket id
// (wg-pubkey hash) from the live config. Empty when config is unset —
// the gate fail-closes such a client OUT of any partial staged
// rollout, which is the safe default.
func (s *Server) clientUpdateBucketID() string {
	s.mutex.Lock()
	cfg := s.config
	s.mutex.Unlock()
	if cfg == nil {
		return ""
	}
	return clientUpdateID(cfg.PrivateKey)
}

// buildUpdateState renders the latest recorded directive + its
// preflight verdict into the proto surface the UI reads (openZro #5).
// Returns nil when no directive has been seen this daemon lifetime —
// after a daemon restart the state is empty until the next Sync
// re-delivers it (state is keyed off the live Sync stream, never
// persisted; the Codex post-restart-staleness fix). available now
// comes from the full rollout-gated preflight (R3c), not the R3b
// minimal check.
func (s *Server) buildUpdateState() *proto.UpdateState {
	s.updateDirectiveMu.Lock()
	d := s.updateDirective
	pf := s.updatePreflight
	s.updateDirectiveMu.Unlock()

	if !d.seen {
		return nil
	}

	// A verdict only counts if it was computed for the current target
	// and has completed; otherwise we are still checking.
	verdictReady := pf.done && pf.forTarget == d.targetVersion
	available := verdictReady && pf.available

	s.autoInstallMu.Lock()
	ai := s.autoInstall
	s.autoInstallMu.Unlock()

	// Only surface auto-install status for THIS directive generation;
	// a verdict from a superseded directive must not bleed through.
	aiCurrent := ai.gen == d.gen && ai.target == d.targetVersion

	var decision string
	switch {
	case !verdictReady:
		decision = "checking update availability…"
	case !available:
		decision = pf.reason
	case d.force && aiCurrent && ai.inFlight:
		decision = "update available — operator forced: installing " + d.targetVersion + "…"
	case d.force && aiCurrent && ai.attempted && ai.lastErr != "":
		decision = "update available — operator forced: install failed (" + ai.lastErr +
			"); will retry on the next management directive"
	case d.force:
		decision = "update available — operator forced (silent install): " + pf.reason
	default:
		decision = "update available — operator offered (user opt-in): " + pf.reason
	}

	return &proto.UpdateState{
		TargetVersion: d.targetVersion,
		Force:         d.force,
		Available:     available,
		LastDecision:  decision,
	}
}

// ---- R6: critical-only static-manifest fallback ----
//
// Last resort for an UNMANAGED client: when management has been
// unreachable for a sustained period AND no operator directive is
// recorded AND the client is below the static manifest's security
// floor (min_version), self-heal SILENTLY — but never slow-roll a
// routine update behind the operator's back (CriticalOnly). Reuses
// the shared install gate so it can never collide with the directive
// or manual install paths; a recorded directive always wins (the
// directive scheduler owns updates even while mgmt is down).
const (
	// Observe connectivity often; the grace + min-interval gate the
	// ACTUAL attempt so a short mgmt blip never triggers a self-heal.
	criticalFallbackTick        = 10 * time.Minute
	criticalFallbackGrace       = time.Hour
	criticalFallbackMinInterval = time.Hour
)

// errFallbackSuperseded aborts an in-flight critical-fallback cycle
// when the authoritative path takes over mid-flight (RunOnce's
// fetch+download+verify can take minutes). Returned from the
// fallback's BeforeInstall so the engine aborts BEFORE Install;
// recognised in attemptCriticalFallback as an expected outcome, not
// a failure (#5 R6 review #1).
var errFallbackSuperseded = errors.New("critical fallback superseded — authoritative path took over")

// fallbackSupersededReason is the pure pre-install re-validation
// (extracted so the #5 R6 review-#1 invariant is unit-testable
// without a macOS install). nil => still eligible to self-heal;
// non-nil (wraps errFallbackSuperseded) => the authoritative path
// took over (client stopped / mgmt reconnected / operator directive)
// and the cycle must abort before Install.
func fallbackSupersededReason(connectActive, mgmtConnected bool, d updateDirective) error {
	switch {
	case !connectActive:
		return fmt.Errorf("%w (client stopped)", errFallbackSuperseded)
	case mgmtConnected:
		return fmt.Errorf("%w (management reconnected)", errFallbackSuperseded)
	case d.seen && d.targetVersion != "":
		return fmt.Errorf("%w (operator directive arrived)", errFallbackSuperseded)
	default:
		return nil
	}
}

func (s *Server) startCriticalFallbackOnce() {
	s.criticalFallbackOnce.Do(func() { go s.runCriticalFallbackWorker() })
}

// criticalFallbackEligible is the pure attempt decision (extracted so
// the timing policy is unit-testable without driving the goroutine).
// Caller has already established management is currently DOWN.
func criticalFallbackEligible(downSince, lastAttempt, now time.Time, d updateDirective) bool {
	if downSince.IsZero() || now.Sub(downSince) < criticalFallbackGrace {
		return false // not down long enough
	}
	if d.seen && d.targetVersion != "" {
		return false // a directive is recorded — the directive path owns it
	}
	if !lastAttempt.IsZero() && now.Sub(lastAttempt) < criticalFallbackMinInterval {
		return false // rate-limit re-attempts
	}
	return true
}

func (s *Server) runCriticalFallbackWorker() {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("client self-update: critical fallback worker recovered from panic: %v", r)
		}
	}()

	base := s.rootCtx
	if base == nil {
		base = context.Background()
	}
	ticker := time.NewTicker(criticalFallbackTick)
	defer ticker.Stop()

	var downSince, lastAttempt time.Time
	for {
		select {
		case <-base.Done():
			return
		case <-ticker.C:
		}

		// A deliberately-stopped client (manual Down, or never Up) is
		// NOT "unmanaged + should be managed" — never self-heal it
		// (#5 R6 review #2). Reset the clock so a later Up starts
		// fresh.
		if !s.connectActive.Load() {
			downSince, lastAttempt = time.Time{}, time.Time{}
			continue
		}
		if s.statusRecorder.GetManagementState().Connected {
			downSince, lastAttempt = time.Time{}, time.Time{}
			continue
		}
		now := time.Now()
		if downSince.IsZero() {
			downSince = now
		}

		s.updateDirectiveMu.Lock()
		d := s.updateDirective
		s.updateDirectiveMu.Unlock()

		if !criticalFallbackEligible(downSince, lastAttempt, now, d) {
			continue
		}
		lastAttempt = now
		s.attemptCriticalFallback(base)
	}
}

// buildCriticalFallbackConfig assembles the non-authoritative,
// critical-only static-manifest cycle. No operator-directed target
// (ExpectedVersion=""), so the static manifest's own version is the
// target; verify + Team-ID pin are unchanged. AutoInstallEnabled is
// true because this fires only when the client is vulnerable AND
// unmanaged AND a tray prompt cannot be relied on — the
// owner-signed-off silent self-heal posture. An explicitly empty
// OPENZRO_UPDATE_MANIFEST_URL disables the fallback (escape hatch).
func (s *Server) buildCriticalFallbackConfig() (selfupdate.Config, error) {
	url := selfupdate.ResolveManifestURL()
	if url == "" {
		return selfupdate.Config{}, errors.New("static fallback disabled (OPENZRO_UPDATE_MANIFEST_URL set empty)")
	}
	return selfupdate.Config{
		CurrentVersion:     version.OpenzroVersion(),
		ManifestURL:        url,
		ExpectedVersion:    "",
		UserAgent:          "openzro-daemon/" + version.OpenzroVersion(),
		ExpectedTeamID:     selfupdate.BuildTeamID(),
		AutoInstallEnabled: true,
		Authoritative:      false,
		CriticalOnly:       true,
		ClientID:           s.clientUpdateBucketID(),
	}, nil
}

func (s *Server) attemptCriticalFallback(base context.Context) {
	// Shared single-flight: if a directive/manual install is running,
	// back off — the fallback is the lowest-priority path.
	if !s.acquireInstallGate() {
		return
	}
	defer s.releaseInstallGate()

	cfg, err := s.buildCriticalFallbackConfig()
	if err != nil {
		log.Infof("client self-update: critical fallback not configured: %v", err)
		return
	}
	cfg.BeforeInstall = func(context.Context) error {
		// Re-validate IMMEDIATELY before the privileged install: the
		// RunOnce fetch/download/verify can take minutes, during
		// which management may reconnect, an operator directive may
		// arrive, or the user may Down() the client. Any of those
		// means the authoritative path now owns updates (or the
		// client must not self-update at all) — abort instead of
		// installing the rolling static target. The engine aborts
		// the cycle on a BeforeInstall error before Install
		// (#5 R6 review #1).
		s.updateDirectiveMu.Lock()
		d := s.updateDirective
		s.updateDirectiveMu.Unlock()
		if err := fallbackSupersededReason(
			s.connectActive.Load(),
			s.statusRecorder.GetManagementState().Connected,
			d,
		); err != nil {
			return err
		}
		// Same pre-restart state flush as the directive path (#5128):
		// best-effort, never aborts a security self-heal.
		if s.connectClient != nil {
			if perr := s.connectClient.PersistState(); perr != nil {
				log.Warnf("client self-update: critical-fallback pre-install flush failed (continuing): %v", perr)
			}
		}
		return nil
	}

	u, err := selfupdate.New(cfg)
	if err == selfupdate.ErrUnsupportedPlatform {
		return // macOS-only in phase 1
	}
	if err != nil {
		log.Errorf("client self-update: critical fallback init: %v", err)
		return
	}
	res, err := u.RunOnce(base)
	if err != nil {
		if errors.Is(err, errFallbackSuperseded) {
			log.Infof("client self-update: critical fallback aborted — %v", err)
			return
		}
		log.Errorf("client self-update: critical fallback: %v", err)
		return
	}
	if res.Installed {
		log.Infof("client self-update: critical fallback installed %s — service will restart", res.Version)
		return
	}
	log.Infof("client self-update: critical fallback: %s", res.Reason)
}
