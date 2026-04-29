package activity_exporters

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.TempDir()+"/test.db"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	keyBytes := make([]byte, 32)
	_, _ = rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)
	store, err := NewStore(db, key)
	require.NoError(t, err)
	return store
}

// TestStore_RoundTrip_Datadog locks in the canonical happy path:
// save with a secret, list returns the row, decrypt yields the
// original config back. Public projection must NOT include the API
// key — that contract is what the dashboard relies on for
// "configured/not configured" rendering without leaking secrets.
func TestStore_RoundTrip_Datadog(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row, err := store.Save(ctx, SaveInput{
		AccountID: "acct-1",
		Name:      "prod-dd",
		Type:      TypeDatadog,
		Enabled:   true,
		Datadog: &DatadogDestConfig{
			Site:    "us1",
			APIKey:  "secret-api-key",
			Service: "openzro-prod",
		},
	})
	require.NoError(t, err)
	assert.NotZero(t, row.ID)
	assert.NotEmpty(t, row.ConfigCipher)
	// Public config never carries the secret.
	assert.NotContains(t, string(row.PublicConfig), "secret-api-key")
	assert.Contains(t, string(row.PublicConfig), `"has_api_key":true`)

	rows, err := store.List(ctx, "acct-1")
	require.NoError(t, err)
	require.Len(t, rows, 1)

	plain, err := store.Decrypt(&rows[0])
	require.NoError(t, err)
	dd, ok := plain.(*DatadogDestConfig)
	require.True(t, ok)
	assert.Equal(t, "secret-api-key", dd.APIKey)
	assert.Equal(t, "openzro-prod", dd.Service)
}

// TestStore_TenantIsolation defends against the cross-tenant ID-
// guessing footgun. Account A creates an exporter; Account B asks
// for the list — must see nothing. Same for Delete.
func TestStore_TenantIsolation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	a, err := store.Save(ctx, SaveInput{
		AccountID: "acct-a", Name: "a-only", Type: TypeHTTP, Enabled: true,
		HTTP: &HTTPDestConfig{URL: "https://a.example/audit"},
	})
	require.NoError(t, err)

	bRows, err := store.List(ctx, "acct-b")
	require.NoError(t, err)
	assert.Empty(t, bRows, "tenant B must not see tenant A's exporters")

	// Delete from B for A's row must report not-found.
	err = store.Delete(ctx, "acct-b", a.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	// Delete from A succeeds.
	require.NoError(t, store.Delete(ctx, "acct-a", a.ID))
}

// TestStore_EmptySecretPreservesExisting locks in the dashboard
// contract: when the operator updates an exporter without re-typing
// the secret, the existing secret is preserved. Without this, every
// edit would silently disable auth.
func TestStore_EmptySecretPreservesExisting(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	saved, err := store.Save(ctx, SaveInput{
		AccountID: "acct-1", Name: "edit-me", Type: TypeDatadog, Enabled: true,
		Datadog: &DatadogDestConfig{Site: "us1", APIKey: "first-key"},
	})
	require.NoError(t, err)

	// Update with empty APIKey but new tags — the secret must persist.
	_, err = store.Save(ctx, SaveInput{
		ID:        saved.ID,
		AccountID: "acct-1",
		Name:      "edit-me",
		Type:      TypeDatadog,
		Enabled:   true,
		Datadog:   &DatadogDestConfig{Site: "us1", APIKey: "", Tags: "env:prod"},
	})
	require.NoError(t, err)

	row, err := store.Get(ctx, saved.ID)
	require.NoError(t, err)
	plain, err := store.Decrypt(row)
	require.NoError(t, err)
	dd := plain.(*DatadogDestConfig)
	assert.Equal(t, "first-key", dd.APIKey, "empty APIKey on update must preserve previous secret")
	assert.Equal(t, "env:prod", dd.Tags)
}

// TestStore_RejectsBrokenTemplate proves the template gate: a
// syntactically broken template at save time is refused with a
// readable error so the operator sees it in the UI rather than at
// 3am when the audit pipeline silently stops.
func TestStore_RejectsBrokenTemplate(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Save(context.Background(), SaveInput{
		AccountID: "acct-1", Name: "bad-tmpl", Type: TypeHTTP, Enabled: true,
		Template: `{{ .NoSuchField }}`,
		HTTP:     &HTTPDestConfig{URL: "https://x.example/audit"},
	})
	require.Error(t, err)
}
