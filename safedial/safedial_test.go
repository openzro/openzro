package safedial

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestIsBlocked locks the exact policy decided in openzro/openzro#40:
// block ONLY loopback + the known cloud-metadata addresses. RFC1918
// and general link-local are deliberately allowed — self-host
// operators legitimately stream telemetry to private-network
// collectors, and blocking that would break the core feature.
func TestIsBlocked(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{"ipv4 loopback", "127.0.0.1", true},
		{"ipv4 loopback non-.1", "127.9.9.9", true},
		{"ipv6 loopback", "::1", true},
		{"aws/gcp/azure metadata v4", "169.254.169.254", true},
		{"metadata v4 as v4-in-v6", "::ffff:169.254.169.254", true},
		{"aws metadata v6", "fd00:ec2::254", true},

		{"rfc1918 10/8 allowed", "10.0.0.5", false},
		{"rfc1918 172.16/12 allowed", "172.16.0.1", false},
		{"rfc1918 192.168/16 allowed", "192.168.1.10", false},
		{"link-local non-metadata allowed", "169.254.1.1", false},
		{"link-local v6 non-metadata allowed", "fe80::1", false},
		{"public v4 allowed", "8.8.8.8", false},
		{"public v6 allowed", "2001:4860:4860::8888", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("test bug: %q did not parse", tc.ip)
			}
			if got := isBlocked(ip); got != tc.want {
				t.Fatalf("isBlocked(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// TestIsBlocked_FailsClosed: an unparseable / nil IP must be treated
// as blocked. A dial we cannot classify is a dial we do not make —
// same fail-closed posture as the rest of the codebase.
func TestIsBlocked_FailsClosed(t *testing.T) {
	if !isBlocked(nil) {
		t.Fatal("nil IP must fail closed (blocked)")
	}
}

// TestGuardedControl proves the dial-time hook itself accepts/rejects
// the resolved ip:port without needing real network. The connection
// handle is unused by the guard, so nil is fine here.
func TestGuardedControl(t *testing.T) {
	if err := guardedControl(context.Background(), "tcp", "10.0.0.5:443", nil); err != nil {
		t.Fatalf("allowed address rejected: %v", err)
	}
	if err := guardedControl(context.Background(), "tcp", "127.0.0.1:6379", nil); err == nil {
		t.Fatal("loopback must be rejected at dial time")
	}
	if err := guardedControl(context.Background(), "tcp", "169.254.169.254:80", nil); err == nil {
		t.Fatal("cloud metadata must be rejected at dial time")
	}
	if err := guardedControl(context.Background(), "tcp", "[fd00:ec2::254]:80", nil); err == nil {
		t.Fatal("ipv6 metadata must be rejected at dial time")
	}
	if err := guardedControl(context.Background(), "tcp", "garbage", nil); err == nil {
		t.Fatal("unparseable address must fail closed")
	}
}

// TestGuardedTransport_BlocksLoopback is the end-to-end proof: httptest
// binds on 127.0.0.1, and the always-guarded Transport must fail to
// reach it at the dial stage. This is what makes the guard immune to
// hostname / DNS rebinding — the ControlContext hook sees the
// post-resolution IP, not the URL string.
//
// It deliberately builds the client from Transport() rather than the
// public Client(): under a test binary Client() returns an unguarded
// client by design (so sink/exporter tests can reach their loopback
// fixtures), so the guarded path must be asserted directly here.
func TestGuardedTransport_BlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	guarded := &http.Client{Timeout: 2 * time.Second, Transport: Transport()}
	_, err := guarded.Get(srv.URL)
	if err == nil {
		t.Fatal("guarded transport reached a loopback server — SSRF guard not wired into the dialer")
	}
}
