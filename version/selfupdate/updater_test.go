package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeVerifier struct {
	called bool
	err    error
}

func (f *fakeVerifier) Verify(_ context.Context, _ string) error {
	f.called = true
	return f.err
}

type fakeInstaller struct {
	called bool
	err    error
}

func (f *fakeInstaller) Install(_ context.Context, _ string) error {
	f.called = true
	return f.err
}

// updaterServer serves a manifest at /manifest and the artifact at
// /artifact with a sha that matches the payload.
func updaterServer(t *testing.T, rollout int, autoVer string) *httptest.Server {
	t.Helper()
	payload := []byte("notarized-pkg-bytes")
	mux := http.NewServeMux()
	mux.HandleFunc("/artifact", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		fmt.Fprintf(w, `{"version":%q,"staged_rollout":%d,
		  "artifacts":{"darwin/arm64":{"url":%q,"sha256":%q}}}`,
			autoVer, rollout, base+"/artifact", sha256hex(payload))
	})
	return httptest.NewServer(mux)
}

func baseCfg(srv *httptest.Server) Config {
	return Config{
		CurrentVersion:     "1.0.0",
		ManifestURL:        srv.URL + "/manifest",
		UserAgent:          "ua/1",
		AutoInstallEnabled: true,
		ClientID:           "client-1",
		GOOS:               "darwin",
		GOARCH:             "arm64",
		HTTPClient:         srv.Client(),
	}
}

func TestNew_PlatformGate(t *testing.T) {
	if _, err := New(Config{GOOS: "linux"}); err != ErrUnsupportedPlatform {
		t.Fatalf("linux must be refused, got %v", err)
	}
	if _, err := New(Config{GOOS: "windows"}); err != ErrUnsupportedPlatform {
		t.Fatalf("windows is phase 2, got %v", err)
	}
	if _, err := New(Config{GOOS: "darwin"}); err != nil {
		t.Fatalf("darwin must be accepted, got %v", err)
	}
}

func TestRunOnce_HappyPath(t *testing.T) {
	srv := updaterServer(t, 0, "1.2.0")
	defer srv.Close()
	cfg := baseCfg(srv)
	v, in := &fakeVerifier{}, &fakeInstaller{}
	cfg.Verifier, cfg.Installer = v, in

	u, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := u.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Installed || res.Version != "1.2.0" {
		t.Fatalf("bad result: %+v", res)
	}
	if !v.called || !in.called {
		t.Fatalf("verify(%v)/install(%v) must both run", v.called, in.called)
	}
}

func TestRunOnce_NotEligibleIsSkipNotError(t *testing.T) {
	srv := updaterServer(t, 0, "1.2.0")
	defer srv.Close()
	cfg := baseCfg(srv)
	cfg.AutoInstallEnabled = false // default-off: surface only
	v, in := &fakeVerifier{}, &fakeInstaller{}
	cfg.Verifier, cfg.Installer = v, in

	u, _ := New(cfg)
	res, err := u.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("not-eligible must not be an error: %v", err)
	}
	if !res.Skipped || res.Installed {
		t.Fatalf("expected skip, got %+v", res)
	}
	if v.called || in.called {
		t.Fatal("nothing should download/verify/install when not eligible")
	}
}

func TestRunOnce_VerifyFailsAbortsBeforeInstall(t *testing.T) {
	srv := updaterServer(t, 0, "1.2.0")
	defer srv.Close()
	cfg := baseCfg(srv)
	v := &fakeVerifier{err: fmt.Errorf("not notarized")}
	in := &fakeInstaller{}
	cfg.Verifier, cfg.Installer = v, in

	u, _ := New(cfg)
	if _, err := u.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error when verification fails")
	}
	if in.called {
		t.Fatal("installer must NOT run after a failed verification")
	}
}

func TestRunOnce_InstallFailsIsError(t *testing.T) {
	srv := updaterServer(t, 0, "1.2.0")
	defer srv.Close()
	cfg := baseCfg(srv)
	cfg.Verifier = &fakeVerifier{}
	cfg.Installer = &fakeInstaller{err: fmt.Errorf("installer exit 1")}

	u, _ := New(cfg)
	if _, err := u.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error when install fails")
	}
}

func TestRunOnce_DisabledWhenNoManifestURL(t *testing.T) {
	u, _ := New(Config{GOOS: "darwin", GOARCH: "arm64"})
	res, err := u.RunOnce(context.Background())
	if err != nil || !res.Skipped {
		t.Fatalf("empty manifest URL must be a clean no-op skip, got res=%+v err=%v", res, err)
	}
}

func TestRunOnce_NoArtifactForPlatform(t *testing.T) {
	srv := updaterServer(t, 0, "1.2.0")
	defer srv.Close()
	cfg := baseCfg(srv)
	cfg.GOARCH = "amd64" // manifest only has darwin/arm64
	cfg.Verifier, cfg.Installer = &fakeVerifier{}, &fakeInstaller{}

	u, _ := New(cfg)
	if _, err := u.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error: manifest has no darwin/amd64 artifact")
	}
}
