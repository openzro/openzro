package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	goversion "github.com/hashicorp/go-version"
	log "github.com/sirupsen/logrus"
)

const (
	fetchPeriod = 30 * time.Minute

	// defaultVersionURL points at GitHub's "list releases" API limited
	// to one entry. The /releases/latest endpoint excludes prereleases
	// and returns 404 when only prereleases exist — which is openZro's
	// situation today (we have v0.53.1-alpha.x but no stable). The
	// /releases?per_page=1 endpoint returns the most recent release of
	// any type, so the version checker keeps working through the alpha
	// phase. The upstream pointed at pkgs.netbird.io which it operated;
	// openZro is GitHub-Releases-only so this is the canonical source.
	defaultVersionURL = "https://api.github.com/repos/openzro/openzro/releases?per_page=1"

	// envVersionURL lets operators override the endpoint or disable
	// the check entirely. Empty value (`OPENZRO_UPDATE_CHECK_URL=`)
	// disables version checking — useful for air-gapped deployments
	// or operators who manage upgrades through CI/CD and do not want
	// the runtime to phone GitHub every 30 minutes.
	envVersionURL = "OPENZRO_UPDATE_CHECK_URL"

	// maxResponseBytes caps the body read to defend against a
	// hostile or misconfigured endpoint. The GitHub release object
	// is typically 5–50 KB; 1 MB is generous headroom.
	maxResponseBytes = 1 * 1024 * 1024
)

// Update fetches the version info periodically and notifies the
// onUpdateListener when the daemon or UI version is older than the
// latest release published on GitHub.
type Update struct {
	httpAgent       string
	uiVersion       *goversion.Version
	daemonVersion   *goversion.Version
	latestAvailable *goversion.Version
	versionsLock    sync.Mutex

	fetchTicker *time.Ticker
	fetchDone   chan struct{}

	onUpdateListener func()
	listenerLock     sync.Mutex

	// versionURL is resolved at construction. Empty means the check
	// is disabled and startFetcher returns early.
	versionURL string
}

// NewUpdate instantiates an Update and starts the periodic fetcher.
// Returns a usable instance even when version checking is disabled
// (env var set to empty) — SetDaemonVersion / SetOnUpdateListener
// stay safe and non-blocking.
func NewUpdate(httpAgent string) *Update {
	return newUpdateWithURL(httpAgent, resolveVersionURL())
}

// newUpdateWithURL is the test seam. Production code calls
// NewUpdate which reads the env var; tests call this directly with
// an httptest server URL so they do not race on a global env.
func newUpdateWithURL(httpAgent, url string) *Update {
	currentVersion, err := goversion.NewVersion(version)
	if err != nil {
		currentVersion, _ = goversion.NewVersion("0.0.0")
	}

	latestAvailable, _ := goversion.NewVersion("0.0.0")

	u := &Update{
		httpAgent:       httpAgent,
		latestAvailable: latestAvailable,
		uiVersion:       currentVersion,
		fetchTicker:     time.NewTicker(fetchPeriod),
		fetchDone:       make(chan struct{}),
		versionURL:      url,
	}
	go u.startFetcher()
	return u
}

// resolveVersionURL reads the env var override, returning the
// default endpoint when unset and an empty string when the operator
// explicitly disabled the check.
func resolveVersionURL() string {
	v, ok := os.LookupEnv(envVersionURL)
	if !ok {
		return defaultVersionURL
	}
	return strings.TrimSpace(v)
}

// StopWatch stops the fetch loop. Idempotent.
func (u *Update) StopWatch() {
	u.fetchTicker.Stop()
	select {
	case u.fetchDone <- struct{}{}:
	default:
	}
}

// SetDaemonVersion records the running daemon version. Returns
// true when the change triggered an update notification.
func (u *Update) SetDaemonVersion(newVersion string) bool {
	daemonVersion, err := goversion.NewVersion(newVersion)
	if err != nil {
		daemonVersion, _ = goversion.NewVersion("0.0.0")
	}

	u.versionsLock.Lock()
	if u.daemonVersion != nil && u.daemonVersion.Equal(daemonVersion) {
		u.versionsLock.Unlock()
		return false
	}
	u.daemonVersion = daemonVersion
	u.versionsLock.Unlock()
	return u.checkUpdate()
}

// SetOnUpdateListener installs a callback fired when a newer release
// is available. Fires immediately if a check has already detected
// an update before the listener was registered.
func (u *Update) SetOnUpdateListener(updateFn func()) {
	u.listenerLock.Lock()
	defer u.listenerLock.Unlock()
	u.onUpdateListener = updateFn
	if u.isUpdateAvailable() {
		u.onUpdateListener()
	}
}

func (u *Update) startFetcher() {
	if u.versionURL == "" {
		log.Infof("version check disabled (%s is empty)", envVersionURL)
		return
	}

	if changed := u.fetchVersion(); changed {
		u.checkUpdate()
	}

	for {
		select {
		case <-u.fetchDone:
			return
		case <-u.fetchTicker.C:
			if changed := u.fetchVersion(); changed {
				u.checkUpdate()
			}
		}
	}
}

// githubRelease is the subset of the GitHub release JSON we need.
// Decoupled from the full schema so the parser is not brittle to
// fields GitHub adds in future API versions.
type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func (u *Update) fetchVersion() bool {
	log.Debugf("fetching version info from %s", u.versionURL)

	req, err := http.NewRequest(http.MethodGet, u.versionURL, nil)
	if err != nil {
		log.Errorf("failed to create request for version info: %s", err)
		return false
	}
	req.Header.Set("User-Agent", u.httpAgent)
	// GitHub recommends this Accept value for the REST API. Harmless
	// for any other endpoint that ignores it.
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Errorf("failed to fetch version info: %s", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Errorf("invalid status code from %s: %d", u.versionURL, resp.StatusCode)
		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		log.Errorf("failed to read content: %s", err)
		return false
	}

	tag, err := parseLatestTag(body)
	if err != nil {
		log.Errorf("failed to parse version response: %s", err)
		return false
	}

	latestAvailable, err := goversion.NewVersion(tag)
	if err != nil {
		log.Errorf("failed to parse version string %q: %s", tag, err)
		return false
	}

	u.versionsLock.Lock()
	defer u.versionsLock.Unlock()
	if u.latestAvailable.Equal(latestAvailable) {
		return false
	}
	u.latestAvailable = latestAvailable
	return true
}

// parseLatestTag accepts three GitHub-flavored response shapes:
//   - an array of release objects (`/releases?per_page=N`, current
//     default), in which case the first element is taken as latest
//   - a single release object (`/releases/latest`, legacy), kept for
//     operators who override OPENZRO_UPDATE_CHECK_URL
//   - a bare version string from a custom endpoint
//
// Drafts are skipped (draft releases are unpublished). Prereleases are
// NOT skipped — openZro's release stream is currently 100% prerelease
// (alpha.x), and rejecting them would silence the version checker
// across the whole alpha phase. The skip will return when we cut a
// stable release; track via TODO. The leading "v" prefix conventional
// in Git tags is stripped before returning.
func parseLatestTag(body []byte) (string, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", fmt.Errorf("empty body")
	}

	pickRelease := func(rel githubRelease) (string, error) {
		if rel.Draft {
			return "", fmt.Errorf("latest release is a draft (%s); skipping", rel.TagName)
		}
		if rel.TagName == "" {
			return "", fmt.Errorf("release json has no tag_name")
		}
		return strings.TrimPrefix(rel.TagName, "v"), nil
	}

	switch trimmed[0] {
	case '[':
		var rels []githubRelease
		if err := json.Unmarshal([]byte(trimmed), &rels); err != nil {
			return "", fmt.Errorf("decode releases json array: %w", err)
		}
		if len(rels) == 0 {
			return "", fmt.Errorf("releases json array is empty")
		}
		return pickRelease(rels[0])
	case '{':
		var rel githubRelease
		if err := json.Unmarshal([]byte(trimmed), &rel); err != nil {
			return "", fmt.Errorf("decode release json: %w", err)
		}
		return pickRelease(rel)
	default:
		// Legacy / custom endpoint that returns a bare version string.
		return strings.TrimPrefix(trimmed, "v"), nil
	}
}

func (u *Update) checkUpdate() bool {
	if !u.isUpdateAvailable() {
		return false
	}
	u.listenerLock.Lock()
	defer u.listenerLock.Unlock()
	if u.onUpdateListener == nil {
		return true
	}
	go u.onUpdateListener()
	return true
}

func (u *Update) isUpdateAvailable() bool {
	u.versionsLock.Lock()
	defer u.versionsLock.Unlock()

	if u.latestAvailable.GreaterThan(u.uiVersion) {
		return true
	}
	if u.daemonVersion == nil {
		return false
	}
	if u.latestAvailable.GreaterThan(u.daemonVersion) {
		return true
	}
	return false
}
