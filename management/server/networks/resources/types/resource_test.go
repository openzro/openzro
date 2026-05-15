package types

import (
	"net/netip"
	"testing"
)

func TestGetResourceType(t *testing.T) {
	tests := []struct {
		input          string
		expectedType   NetworkResourceType
		expectedErr    bool
		expectedDomain string
		expectedPrefix netip.Prefix
	}{
		// Valid host IPs
		{"1.1.1.1", Host, false, "", netip.MustParsePrefix("1.1.1.1/32")},
		{"1.1.1.1/32", Host, false, "", netip.MustParsePrefix("1.1.1.1/32")},
		// Valid subnets
		{"192.168.1.0/24", Subnet, false, "", netip.MustParsePrefix("192.168.1.0/24")},
		{"10.0.0.0/16", Subnet, false, "", netip.MustParsePrefix("10.0.0.0/16")},
		// Valid domains
		{"example.com", Domain, false, "example.com", netip.Prefix{}},
		{"*.example.com", Domain, false, "*.example.com", netip.Prefix{}},
		{"sub.example.com", Domain, false, "sub.example.com", netip.Prefix{}},
		// Invalid inputs
		{"invalid", "", true, "", netip.Prefix{}},
		{"1.1.1.1/abc", "", true, "", netip.Prefix{}},
		{"1234", "", true, "", netip.Prefix{}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, domain, prefix, err := GetResourceType(tt.input)

			if result != tt.expectedType {
				t.Errorf("Expected type %v, got %v", tt.expectedType, result)
			}

			if tt.expectedErr && err == nil {
				t.Errorf("Expected error, got nil")
			}

			if prefix != tt.expectedPrefix {
				t.Errorf("Expected address %v, got %v", tt.expectedPrefix, prefix)
			}

			if domain != tt.expectedDomain {
				t.Errorf("Expected domain %v, got %v", tt.expectedDomain, domain)
			}
		})
	}
}

func TestToAPIResponse_ResolvedAddressesOnlyOnDomain(t *testing.T) {
	cases := []struct {
		name     string
		resType  NetworkResourceType
		resolved []string
		// wantPresent: should the resolved_addresses field be set on the response?
		wantPresent bool
		// wantIPs: the expected slice when present
		wantIPs []string
	}{
		{
			name:        "domain with resolved ips → field present",
			resType:     Domain,
			resolved:    []string{"10.0.0.1", "10.0.0.2"},
			wantPresent: true,
			wantIPs:     []string{"10.0.0.1", "10.0.0.2"},
		},
		{
			name:        "domain with empty slice → field omitted (avoid misreading as zero IPs)",
			resType:     Domain,
			resolved:    []string{},
			wantPresent: false,
		},
		{
			name:        "domain with nil → field omitted",
			resType:     Domain,
			resolved:    nil,
			wantPresent: false,
		},
		{
			name:        "host with resolved ips → field omitted (address already explicit)",
			resType:     Host,
			resolved:    []string{"10.0.0.1"},
			wantPresent: false,
		},
		{
			name:        "subnet with resolved ips → field omitted (address already explicit)",
			resType:     Subnet,
			resolved:    []string{"10.0.0.1"},
			wantPresent: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := &NetworkResource{
				ID:     "res-x",
				Name:   "x",
				Type:   tc.resType,
				Domain: "example.com",
				Prefix: netip.MustParsePrefix("10.0.0.0/24"),
			}
			resp := n.ToAPIResponse(nil, tc.resolved)
			if tc.wantPresent {
				if resp.ResolvedAddresses == nil {
					t.Fatalf("expected resolved_addresses to be present, got nil")
				}
				if got, want := len(*resp.ResolvedAddresses), len(tc.wantIPs); got != want {
					t.Fatalf("resolved_addresses length: got %d want %d", got, want)
				}
				for i, ip := range tc.wantIPs {
					if (*resp.ResolvedAddresses)[i] != ip {
						t.Errorf("resolved_addresses[%d]: got %q want %q", i, (*resp.ResolvedAddresses)[i], ip)
					}
				}
			} else if resp.ResolvedAddresses != nil {
				t.Errorf("expected resolved_addresses to be nil/omitted, got %v", *resp.ResolvedAddresses)
			}
		})
	}
}

func TestToAPIResponse_ResolvedAddressesIsDefensivelyCopied(t *testing.T) {
	// The caller's slice must not alias the response slice — otherwise
	// a later append by the caller could mutate the response.
	n := &NetworkResource{Type: Domain, Domain: "x.com"}
	in := []string{"10.0.0.1"}
	resp := n.ToAPIResponse(nil, in)
	if resp.ResolvedAddresses == nil {
		t.Fatal("response should have resolved_addresses set")
	}
	in[0] = "MUTATED"
	if (*resp.ResolvedAddresses)[0] != "10.0.0.1" {
		t.Fatalf("response was aliased to caller's slice; got %q", (*resp.ResolvedAddresses)[0])
	}
}
