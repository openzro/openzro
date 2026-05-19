package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/kardianos/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	serviceStartTimeout = 10 * time.Second
	serviceStopTimeout  = 5 * time.Second
	statusPollInterval  = 500 * time.Millisecond
)

// waitForServiceStatus waits for service to reach expected status with timeout
func waitForServiceStatus(expectedStatus service.Status, timeout time.Duration) (bool, error) {
	cfg, err := newSVCConfig()
	if err != nil {
		return false, err
	}

	ctxSvc, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := newSVC(newProgram(ctxSvc, cancel), cfg)
	if err != nil {
		return false, err
	}

	ctx, timeoutCancel := context.WithTimeout(context.Background(), timeout)
	defer timeoutCancel()

	ticker := time.NewTicker(statusPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("timeout waiting for service status %v", expectedStatus)
		case <-ticker.C:
			status, err := s.Status()
			if err != nil {
				// Continue polling on transient errors
				continue
			}
			if status == expectedStatus {
				return true, nil
			}
		}
	}
}

// TestServiceLifecycle tests the complete service lifecycle
func TestServiceLifecycle(t *testing.T) {
	// TODO: Add support for Windows and macOS
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" {
		t.Skipf("Skipping service lifecycle test on unsupported OS: %s", runtime.GOOS)
	}

	if os.Getenv("CONTAINER") == "true" {
		t.Skip("Skipping service lifecycle test in container environment")
	}

	// Driving the real OS service manager makes the started daemon
	// perform a login against the configured management server. With
	// no -m set the daemon falls back to DefaultManagementURL
	// (api.openzro.io:443). openZro is self-hosted only — there is no
	// managed api.openzro.io service — so on the FreeBSD CI runner,
	// whose sandboxed resolver returns NXDOMAIN for it, that mandatory
	// login hard-fails ("context deadline exceeded") and Restart
	// returns exit status 1. (Linux CI happens to resolve the public
	// domain and limps through, so its service-manager coverage is
	// kept.) This is a non-hermetic test dependency, not a product
	// bug — the same situation the TestUpdateOldManagementURL comment
	// documents. Real FreeBSD rc.d lifecycle validation needs an
	// integration env with a reachable management server, not unit CI.
	if runtime.GOOS == "freebsd" && os.Getenv("CI") == "true" {
		t.Skip("non-hermetic on FreeBSD CI: default management URL (api.openzro.io) is unresolvable there, so the daemon login hard-fails — see TestUpdateOldManagementURL")
	}

	originalServiceName := serviceName
	serviceName = "openzrotest" + fmt.Sprintf("%d", time.Now().Unix())
	defer func() {
		serviceName = originalServiceName
	}()

	tempDir := t.TempDir()
	configPath = fmt.Sprintf("%s/openzro-test-config.json", tempDir)
	logLevel = "info"
	daemonAddr = fmt.Sprintf("unix://%s/openzro-test.sock", tempDir)

	ctx := context.Background()

	t.Run("Install", func(t *testing.T) {
		installCmd.SetContext(ctx)
		err := installCmd.RunE(installCmd, []string{})
		require.NoError(t, err)

		cfg, err := newSVCConfig()
		require.NoError(t, err)

		ctxSvc, cancel := context.WithCancel(context.Background())
		defer cancel()

		s, err := newSVC(newProgram(ctxSvc, cancel), cfg)
		require.NoError(t, err)

		status, err := s.Status()
		assert.NoError(t, err)
		assert.NotEqual(t, service.StatusUnknown, status)
	})

	t.Run("Start", func(t *testing.T) {
		startCmd.SetContext(ctx)
		err := startCmd.RunE(startCmd, []string{})
		require.NoError(t, err)

		running, err := waitForServiceStatus(service.StatusRunning, serviceStartTimeout)
		require.NoError(t, err)
		assert.True(t, running)
	})

	t.Run("Restart", func(t *testing.T) {
		restartCmd.SetContext(ctx)
		err := restartCmd.RunE(restartCmd, []string{})
		require.NoError(t, err)

		running, err := waitForServiceStatus(service.StatusRunning, serviceStartTimeout)
		require.NoError(t, err)
		assert.True(t, running)
	})

	t.Run("Reconfigure", func(t *testing.T) {
		originalLogLevel := logLevel
		logLevel = "debug"
		defer func() {
			logLevel = originalLogLevel
		}()

		reconfigureCmd.SetContext(ctx)
		err := reconfigureCmd.RunE(reconfigureCmd, []string{})
		require.NoError(t, err)

		running, err := waitForServiceStatus(service.StatusRunning, serviceStartTimeout)
		require.NoError(t, err)
		assert.True(t, running)
	})

	t.Run("Stop", func(t *testing.T) {
		stopCmd.SetContext(ctx)
		err := stopCmd.RunE(stopCmd, []string{})
		require.NoError(t, err)

		stopped, err := waitForServiceStatus(service.StatusStopped, serviceStopTimeout)
		require.NoError(t, err)
		assert.True(t, stopped)
	})

	t.Run("Uninstall", func(t *testing.T) {
		uninstallCmd.SetContext(ctx)
		err := uninstallCmd.RunE(uninstallCmd, []string{})
		require.NoError(t, err)

		cfg, err := newSVCConfig()
		require.NoError(t, err)

		ctxSvc, cancel := context.WithCancel(context.Background())
		defer cancel()

		s, err := newSVC(newProgram(ctxSvc, cancel), cfg)
		require.NoError(t, err)

		_, err = s.Status()
		assert.Error(t, err)
	})
}

// TestServiceEnvVars tests environment variable parsing
func TestServiceEnvVars(t *testing.T) {
	tests := []struct {
		name      string
		envVars   []string
		expected  map[string]string
		expectErr bool
	}{
		{
			name:    "Valid single env var",
			envVars: []string{"LOG_LEVEL=debug"},
			expected: map[string]string{
				"LOG_LEVEL": "debug",
			},
		},
		{
			name:    "Valid multiple env vars",
			envVars: []string{"LOG_LEVEL=debug", "CUSTOM_VAR=value"},
			expected: map[string]string{
				"LOG_LEVEL":  "debug",
				"CUSTOM_VAR": "value",
			},
		},
		{
			name:    "Env var with spaces",
			envVars: []string{" KEY = value "},
			expected: map[string]string{
				"KEY": "value",
			},
		},
		{
			name:      "Invalid format - no equals",
			envVars:   []string{"INVALID"},
			expectErr: true,
		},
		{
			name:      "Invalid format - empty key",
			envVars:   []string{"=value"},
			expectErr: true,
		},
		{
			name:    "Empty value is valid",
			envVars: []string{"KEY="},
			expected: map[string]string{
				"KEY": "",
			},
		},
		{
			name:     "Empty slice",
			envVars:  []string{},
			expected: map[string]string{},
		},
		{
			name:     "Empty string in slice",
			envVars:  []string{"", "KEY=value", ""},
			expected: map[string]string{"KEY": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseServiceEnvVars(tt.envVars)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestServiceConfigWithEnvVars tests service config creation with env vars
func TestServiceConfigWithEnvVars(t *testing.T) {
	originalServiceName := serviceName
	originalServiceEnvVars := serviceEnvVars
	defer func() {
		serviceName = originalServiceName
		serviceEnvVars = originalServiceEnvVars
	}()

	serviceName = "test-service"
	serviceEnvVars = []string{"TEST_VAR=test_value", "ANOTHER_VAR=another_value"}

	cfg, err := newSVCConfig()
	require.NoError(t, err)

	assert.Equal(t, "test-service", cfg.Name)
	assert.Equal(t, "test_value", cfg.EnvVars["TEST_VAR"])
	assert.Equal(t, "another_value", cfg.EnvVars["ANOTHER_VAR"])

	if runtime.GOOS == "linux" {
		assert.Equal(t, "test-service", cfg.EnvVars["SYSTEMD_UNIT"])
	}
}
