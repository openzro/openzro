package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
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
		// PinnedVersion intentionally empty: in the management-driven
		// model the directive target IS the pin and ExpectedVersion
		// enforces it. There is no client-side pin/auto toggle.
	}, nil
}

// runSelfUpdate executes one rollout-gated cycle for the CURRENT
// management directive. manual=true is the explicit user "Install
// now" path. With no directive (or a cleared one) there is nothing to
// install — that is a normal Skipped result, not an error.
func (s *Server) runSelfUpdate(ctx context.Context, manual bool) (*proto.UpdateResponse, error) {
	s.updateDirectiveMu.Lock()
	d := s.updateDirective
	s.updateDirectiveMu.Unlock()

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
// auto-install (#5 review-#1). target is the version the attempt was
// for; one attempt per target — a success restarts the service onto
// the new version (so the next directive's target == running and the
// gate no-ops), a failure waits for a fresh directive rather than
// looping a root install.
type autoInstallState struct {
	target   string
	inFlight bool
	lastErr  string
}

// maybeAutoInstall silently installs a force=true directive whose
// preflight is positive, through the SAME rollout-gated secure
// pipeline as the manual RPC (download → Team-ID verify → install).
// This is what makes force actually mean "silent install" rather
// than merely "available" (review finding #1). It is a no-op unless
// force && available, and it never starts a second attempt for a
// target it is already installing or has already attempted.
func (s *Server) maybeAutoInstall(target string, force, available bool) {
	if !force || !available || target == "" {
		return
	}

	s.autoInstallMu.Lock()
	if s.autoInstall.inFlight || s.autoInstall.target == target {
		s.autoInstallMu.Unlock()
		return
	}
	s.autoInstall = autoInstallState{target: target, inFlight: true}
	s.autoInstallMu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.autoInstallMu.Lock()
				s.autoInstall.inFlight = false
				s.autoInstall.lastErr = "panic during auto-install"
				s.autoInstallMu.Unlock()
				log.Errorf("client self-update: auto-install recovered from panic: %v", r)
			}
		}()

		base := s.rootCtx
		if base == nil {
			base = context.Background()
		}
		log.Infof("client self-update: force directive — auto-installing %s", target)

		// manual=false: the operator's force IS the opt-in, so
		// buildSelfUpdateConfig sets AutoInstallEnabled = false||force
		// = true. Every other safety (version check, staged rollout,
		// min-version, Team-ID verify, I2 binding) still applies.
		resp, err := s.runSelfUpdate(base, false)

		s.autoInstallMu.Lock()
		s.autoInstall.inFlight = false
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
	s.updateDirective = updateDirective{
		targetVersion: targetVersion,
		force:         force,
		seen:          true,
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
		go s.updatePreflightScheduler(kick)
	} else {
		s.preflightMu.Unlock()
	}

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
		published := s.updateDirective.targetVersion == pf.forTarget
		if published {
			s.updatePreflight = pf
		}
		s.updateDirectiveMu.Unlock()

		log.Infof("client self-update: preflight target=%q available=%t transient=%t reason=%q",
			pf.forTarget, pf.available, pf.transient, pf.reason)

		// Force directive + positive preflight => install silently
		// now (review-#1). No-op otherwise; self-guards against
		// repeat/concurrent attempts.
		if published {
			s.maybeAutoInstall(pf.forTarget, d.force, pf.available)
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
		// Transient: the endpoint may be momentarily down, DNS not yet
		// up, network still coming online during boot, etc. The
		// scheduler retries this with backoff so a single blip does
		// not pin the client "unavailable" until reconnect/restart.
		pf.transient = true
		pf.reason = "manifest temporarily unreachable, retrying: " + err.Error()
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

	var decision string
	switch {
	case !verdictReady:
		decision = "checking update availability…"
	case !available:
		decision = pf.reason
	case d.force && ai.target == d.targetVersion && ai.inFlight:
		decision = "update available — operator forced: installing " + d.targetVersion + "…"
	case d.force && ai.target == d.targetVersion && ai.lastErr != "":
		decision = "update available — operator forced: install failed (" + ai.lastErr +
			"); will retry on the next directive"
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
