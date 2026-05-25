package jwt

import (
	"encoding/base64"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/protobuf/encoding/protowire"
)

// dexSub builds a Dex-shaped sub claim (base64url-encoded protobuf
// of {field1=userID, field2=connID}) the same way
// `internal/server/handlers.go:formatSubject` does on the Dex side.
// Used to feed the extractor realistic fixtures for the protobuf
// fallback path.
func dexSub(t *testing.T, userID, connID string) string {
	t.Helper()
	var buf []byte
	buf = protowire.AppendTag(buf, 1, protowire.BytesType)
	buf = protowire.AppendString(buf, userID)
	buf = protowire.AppendTag(buf, 2, protowire.BytesType)
	buf = protowire.AppendString(buf, connID)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// Regression: the MFA gate (issue #31) routes on userAuth.ConnectorID.
// Dex's bundled staticPasswords connector does not emit
// federated_claims on the access token the dashboard sends as Bearer,
// so the extractor falls back to parsing the connector id out of
// Dex's `sub` protobuf — covering staticPasswords + every federated
// connector without depending on federated_claims.
func TestClaimsExtractor_ConnectorID(t *testing.T) {
	tests := []struct {
		name   string
		claims jwt.MapClaims
		want   string
	}{
		{
			name: "federated_claims connector_id wins over sub fallback",
			claims: jwt.MapClaims{
				"sub":              dexSub(t, "user-1", "google"),
				"federated_claims": map[string]any{"connector_id": "github"},
			},
			want: "github",
		},
		{
			name: "Dex staticPasswords sub yields local",
			claims: jwt.MapClaims{
				"sub": dexSub(t, "openzro-bootstrap-admin", "local"),
			},
			want: "local",
		},
		{
			name: "Dex google sub yields google",
			claims: jwt.MapClaims{
				"sub": dexSub(t, "ChVhbm5hQGV4YW1wbGUuY29tEgZnb29nbGU", "google"),
			},
			want: "google",
		},
		{
			name: "non-Dex UUID sub stays empty",
			claims: jwt.MapClaims{
				"sub": "5f6c1c0a-3d4e-4d5b-9c8a-2f1a3b4c5d6e",
			},
			want: "",
		},
		{
			name: "federated_claims present without connector_id falls back",
			claims: jwt.MapClaims{
				"sub":              dexSub(t, "user-1", "local"),
				"federated_claims": map[string]any{"user_id": "x"},
			},
			want: "local",
		},
		{
			name: "federated_claims with empty connector_id keeps it empty",
			claims: jwt.MapClaims{
				"sub":              "non-dex-uuid",
				"federated_claims": map[string]any{"connector_id": ""},
			},
			want: "",
		},
	}

	extractor := NewClaimsExtractor()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token := &jwt.Token{Claims: tc.claims}
			userAuth, err := extractor.ToUserAuth(token)
			if err != nil {
				t.Fatalf("ToUserAuth: %v", err)
			}
			if userAuth.ConnectorID != tc.want {
				t.Fatalf("ConnectorID: got %q want %q", userAuth.ConnectorID, tc.want)
			}
		})
	}
}

func TestParseDexSubConnector(t *testing.T) {
	tests := []struct {
		name string
		sub  string
		want string
	}{
		{"empty", "", ""},
		{"non-base64", "not-base64-!!!", ""},
		{"non-protobuf UUID", base64.RawURLEncoding.EncodeToString([]byte("5f6c1c0a-3d4e-4d5b")), ""},
		{"Dex local", dexSub(t, "u1", "local"), "local"},
		{"Dex google", dexSub(t, "u2", "google"), "google"},
		{"Dex empty connector", dexSub(t, "u3", ""), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDexSubConnector(tc.sub)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
