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
