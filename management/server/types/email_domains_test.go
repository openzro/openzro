package types

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/openzro/openzro/management/server/status"
)

func TestNormalizeAllowedEmailDomains(t *testing.T) {
	tooMany := make([]string, MaxAllowedEmailDomains+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("d%d.example.com", i)
	}

	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{name: "nil stays empty", in: nil, want: nil},
		{name: "empty stays empty", in: []string{}, want: nil},
		{name: "lowercased and trimmed", in: []string{"  Example.COM "}, want: []string{"example.com"}},
		{name: "leading at stripped", in: []string{"@example.com"}, want: []string{"example.com"}},
		{name: "duplicates removed, order kept", in: []string{"b.com", "a.com", "B.com"}, want: []string{"b.com", "a.com"}},
		{name: "subdomain allowed", in: []string{"eng.example.com"}, want: []string{"eng.example.com"}},
		{name: "wildcard rejected", in: []string{"*.example.com"}, wantErr: true},
		{name: "single label rejected", in: []string{"localhost"}, wantErr: true},
		{name: "empty entry rejected", in: []string{"   "}, wantErr: true},
		{name: "email rejected", in: []string{"user@example.com"}, wantErr: true},
		{name: "leading hyphen label rejected", in: []string{"-a.example.com"}, wantErr: true},
		{name: "too many rejected", in: tooMany, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeAllowedEmailDomains(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				var sErr *status.Error
				if !errors.As(err, &sErr) || sErr.Type() != status.InvalidArgument {
					t.Fatalf("expected InvalidArgument status error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v want %#v", got, tc.want)
			}
		})
	}
}

func TestSettings_EmailDomainAllowed(t *testing.T) {
	tests := []struct {
		name    string
		domains []string
		email   string
		want    bool
	}{
		{name: "empty list allows anything", domains: nil, email: "a@anything.io", want: true},
		{name: "empty list allows empty email", domains: nil, email: "", want: true},
		{name: "exact match", domains: []string{"example.com"}, email: "a@example.com", want: true},
		{name: "case-insensitive", domains: []string{"example.com"}, email: "A@Example.COM", want: true},
		{name: "subdomain not implied", domains: []string{"example.com"}, email: "a@eng.example.com", want: false},
		{name: "explicit subdomain", domains: []string{"eng.example.com"}, email: "a@eng.example.com", want: true},
		{name: "suffix trick rejected", domains: []string{"example.com"}, email: "a@evilexample.com", want: false},
		{name: "last at wins", domains: []string{"example.com"}, email: "a@evil.com@example.com", want: true},
		{name: "last at wins, reversed", domains: []string{"example.com"}, email: "a@example.com@evil.com", want: false},
		{name: "no at", domains: []string{"example.com"}, email: "example.com", want: false},
		{name: "empty email", domains: []string{"example.com"}, email: "", want: false},
		{name: "second entry", domains: []string{"a.com", "example.com"}, email: "x@example.com", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Settings{AllowedEmailDomains: tc.domains}
			if got := s.EmailDomainAllowed(tc.email); got != tc.want {
				t.Fatalf("EmailDomainAllowed(%q) = %v, want %v", tc.email, got, tc.want)
			}
		})
	}
}

func TestSettings_CopyAllowedEmailDomains(t *testing.T) {
	s := &Settings{AllowedEmailDomains: []string{"example.com"}}
	c := s.Copy()
	c.AllowedEmailDomains[0] = "mutated.com"
	if s.AllowedEmailDomains[0] != "example.com" {
		t.Fatalf("Copy aliases AllowedEmailDomains")
	}
}
