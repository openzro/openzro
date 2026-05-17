package selfupdate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNew_DefaultsCycleTimeout: C2 — a cycle must be bounded so a hung
// installer cannot wedge self-update forever behind single-flight.
func TestNew_DefaultsCycleTimeout(t *testing.T) {
	u, err := New(Config{GOOS: "darwin", GOARCH: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if u.cfg.CycleTimeout != 15*time.Minute {
		t.Fatalf("default CycleTimeout = %v, want 15m", u.cfg.CycleTimeout)
	}
}

// TestRunOnce_CycleTimeoutAborts: a slow artifact past the cycle
// deadline must abort with an error, not hang.
func TestRunOnce_CycleTimeoutAborts(t *testing.T) {
	payload := []byte("slow-pkg")
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_, _ = w.Write([]byte(`{"version":"9.9.9","staged_rollout":100,
		  "artifacts":{"darwin/arm64":{"url":"` + base + `/artifact","sha256":"` + sha256hex(payload) + `"}}}`))
	})
	mux.HandleFunc("/artifact", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // exceeds the tiny cycle timeout
		_, _ = w.Write(payload)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := baseCfg(srv)
	cfg.CycleTimeout = 120 * time.Millisecond
	cfg.Verifier, cfg.Installer = &fakeVerifier{}, &fakeInstaller{}

	u, _ := New(cfg)
	start := time.Now()
	_, err := u.RunOnce(context.Background())
	if err == nil {
		t.Fatal("expected a deadline error, not a hang")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("RunOnce did not honour the %v cycle timeout (took %v)", cfg.CycleTimeout, time.Since(start))
	}
}

// TestRunOnce_ExpectedVersionMismatchRefused pins openZro #5 I2: when
// the cycle is bound to a directed target, a manifest advertising any
// other version is refused BEFORE download/verify/install — a
// misconfigured or hostile per-version endpoint cannot smuggle in an
// unrequested release.
func TestRunOnce_ExpectedVersionMismatchRefused(t *testing.T) {
	srv := updaterServer(t, 100, "1.2.0") // endpoint serves 1.2.0
	defer srv.Close()

	cfg := baseCfg(srv)
	cfg.ExpectedVersion = "9.9.9" // but we directed 9.9.9
	v, in := &fakeVerifier{}, &fakeInstaller{}
	cfg.Verifier, cfg.Installer = v, in

	u, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.RunOnce(context.Background()); err == nil {
		t.Fatal("a version mismatch must be a hard refusal, not an install")
	}
	if v.called || in.called {
		t.Fatalf("refusal must precede verify(%v)/install(%v)", v.called, in.called)
	}
}

// TestRunOnce_ExpectedVersionMatchInstalls is the positive control:
// the same binding is transparent when the manifest matches.
func TestRunOnce_ExpectedVersionMatchInstalls(t *testing.T) {
	srv := updaterServer(t, 100, "1.2.0")
	defer srv.Close()

	cfg := baseCfg(srv)
	cfg.ExpectedVersion = "1.2.0"
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
	if !res.Installed || res.Version != "1.2.0" || !v.called || !in.called {
		t.Fatalf("matched binding must install: %+v verify=%v install=%v", res, v.called, in.called)
	}
}

// TestRunOnce_BeforeInstallHook pins openZro #5 R4 review #3: the
// BeforeInstall hook runs AFTER Verify and IMMEDIATELY before
// Install (glued to the privileged restart), and a hook error aborts
// the cycle before any install happens.
func TestRunOnce_BeforeInstallHook(t *testing.T) {
	t.Run("runs after verify, before install", func(t *testing.T) {
		srv := updaterServer(t, 100, "1.2.0")
		defer srv.Close()
		cfg := baseCfg(srv)
		v, in := &fakeVerifier{}, &fakeInstaller{}
		cfg.Verifier, cfg.Installer = v, in
		hookRan := false
		cfg.BeforeInstall = func(context.Context) error {
			if !v.called {
				t.Error("BeforeInstall must run AFTER Verify")
			}
			if in.called {
				t.Error("BeforeInstall must run BEFORE Install")
			}
			hookRan = true
			return nil
		}
		u, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		res, err := u.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !hookRan || !res.Installed || !in.called {
			t.Fatalf("hook=%v installed=%v install.called=%v", hookRan, res.Installed, in.called)
		}
	})

	t.Run("hook error aborts before install", func(t *testing.T) {
		srv := updaterServer(t, 100, "1.2.0")
		defer srv.Close()
		cfg := baseCfg(srv)
		v, in := &fakeVerifier{}, &fakeInstaller{}
		cfg.Verifier, cfg.Installer = v, in
		cfg.BeforeInstall = func(context.Context) error { return errors.New("flush boom") }
		u, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := u.RunOnce(context.Background()); err == nil {
			t.Fatal("a BeforeInstall error must abort the cycle")
		}
		if in.called {
			t.Fatal("install must NOT run after a BeforeInstall error")
		}
		if !v.called {
			t.Fatal("verify should have run before the hook")
		}
	})
}

// criticalManifestServer serves a manifest with an optional
// min_version so tests can drive the gate's Critical flag.
func criticalManifestServer(t *testing.T, version, minVersion string) *httptest.Server {
	t.Helper()
	payload := []byte("notarized-pkg-bytes")
	mux := http.NewServeMux()
	mux.HandleFunc("/artifact", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		mv := ""
		if minVersion != "" {
			mv = `"min_version":"` + minVersion + `",`
		}
		_, _ = w.Write([]byte(`{"version":"` + version + `",` + mv +
			`"staged_rollout":100,"artifacts":{"darwin/arm64":{"url":"http://` +
			r.Host + `/artifact","sha256":"` + sha256hex(payload) + `"}}}`))
	})
	return httptest.NewServer(mux)
}

// TestRunOnce_CriticalOnly pins openZro #5 R6: the last-resort
// fallback self-heals ONLY a security-floor breach (Critical) and
// never slow-rolls a routine update while unmanaged.
func TestRunOnce_CriticalOnly(t *testing.T) {
	t.Run("eligible but NOT critical -> skipped, no install", func(t *testing.T) {
		srv := criticalManifestServer(t, "1.2.0", "") // no min_version
		defer srv.Close()
		cfg := baseCfg(srv) // Current 1.0.0 < 1.2.0, AutoInstall true
		cfg.CriticalOnly = true
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
		if res.Installed || !res.Skipped || in.called || v.called {
			t.Fatalf("non-critical must be skipped without install: %+v verify=%v install=%v", res, v.called, in.called)
		}
	})

	t.Run("critical -> installs", func(t *testing.T) {
		// min_version 1.5.0 > Current 1.0.0 => Critical.
		srv := criticalManifestServer(t, "1.6.0", "1.5.0")
		defer srv.Close()
		cfg := baseCfg(srv)
		cfg.CriticalOnly = true
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
		if !res.Installed || !res.Critical || !in.called {
			t.Fatalf("critical breach must install: %+v install=%v", res, in.called)
		}
	})

	t.Run("control: CriticalOnly=false installs the routine eligible update", func(t *testing.T) {
		srv := criticalManifestServer(t, "1.2.0", "")
		defer srv.Close()
		cfg := baseCfg(srv)
		cfg.CriticalOnly = false
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
		if !res.Installed || !in.called {
			t.Fatalf("without CriticalOnly the eligible update must install: %+v", res)
		}
	})
}
