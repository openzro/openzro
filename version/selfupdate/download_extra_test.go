package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestDownload_StagingDirIsPrivate locks review finding C3: the staged
// installer (later consumed by a root `installer`) must sit in a 0700
// dir owned by us, not directly in a world-writable parent, so a
// local user cannot TOCTOU-swap it.
func TestDownload_StagingDirIsPrivate(t *testing.T) {
	payload := []byte("pkg")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	path, err := Download(context.Background(), srv.Client(),
		Artifact{URL: srv.URL, SHA256: sha256hex(payload)}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("staging dir perm = %o, want 0700", perm)
	}
}

// TestDownload_RejectsUnsafeScheme locks S2 on the artifact URL.
func TestDownload_RejectsUnsafeScheme(t *testing.T) {
	_, err := Download(context.Background(), http.DefaultClient,
		Artifact{URL: "http://malicious.example/openzro.pkg", SHA256: "x"}, t.TempDir())
	if err == nil {
		t.Fatal("plain-http non-loopback artifact URL must be refused before any fetch")
	}
}

// TestDownload_RejectsDowngradeRedirect mirrors the manifest redirect
// test on the artifact path (Codex follow-up): the initial URL passes
// (loopback http) but a 302 to a routable plain-http mirror must be
// rejected at the hop, not silently followed. The redirect guard is a
// shared helper and correct today — this blinds the intent against a
// future regression that only kept it on the manifest path.
func TestDownload_RejectsDowngradeRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://updates.evil.example/openzro.pkg", http.StatusFound)
	}))
	defer srv.Close()
	_, err := Download(context.Background(), srv.Client(),
		Artifact{URL: srv.URL, SHA256: "irrelevant"}, t.TempDir())
	if err == nil {
		t.Fatal("a redirect downgrading the artifact to non-loopback http must be refused")
	}
}
