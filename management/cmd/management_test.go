package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	flowArchive "github.com/openzro/openzro/flow/store/archive"
)

const (
	exampleConfig = `{
	  "Relay": {
		"Addresses": [
		  "rel://192.168.100.1:8085",
		  "rel://192.168.100.1:8086"
		],
		"CredentialsTTL": "12h0m0s",
		"Secret": "8f7e9d6c5b4a3f2e1d0c9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0a9f8"
	  },
	  "HttpConfig": {
		"AuthAudience": "https://stageapp/",
		"AuthIssuer": "https://something.eu.auth0.com/",
		"OIDCConfigEndpoint": "https://something.eu.auth0.com/.well-known/openid-configuration"
	  }
	}`
)

func Test_loadMgmtConfig(t *testing.T) {
	tmpFile, err := createConfig()
	if err != nil {
		t.Fatalf("failed to create config: %s", err)
	}

	cfg, err := loadMgmtConfig(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("failed to load management config: %s", err)
	}
	if cfg.Relay == nil {
		t.Fatalf("config is nil")
	}
	if len(cfg.Relay.Addresses) == 0 {
		t.Fatalf("relay address is empty")
	}
}

func TestFlowArchiveConfigErrorIsFatal(t *testing.T) {
	if flowArchiveConfigErrorIsFatal(nil) {
		t.Fatal("nil archive config error should not be fatal")
	}
	for _, err := range []error{
		flowArchive.ErrUnavailable,
		fmt.Errorf("wrapped: %w", flowArchive.ErrUnavailable),
		flowArchive.ErrMissingCredentials,
		fmt.Errorf("wrapped: %w", flowArchive.ErrMissingCredentials),
	} {
		if flowArchiveConfigErrorIsFatal(err) {
			t.Fatalf("optional archive error should not be fatal: %v", err)
		}
	}

	if !flowArchiveConfigErrorIsFatal(errors.New("bad archive config")) {
		t.Fatal("unexpected archive config error should remain fatal")
	}
}

func createConfig() (string, error) {
	tmpfile, err := os.CreateTemp("", "config.json")
	if err != nil {
		return "", err
	}
	_, err = tmpfile.Write([]byte(exampleConfig))
	if err != nil {
		return "", err
	}

	if err := tmpfile.Close(); err != nil {
		return "", err
	}
	return tmpfile.Name(), nil
}
