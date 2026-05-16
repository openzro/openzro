package selfupdate

import (
	"fmt"
	"net"
	"net/url"
)

// requireSafeScheme refuses any URL that is not https, EXCEPT http to
// a loopback host (127.0.0.0/8, ::1, localhost) — loopback cannot be
// MITM'd and is the legitimate local-mirror / test case.
//
// Rationale (review finding S2): the phase-1 manifest is unsigned
// (detached signature is phase 2). Its only transport integrity is
// TLS, so a plain-http manifest — or artifact — is a direct
// package-substitution vector. The download SHA-256 does not help
// when the same attacker controls the manifest that states it.
func requireSafeScheme(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("selfupdate: unparseable URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("selfupdate: refusing plain-http non-loopback URL (MITM risk): %s", u.Redacted())
	default:
		return fmt.Errorf("selfupdate: refusing non-https URL scheme %q", u.Scheme)
	}
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
