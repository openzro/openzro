package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"os"
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
	if cfg.Verifier == nil {
		cfg.Verifier = macVerifier{run: execRunner}
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

	m, err := FetchManifest(ctx, c.HTTPClient, c.ManifestURL, c.UserAgent)
	if err != nil {
		return Result{}, err
	}

	d := Evaluate(GateInput{
		Current:            c.CurrentVersion,
		Manifest:           m,
		AutoInstallEnabled: c.AutoInstallEnabled,
		PinnedVersion:      c.PinnedVersion,
		ClientID:           c.ClientID,
	})
	if !d.Eligible {
		log.Infof("selfupdate: skipping — %s", d.Reason)
		return Result{Skipped: true, Version: m.Version, Reason: d.Reason, Critical: d.Critical}, nil
	}

	art, ok := m.ArtifactFor(c.GOOS, c.GOARCH)
	if !ok {
		return Result{}, fmt.Errorf("selfupdate: manifest has no artifact for %s", PlatformKey(c.GOOS, c.GOARCH))
	}

	path, err := Download(ctx, c.HTTPClient, art, c.StagingDir)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.Remove(path) }()

	if err := c.Verifier.Verify(ctx, path); err != nil {
		return Result{}, err
	}
	if err := c.Installer.Install(ctx, path); err != nil {
		return Result{}, err
	}

	log.Infof("selfupdate: installed %s (critical=%v)", m.Version, d.Critical)
	return Result{Installed: true, Version: m.Version, Reason: d.Reason, Critical: d.Critical}, nil
}
