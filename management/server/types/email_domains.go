package types

import (
	"regexp"
	"strings"

	"github.com/openzro/openzro/management/server/status"
)

// MaxAllowedEmailDomains caps Settings.AllowedEmailDomains (ADR-0025 D2).
const MaxAllowedEmailDomains = 100

// emailDomainRegexp matches a lowercase DNS name with at least two labels
// and an alphabetic TLD. Labels may contain inner hyphens only.
var emailDomainRegexp = regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`)

// NormalizeAllowedEmailDomains trims, lowercases and de-duplicates an
// allowlist of email domains, stripping a leading "@". It rejects entries
// that are not plain DNS names (wildcards, emails, single labels) and lists
// longer than MaxAllowedEmailDomains. An empty input normalizes to nil,
// which disables the allowlist.
func NormalizeAllowedEmailDomains(domains []string) ([]string, error) {
	if len(domains) > MaxAllowedEmailDomains {
		return nil, status.Errorf(status.InvalidArgument,
			"too many email domains: %d, maximum is %d", len(domains), MaxAllowedEmailDomains)
	}

	var out []string
	seen := make(map[string]struct{}, len(domains))
	for _, raw := range domains {
		d := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "@")
		if !validEmailDomain(d) {
			return nil, status.Errorf(status.InvalidArgument, "invalid email domain %q", raw)
		}
		if _, dup := seen[d]; dup {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out, nil
}

// validEmailDomain applies the DNS wire limits in addition to the lexical
// rules above. Without these bounds a syntactically-shaped, multi-megabyte
// value could be stored in settings and loaded on every join attempt.
func validEmailDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 || !emailDomainRegexp.MatchString(domain) {
		return false
	}
	for label := range strings.SplitSeq(domain, ".") {
		if len(label) > 63 {
			return false
		}
	}
	return true
}

// EmailDomainAllowed reports whether email may join the account under
// AllowedEmailDomains. An empty allowlist allows everything. Otherwise the
// part after the last "@" must equal a listed domain exactly
// (case-insensitive); subdomains are not implied.
func (s *Settings) EmailDomainAllowed(email string) bool {
	if len(s.AllowedEmailDomains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range s.AllowedEmailDomains {
		if d == domain {
			return true
		}
	}
	return false
}
