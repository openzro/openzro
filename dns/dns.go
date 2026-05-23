// Package dns implement dns types and standard methods and functions
// to parse and normalize dns records and configuration
package dns

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/miekg/dns"
	"golang.org/x/net/idna"
)

const (
	// DefaultDNSPort well-known port number
	DefaultDNSPort = 53
	// RootZone is a string representation of the root zone
	RootZone = "."
	// DefaultClass is the class supported by the system
	DefaultClass = "IN"
)

const invalidHostLabel = "[^a-zA-Z0-9-]+"

// Config represents a dns configuration that is exchanged between management and peers
type Config struct {
	// ServiceEnable indicates if the service should be enabled
	ServiceEnable bool
	// NameServerGroups contains a list of nameserver group
	NameServerGroups []*NameServerGroup
	// CustomZones contains a list of custom zone
	CustomZones []CustomZone
}

// CustomZoneSource enumerates the origin of a CustomZone — see
// ADR-0022 D4b. Mirrors the protobuf enum
// proto.CustomZoneSource. UNSPECIFIED is the zero value and the
// default for older management daemons that don't set the field;
// new agents treat UNSPECIFIED as PEERS (legacy behavior).
type CustomZoneSource int32

const (
	// CustomZoneSourceUnspecified is the wire-default for older
	// daemons that don't populate the field. Treated as PEERS by
	// new agents.
	CustomZoneSourceUnspecified CustomZoneSource = 0
	// CustomZoneSourcePeers — the synthetic peer DNS zone built
	// from peer.DNSLabel + ExtraDNSLabels.
	CustomZoneSourcePeers CustomZoneSource = 1
	// CustomZoneSourceUser — an operator-managed zone created via
	// the /api/dns/zones endpoints (issue #108).
	CustomZoneSourceUser CustomZoneSource = 2
)

// CustomZone represents a custom zone to be resolved by the dns
// server. Two source kinds today (see CustomZoneSource):
//
//   - PEERS: the synthetic zone openZro builds from the account's
//     peer DNS labels; one apex (`<dnsDomain>`), one A record per
//     peer. Always SearchDomainEnabled=true.
//   - USER: operator-managed zones distributed via the dashboard.
//     SearchDomainEnabled is per-zone (default false).
//
// The agent does not need to distinguish on resolution — both
// sources flow through the same buildLocalHandlerUpdate path. The
// Source field is informational (telemetry, dashboard UX); the
// resolution-time behavior is driven by Domain/Records/
// SearchDomainEnabled.
type CustomZone struct {
	// Domain is the zone's domain
	Domain string
	// Records custom zone records
	Records []SimpleRecord
	// SearchDomainEnabled — when true, the agent appends Domain to
	// the OS DNS search list. The synthetic peer zone is emitted
	// with this = true (preserves bare-name peer resolution like
	// `dig myhost`). User-managed zones default to false; the
	// operator opts in per zone via the API.
	SearchDomainEnabled bool
	// Source identifies whether the zone is the synthetic peer
	// zone (PEERS) or operator-managed (USER). Informational; see
	// the struct doc above.
	Source CustomZoneSource
}

// SimpleRecord provides a simple DNS record specification for CNAME, A and AAAA records
type SimpleRecord struct {
	// Name domain name
	Name string
	// Type of record, 1 for A, 5 for CNAME, 28 for AAAA. see https://pkg.go.dev/github.com/miekg/dns@v1.1.41#pkg-constants
	Type int
	// Class dns class, currently use the DefaultClass for all records
	Class string
	// TTL time-to-live for the record
	TTL int
	// RData is the actual value resolved in a dns query
	RData string
}

// String returns a string of the simple record formatted as:
// <Name> <TTL> <Class> <Type> <RDATA>
func (s SimpleRecord) String() string {
	fqdn := dns.Fqdn(s.Name)
	return fmt.Sprintf("%s %d %s %s %s", fqdn, s.TTL, s.Class, dns.Type(s.Type).String(), s.RData)
}

// Len returns the length of the RData field, based on its type
func (s SimpleRecord) Len() uint16 {
	emptyString := s.RData == ""
	switch s.Type {
	case int(dns.TypeA):
		if emptyString {
			return 0
		}
		return net.IPv4len
	case int(dns.TypeCNAME):
		if emptyString || s.RData == "." {
			return 1
		}
		return uint16(len(s.RData) + 1)
	case int(dns.TypeAAAA):
		if emptyString {
			return 0
		}
		return net.IPv6len
	default:
		return 0
	}
}

var invalidHostMatcher = regexp.MustCompile(invalidHostLabel)

// GetParsedDomainLabel returns a domain label with max 59 characters,
// parsed for old Hosts.txt requirements, and converted to ASCII and lowercase
func GetParsedDomainLabel(name string) (string, error) {
	labels := dns.SplitDomainName(name)
	if len(labels) == 0 {
		return "", fmt.Errorf("got empty label list for name \"%s\"", name)
	}
	rawLabel := labels[0]
	ascii, err := idna.Punycode.ToASCII(rawLabel)
	if err != nil {
		return "", fmt.Errorf("unable to convert host label to ASCII, error: %v", err)
	}

	validHost := strings.ToLower(invalidHostMatcher.ReplaceAllString(ascii, "-"))
	if len(validHost) > 58 {
		validHost = validHost[:59]
	}

	return validHost, nil
}

// NormalizeZone returns a normalized domain name without the wildcard prefix
func NormalizeZone(domain string) string {
	return strings.TrimPrefix(domain, "*.")
}
