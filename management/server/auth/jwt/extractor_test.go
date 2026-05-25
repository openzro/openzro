package jwt

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// Regression: Dex's staticPasswords connector does not emit
// federated_claims on the access_token the dashboard sends as Bearer.
// Before the fix, ConnectorID arrived empty and resolveMFAEnforcement
// routed staticPasswords logins to the federated branch, silently
// bypassing MFAEnforceLocal (issue #31). The extractor now defaults
// ConnectorID to "local" when the claim is absent so the gate honours
// the operator's local-enforcement toggle.
func TestClaimsExtractor_ConnectorID(t *testing.T) {
	tests := []struct {
		name   string
		claims jwt.MapClaims
		want   string
	}{
		{
			name: "federated_claims absent defaults to local",
			claims: jwt.MapClaims{
				"sub": "user-1",
			},
			want: "local",
		},
		{
			name: "federated_claims present without connector_id defaults to local",
			claims: jwt.MapClaims{
				"sub":              "user-1",
				"federated_claims": map[string]any{"user_id": "x"},
			},
			want: "local",
		},
		{
			name: "federated_claims connector_id empty string defaults to local",
			claims: jwt.MapClaims{
				"sub":              "user-1",
				"federated_claims": map[string]any{"connector_id": ""},
			},
			want: "local",
		},
		{
			name: "federated_claims connector_id local is honoured",
			claims: jwt.MapClaims{
				"sub":              "user-1",
				"federated_claims": map[string]any{"connector_id": "local"},
			},
			want: "local",
		},
		{
			name: "federated_claims connector_id google is honoured",
			claims: jwt.MapClaims{
				"sub":              "user-1",
				"federated_claims": map[string]any{"connector_id": "google"},
			},
			want: "google",
		},
		{
			name: "federated_claims connector_id github is honoured",
			claims: jwt.MapClaims{
				"sub":              "user-1",
				"federated_claims": map[string]any{"connector_id": "github"},
			},
			want: "github",
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
