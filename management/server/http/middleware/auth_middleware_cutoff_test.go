package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	"github.com/openzro/openzro/management/server/auth"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/types"
)

// ADR-0027 D3: after a local password reset, a JWT issued at or before the
// user's credentials cutoff is refused with 401 so the dashboard's OIDC
// client ends the session. Token validation itself rejects it (the mock
// below stands in for auth.Manager's check), so the middleware acts on
// nothing first: no account bootstrap, no JWT group sync. Tokens issued
// later, users without a cutoff and PATs are unaffected.
func TestAuthMiddleware_CredentialsCutoff(t *testing.T) {
	cutoff := time.Date(2026, 10, 18, 12, 0, 5, 0, time.UTC)
	tests := []struct {
		name       string
		authHeader string
		iat        time.Time
		cutoff     *time.Time
		want       int
	}{
		{name: "JWT issued before the cutoff", authHeader: "Bearer " + JWT, iat: cutoff.Add(-time.Minute),
			cutoff: &cutoff, want: http.StatusUnauthorized},
		{name: "JWT issued in the cutoff second", authHeader: "Bearer " + JWT, iat: cutoff,
			cutoff: &cutoff, want: http.StatusUnauthorized},
		{name: "JWT without iat for a user with a cutoff", authHeader: "Bearer " + JWT,
			cutoff: &cutoff, want: http.StatusUnauthorized},
		{name: "JWT issued after the cutoff", authHeader: "Bearer " + JWT, iat: cutoff.Add(time.Second),
			cutoff: &cutoff, want: http.StatusOK},
		{name: "user without a cutoff", authHeader: "Bearer " + JWT, iat: cutoff.Add(-time.Hour),
			want: http.StatusOK},
		{name: "user without a cutoff, JWT without iat", authHeader: "Bearer " + JWT, want: http.StatusOK},
		{name: "PAT of a user with a cutoff", authHeader: "Token " + PAT, cutoff: &cutoff, want: http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockAuth := &auth.MockManager{
				ValidateAndParseTokenFunc: func(ctx context.Context, token string) (nbcontext.UserAuth, *jwt.Token, error) {
					userAuth, parsed, err := mockValidateAndParseToken(ctx, token)
					userAuth.IssuedAt = tc.iat
					if (&types.User{CredentialsValidAfter: tc.cutoff}).JWTRevoked(tc.iat) {
						return nbcontext.UserAuth{}, nil, auth.ErrJWTRevoked
					}
					return userAuth, parsed, err
				},
				EnsureUserAccessByJWTGroupsFunc: mockEnsureUserAccessByJWTGroups,
				MarkPATUsedFunc:                 mockMarkPATUsed,
				GetPATInfoFunc: func(ctx context.Context,
					token string) (*types.User, *types.PersonalAccessToken, string, string, error) {
					user, pat, d, c, err := mockGetAccountInfoFromPAT(ctx, token)
					if user != nil {
						user = user.Copy()
						user.CredentialsValidAfter = tc.cutoff
					}
					return user, pat, d, c, err
				},
			}
			reached, bootstrapped, synced := false, false, false
			handler := NewAuthMiddleware(
				mockAuth,
				func(_ context.Context, userAuth nbcontext.UserAuth) (string, string, error) {
					bootstrapped = true
					return userAuth.AccountId, userAuth.UserId, nil
				},
				func(context.Context, nbcontext.UserAuth) error { synced = true; return nil },
				func(context.Context, nbcontext.UserAuth) (*types.User, error) {
					return &types.User{Id: userID, AccountID: accountID, CredentialsValidAfter: tc.cutoff}, nil
				},
				nil, nil,
			).Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

			req := httptest.NewRequest(http.MethodGet, "http://testing/test", nil)
			req.Header.Set("Authorization", tc.authHeader)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tc.want, rec.Code)
			assert.Equal(t, tc.want == http.StatusOK, reached, "the handler runs only for an accepted token")
			if tc.want == http.StatusUnauthorized {
				// The dashboard signs the user in again on "token expired"
				// and only shows an error on "token invalid".
				assert.Contains(t, rec.Body.String(), `"token expired"`,
					"a revoked JWT reads as an ended session, so the dashboard re-authenticates")
				assert.False(t, bootstrapped, "a revoked JWT never bootstraps an account")
				assert.False(t, synced, "a revoked JWT never syncs JWT groups")
			}
			assert.NotContains(t, rec.Body.String(), JWT, "the token is never echoed back")
		})
	}
}
