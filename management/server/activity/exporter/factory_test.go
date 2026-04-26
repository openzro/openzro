package exporter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFromEnv_NoConfigReturnsEmpty(t *testing.T) {
	t.Setenv(envURL, "")
	exps, err := NewFromEnv(context.Background())
	require.NoError(t, err)
	assert.Empty(t, exps, "no env vars set must produce no exporters")
}

func TestNewFromEnv_URLOnly(t *testing.T) {
	t.Setenv(envURL, "https://example.com/audit")
	t.Setenv(envHeadersJSON, "")

	exps, err := NewFromEnv(context.Background())
	require.NoError(t, err)
	require.Len(t, exps, 1)
	assert.Equal(t, "http-webhook", exps[0].Name())
}

func TestNewFromEnv_ParsesHeadersJSON(t *testing.T) {
	t.Setenv(envURL, "https://example.com/audit")
	t.Setenv(envHeadersJSON, `{"Authorization":"Bearer abc","X-Org":"openzro"}`)

	exps, err := NewFromEnv(context.Background())
	require.NoError(t, err)
	require.Len(t, exps, 1)

	hw, ok := exps[0].(*HTTPWebhook)
	require.True(t, ok)
	assert.Equal(t, "Bearer abc", hw.headers["Authorization"])
	assert.Equal(t, "openzro", hw.headers["X-Org"])
}

func TestNewFromEnv_RejectsInvalidHeadersJSON(t *testing.T) {
	t.Setenv(envURL, "https://example.com/audit")
	t.Setenv(envHeadersJSON, "{not-json")

	_, err := NewFromEnv(context.Background())
	assert.Error(t, err)
}

func TestNewFromEnv_TimeoutAndAttemptsOverride(t *testing.T) {
	t.Setenv(envURL, "https://example.com/audit")
	t.Setenv(envHeadersJSON, "")
	t.Setenv(envTimeout, "12s")
	t.Setenv(envMaxAttempts, "7")
	t.Setenv(envInitialBackoff, "500ms")

	exps, err := NewFromEnv(context.Background())
	require.NoError(t, err)
	hw := exps[0].(*HTTPWebhook)
	assert.Equal(t, 7, hw.maxAttempts)
	assert.Equal(t, 500*int64(1e6), int64(hw.initialBackoff))
}
