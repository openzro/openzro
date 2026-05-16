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
