package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The dashboard URL carries an approval token and a TOTP code to the
// approval page, so it must be https, except on loopback for development.
func TestNormalizeDashboardURL(t *testing.T) {
	ok := []struct{ in, want string }{
		{"", ""},
		{"https://app.example.com", "https://app.example.com"},
		{"https://app.example.com/", "https://app.example.com"},
		{"https://example.com/dashboard/", "https://example.com/dashboard"},
		{"  https://app.example.com  ", "https://app.example.com"},
		{"http://localhost:3000", "http://localhost:3000"},
		{"http://127.0.0.1:3000", "http://127.0.0.1:3000"},
		{"http://127.1.2.3", "http://127.1.2.3"},
		{"http://[::1]:3000", "http://[::1]:3000"},
	}
	for _, tc := range ok {
		got, err := NormalizeDashboardURL(tc.in)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
	}

	bad := []string{
		"http://app.example.com",
		"http://10.0.0.5:3000",
		"http://localhost.example.com",
		"ftp://app.example.com",
		"app.example.com",
		"https://",
		"https://app.example.com?x=1",
		"https://app.example.com#frag",
		"https://user:pass@app.example.com",
		"https://:443",
		"https://app.example.com:99999",
		"https://app.example.com:0",
		"https://app.example.com:",
	}
	for _, in := range bad {
		_, err := NormalizeDashboardURL(in)
		assert.Error(t, err, in)
	}
}
