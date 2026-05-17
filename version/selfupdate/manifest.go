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
// Design (agreed in #5, management-driven model — superseded the
// earlier decoupled-static design):
//   - The TRIGGER is management-driven: Management conveys the
//     operator's fleet decision (target version + force/silent) over
//     the existing peer Sync stream. Management conveys only the
//     decision, never the binary.
//   - The client still fetches a release manifest and the signed
//     package itself and verifies them — the trust root is unchanged
//     (the engine here is the security reinforcement over a bare
//     "management said so"). For the management-driven path the
//     manifest is per-version, resolved from a TEMPLATE
//     (ResolveManifestTemplateURL) so the fetched descriptor is
//     bound to the exact directed version.
//   - The legacy single static manifest (ResolveManifestURL) is
//     retained as the critical-only fallback for when management is
//     unreachable AND the client is below min_version (#5 R6).
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
	"net/url"
	"os"
	"strings"

	goversion "github.com/hashicorp/go-version"
)

// maxManifestBytes caps the manifest body. The document is a few KB;
// 256 KB is generous headroom and defends against a hostile or
// misconfigured endpoint streaming forever.
const maxManifestBytes = 256 * 1024

// httpDrainCap bounds the keep-alive drain on a finished/rejected
// response so a hostile endpoint cannot turn the cleanup into a
// bandwidth/latency DoS (Codex-4).
const httpDrainCap = 4 * 1024

// envManifestURL overrides the manifest location. Empty disables
// self-update (parity with the version-check env switch). The default
// publishing path on the release infra is an open release-pipeline
// item (tracked in #5) — the client side is complete and only needs
// the URL pointed at it.
const envManifestURL = "OPENZRO_UPDATE_MANIFEST_URL"

const defaultManifestURL = "https://github.com/openzro/openzro/releases/download/update/update-manifest.json"

// envManifestTemplate is the per-version manifest URL template used by
// the management-driven path (#5). It MUST contain manifestVersionToken;
// the daemon substitutes the directed target version into it so the
// fetched manifest is bound to exactly that release. Unset/empty means
// the management-driven manifest path is not configured.
const envManifestTemplate = "OPENZRO_UPDATE_MANIFEST_TEMPLATE"

// manifestVersionToken is the placeholder replaced with the directed
// target version in the manifest template.
const manifestVersionToken = "{version}"

// defaultManifestTemplate is the per-version manifest published by the
// release infra: one manifest per GitHub release tag. The
// management-driven path is therefore live in a normal build with NO
// external configuration; OPENZRO_UPDATE_MANIFEST_TEMPLATE only
// overrides it (e.g. an internal mirror), and an explicitly empty env
// value remains the operator escape hatch that disables the path.
const defaultManifestTemplate = "https://github.com/openzro/openzro/releases/download/v{version}/update-manifest.json"

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
	// StagedRollout is the percent of the fleet eligible. It is a
	// POINTER on purpose (review finding S1/Codex-1): an omitted JSON
	// field must be distinguishable from an explicit 0, otherwise a
	// malformed/incomplete manifest silently becomes a full rollout via
	// Go's zero value. ParseManifest REQUIRES it. Semantics are
	// fail-closed: 100 = everyone, 0 = nobody (not "everyone"), 1..99 =
	// stable-bucket gate; an absent field is a rejected manifest, never
	// a full rollout.
	StagedRollout *int `json:"staged_rollout"`
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
	ver, _ := goversion.NewVersion(m.Version)
	if m.MinVersion != "" {
		minv, err := goversion.NewVersion(m.MinVersion)
		if err != nil {
			return nil, fmt.Errorf("selfupdate: manifest min_version %q invalid: %w", m.MinVersion, err)
		}
		// A floor above the offered version is an incoherent manifest:
		// installing it still leaves the client below its own declared
		// floor (Codex-5). Reject at the edge.
		if minv.GreaterThan(ver) {
			return nil, fmt.Errorf("selfupdate: min_version %s is greater than version %s", m.MinVersion, m.Version)
		}
	}
	if m.StagedRollout == nil {
		return nil, fmt.Errorf("selfupdate: manifest must declare staged_rollout explicitly")
	}
	if *m.StagedRollout < 0 || *m.StagedRollout > 100 {
		return nil, fmt.Errorf("selfupdate: staged_rollout %d out of range 0..100", *m.StagedRollout)
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

// ResolveManifestTemplateURL builds the per-version manifest URL for
// the management-driven path (#5) by substituting target into the
// template.
//
//   - env unset -> use defaultManifestTemplate (the path is live by
//     default in a normal build).
//   - env set, non-empty -> override with that value.
//   - env set but EXPLICITLY empty/whitespace -> ("", nil): the
//     operator escape hatch that disables the path; the caller treats
//     "" as "not configured" (NOT an error — a deliberate opt-out
//     must not be log noise).
//   - effective template missing {version} -> fail-closed config
//     error: it would resolve EVERY target to one URL and silently
//     install the wrong version. Refuse loudly.
//   - empty target with a usable template -> error: nothing to
//     resolve.
//
// target is path-escaped before substitution so a hostile/garbled
// version string cannot inject extra path segments or a different host.
func ResolveManifestTemplateURL(target string) (string, error) {
	tpl := defaultManifestTemplate
	if v, ok := os.LookupEnv(envManifestTemplate); ok {
		tpl = strings.TrimSpace(v)
		if tpl == "" {
			return "", nil // explicit opt-out
		}
	}
	if !strings.Contains(tpl, manifestVersionToken) {
		return "", fmt.Errorf(
			"selfupdate: %s is set but missing the %s token — refusing "+
				"(it would resolve every target to one URL and install the wrong version)",
			envManifestTemplate, manifestVersionToken)
	}
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("selfupdate: cannot resolve manifest template with an empty target version")
	}
	return strings.ReplaceAll(tpl, manifestVersionToken, url.PathEscape(target)), nil
}

// FetchManifest GETs and validates the manifest. The body is hard
// size-capped so a hostile endpoint cannot exhaust memory; an
// oversized or truncated body fails ParseManifest rather than being
// trusted.
func FetchManifest(ctx context.Context, client *http.Client, url, userAgent string) (*Manifest, error) {
	if err := requireSafeScheme(url); err != nil {
		return nil, err
	}
	client = clientWithRedirectGuard(client)
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
		// Bounded drain only (Codex-4): for a rejected/hostile/oversized
		// response, draining the whole body wastes bandwidth and holds
		// the goroutine. A small drain is enough for keep-alive reuse on
		// the normal small-body path; anything bigger is abandoned.
		_, _ = io.CopyN(io.Discard, resp.Body, httpDrainCap)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("selfupdate: manifest endpoint returned HTTP %d", resp.StatusCode)
	}
	// Read one byte past the cap so an over-limit body is DETECTED
	// (Codex-3), not silently truncated to a valid-looking prefix.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: read manifest body: %w", err)
	}
	if int64(len(body)) > maxManifestBytes {
		return nil, fmt.Errorf("selfupdate: manifest exceeds %d byte cap", maxManifestBytes)
	}
	return ParseManifest(body)
}
