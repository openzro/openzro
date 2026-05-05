package geolocation

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDownloadSource_OpenzroMirrorByDefault(t *testing.T) {
	s := DownloadSource{}
	for name, got := range map[string]string{
		"MMDB":         s.MMDB(),
		"MMDBChecksum": s.MMDBChecksum(),
		"CSV":          s.CSV(),
		"CSVChecksum":  s.CSVChecksum(),
	} {
		require.True(t, strings.HasPrefix(got, geoLiteOpenzroMirror),
			"%s should hit the openZro mirror when LicenseKey is empty, got %s", name, got)
		require.NotContains(t, got, "license_key",
			"%s must not carry a license_key when LicenseKey is empty", name)
	}
}

func TestDownloadSource_MaxMindDirectWhenKeyIsSet(t *testing.T) {
	const key = "abc123-test-key"
	s := DownloadSource{LicenseKey: key}

	cases := map[string]struct {
		got       string
		editionID string
		suffix    string
	}{
		"MMDB":         {s.MMDB(), "GeoLite2-City", "tar.gz"},
		"MMDBChecksum": {s.MMDBChecksum(), "GeoLite2-City", "tar.gz.sha256"},
		"CSV":          {s.CSV(), "GeoLite2-City-CSV", "zip"},
		"CSVChecksum":  {s.CSVChecksum(), "GeoLite2-City-CSV", "zip.sha256"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.True(t, strings.HasPrefix(tc.got, geoLiteMaxMindDirect),
				"%s should hit MaxMind direct when LicenseKey is set, got %s", name, tc.got)

			u, err := url.Parse(tc.got)
			require.NoError(t, err)
			require.Equal(t, key, u.Query().Get("license_key"),
				"license_key query param must be set verbatim")
			require.Equal(t, tc.editionID, u.Query().Get("edition_id"))
			require.Equal(t, tc.suffix, u.Query().Get("suffix"))
		})
	}
}

func TestDownloadSource_StringNeverLeaksKey(t *testing.T) {
	s := DownloadSource{LicenseKey: "secret-key-do-not-leak"}
	got := s.String()
	require.NotContains(t, got, "secret-key-do-not-leak",
		"DownloadSource.String must NEVER include the license key")
	require.Contains(t, got, "with license key",
		"the operator should still be able to tell from logs that auth is on")

	require.Equal(t, geoLiteOpenzroMirror, DownloadSource{}.String(),
		"empty source stringifies to the public mirror URL")
}

func TestRedactURL_ScrubsLicenseKey(t *testing.T) {
	in := "https://download.maxmind.com/app/geoip_download?edition_id=GeoLite2-City&license_key=secret-key&suffix=tar.gz"
	got := redactURL(in)
	require.NotContains(t, got, "secret-key",
		"redactURL must scrub the license_key value from the URL")
	require.Contains(t, got, "license_key=REDACTED",
		"redacted form must be self-documenting in logs")
	require.Contains(t, got, "edition_id=GeoLite2-City",
		"other query params must survive redaction")
}

func TestRedactURL_NoOpWhenNoKey(t *testing.T) {
	in := "https://pkg.openzro.io/geolocation-dbs/GeoLite2-City/download?suffix=tar.gz"
	require.Equal(t, in, redactURL(in),
		"URLs without a license_key must pass through unchanged")
}

func TestRedactURL_HandlesUnparseable(t *testing.T) {
	// A URL that fails url.Parse should pass through. We never want
	// redactURL itself to be a footgun that hides data.
	in := "://not-a-url"
	require.Equal(t, in, redactURL(in))
}
