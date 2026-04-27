package mdm

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
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
