package sinks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoUnguardedHTTPClient is an architectural fitness test. Every
// outbound HTTP client in this package MUST be built via
// safedial.Client so the SSRF dial guard (loopback + cloud metadata)
// is enforced in production.
//
// This static check exists because safedial.Client is
// testing.Testing()-aware: under `go test` it returns an UNGUARDED
// client (so sink tests can reach loopback fixtures). That means NO
// behavioral test can catch a regression where a sink reverts to a
// raw http.Client — the guard would silently vanish in production
// with a green suite. This grep is the only thing that fails.
func TestNoUnguardedHTTPClient(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"&http.Client{", "http.DefaultClient"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, bad := range forbidden {
			if strings.Contains(string(src), bad) {
				t.Errorf("%s contains %q — outbound HTTP must use "+
					"safedial.Client; a raw client silently bypasses the "+
					"SSRF guard in production", f, bad)
			}
		}
	}
}
