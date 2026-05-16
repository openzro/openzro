// Package selfupdate is the desktop client's background self-update
// machinery: fetch a static release manifest, decide (rollout-gated)
// whether this client should take it, download + verify, then apply
// via the platform installer.
//
// This is openZro issue #5, Phase 1 (macOS). The version *check* half
// already exists in the parent `version` package
// (version/update.go polls GitHub every 30m and surfaces a "newer
// release" notification); this package is the *act-on-it* half. It is
// extend-not-greenfield and lives in BSD-licensed client territory —
// the structure follows upstream NetBird's Win/Mac client self-update
// (BSD), adapted; no AGPL management code is involved.
//
// Design (agreed in #5):
//   - The manifest is STATIC, served from the release infra (GitHub
//     Releases / openzro-bin), NOT the management API. A peer must be
//     able to self-update even when its management is older; coupling
//     client-update to the control plane is an outage footgun.
//   - Rollout control is MANDATORY (see gate.go): opt-out, version
//     pin, min-version floor, staged rollout. One bad release must not
//     be able to take the whole fleet down.
//   - Authenticity is anchored per-platform (see verify_*.go). On
//     macOS that is Apple notarization (pkgutil/spctl); the manifest
//     SHA-256 is the cross-platform integrity check on top.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	goversion "github.com/hashicorp/go-version"
)

// maxManifestBytes caps the manifest body. The document is a few KB;
// 256 KB is generous headroom and defends against a hostile or
// misconfigured endpoint streaming forever.
const maxManifestBytes = 256 * 1024

// envManifestURL overrides the manifest location. Empty disables
// self-update (parity with the version-check env switch). The default
// publishing path on the release infra is an open release-pipeline
// item (tracked in #5) — the client side is complete and only needs
// the URL pointed at it.
const envManifestURL = "OPENZRO_UPDATE_MANIFEST_URL"

const defaultManifestURL = "https://github.com/openzro/openzro/releases/download/update/update-manifest.json"

// Artifact is one platform's downloadable. Signature is an optional
// detached signature; on macOS authenticity comes from notarization
// (verify_darwin.go), so Signature is reserved for the cross-platform
// scheme finalized with the Windows phase.
type Artifact struct {
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature,omitempty"`
}

// Manifest is the static release descriptor.
type Manifest struct {
	// Version is the release this manifest advertises (semver, no
	// leading "v").
	Version string `json:"version"`
	// MinVersion is a hard floor: a client older than this is on a
	// release we will not keep alive (e.g. a security cut). It makes
	// the update critical — see gate.go.
	MinVersion string `json:"min_version,omitempty"`
	// StagedRollout is the percent of the fleet eligible (0..100).
	// 0 and 100 both mean "everyone"; an intermediate N gates by a
	// stable per-client bucket so a bad release is caught on a slice.
	StagedRollout int `json:"staged_rollout"`
	// Artifacts is keyed by "<goos>/<goarch>" (e.g. "darwin/arm64").
	Artifacts map[string]Artifact `json:"artifacts"`
}

// ParseManifest decodes and validates a manifest document.
func ParseManifest(body []byte) (*Manifest, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, fmt.Errorf("selfupdate: empty manifest body")
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("selfupdate: decode manifest: %w", err)
	}
	if m.Version == "" {
		return nil, fmt.Errorf("selfupdate: manifest has no version")
	}
	if _, err := goversion.NewVersion(m.Version); err != nil {
		return nil, fmt.Errorf("selfupdate: manifest version %q invalid: %w", m.Version, err)
	}
	if m.MinVersion != "" {
		if _, err := goversion.NewVersion(m.MinVersion); err != nil {
			return nil, fmt.Errorf("selfupdate: manifest min_version %q invalid: %w", m.MinVersion, err)
		}
	}
	if m.StagedRollout < 0 || m.StagedRollout > 100 {
		return nil, fmt.Errorf("selfupdate: staged_rollout %d out of range 0..100", m.StagedRollout)
	}
	if len(m.Artifacts) == 0 {
		return nil, fmt.Errorf("selfupdate: manifest has no artifacts")
	}
	for key, a := range m.Artifacts {
		if a.URL == "" {
			return nil, fmt.Errorf("selfupdate: artifact %q has no url", key)
		}
		if a.SHA256 == "" {
			return nil, fmt.Errorf("selfupdate: artifact %q has no sha256", key)
		}
	}
	return &m, nil
}

// PlatformKey is the Artifacts map key for an OS/arch pair.
func PlatformKey(goos, goarch string) string {
	return goos + "/" + goarch
}

// ArtifactFor returns the artifact for a goos/goarch, if present.
func (m *Manifest) ArtifactFor(goos, goarch string) (Artifact, bool) {
	a, ok := m.Artifacts[PlatformKey(goos, goarch)]
	return a, ok
}

// ResolveManifestURL returns the configured manifest URL: the env
// override when set, the default otherwise. An explicitly empty env
// value disables self-update (caller treats "" as disabled).
func ResolveManifestURL() string {
	if v, ok := os.LookupEnv(envManifestURL); ok {
		return strings.TrimSpace(v)
	}
	return defaultManifestURL
}

// FetchManifest GETs and validates the manifest. The body is hard
// size-capped so a hostile endpoint cannot exhaust memory; an
// oversized or truncated body fails ParseManifest rather than being
// trusted.
func FetchManifest(ctx context.Context, client *http.Client, url, userAgent string) (*Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: build manifest request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: fetch manifest: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("selfupdate: manifest endpoint returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: read manifest body: %w", err)
	}
	return ParseManifest(body)
}
