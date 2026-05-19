package version

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const httpAgent = "pkg/test"

// TestUpdate_BareVersionString locks the legacy / custom-endpoint
// path: a server that returns a plain `1.2.3` body still works.
// Useful for operators who run an internal version-check service
// and don't want to model GitHub's release JSON shape.
func TestUpdate_BareVersionString(t *testing.T) {
	version = "1.0.0"
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "10.0.0")
	}))
	defer svr.Close()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	onUpdate := false
	u := newUpdateWithURL(httpAgent, svr.URL)
	defer u.StopWatch()
	u.SetOnUpdateListener(func() {
		onUpdate = true
		wg.Done()
	})

	waitTimeout(wg)
	if !onUpdate {
		t.Errorf("update not found")
	}
}

// TestUpdate_GitHubReleaseJSON exercises the canonical happy path:
// the endpoint returns the GitHub releases JSON shape, the parser
// extracts tag_name, strips the leading `v`, and the listener
// fires when the new tag is greater.
func TestUpdate_GitHubReleaseJSON(t *testing.T) {
	version = "1.0.0"
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"tag_name":"v10.0.0","draft":false,"prerelease":false,"name":"v10.0.0"}`)
	}))
	defer svr.Close()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	onUpdate := false
	u := newUpdateWithURL(httpAgent, svr.URL)
	defer u.StopWatch()
	u.SetOnUpdateListener(func() {
		onUpdate = true
		wg.Done()
	})

	waitTimeout(wg)
	if !onUpdate {
		t.Errorf("update not found via GitHub release JSON path")
	}
}

// TestUpdate_SkipsDrafts confirms a draft release does not fire
// the listener even when its tag would otherwise be greater. The
// auditor view of a release-train is what we follow; drafts are
// editorial state and should never nudge users.
func TestUpdate_SkipsDrafts(t *testing.T) {
	version = "1.0.0"
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"tag_name":"v99.0.0","draft":true,"prerelease":false}`)
	}))
	defer svr.Close()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	onUpdate := false
	u := newUpdateWithURL(httpAgent, svr.URL)
	defer u.StopWatch()
	u.SetOnUpdateListener(func() {
		onUpdate = true
		wg.Done()
	})

	waitTimeout(wg)
	if onUpdate {
		t.Errorf("draft release must not trigger update notification")
	}
}

// TestUpdate_AcceptsPrereleases — openZro's release stream is currently
// 100% prerelease (alpha.x). Skipping them would silence the update
// notifier across the whole alpha phase, leaving operators in the dark
// about CVE backports. Drafts remain skipped (see TestUpdate_SkipsDrafts).
// The skip-prereleases behavior will return when we cut a stable.
func TestUpdate_AcceptsPrereleases(t *testing.T) {
	version = "1.0.0"
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"tag_name":"v99.0.0-rc1","draft":false,"prerelease":true}`)
	}))
	defer svr.Close()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	onUpdate := false
	u := newUpdateWithURL(httpAgent, svr.URL)
	defer u.StopWatch()
	u.SetOnUpdateListener(func() {
		onUpdate = true
		wg.Done()
	})

	waitTimeout(wg)
	if !onUpdate {
		t.Errorf("prerelease must trigger update notification during alpha phase")
	}
}

// TestUpdate_AcceptsArrayResponse covers the /releases?per_page=N
// endpoint shape used by default since GitHub's /releases/latest
// excludes prereleases (see defaultVersionURL comment).
func TestUpdate_AcceptsArrayResponse(t *testing.T) {
	version = "1.0.0"
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[{"tag_name":"v99.0.0-alpha.5","draft":false,"prerelease":true}]`)
	}))
	defer svr.Close()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	onUpdate := false
	u := newUpdateWithURL(httpAgent, svr.URL)
	defer u.StopWatch()
	u.SetOnUpdateListener(func() {
		onUpdate = true
		wg.Done()
	})

	waitTimeout(wg)
	if !onUpdate {
		t.Errorf("array-shaped releases response must trigger update notification")
	}
}

func TestDoNotUpdate(t *testing.T) {
	version = "11.0.0"
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"tag_name":"v10.0.0","draft":false,"prerelease":false}`)
	}))
	defer svr.Close()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	onUpdate := false
	u := newUpdateWithURL(httpAgent, svr.URL)
	defer u.StopWatch()
	u.SetOnUpdateListener(func() {
		onUpdate = true
		wg.Done()
	})

	waitTimeout(wg)
	if onUpdate {
		t.Errorf("invalid update")
	}
}

func TestDaemonUpdate(t *testing.T) {
	version = "11.0.0"
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"tag_name":"v11.0.0","draft":false,"prerelease":false}`)
	}))
	defer svr.Close()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	onUpdate := false
	u := newUpdateWithURL(httpAgent, svr.URL)
	defer u.StopWatch()
	u.SetOnUpdateListener(func() {
		onUpdate = true
		wg.Done()
	})

	u.SetDaemonVersion("10.0.0")

	waitTimeout(wg)
	if !onUpdate {
		t.Errorf("invalid daemon version check")
	}
}

// TestUpdate_DisabledByEmptyURL proves the operator can fully
// disable version checking by setting OPENZRO_UPDATE_CHECK_URL
// to an empty string. Useful for air-gapped deployments and for
// CI environments that do not need the runtime to phone home.
func TestUpdate_DisabledByEmptyURL(t *testing.T) {
	version = "1.0.0"

	wg := &sync.WaitGroup{}
	wg.Add(1)

	onUpdate := false
	u := newUpdateWithURL(httpAgent, "")
	defer u.StopWatch()
	u.SetOnUpdateListener(func() {
		onUpdate = true
		wg.Done()
	})

	waitTimeout(wg)
	if onUpdate {
		t.Errorf("disabled URL must not trigger updates")
	}
}

func waitTimeout(wg *sync.WaitGroup) {
	c := make(chan struct{})
	go func() {
		wg.Wait()
		close(c)
	}()
	select {
	case <-c:
		return
	case <-time.After(time.Second):
		return
	}
}
