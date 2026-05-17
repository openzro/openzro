package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestDownload(t *testing.T) {
	payload := []byte("a fake notarized pkg payload")
	good := sha256hex(payload)

	serve := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(payload)
		}))
	}

	t.Run("ok: correct sha, file staged with exact bytes", func(t *testing.T) {
		srv := serve()
		defer srv.Close()
		dir := t.TempDir()
		path, err := Download(context.Background(), srv.Client(),
			Artifact{URL: srv.URL, SHA256: good}, dir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(payload) {
			t.Fatalf("staged bytes differ")
		}
	})

	t.Run("sha mismatch: error and staged file removed", func(t *testing.T) {
		srv := serve()
		defer srv.Close()
		dir := t.TempDir()
		path, err := Download(context.Background(), srv.Client(),
			Artifact{URL: srv.URL, SHA256: "deadbeef"}, dir)
		if err == nil {
			t.Fatal("expected integrity error")
		}
		if path != "" {
			if _, statErr := os.Stat(path); statErr == nil {
				t.Fatal("a sha-mismatched download must not be left on disk")
			}
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatalf("staging dir not cleaned: %v", entries)
		}
	})

	t.Run("sha is case-insensitive", func(t *testing.T) {
		srv := serve()
		defer srv.Close()
		_, err := Download(context.Background(), srv.Client(),
			Artifact{URL: srv.URL, SHA256: upper(good)}, t.TempDir())
		if err != nil {
			t.Fatalf("uppercase sha should match: %v", err)
		}
	})

	t.Run("non-200 errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		if _, err := Download(context.Background(), srv.Client(),
			Artifact{URL: srv.URL, SHA256: good}, t.TempDir()); err == nil {
			t.Fatal("expected error on 404")
		}
	})

	t.Run("oversize body rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf := make([]byte, 1<<20)
			for written := int64(0); written <= maxArtifactBytes+int64(len(buf)); written += int64(len(buf)) {
				if _, err := w.Write(buf); err != nil {
					return
				}
			}
		}))
		defer srv.Close()
		if _, err := Download(context.Background(), srv.Client(),
			Artifact{URL: srv.URL, SHA256: good}, t.TempDir()); err == nil {
			t.Fatal("expected error: artifact exceeds size cap")
		}
	})

	t.Run("context cancel", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
		}))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := Download(ctx, srv.Client(),
			Artifact{URL: srv.URL, SHA256: good}, t.TempDir()); err == nil {
			t.Fatal("expected error on context timeout")
		}
	})
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 32
		}
	}
	return string(b)
}
