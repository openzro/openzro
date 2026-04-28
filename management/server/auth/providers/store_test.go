package providers

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

func validInput() SaveInput {
	return SaveInput{
		Name:    "prod-zitadel",
		Type:    TypeZitadel,
		Enabled: true,
		Config: Config{
			IssuerURL:    "https://auth.example.com",
			ClientID:     "client-uuid",
			ClientSecret: "super-secret",
			Scopes:       []string{"openid", "profile", "email"},
		},
		BrandLabel: "Acme SSO",
	}
}

func TestStore_RejectsInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []struct {
		name string
		in   SaveInput
	}{
		{"missing name", SaveInput{
			Type:   TypeGeneric,
			Config: Config{IssuerURL: "https://i", ClientID: "c", ClientSecret: "s"},
		}},
		{"missing type", SaveInput{
			Name:   "x",
			Config: Config{IssuerURL: "https://i", ClientID: "c", ClientSecret: "s"},
		}},
		{"missing issuer_url", SaveInput{
			Name: "x", Type: TypeGeneric,
			Config: Config{ClientID: "c", ClientSecret: "s"},
		}},
		{"missing client_id", SaveInput{
			Name: "x", Type: TypeGeneric,
			Config: Config{IssuerURL: "https://i", ClientSecret: "s"},
		}},
		{"missing client_secret", SaveInput{
			Name: "x", Type: TypeGeneric,
			Config: Config{IssuerURL: "https://i", ClientID: "c"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Save(ctx, tc.in)
			assert.Error(t, err)
		})
	}
}

func TestStore_PersistsAndDecrypts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	row, err := s.Save(ctx, validInput())
	require.NoError(t, err)

	assert.NotZero(t, row.ID)
	assert.NotContains(t, string(row.ConfigCipher), "super-secret",
		"client_secret leaked into ciphertext column")
	assert.NotContains(t, string(row.PublicConfig), "super-secret",
		"client_secret leaked into public projection")

	cfg, err := s.Decrypt(row)
	require.NoError(t, err)
	assert.Equal(t, "super-secret", cfg.ClientSecret,
		"decrypt must round-trip the secret")
	assert.Equal(t, "https://auth.example.com", cfg.IssuerURL)
	assert.Equal(t, "client-uuid", cfg.ClientID)
	assert.Equal(t, []string{"openid", "profile", "email"}, cfg.Scopes)
}

func TestStore_PublicConfigOmitsCredentials(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := validInput()
	in.Config.ClientSecret = "TOKEN-LEAKED"
	row, err := s.Save(ctx, in)
	require.NoError(t, err)

	pub := string(row.PublicConfig)
	assert.NotContains(t, pub, "TOKEN-LEAKED")
	assert.Contains(t, pub, "auth.example.com",
		"public projection must include the issuer")
	assert.Contains(t, pub, `"has_client_secret":true`)
}

func TestStore_PublicConfigMissingSecretFlag(t *testing.T) {
	// has_client_secret should reflect whether ClientSecret is set,
	// not be hard-coded true. The current schema rejects empty
	// ClientSecret at validation, so we test the projection
	// directly.
	cfg := Config{IssuerURL: "https://i", ClientID: "c"}
	pub := cfg.PublicView()
	assert.False(t, pub.HasClientSecret)
	assert.Equal(t, "https://i", pub.IssuerURL)
}

func TestStore_UpdatePreservesCreatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := validInput()
	row, err := s.Save(ctx, in)
	require.NoError(t, err)
	created := row.CreatedAt

	in.ID = row.ID
	in.Name = "renamed"
	updated, err := s.Save(ctx, in)
	require.NoError(t, err)

	assert.Equal(t, created.UTC(), updated.CreatedAt.UTC(),
		"update must not move CreatedAt")
	assert.Equal(t, "renamed", updated.Name)
	assert.False(t, updated.UpdatedAt.Before(created),
		"UpdatedAt must not regress on update")
}

func TestStore_UpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := validInput()
	in.ID = 9999
	_, err := s.Save(ctx, in)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStore_GetNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), 9999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStore_DeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.Delete(context.Background(), 9999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStore_ListEnabledFiltersDisabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a := validInput()
	a.Name = "enabled-a"
	a.Enabled = true
	_, err := s.Save(ctx, a)
	require.NoError(t, err)

	b := validInput()
	b.Name = "disabled-b"
	b.Enabled = false
	_, err = s.Save(ctx, b)
	require.NoError(t, err)

	c := validInput()
	c.Name = "enabled-c"
	c.Enabled = true
	_, err = s.Save(ctx, c)
	require.NoError(t, err)

	all, err := s.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	enabled, err := s.ListEnabled(ctx)
	require.NoError(t, err)
	assert.Len(t, enabled, 2)
	for _, row := range enabled {
		assert.True(t, row.Enabled)
	}
}

func TestStore_ToPublicView(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := validInput()
	in.BrandLabel = "Acme"
	in.BrandLogoURL = "https://logo.example.com/acme.svg"
	row, err := s.Save(ctx, in)
	require.NoError(t, err)

	pv := row.ToPublicView()
	assert.Equal(t, row.ID, pv.ID)
	assert.Equal(t, "prod-zitadel", pv.Name)
	assert.Equal(t, TypeZitadel, pv.Type)
	assert.True(t, pv.Enabled)
	assert.Equal(t, "Acme", pv.BrandLabel)
	assert.Equal(t, "https://logo.example.com/acme.svg", pv.BrandLogoURL)
}

func TestStore_DecryptNilRow(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Decrypt(nil)
	assert.Error(t, err)
}

func TestStore_IsKnownType(t *testing.T) {
	for _, k := range []ProviderType{
		TypeGeneric, TypeGoogle, TypeGitHub, TypeMicrosoft,
		TypeEntraID, TypeOkta, TypeKeycloak, TypeAuthentik, TypeZitadel,
	} {
		assert.True(t, IsKnownType(k), "%s should be known", k)
	}
	assert.False(t, IsKnownType("acme-saml"))
	assert.False(t, IsKnownType(""))
}
