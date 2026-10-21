package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/types"
)

func writeConfigWithDashboardURL(t *testing.T, dashboardURL string) string {
	t.Helper()
	dir := t.TempDir()
	body, err := json.Marshal(map[string]any{
		"Datadir":     dir,
		"StoreConfig": map[string]any{"Engine": "sqlite"},
		"HttpConfig":  map[string]any{"DashboardURL": dashboardURL},
	})
	require.NoError(t, err)
	path := filepath.Join(dir, "management.json")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	return path
}

// The management server refuses to start with a dashboard URL that would
// carry approval tokens in the clear; the environment overrides the file.
func TestLoadMgmtConfig_DashboardURL(t *testing.T) {
	ctx := context.Background()

	cfg, err := loadMgmtConfig(ctx, writeConfigWithDashboardURL(t, "https://app.example.com/"))
	require.NoError(t, err)
	assert.Equal(t, "https://app.example.com", cfg.HttpConfig.DashboardURL)

	_, err = loadMgmtConfig(ctx, writeConfigWithDashboardURL(t, "http://app.example.com"))
	require.Error(t, err)

	t.Setenv(types.DashboardURLEnv, "http://localhost:3000")
	cfg, err = loadMgmtConfig(ctx, writeConfigWithDashboardURL(t, "https://app.example.com"))
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:3000", cfg.HttpConfig.DashboardURL, "the environment overrides the file")

	t.Setenv(types.DashboardURLEnv, "http://10.0.0.5")
	_, err = loadMgmtConfig(ctx, writeConfigWithDashboardURL(t, ""))
	require.Error(t, err)
}
