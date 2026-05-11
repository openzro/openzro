package flow_exports

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

func TestSave_RejectsInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []SaveInput{
		{Type: TypeElastic, Elastic: &ElasticDestConfig{URL: "x", APIKey: "k"}},             // no Name
		{Name: "x", Type: TypeElastic, Elastic: &ElasticDestConfig{}},                       // missing URL
		{Name: "x", Type: TypeElastic, Elastic: &ElasticDestConfig{URL: "https://es:9200"}}, // missing auth
		{Name: "x", Type: TypeS3, S3: &S3DestConfig{}},                                      // missing bucket
		{Name: "x", Type: TypeHTTP, HTTP: &HTTPDestConfig{}},                                // missing URL
		{Name: "x", Type: "wat"},       // unsupported type
		{Name: "x", Type: TypeElastic}, // no config block
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

// Regression: the dashboard sends "" for credential fields on update
// to mean "leave as is" (the API never returns secrets back). Without
// MergeIncomingSecret the Save() update path wiped the encrypted blob
// and Validate() rejected the request with "auth required", surfacing
// as a 500 in the UI. Verified by running both the API-key and
// basic-auth (username/password) paths through an empty re-Save.
func TestUpdate_PreservesSecretsWhenIncomingIsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		seed SaveInput
		want func(t *testing.T, cfg *ElasticDestConfig)
	}{
		{
			name: "api_key",
			seed: SaveInput{
				Name: "es-key", Type: TypeElastic, Enabled: true,
				Elastic: &ElasticDestConfig{URL: "https://es:9200", APIKey: "seed-key"},
			},
			want: func(t *testing.T, cfg *ElasticDestConfig) {
				assert.Equal(t, "seed-key", cfg.APIKey)
			},
		},
		{
			name: "basic_auth",
			seed: SaveInput{
				Name: "es-basic", Type: TypeElastic, Enabled: true,
				Elastic: &ElasticDestConfig{
					URL: "https://es:9200", Username: "elastic", Password: "pw",
				},
			},
			want: func(t *testing.T, cfg *ElasticDestConfig) {
				assert.Equal(t, "elastic", cfg.Username,
					"username must be preserved across empty-on-update")
				assert.Equal(t, "pw", cfg.Password)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, err := s.Save(ctx, tc.seed)
			require.NoError(t, err)

			// Caller posts the public fields again but leaves every
			// credential blank — the exact wire shape the dashboard
			// sends when the operator just edits the URL or Index.
			updated, err := s.Save(ctx, SaveInput{
				ID: row.ID, Name: tc.seed.Name, Type: TypeElastic, Enabled: true,
				Elastic: &ElasticDestConfig{URL: "https://es-renamed:9200"},
			})
			require.NoError(t, err, "empty credentials on update must not error")

			decoded, err := s.Decrypt(updated)
			require.NoError(t, err)
			cfg := decoded.(*ElasticDestConfig)
			assert.Equal(t, "https://es-renamed:9200", cfg.URL,
				"non-secret fields must take the new value")
			tc.want(t, cfg)
		})
	}
}

// Regression: same merge needs to apply to the S3, Datadog, and GCS
// destinations — leaving secrets blank on update must not wipe them.
func TestUpdate_PreservesSecretsAcrossDestinations(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("s3", func(t *testing.T) {
		row, err := s.Save(ctx, SaveInput{
			Name: "s3", Type: TypeS3, Enabled: true,
			S3: &S3DestConfig{
				Bucket: "b", AccessKey: "AKIA-x", SecretKey: "SK-x",
			},
		})
		require.NoError(t, err)

		updated, err := s.Save(ctx, SaveInput{
			ID: row.ID, Name: "s3", Type: TypeS3, Enabled: true,
			S3: &S3DestConfig{Bucket: "b-renamed"},
		})
		require.NoError(t, err)
		decoded, _ := s.Decrypt(updated)
		cfg := decoded.(*S3DestConfig)
		assert.Equal(t, "AKIA-x", cfg.AccessKey)
		assert.Equal(t, "SK-x", cfg.SecretKey)
	})

	t.Run("datadog", func(t *testing.T) {
		row, err := s.Save(ctx, SaveInput{
			Name: "dd", Type: TypeDatadog, Enabled: true,
			Datadog: &DatadogDestConfig{Site: "us1", APIKey: "DD-API"},
		})
		require.NoError(t, err)

		updated, err := s.Save(ctx, SaveInput{
			ID: row.ID, Name: "dd", Type: TypeDatadog, Enabled: true,
			Datadog: &DatadogDestConfig{Site: "us3"},
		})
		require.NoError(t, err)
		decoded, _ := s.Decrypt(updated)
		cfg := decoded.(*DatadogDestConfig)
		assert.Equal(t, "DD-API", cfg.APIKey)
		assert.Equal(t, "us3", cfg.Site)
	})

	t.Run("gcs_inline_json", func(t *testing.T) {
		row, err := s.Save(ctx, SaveInput{
			Name: "gcs", Type: TypeGCS, Enabled: true,
			GCS: &GCSDestConfig{
				Bucket: "g", CredentialsJSON: `{"type":"service_account"}`,
			},
		})
		require.NoError(t, err)

		updated, err := s.Save(ctx, SaveInput{
			ID: row.ID, Name: "gcs", Type: TypeGCS, Enabled: true,
			GCS: &GCSDestConfig{Bucket: "g-renamed"},
		})
		require.NoError(t, err)
		decoded, _ := s.Decrypt(updated)
		cfg := decoded.(*GCSDestConfig)
		assert.Equal(t, `{"type":"service_account"}`, cfg.CredentialsJSON)
	})
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
