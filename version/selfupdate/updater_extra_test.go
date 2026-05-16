package selfupdate

import (
	"context"
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
		_, _ = w.Write([]byte(`{"version":"9.9.9","staged_rollout":0,
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
