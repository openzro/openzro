package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseManifest(t *testing.T) {
	good := `{
	  "version": "0.53.1-alpha.75",
	  "min_version": "0.53.1-alpha.40",
	  "staged_rollout": 25,
	  "artifacts": {
	    "darwin/arm64": {"url":"https://x/openzro.pkg","sha256":"abc","signature":"sig"},
	    "darwin/amd64": {"url":"https://x/openzro-amd64.pkg","sha256":"def"}
	  }
	}`
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"valid", good, false},
		{"empty", "", true},
		{"not json", "not-json", true},
		{"missing version", `{"staged_rollout":0,"artifacts":{"x/y":{"url":"u","sha256":"s"}}}`, true},
		{"unparseable version", `{"version":"not.a.version!!","artifacts":{"x/y":{"url":"u","sha256":"s"}}}`, true},
		{"bad min_version", `{"version":"1.0.0","min_version":"@@@","artifacts":{"x/y":{"url":"u","sha256":"s"}}}`, true},
		{"rollout over 100", `{"version":"1.0.0","staged_rollout":101,"artifacts":{"x/y":{"url":"u","sha256":"s"}}}`, true},
		{"rollout negative", `{"version":"1.0.0","staged_rollout":-1,"artifacts":{"x/y":{"url":"u","sha256":"s"}}}`, true},
		{"no artifacts", `{"version":"1.0.0","staged_rollout":0,"artifacts":{}}`, true},
		{"artifact missing url", `{"version":"1.0.0","staged_rollout":0,"artifacts":{"x/y":{"sha256":"s"}}}`, true},
		{"artifact missing sha256", `{"version":"1.0.0","staged_rollout":0,"artifacts":{"x/y":{"url":"u"}}}`, true},
		{"missing staged_rollout (must be explicit)", `{"version":"1.0.0","artifacts":{"x/y":{"url":"u","sha256":"s"}}}`, true},
		{"min_version greater than version", `{"version":"1.0.0","min_version":"2.0.0","staged_rollout":0,"artifacts":{"x/y":{"url":"u","sha256":"s"}}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(tc.body))
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestManifest_ArtifactFor(t *testing.T) {
	m, err := ParseManifest([]byte(`{"version":"1.2.3","staged_rollout":0,
	  "artifacts":{"darwin/arm64":{"url":"u","sha256":"s"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.ArtifactFor("darwin", "arm64"); !ok {
		t.Fatal("expected darwin/arm64 hit")
	}
	if _, ok := m.ArtifactFor("windows", "amd64"); ok {
		t.Fatal("expected windows/amd64 miss")
	}
}

func TestFetchManifest(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("User-Agent") == "" {
				t.Error("missing User-Agent")
			}
			_, _ = w.Write([]byte(`{"version":"9.9.9","staged_rollout":0,
			  "artifacts":{"darwin/arm64":{"url":"u","sha256":"s"}}}`))
		}))
		defer srv.Close()
		m, err := FetchManifest(context.Background(), srv.Client(), srv.URL, "ua/1")
		if err != nil {
			t.Fatal(err)
		}
		if m.Version != "9.9.9" {
			t.Fatalf("got %q", m.Version)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		if _, err := FetchManifest(context.Background(), srv.Client(), srv.URL, "ua/1"); err == nil {
			t.Fatal("expected error on 500")
		}
	})

	t.Run("oversize body rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			big := make([]byte, maxManifestBytes+1024)
			for i := range big {
				big[i] = '{'
			}
			_, _ = w.Write(big)
		}))
		defer srv.Close()
		if _, err := FetchManifest(context.Background(), srv.Client(), srv.URL, "ua/1"); err == nil {
			t.Fatal("expected error: oversize body must not parse to a valid manifest")
		}
	})

	t.Run("context cancel", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
		}))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := FetchManifest(ctx, srv.Client(), srv.URL, "ua/1"); err == nil {
			t.Fatal("expected error on context timeout")
		}
	})
}
