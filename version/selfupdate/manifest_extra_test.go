package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetchManifest_HTTPSAccepted: a real https endpoint passes the
// S2 scheme guard (httptest TLS server; its client trusts the cert).
func TestFetchManifest_HTTPSAccepted(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.0.0","staged_rollout":0,
		  "artifacts":{"darwin/arm64":{"url":"u","sha256":"s"}}}`))
	}))
	defer srv.Close()
	if _, err := FetchManifest(context.Background(), srv.Client(), srv.URL, "ua/1"); err != nil {
		t.Fatalf("https manifest must be accepted: %v", err)
	}
}

// TestFetchManifest_RejectsPlainHTTPNonLoopback: S2 — a routable
// plain-http manifest is refused before any network I/O.
func TestFetchManifest_RejectsPlainHTTPNonLoopback(t *testing.T) {
	if _, err := FetchManifest(context.Background(), http.DefaultClient,
		"http://updates.evil.example/manifest.json", "ua/1"); err == nil {
		t.Fatal("plain-http non-loopback manifest URL must be refused")
	}
}

// TestFetchManifest_RejectsDowngradeRedirect: Codex-2 — the initial
// URL passes (loopback http), but a 302 to a routable plain-http
// mirror must be rejected at the redirect hop, not silently followed.
func TestFetchManifest_RejectsDowngradeRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://updates.evil.example/manifest.json", http.StatusFound)
	}))
	defer srv.Close()
	if _, err := FetchManifest(context.Background(), srv.Client(), srv.URL, "ua/1"); err == nil {
		t.Fatal("a redirect downgrading to non-loopback http must be refused")
	}
}

// TestFetchManifest_OverCapDetected: Codex-3 — a valid manifest
// followed by padding beyond the cap must be REJECTED, not silently
// truncated to the valid-looking prefix and trusted.
func TestFetchManifest_OverCapDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.0.0","staged_rollout":0,"artifacts":{"x/y":{"url":"u","sha256":"s"}}}`))
		pad := make([]byte, maxManifestBytes+1024)
		for i := range pad {
			pad[i] = ' '
		}
		_, _ = w.Write(pad)
	}))
	defer srv.Close()
	if _, err := FetchManifest(context.Background(), srv.Client(), srv.URL, "ua/1"); err == nil {
		t.Fatal("manifest over the size cap must be rejected, not truncated+trusted")
	}
}
