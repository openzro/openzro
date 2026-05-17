// Package safedial builds net/http clients whose dialer refuses to
// connect to addresses that are never a legitimate destination for an
// operator-configured outbound URL: local loopback (including the
// 0.0.0.0/:: unspecified address, which connect()s to loopback on
// Linux) and the cloud instance/container-credential metadata
// endpoints (classic IMDS plus the AWS ECS/EKS credential addresses).
//
// Scope is deliberately narrow (see openzro/openzro#40). It does NOT
// block RFC1918 or general link-local: self-host operators legitimately
// stream flow/activity telemetry to private-network collectors
// (http://elastic.internal:9200, http://10.0.0.5/collector), and
// blocking that would break the core feature. The residual "an admin
// can point an exporter at an internal RFC1918 service" is an accepted
// property of "admin configures an outbound URL" and is out of scope —
// this guard does not pretend to solve what cannot be solved without
// breaking the feature. It blocks only what is never legitimate: the
// process talking to its own loopback (Redis, NATS, the management
// API) and metadata-based cloud-credential theft.
//
// The check runs in net.Dialer.ControlContext, i.e. AFTER name
// resolution on the concrete IP the connection will use. That is what
// makes it immune to hostname smuggling and DNS rebinding — validating
// the URL string or resolving at config-save time is bypassable; this
// is not.
//
// This package is BSD-3 (root LICENSE) and pulls in nothing else, so
// the AGPL activity/exporter callers can import it without licence
// contamination just as the BSD flow/sinks callers do.
package safedial

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"
)

// blockedMeta are the cloud instance/container-credential metadata
// addresses. Stealing creds from one of these is the canonical SSRF
// payoff, so all of them are refused — not just the classic IMDS IP.
// The 169.254.170.x pair matters specifically because openZro ships a
// Helm chart: EKS/ECS is a first-class deployment and that is where
// the credential endpoint is NOT 169.254.169.254.
//
// Alibaba's 100.100.100.200 is intentionally out of scope (#40).
var blockedMeta = []net.IP{
	net.IPv4(169, 254, 169, 254), // AWS/GCP/Azure/DigitalOcean/OpenStack IMDS
	net.IPv4(169, 254, 170, 2),   // AWS ECS task-credentials endpoint
	net.IPv4(169, 254, 170, 23),  // AWS EKS Pod Identity (IPv4)
	net.ParseIP("fd00:ec2::254"), // AWS IMDSv6
	net.ParseIP("fd00:ec2::23"),  // AWS EKS Pod Identity (IPv6)
}

// isBlocked reports whether ip must never be dialed. A nil / unparsed
// ip fails closed: an address we cannot classify is one we refuse.
func isBlocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// IsUnspecified catches 0.0.0.0 / :: — on Linux a connect() to the
	// unspecified address goes to loopback, so it is a loopback bypass
	// that IsLoopback() does not cover.
	if ip.IsLoopback() || ip.IsUnspecified() {
		return true
	}
	for _, m := range blockedMeta {
		if ip.Equal(m) {
			return true
		}
	}
	return false
}

// guardedControl is the net.Dialer.ControlContext hook. address is the
// already-resolved "ip:port" the dialer is about to connect to; the
// raw connection is unused. Returning an error aborts that dial.
func guardedControl(_ context.Context, _ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		// Unparseable post-resolution address — fail closed.
		return fmt.Errorf("safedial: cannot parse dial address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if isBlocked(ip) {
		return fmt.Errorf("safedial: refusing to dial blocked address %s (loopback or cloud metadata)", host)
	}
	return nil
}

// Transport returns an *http.Transport with production-safe defaults
// (a clone of http.DefaultTransport) whose dialer enforces the guard.
// A fresh transport per call is intentional — callers own its
// connection-pool lifecycle, mirroring the http.Client they wrap it in.
func Transport() *http.Transport {
	d := &net.Dialer{
		Timeout:        30 * time.Second,
		KeepAlive:      30 * time.Second,
		ControlContext: guardedControl,
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = d.DialContext
	return tr
}

// Client returns an *http.Client whose transport refuses loopback and
// cloud-metadata destinations. timeout is the overall client timeout;
// pass 0 for "no client-level timeout" (e.g. when an SDK manages its
// own per-operation deadlines and only the dial guard is wanted).
//
// Under a `go test` binary the guard is intentionally NOT installed:
// sink/exporter unit tests point at loopback httptest fixtures, and a
// loopback-blocking dialer would make every one of them fail for the
// wrong reason. The guard itself stays covered — safedial's own tests
// exercise the always-guarded Transport directly (see safedial_test).
// Production builds are never test binaries, so they always get the
// guarded transport. testing.Testing() (Go 1.21+) is the right signal
// here precisely because it cannot be flipped on in production the way
// an env var could.
func Client(timeout time.Duration) *http.Client {
	if testing.Testing() {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: Transport(),
	}
}
