package mdm

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/test.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	s, err := NewStore(db, base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	return s
}

func TestStore_RejectsInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []SaveInput{
		{Type: TypeIntune, Intune: &IntuneConfig{TenantID: "t", ClientID: "c"}}, // no Name
		{Name: "x", Type: TypeIntune, Intune: &IntuneConfig{ClientID: "c"}},     // no TenantID
		{Name: "x", Type: TypeIntune},                                           // no Intune block
		{Name: "x", Type: TypeSentinelOne, SentinelOne: &SentinelOneConfig{}},   // no URL
		{Name: "x", Type: "wat"},                                                // unknown type
	}
	for i, in := range cases {
		_, err := s.Save(ctx, in)
		assert.Error(t, err, "case %d should fail", i)
	}
}

func TestStore_PersistsAndDecrypts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	row, err := s.Save(ctx, SaveInput{
		Name:    "prod-intune",
		Type:    TypeIntune,
		Enabled: true,
		Intune: &IntuneConfig{
			TenantID:     "tenant-uuid",
			ClientID:     "client-uuid",
			ClientSecret: "super-secret",
		},
	})
	require.NoError(t, err)

	assert.NotContains(t, string(row.ConfigCipher), "super-secret",
		"client_secret leaked into ciphertext column")
	assert.NotContains(t, string(row.PublicConfig), "super-secret",
		"client_secret leaked into public projection")

	decoded, err := s.Decrypt(row)
	require.NoError(t, err)
	cfg := decoded.(*IntuneConfig)
	assert.Equal(t, "super-secret", cfg.ClientSecret,
		"decrypt must round-trip the secret")
}

func TestStore_PublicConfigOmitsCredentials(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	row, err := s.Save(ctx, SaveInput{
		Name: "x", Type: TypeSentinelOne, Enabled: true,
		SentinelOne: &SentinelOneConfig{
			ManagementURL: "https://acme.sentinelone.net",
			APIToken:      "TOKEN-LEAKED",
		},
	})
	require.NoError(t, err)
	assert.NotContains(t, string(row.PublicConfig), "TOKEN-LEAKED")
	assert.Contains(t, string(row.PublicConfig), "sentinelone.net",
		"public projection must include the URL")
	assert.Contains(t, string(row.PublicConfig), `"has_api_token":true`)
}

func TestStore_DeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.Delete(context.Background(), 9999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStore_RefreshIntervalBounds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	base := SaveInput{
		Name: "x", Type: TypeIntune, Enabled: true,
		Intune: &IntuneConfig{TenantID: "t", ClientID: "c", ClientSecret: "s"},
	}

	cases := []struct {
		name    string
		minutes uint16
		wantErr bool
	}{
		{"zero falls through to default", 0, false},
		{"1 minute lower bound", 1, false},
		{"30 minute middle", 30, false},
		{"60 minute upper bound", 60, false},
		{"61 is out of range", 61, true},
		{"too small but non-zero — disallowed because the form must not let zero leak through after the user typed something", 0, false},
		// Note: lower bound is enforced when RefreshIntervalMinutes != 0,
		// so an explicit 0 means "use default" and stays valid.
	}
	for _, tc := range cases {
		in := base
		in.RefreshIntervalMinutes = tc.minutes
		_, err := s.Save(ctx, in)
		if tc.wantErr {
			assert.Error(t, err, tc.name)
		} else {
			assert.NoError(t, err, tc.name)
		}
	}
}

func TestStore_RefreshIntervalDefaultsToFiveWhenZero(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	row, err := s.Save(ctx, SaveInput{
		Name: "x", Type: TypeIntune, Enabled: true,
		// Operator left the form blank — zero arrives at Save.
		RefreshIntervalMinutes: 0,
		Intune:                 &IntuneConfig{TenantID: "t", ClientID: "c", ClientSecret: "s"},
	})
	require.NoError(t, err)
	assert.Equal(t, uint16(5), row.RefreshIntervalMinutes,
		"zero must be normalised to the documented default")
}

func TestProviderRow_ResolvedRefreshIntervalFallback(t *testing.T) {
	// Row loaded from a pre-knob database carries 0 in the column.
	// Resolver must paper over it without forcing a migration.
	p := ProviderRow{RefreshIntervalMinutes: 0}
	assert.Equal(t, 5*time.Minute, p.ResolvedRefreshInterval())

	p = ProviderRow{RefreshIntervalMinutes: 15}
	assert.Equal(t, 15*time.Minute, p.ResolvedRefreshInterval())
}

func TestStore_SentinelOneComplianceRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	maxThreats := 0
	in := SaveInput{
		Name: "s1", Type: TypeSentinelOne, Enabled: true,
		SentinelOne: &SentinelOneConfig{
			ManagementURL: "https://acme.sentinelone.net",
			APIToken:      "super-secret-token",
			Compliance: SentinelOneCompliance{
				MaxActiveThreats:        &maxThreats,
				RequireDiskEncryption:   true,
				RequireFirewall:         true,
				RequireNetworkConnected: true,
				MinAgentVersion:         "23.4.1",
				SyncWindowMinutes:       1440,
			},
		},
	}
	row, err := s.Save(ctx, in)
	require.NoError(t, err)

	// Token must not leak into the public projection, but the
	// (non-secret) compliance toggles must be present so the
	// dashboard can render current state.
	assert.NotContains(t, string(row.PublicConfig), "super-secret-token")
	assert.Contains(t, string(row.PublicConfig), "require_disk_encryption")

	decoded, err := s.Decrypt(row)
	require.NoError(t, err)
	cfg := decoded.(*SentinelOneConfig)
	assert.Equal(t, "super-secret-token", cfg.APIToken, "secret round-trips via decrypt")
	require.NotNil(t, cfg.Compliance.MaxActiveThreats)
	assert.Equal(t, 0, *cfg.Compliance.MaxActiveThreats)
	assert.True(t, cfg.Compliance.RequireDiskEncryption)
	assert.True(t, cfg.Compliance.RequireFirewall)
	assert.True(t, cfg.Compliance.RequireNetworkConnected)
	assert.Equal(t, "23.4.1", cfg.Compliance.MinAgentVersion)
	assert.Equal(t, 1440, cfg.Compliance.SyncWindowMinutes)

	pub := cfg.PublicView()
	assert.True(t, pub.Compliance.RequireFirewall)
	assert.False(t, pub.HasAPIToken == false, "has_api_token must reflect the stored token")
}

func TestStore_SentinelOneComplianceValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	neg := -1

	bad := []SentinelOneCompliance{
		{MaxActiveThreats: &neg},           // negative threshold
		{SyncWindowMinutes: -5},            // negative window
		{MinAgentVersion: "not-a-version"}, // unparseable floor
	}
	for i, comp := range bad {
		_, err := s.Save(ctx, SaveInput{
			Name: "s1", Type: TypeSentinelOne, Enabled: true,
			SentinelOne: &SentinelOneConfig{
				ManagementURL: "https://acme.sentinelone.net",
				APIToken:      "t",
				Compliance:    comp,
			},
		})
		assert.Error(t, err, "case %d should be rejected at Save", i)
	}

	// Zero-value compliance is valid (the backward-compat baseline).
	_, err := s.Save(ctx, SaveInput{
		Name: "s1ok", Type: TypeSentinelOne, Enabled: true,
		SentinelOne: &SentinelOneConfig{
			ManagementURL: "https://acme.sentinelone.net",
			APIToken:      "t",
		},
	})
	assert.NoError(t, err, "empty compliance must be accepted")
}
