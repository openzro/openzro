package factory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFromEnv_DisabledByDefault(t *testing.T) {
	t.Setenv(envEngine, "")
	got, err := NewFromEnv(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got, "engine unset must return nil — persistence disabled")
}

func TestNewFromEnv_NoneIsExplicitlySupported(t *testing.T) {
	t.Setenv(envEngine, "none")
	got, err := NewFromEnv(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestNewFromEnv_RequiresDSN(t *testing.T) {
	t.Setenv(envEngine, "sqlite")
	t.Setenv(envDSN, "")
	_, err := NewFromEnv(context.Background())
	assert.Error(t, err, "engine=sqlite without DSN must error")
}

func TestNewFromEnv_OpensSQLite(t *testing.T) {
	dsn := "file:" + t.TempDir() + "/flow.db"
	t.Setenv(envEngine, "sqlite")
	t.Setenv(envDSN, dsn)

	got, err := NewFromEnv(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	defer got.Close()

	assert.Equal(t, 7*24*time.Hour, got.Retention,
		"default retention must be 7 days per ADR-0002")
	assert.NotNil(t, got.Store)
}

func TestNewFromEnv_HonorsCustomRetention(t *testing.T) {
	dsn := "file:" + t.TempDir() + "/flow.db"
	t.Setenv(envEngine, "sqlite")
	t.Setenv(envDSN, dsn)
	t.Setenv(envRetention, "72h")

	got, err := NewFromEnv(context.Background())
	require.NoError(t, err)
	defer got.Close()

	assert.Equal(t, 72*time.Hour, got.Retention)
}

func TestNewFromEnv_FallsBackOnInvalidRetention(t *testing.T) {
	dsn := "file:" + t.TempDir() + "/flow.db"
	t.Setenv(envEngine, "sqlite")
	t.Setenv(envDSN, dsn)
	t.Setenv(envRetention, "not-a-duration")

	got, err := NewFromEnv(context.Background())
	require.NoError(t, err)
	defer got.Close()

	assert.Equal(t, 7*24*time.Hour, got.Retention,
		"invalid retention must fall back to default, not crash")
}

func TestNewFromEnv_RejectsUnknownEngine(t *testing.T) {
	t.Setenv(envEngine, "wat")
	t.Setenv(envDSN, "ignored")
	_, err := NewFromEnv(context.Background())
	assert.Error(t, err)
}

func TestNewFromEnv_ClickhouseNotImplementedYet(t *testing.T) {
	t.Setenv(envEngine, "clickhouse")
	t.Setenv(envDSN, "tcp://localhost:9000")
	_, err := NewFromEnv(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clickhouse",
		"error must point operator at the deferred PR")
}
