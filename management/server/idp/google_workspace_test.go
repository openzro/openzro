package idp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGetGoogleCredentials_ParsesServiceAccountKey pins openzro #82:
// the migration off the SA1019-deprecated
// `golang.org/x/oauth2/google.CredentialsFromJSON` onto
// `cloud.google.com/go/auth/credentials.DetectDefault` must still
// accept a well-formed service-account JSON (base64-wrapped, as the
// operator passes it through the IdP config), parse it, and return
// non-nil credentials.
//
// The smoke test against a stubbed Workspace endpoint (per the
// issue's "Add a smoke test that exercises both flows" line) is
// scoped to a follow-up — that needs the Admin Directory API stub
// infra which doesn't exist today. This unit test pins the JSON
// shape that the migration must keep accepting.
func TestGetGoogleCredentials_ParsesServiceAccountKey(t *testing.T) {
	encoded := serviceAccountKeyB64(t)

	creds, err := getGoogleCredentials(context.Background(), encoded)
	require.NoError(t, err, "valid service-account JSON must produce credentials")
	require.NotNil(t, creds, "credentials must not be nil")
}

// TestGetGoogleCredentials_RejectsBadBase64 confirms the base64
// decode error path still surfaces — the migration preserved this
// boundary check verbatim because operators paste the key into the
// IdP config field and a mistyped value is the common failure mode.
func TestGetGoogleCredentials_RejectsBadBase64(t *testing.T) {
	_, err := getGoogleCredentials(context.Background(), "not-base64-{}")
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode service account key",
		"error must identify the base64 boundary")
}

// serviceAccountKeyB64 builds the minimum-viable JSON that
// credentials.DetectDefault accepts as a service account — type,
// project_id, private_key_id, private_key (PEM), client_email, and
// token_uri at a minimum. A 2048-bit RSA key is generated per test
// (~50ms) so the fixture stays self-contained and doesn't ship a
// secret.
func serviceAccountKeyB64(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	payload := map[string]string{
		"type":                        "service_account",
		"project_id":                  "openzro-test",
		"private_key_id":              "test-key-id",
		"private_key":                 string(pemBlock),
		"client_email":                "openzro-test@openzro-test.iam.gserviceaccount.com",
		"client_id":                   "100000000000000000000",
		"token_uri":                   "https://oauth2.googleapis.com/token",
		"auth_uri":                    "https://accounts.google.com/o/oauth2/auth",
		"auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	// Sanity: token_uri must round-trip — DetectDefault rejects an
	// empty token_uri with a confusing "no token_uri found" message.
	require.Contains(t, string(raw), "token_uri")
	return base64.StdEncoding.EncodeToString(raw)
}
