package selfupdate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

	t.Run("non-200 returns a typed HTTPStatusError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		_, err := FetchManifest(context.Background(), srv.Client(), srv.URL, "ua/1")
		if err == nil {
			t.Fatal("expected error on 500")
		}
		var he *HTTPStatusError
		if !errors.As(err, &he) || he.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected *HTTPStatusError{500}, got %T %v", err, err)
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

// TestResolveManifestTemplateURL pins the openZro #5 management-driven
// per-version manifest resolution and its fail-closed posture.
func TestResolveManifestTemplateURL(t *testing.T) {
	t.Run("unset env falls back to the built-in default template", func(t *testing.T) {
		old, had := os.LookupEnv(envManifestTemplate)
		_ = os.Unsetenv(envManifestTemplate)
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(envManifestTemplate, old)
			}
		})
		got, err := ResolveManifestTemplateURL("1.2.3")
		if err != nil {
			t.Fatalf("unset must use the default, got error %v", err)
		}
		want := "https://github.com/openzro/openzro/releases/download/v1.2.3/update-manifest.json"
		if got != want {
			t.Fatalf("unset must resolve the default template: got %q want %q", got, want)
		}
	})

	t.Run("explicitly empty env is the disable escape hatch", func(t *testing.T) {
		t.Setenv(envManifestTemplate, "   ")
		got, err := ResolveManifestTemplateURL("1.2.3")
		if err != nil || got != "" {
			t.Fatalf("explicit-empty must disable -> (\"\",nil), got (%q,%v)", got, err)
		}
	})

	t.Run("set without {version} token is a fail-closed config error", func(t *testing.T) {
		t.Setenv(envManifestTemplate, "https://dl.example.com/manifest.json")
		got, err := ResolveManifestTemplateURL("1.2.3")
		if err == nil {
			t.Fatalf("missing token must error, got url %q", got)
		}
		if !strings.Contains(err.Error(), manifestVersionToken) {
			t.Fatalf("error should name the required token: %v", err)
		}
	})

	t.Run("configured template but empty target errors", func(t *testing.T) {
		t.Setenv(envManifestTemplate, "https://dl.example.com/{version}/manifest.json")
		if _, err := ResolveManifestTemplateURL("  "); err == nil {
			t.Fatal("empty target with a configured template must error")
		}
	})

	t.Run("substitutes and path-escapes the target", func(t *testing.T) {
		t.Setenv(envManifestTemplate, "https://dl.example.com/{version}/update-manifest.json")
		got, err := ResolveManifestTemplateURL("0.30.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://dl.example.com/0.30.0/update-manifest.json" {
			t.Fatalf("unexpected url: %q", got)
		}

		// A garbled version must not be able to inject a path segment
		// or escape the intended host/path layout.
		got, err = ResolveManifestTemplateURL("../../evil")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(got, "../") {
			t.Fatalf("target must be path-escaped, got %q", got)
		}
	})
}

// TestManifest_ReleasePipelineShape pins the exact update-manifest.json
// the release pipeline emits (.github/workflows/release-binaries.yml,
// openZro #5 R4b). The CI YAML is not unit-testable here, so this is
// the contract guard: if Manifest/ParseManifest drifts, this fails and
// reminds whoever changed it that the pipeline generator must match.
// Shape: single universal pkg, both darwin arches -> same url+sha256,
// min_version "" (no floor), staged_rollout 100 (R6-only; the
// management-driven path skips it via GateInput.Authoritative).
func TestManifest_ReleasePipelineShape(t *testing.T) {
	const url = "https://github.com/openzro/openzro/releases/download/v0.53.1-alpha.1/openzro_0.53.1-alpha.1_darwin_universal.pkg"
	const sha = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	body := `{
	  "version": "0.53.1-alpha.1",
	  "min_version": "",
	  "staged_rollout": 100,
	  "artifacts": {
	    "darwin/arm64": { "url": "` + url + `", "sha256": "` + sha + `" },
	    "darwin/amd64": { "url": "` + url + `", "sha256": "` + sha + `" }
	  }
	}`

	m, err := ParseManifest([]byte(body))
	if err != nil {
		t.Fatalf("pipeline manifest must parse, got %v", err)
	}
	if m.Version != "0.53.1-alpha.1" {
		t.Fatalf("version: got %q", m.Version)
	}
	if m.MinVersion != "" {
		t.Fatalf("min_version must be empty (no floor), got %q", m.MinVersion)
	}
	if m.StagedRollout == nil || *m.StagedRollout != 100 {
		t.Fatalf("staged_rollout must be 100, got %v", m.StagedRollout)
	}
	for _, arch := range []string{"arm64", "amd64"} {
		a, ok := m.ArtifactFor("darwin", arch)
		if !ok {
			t.Fatalf("missing artifact for darwin/%s", arch)
		}
		if a.URL != url || a.SHA256 != sha {
			t.Fatalf("darwin/%s artifact mismatch: %+v", arch, a)
		}
	}
}

// TestManifest_StaticFallbackShape pins the R6 rolling static
// manifest the pipeline emits (same schema as the per-version one,
// but with an operator-set min_version). A client below that floor
// must read it as Critical so the unmanaged fallback self-heals.
func TestManifest_StaticFallbackShape(t *testing.T) {
	const u = "https://github.com/openzro/openzro/releases/download/v2.0.0/openzro_2.0.0_darwin_universal.pkg"
	const sh = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	body := `{"version":"2.0.0","min_version":"1.9.0","staged_rollout":100,
	  "artifacts":{"darwin/arm64":{"url":"` + u + `","sha256":"` + sh + `"},
	               "darwin/amd64":{"url":"` + u + `","sha256":"` + sh + `"}}}`
	m, err := ParseManifest([]byte(body))
	if err != nil {
		t.Fatalf("static fallback manifest must parse, got %v", err)
	}
	// An old client (below min_version) — non-authoritative path.
	d := Evaluate(GateInput{Current: "1.5.0", Manifest: m, AutoInstallEnabled: true, ClientID: "c"})
	if !d.Eligible || !d.Critical {
		t.Fatalf("a client below min_version must be Eligible+Critical, got %+v", d)
	}
	// A current client (>= min_version, already at version) — nothing.
	d = Evaluate(GateInput{Current: "2.0.0", Manifest: m, AutoInstallEnabled: true, ClientID: "c"})
	if d.Eligible {
		t.Fatalf("a current client must not be eligible, got %+v", d)
	}
}
