package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"
)

// Config wires the updater. Zero values get safe defaults from New;
// every external dependency is injectable so RunOnce is fully
// unit-testable without a Mac, a network or a disk install.
type Config struct {
	CurrentVersion     string
	ManifestURL        string // "" disables self-update (parity with the version-check switch)
	UserAgent          string
	AutoInstallEnabled bool // the default-OFF "Auto-install updates" setting (#5)
	PinnedVersion      string
	ClientID           string // stable per-client id for staged-rollout bucketing
	ServiceLabel       string // optional explicit launchctl label (see install.go)
	StagingDir         string

	// ExpectedTeamID is openZro's Apple Developer Team ID. Verify pins
	// the package's signing identity to it (review finding S1:
	// notarization alone only proves "signed by *some* Apple dev").
	// Empty => Verify fails closed. The value is a release-infra input
	// wired at the binding layer, like the signing cert itself.
	ExpectedTeamID string

	// ExpectedVersion, when set, binds this cycle to exactly that
	// release: RunOnce refuses if the fetched manifest advertises a
	// different version (openZro #5, I2 — fail-closed). The
	// management-driven path sets it to the operator's directed
	// target so a misconfigured/hostile per-version manifest endpoint
	// cannot trick the client into installing an unrequested version.
	// Empty disables the check (legacy single-manifest / R6 fallback).
	ExpectedVersion string

	// Authoritative marks this cycle as a management-directed install
	// (openZro #5 Q2): the server already evaluated the operator's
	// subset/ring for this peer, so Evaluate skips the manifest
	// staged_rollout. Set true on the directive path; left false on
	// the critical-only static-manifest fallback (R6) so that path
	// still honours staged_rollout. See GateInput.Authoritative.
	Authoritative bool

	// CriticalOnly restricts this cycle to a SECURITY-FLOOR breach:
	// RunOnce proceeds to download/verify/install only when the gate
	// returns Critical (Current < Manifest.MinVersion); a normal
	// eligible-but-not-critical update is reported Skipped instead of
	// installed. It is the openZro #5 R6 last-resort posture: when
	// management is unreachable AND the client is below the static
	// manifest's min_version, self-heal silently — but NEVER
	// slow-roll a routine update behind the operator's back while
	// unmanaged. Independent of Authoritative (the fallback path is
	// non-authoritative so staged_rollout still applies, though
	// Critical bypasses it anyway).
	CriticalOnly bool

	// BeforeInstall, when set, is called AFTER Verify succeeds and
	// IMMEDIATELY before Installer.Install — i.e. glued to the
	// privileged install/restart, not merely before the whole cycle
	// (openZro #5 R4 review #3). The daemon uses it to flush the
	// user's route / exit-node selection right before the restart so
	// the residual 10s-tick race window (selection changed during the
	// download/verify) is closed. Returning an error aborts the cycle
	// before any install; the daemon's hook is best-effort and
	// returns nil even on flush failure so it never blocks a
	// (potentially security) update.
	BeforeInstall func(context.Context) error

	// CycleTimeout bounds one full cycle (fetch+download+verify+
	// install). Without it a hung installer wedges self-update forever
	// behind single-flight. Default 15m.
	CycleTimeout time.Duration

	// Injection seams (defaulted by New).
	GOOS       string
	GOARCH     string
	HTTPClient *http.Client
	Verifier   Verifier
	Installer  Installer
}

// Result is the observable outcome of one cycle.
type Result struct {
	Installed bool
	Skipped   bool
	Version   string
	Reason    string
	Critical  bool
}

// Updater runs the manifest→gate→download→verify→install pipeline.
type Updater struct{ cfg Config }

// New validates the platform and fills defaults. Phase 1 is macOS
// only: on any other OS it returns ErrUnsupportedPlatform rather than
// a half-working updater.
func New(cfg Config) (*Updater, error) {
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	if cfg.GOOS != "darwin" {
		return nil, ErrUnsupportedPlatform
	}
	if cfg.HTTPClient == nil {
		// Generous: a notarized PKG is tens of MB. Not safedial-guarded
		// — the manifest URL is operator-overridable to an internal
		// mirror on purpose, exactly the legitimate-private-target case
		// the SSRF guard must not block.
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
	}
	if cfg.StagingDir == "" {
		cfg.StagingDir = os.TempDir()
	}
	if cfg.CycleTimeout <= 0 {
		cfg.CycleTimeout = 15 * time.Minute
	}
	if cfg.Verifier == nil {
		cfg.Verifier = macVerifier{run: execRunner, expectedTeamID: cfg.ExpectedTeamID}
	}
	if cfg.Installer == nil {
		cfg.Installer = macInstaller{run: execRunner, serviceLabel: cfg.ServiceLabel}
	}
	return &Updater{cfg: cfg}, nil
}

// RunOnce executes a single self-update cycle. A "not eligible"
// outcome is a normal Skipped result, not an error; only an actual
// failure (fetch, missing artifact, integrity, authenticity, install)
// is an error. The staged installer is always removed afterwards.
func (u *Updater) RunOnce(ctx context.Context) (Result, error) {
	c := u.cfg
	if c.ManifestURL == "" {
		return Result{Skipped: true, Reason: "self-update disabled (no manifest URL)"}, nil
	}

	// Bound the whole cycle. execRunner uses exec.CommandContext, so a
	// hung pkgutil/spctl/installer is killed at the deadline rather
	// than wedging self-update forever behind single-flight.
	ctx, cancel := context.WithTimeout(ctx, c.CycleTimeout)
	defer cancel()

	m, err := FetchManifest(ctx, c.HTTPClient, c.ManifestURL, c.UserAgent)
	if err != nil {
		return Result{}, err
	}

	// I2 — strict target binding (openZro #5). When the caller
	// directed an exact version, a manifest advertising anything else
	// is misconfigured or hostile: refuse before the gate, never
	// install an unrequested version.
	if c.ExpectedVersion != "" && m.Version != c.ExpectedVersion {
		return Result{}, fmt.Errorf(
			"selfupdate: manifest version %q != directed target %q — refusing",
			m.Version, c.ExpectedVersion)
	}

	d := Evaluate(GateInput{
		Current:            c.CurrentVersion,
		Manifest:           m,
		AutoInstallEnabled: c.AutoInstallEnabled,
		PinnedVersion:      c.PinnedVersion,
		ClientID:           c.ClientID,
		Authoritative:      c.Authoritative,
	})
	if !d.Eligible {
		log.Infof("selfupdate: skipping — %s", d.Reason)
		return Result{Skipped: true, Version: m.Version, Reason: d.Reason, Critical: d.Critical}, nil
	}

	// R6 last-resort posture: an unmanaged client self-heals ONLY a
	// security-floor breach, never a routine update. An eligible but
	// non-critical update is a normal Skip here, not an install.
	if c.CriticalOnly && !d.Critical {
		reason := "critical-only fallback: " + m.Version + " is eligible but not critical — not self-installing while unmanaged"
		log.Infof("selfupdate: skipping — %s", reason)
		return Result{Skipped: true, Version: m.Version, Reason: reason, Critical: false}, nil
	}

	art, ok := m.ArtifactFor(c.GOOS, c.GOARCH)
	if !ok {
		return Result{}, fmt.Errorf("selfupdate: manifest has no artifact for %s", PlatformKey(c.GOOS, c.GOARCH))
	}

	path, err := Download(ctx, c.HTTPClient, art, c.StagingDir)
	if err != nil {
		return Result{}, err
	}
	// Download stages into its own private 0700 dir (C3); that dir is
	// the unit of cleanup, not just the file.
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()

	if err := c.Verifier.Verify(ctx, path); err != nil {
		return Result{}, err
	}
	// Glued to the install: last point before the privileged
	// install + daemon restart (#5 R4 review #3 — closes the
	// residual selection-change race window).
	if c.BeforeInstall != nil {
		if err := c.BeforeInstall(ctx); err != nil {
			return Result{}, fmt.Errorf("selfupdate: before-install hook: %w", err)
		}
	}
	if err := c.Installer.Install(ctx, path); err != nil {
		return Result{}, err
	}

	log.Infof("selfupdate: installed %s (critical=%v)", m.Version, d.Critical)
	return Result{Installed: true, Version: m.Version, Reason: d.Reason, Critical: d.Critical}, nil
}
