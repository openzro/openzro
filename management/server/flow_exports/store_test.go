package flow_exports

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

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

func TestSave_RejectsInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []SaveInput{
		{Type: TypeElastic, Elastic: &ElasticDestConfig{URL: "x", APIKey: "k"}},               // no Name
		{Name: "x", Type: TypeElastic, Elastic: &ElasticDestConfig{}},                          // missing URL
		{Name: "x", Type: TypeElastic, Elastic: &ElasticDestConfig{URL: "https://es:9200"}},   // missing auth
		{Name: "x", Type: TypeS3, S3: &S3DestConfig{}},                                         // missing bucket
		{Name: "x", Type: TypeHTTP, HTTP: &HTTPDestConfig{}},                                   // missing URL
		{Name: "x", Type: "wat"},                                                                // unsupported type
		{Name: "x", Type: TypeElastic},                                                          // no config block
	}
	for i, in := range cases {
		_, err := s.Save(ctx, in)
		assert.Error(t, err, "case %d should fail validation", i)
	}
}

func TestSave_PersistsAndDecrypts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	row, err := s.Save(ctx, SaveInput{
		Name:    "prod-elastic",
		Type:    TypeElastic,
		Enabled: true,
		Elastic: &ElasticDestConfig{
			URL:    "https://es.prod:9200",
			APIKey: "super-secret-key",
			Index:  "openzro-flow-prod",
		},
	})
	require.NoError(t, err)
	require.NotZero(t, row.ID)

	// ConfigCipher must NOT contain the secret in plaintext.
	assert.NotContains(t, string(row.ConfigCipher), "super-secret-key",
		"plaintext credential leaked into ciphertext column")

	// Decrypt round-trip.
	decoded, err := s.Decrypt(row)
	require.NoError(t, err)
	cfg := decoded.(*ElasticDestConfig)
	assert.Equal(t, "super-secret-key", cfg.APIKey,
		"decrypt must round-trip the secret unchanged")
	assert.Equal(t, "https://es.prod:9200", cfg.URL)
	assert.Equal(t, "openzro-flow-prod", cfg.Index)
}

func TestSave_PublicConfigOmitsCredentials(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	row, err := s.Save(ctx, SaveInput{
		Name:    "x",
		Type:    TypeS3,
		Enabled: true,
		S3: &S3DestConfig{
			Bucket:    "my-archive",
			Region:    "us-east-1",
			AccessKey: "AKIA-LEAKED",
			SecretKey: "SECRET-LEAKED",
		},
	})
	require.NoError(t, err)

	pub := string(row.PublicConfig)
	assert.NotContains(t, pub, "AKIA-LEAKED",
		"public config must never contain the access key — even base64")
	assert.NotContains(t, pub, "SECRET-LEAKED")
	assert.Contains(t, pub, "my-archive",
		"public config must include the non-secret subset (bucket/region)")
	assert.Contains(t, pub, `"has_credentials":true`,
		"caller can see that credentials are configured without reading them")
}

func TestUpdate_PreservesCreatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	row, err := s.Save(ctx, SaveInput{
		Name: "x", Type: TypeElastic, Enabled: true,
		Elastic: &ElasticDestConfig{URL: "u", APIKey: "k"},
	})
	require.NoError(t, err)
	originalCreated := row.CreatedAt

	time.Sleep(10 * time.Millisecond)

	updated, err := s.Save(ctx, SaveInput{
		ID: row.ID, Name: "renamed", Type: TypeElastic, Enabled: true,
		Elastic: &ElasticDestConfig{URL: "u2", APIKey: "k"},
	})
	require.NoError(t, err)
	assert.Equal(t, originalCreated.UnixNano(), updated.CreatedAt.UnixNano(),
		"CreatedAt must be preserved across updates")
	assert.True(t, updated.UpdatedAt.After(originalCreated),
		"UpdatedAt must move forward")
}

func TestList_OrderedByID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c"} {
		_, err := s.Save(ctx, SaveInput{
			Name: name, Type: TypeElastic, Enabled: true,
			Elastic: &ElasticDestConfig{URL: "u", APIKey: "k"},
		})
		require.NoError(t, err)
	}
	got, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "a", got[0].Name)
	assert.Equal(t, "b", got[1].Name)
	assert.Equal(t, "c", got[2].Name)
}

func TestDelete_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.Delete(context.Background(), 9999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFieldEncrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	enc, err := NewFieldEncrypt(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)

	plain := []byte("API-KEY-VERY-SECRET")
	cipher, err := enc.Encrypt(plain)
	require.NoError(t, err)
	assert.NotContains(t, string(cipher), "API-KEY-VERY-SECRET")

	round, err := enc.Decrypt(cipher)
	require.NoError(t, err)
	assert.Equal(t, plain, round)
}

func TestFieldEncrypt_UsesFreshNonce(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	enc, _ := NewFieldEncrypt(base64.StdEncoding.EncodeToString(key))

	a, _ := enc.Encrypt([]byte("hello"))
	b, _ := enc.Encrypt([]byte("hello"))
	assert.NotEqual(t, string(a), string(b),
		"identical plaintexts must produce different ciphertexts (nonce reuse breaks GCM)")
}
