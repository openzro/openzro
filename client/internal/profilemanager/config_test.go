package profilemanager

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/util"
)

func TestGetConfig(t *testing.T) {
	// case 1: new default config has to be generated
	config, err := UpdateOrCreateConfig(ConfigInput{
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})
	if err != nil {
		return
	}

	assert.Equal(t, config.ManagementURL.String(), DefaultManagementURL)
	assert.Equal(t, config.AdminURL.String(), DefaultAdminURL)

	managementURL := "https://test.management.url:33071"
	adminURL := "https://app.admin.url:443"
	path := filepath.Join(t.TempDir(), "config.json")
	preSharedKey := "preSharedKey"

	// case 2: new config has to be generated
	config, err = UpdateOrCreateConfig(ConfigInput{
		ManagementURL: managementURL,
		AdminURL:      adminURL,
		ConfigPath:    path,
		PreSharedKey:  &preSharedKey,
	})
	if err != nil {
		return
	}

	assert.Equal(t, config.ManagementURL.String(), managementURL)
	assert.Equal(t, config.PreSharedKey, preSharedKey)

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Errorf("config file was expected to be created under path %s", path)
	}

	// case 3: existing config -> fetch it
	config, err = UpdateOrCreateConfig(ConfigInput{
		ManagementURL: managementURL,
		AdminURL:      adminURL,
		ConfigPath:    path,
		PreSharedKey:  &preSharedKey,
	})
	if err != nil {
		return
	}

	assert.Equal(t, config.ManagementURL.String(), managementURL)
	assert.Equal(t, config.PreSharedKey, preSharedKey)

	// case 4: existing config, but new managementURL has been provided -> update config
	newManagementURL := "https://test.newManagement.url:33071"
	config, err = UpdateOrCreateConfig(ConfigInput{
		ManagementURL: newManagementURL,
		AdminURL:      adminURL,
		ConfigPath:    path,
		PreSharedKey:  &preSharedKey,
	})
	if err != nil {
		return
	}

	assert.Equal(t, config.ManagementURL.String(), newManagementURL)
	assert.Equal(t, config.PreSharedKey, preSharedKey)

	// read once more to make sure that config file has been updated with the new management URL
	readConf, err := util.ReadJson(path, config)
	if err != nil {
		return
	}
	assert.Equal(t, readConf.(*Config).ManagementURL.String(), newManagementURL)
}

func TestExtraIFaceBlackList(t *testing.T) {
	extraIFaceBlackList := []string{"eth1"}
	path := filepath.Join(t.TempDir(), "config.json")
	config, err := UpdateOrCreateConfig(ConfigInput{
		ConfigPath:          path,
		ExtraIFaceBlackList: extraIFaceBlackList,
	})
	if err != nil {
		return
	}

	assert.Contains(t, config.IFaceBlackList, "eth1")
	readConf, err := util.ReadJson(path, config)
	if err != nil {
		return
	}

	assert.Contains(t, readConf.(*Config).IFaceBlackList, "eth1")
}

// TestDNSSelectionPolicySurvivesUpdate pins the opt-in mechanism of the DNS
// response-selection policy (ADR-0023 D8): it is a persisted config field with no
// command line flag of its own, so an operator writes it into config.json and
// every later config update — which rewrites the whole file — has to leave it
// alone. Defaulting to empty keeps the pre-existing resolver behavior.
func TestDNSSelectionPolicySurvivesUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config, err := UpdateOrCreateConfig(ConfigInput{ConfigPath: path})
	require.NoError(t, err)
	require.Empty(t, config.DNSSelectionPolicy, "no selection policy by default")

	config.DNSSelectionPolicy = "prefer_private"
	require.NoError(t, WriteOutConfig(path, config))

	// An unrelated change re-persists the file, which is where a field the input
	// does not carry would be lost.
	updated, err := UpdateConfig(ConfigInput{
		ConfigPath:    path,
		ManagementURL: "https://mgm.example.com:443",
	})
	require.NoError(t, err)
	assert.Equal(t, "prefer_private", updated.DNSSelectionPolicy)

	reread, err := GetConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "prefer_private", reread.DNSSelectionPolicy)
}

func TestHiddenPreSharedKey(t *testing.T) {
	hidden := "**********"
	samplePreSharedKey := "mysecretpresharedkey"
	tests := []struct {
		name         string
		preSharedKey *string
		want         string
	}{
		{"nil", nil, ""},
		{"hidden", &hidden, ""},
		{"filled", &samplePreSharedKey, samplePreSharedKey},
	}

	// generate default cfg
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	_, _ = UpdateOrCreateConfig(ConfigInput{
		ConfigPath: cfgFile,
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := UpdateOrCreateConfig(ConfigInput{
				ConfigPath:   cfgFile,
				PreSharedKey: tt.preSharedKey,
			})
			if err != nil {
				t.Fatalf("failed to get cfg: %s", err)
			}

			if cfg.PreSharedKey != tt.want {
				t.Fatalf("invalid preshared key: '%s', expected: '%s' ", cfg.PreSharedKey, tt.want)
			}
		})
	}
}

func TestUpdateOldManagementURL(t *testing.T) {
	// The two "Update old management URL" cases the upstream
	// NetBird suite carried (api.wiretrustee.com:33073 → 443
	// and api.wiretrustee.com:443 → api.netbird.io) require
	// reaching a real gRPC endpoint at DefaultManagementURL
	// (api.openzro.io:443) to verify the new server is alive
	// before flipping the local config. openZro is self-hosted
	// only — there's no managed `api.openzro.io` service running,
	// and the test timed out for 30s × N cases on every CI run.
	//
	// Kept the no-op cases (URL already current, custom hostname)
	// because they exercise the early-return paths that don't
	// touch the network. The migration cases still work for
	// users coming from NetBird Cloud — the production code is
	// unchanged — they just aren't asserted in unit tests.
	tests := []struct {
		name                  string
		previousManagementURL string
		expectedManagementURL string
		fileShouldNotChange   bool
	}{
		{
			name:                  "No update needed when management URL is up to date",
			previousManagementURL: DefaultManagementURL,
			expectedManagementURL: DefaultManagementURL,
			fileShouldNotChange:   true,
		},
		{
			name:                  "No update needed when not using cloud management",
			previousManagementURL: "https://openzro.example.com:33073",
			expectedManagementURL: "https://openzro.example.com:33073",
			fileShouldNotChange:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			configPath := filepath.Join(tempDir, "config.json")
			config, err := UpdateOrCreateConfig(ConfigInput{
				ManagementURL: tt.previousManagementURL,
				ConfigPath:    configPath,
			})
			require.NoError(t, err, "failed to create testing config")
			previousStats, err := os.Stat(configPath)
			require.NoError(t, err, "failed to create testing config stats")
			resultConfig, err := UpdateOldManagementURL(context.TODO(), config, configPath)
			require.NoError(t, err, "got error when updating old management url")
			require.Equal(t, tt.expectedManagementURL, resultConfig.ManagementURL.String())
			newStats, err := os.Stat(configPath)
			require.NoError(t, err, "failed to create testing config stats")
			switch tt.fileShouldNotChange {
			case true:
				require.Equal(t, previousStats.ModTime(), newStats.ModTime(), "file should not change")
			case false:
				require.NotEqual(t, previousStats.ModTime(), newStats.ModTime(), "file should have changed")
			}
		})
	}
}

// TestGetConfigLoadsClientCertKeyPair guards the daemon's profile load path.
// ClientCertKeyPair is a `json:"-"` field, so every path that hands a Config to
// the PKCE flow has to load the pair from disk — otherwise the back-channel
// token exchange goes out without a client certificate and an mTLS gate rejects
// it.
func TestGetConfigLoadsClientCertKeyPair(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeTestCertPair(t, dir)

	tt := []struct {
		name       string
		certPath   string
		keyPath    string
		expectPair bool
	}{
		{
			name:       "cert and key configured",
			certPath:   certPath,
			keyPath:    keyPath,
			expectPair: true,
		},
		{
			name: "no mTLS configured",
		},
		{
			name:     "cert without key",
			certPath: certPath,
		},
		{
			name:    "key without cert",
			keyPath: keyPath,
		},
		{
			name:     "pair missing on disk",
			certPath: filepath.Join(dir, "absent.crt"),
			keyPath:  filepath.Join(dir, "absent.key"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.json")
			require.NoError(t, WriteOutConfig(configPath, &Config{
				ClientCertPath:    tc.certPath,
				ClientCertKeyPath: tc.keyPath,
			}))

			config, err := GetConfig(configPath)
			require.NoError(t, err)

			if !tc.expectPair {
				assert.Nil(t, config.ClientCertKeyPair)
				return
			}

			require.NotNil(t, config.ClientCertKeyPair)
			assert.NotEmpty(t, config.ClientCertKeyPair.Certificate)
		})
	}
}

// writeTestCertPair writes a throwaway self-signed client certificate and its
// private key into dir and returns the two paths.
func writeTestCertPair(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "openzro-test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	certPath = filepath.Join(dir, "client.crt")
	keyPath = filepath.Join(dir, "client.key")

	require.NoError(t, os.WriteFile(certPath,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyPath,
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))

	return certPath, keyPath
}
