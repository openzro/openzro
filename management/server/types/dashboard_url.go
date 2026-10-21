package types

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// DashboardURLEnv overrides HttpServerConfig.DashboardURL.
const DashboardURLEnv = "OPENZRO_DASHBOARD_URL"

// NormalizeDashboardURL validates the dashboard's public base URL and
// returns it without a trailing slash; empty stays empty. Clients open the
// device-login approval page under it with a bearer token and the user's
// TOTP, so it must be https://. http:// is accepted only on loopback
// (localhost, 127.0.0.0/8, ::1), for development.
func NormalizeDashboardURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("dashboard URL %q: %w", raw, err)
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", fmt.Errorf("dashboard URL %q: want scheme://host[:port][/path] with no credentials, query or fragment", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", fmt.Errorf("dashboard URL %q: http:// is allowed only on loopback; use https://", raw)
		}
	default:
		return "", fmt.Errorf("dashboard URL %q: scheme must be https", raw)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
