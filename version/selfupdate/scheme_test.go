package selfupdate

import "testing"

func TestRequireSafeScheme(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"https://github.com/openzro/openzro/releases/download/update/m.json", false},
		{"https://downloads.openzro.io/m.json", false},
		{"http://127.0.0.1:8080/m.json", false}, // loopback ok (test/mirror)
		{"http://[::1]:9/m.json", false},        // ipv6 loopback ok
		{"http://localhost:1234/m.json", false}, // localhost ok
		{"http://example.com/m.json", true},     // plain http, routable -> MITM risk
		{"http://169.254.169.254/m.json", true}, // not loopback
		{"ftp://host/m.json", true},             // non-http(s) scheme
		{"file:///etc/passwd", true},            // local file
		{"://nonsense", true},                   // unparseable / no scheme
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			err := requireSafeScheme(tc.url)
			if tc.wantErr && err == nil {
				t.Fatalf("expected rejection for %q", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected rejection for %q: %v", tc.url, err)
			}
		})
	}
}
