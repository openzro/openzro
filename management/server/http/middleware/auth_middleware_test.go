package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	"github.com/openzro/openzro/management/server/auth"
	nbjwt "github.com/openzro/openzro/management/server/auth/jwt"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/util"

	authHandlerPkg "github.com/openzro/openzro/management/server/http/handlers/auth"
	"github.com/openzro/openzro/management/server/http/middleware/bypass"
	"github.com/openzro/openzro/management/server/types"
)

var base64StdEncoder = base64.StdEncoding

const (
	audience       = "audience"
	userIDClaim    = "userIDClaim"
	accountID      = "accountID"
	domain         = "domain"
	domainCategory = "domainCategory"
	userID         = "userID"
	tokenID        = "tokenID"
	PAT            = "nbp_PAT"
	JWT            = "JWT"
	wrongToken     = "wrongToken"
)

var testAccount = &types.Account{
	Id:     accountID,
	Domain: domain,
	Users: map[string]*types.User{
		userID: {
			Id:        userID,
			AccountID: accountID,
			PATs: map[string]*types.PersonalAccessToken{
				tokenID: {
					ID:             tokenID,
					Name:           "My first token",
					HashedToken:    "someHash",
					ExpirationDate: util.ToPtr(time.Now().UTC().AddDate(0, 0, 7)),
					CreatedBy:      userID,
					CreatedAt:      time.Now().UTC(),
					LastUsed:       util.ToPtr(time.Now().UTC()),
				},
			},
		},
	},
}

func mockGetAccountInfoFromPAT(_ context.Context, token string) (user *types.User, pat *types.PersonalAccessToken, domain string, category string, err error) {
	if token == PAT {
		return testAccount.Users[userID], testAccount.Users[userID].PATs[tokenID], testAccount.Domain, testAccount.DomainCategory, nil
	}
	return nil, nil, "", "", fmt.Errorf("PAT invalid")
}

func mockValidateAndParseToken(_ context.Context, token string) (nbcontext.UserAuth, *jwt.Token, error) {
	if token == JWT {
		return nbcontext.UserAuth{
				UserId:         userID,
				AccountId:      accountID,
				Domain:         testAccount.Domain,
				DomainCategory: testAccount.DomainCategory,
			},
			&jwt.Token{
				Claims: jwt.MapClaims{
					userIDClaim:                      userID,
					audience + nbjwt.AccountIDSuffix: accountID,
				},
				Valid: true,
			}, nil
	}
	return nbcontext.UserAuth{}, nil, fmt.Errorf("JWT invalid")
}

func mockMarkPATUsed(_ context.Context, token string) error {
	if token == tokenID {
		return nil
	}
	return fmt.Errorf("Should never get reached")
}

func mockEnsureUserAccessByJWTGroups(_ context.Context, userAuth nbcontext.UserAuth, token *jwt.Token) (nbcontext.UserAuth, error) {
	if userAuth.IsChild || userAuth.IsPAT {
		return userAuth, nil
	}

	if testAccount.Id != userAuth.AccountId {
		return userAuth, fmt.Errorf("account with id %s does not exist", userAuth.AccountId)
	}

	if _, ok := testAccount.Users[userAuth.UserId]; !ok {
		return userAuth, fmt.Errorf("user with id %s does not exist", userAuth.UserId)
	}

	return userAuth, nil
}

func TestAuthMiddleware_Handler(t *testing.T) {
	tt := []struct {
		name               string
		path               string
		authHeader         string
		expectedStatusCode int
		shouldBypassAuth   bool
	}{
		{
			name:               "Valid PAT Token",
			path:               "/test",
			authHeader:         "Token " + PAT,
			expectedStatusCode: 200,
		},
		{
			name:               "Invalid PAT Token",
			path:               "/test",
			authHeader:         "Token " + wrongToken,
			expectedStatusCode: 401,
		},
		{
			name:               "Fallback to PAT Token",
			path:               "/test",
			authHeader:         "Bearer " + PAT,
			expectedStatusCode: 200,
		},
		{
			name:               "Valid JWT Token",
			path:               "/test",
			authHeader:         "Bearer " + JWT,
			expectedStatusCode: 200,
		},
		{
			name:               "Invalid JWT Token",
			path:               "/test",
			authHeader:         "Bearer " + wrongToken,
			expectedStatusCode: 401,
		},
		{
			name:               "Basic Auth",
			path:               "/test",
			authHeader:         "Basic  " + PAT,
			expectedStatusCode: 401,
		},
		{
			name:               "Webhook Path Bypass",
			path:               "/webhook",
			authHeader:         "",
			expectedStatusCode: 200,
			shouldBypassAuth:   true,
		},
		{
			name:               "Webhook Path Bypass with Subpath",
			path:               "/webhook/test",
			authHeader:         "",
			expectedStatusCode: 200,
			shouldBypassAuth:   true,
		},
		{
			name:               "Different Webhook Path",
			path:               "/webhooktest",
			authHeader:         "",
			expectedStatusCode: 401,
			shouldBypassAuth:   false,
		},
	}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

	})

	mockAuth := &auth.MockManager{
		ValidateAndParseTokenFunc:       mockValidateAndParseToken,
		EnsureUserAccessByJWTGroupsFunc: mockEnsureUserAccessByJWTGroups,
		MarkPATUsedFunc:                 mockMarkPATUsed,
		GetPATInfoFunc:                  mockGetAccountInfoFromPAT,
	}

	authMiddleware := NewAuthMiddleware(
		mockAuth,
		nil,
		func(ctx context.Context, userAuth nbcontext.UserAuth) (string, string, error) {
			return userAuth.AccountId, userAuth.UserId, nil
		},
		func(ctx context.Context, userAuth nbcontext.UserAuth) error {
			return nil
		},
		func(ctx context.Context, userAuth nbcontext.UserAuth) (*types.User, error) {
			return &types.User{}, nil
		},
	)

	handlerToTest := authMiddleware.Handler(nextHandler)

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if tc.shouldBypassAuth {
				err := bypass.AddBypassPath(tc.path)
				if err != nil {
					t.Fatalf("failed to add bypass path: %v", err)
				}
			}

			req := httptest.NewRequest("GET", "http://testing"+tc.path, nil)
			req.Header.Set("Authorization", tc.authHeader)
			rec := httptest.NewRecorder()

			handlerToTest.ServeHTTP(rec, req)

			result := rec.Result()
			defer result.Body.Close()

			if result.StatusCode != tc.expectedStatusCode {
				t.Errorf("expected status code %d, got %d", tc.expectedStatusCode, result.StatusCode)
			}
		})
	}
}

func TestAuthMiddleware_Handler_Child(t *testing.T) {
	tt := []struct {
		name             string
		path             string
		authHeader       string
		expectedUserAuth *nbcontext.UserAuth // nil expects 401 response status
	}{
		{
			name:       "Valid PAT Token",
			path:       "/test",
			authHeader: "Token " + PAT,
			expectedUserAuth: &nbcontext.UserAuth{
				AccountId:      accountID,
				UserId:         userID,
				Domain:         testAccount.Domain,
				DomainCategory: testAccount.DomainCategory,
				IsPAT:          true,
			},
		},
		{
			// Regression: ?account= must NOT override the AccountId derived
			// from the token. See CWE-639 fix in auth_middleware.go.
			name:       "PAT Token ignores ?account= override",
			path:       "/test?account=xyz",
			authHeader: "Token " + PAT,
			expectedUserAuth: &nbcontext.UserAuth{
				AccountId:      accountID,
				UserId:         userID,
				Domain:         testAccount.Domain,
				DomainCategory: testAccount.DomainCategory,
				IsPAT:          true,
			},
		},
		{
			name:       "Valid JWT Token",
			path:       "/test",
			authHeader: "Bearer " + JWT,
			expectedUserAuth: &nbcontext.UserAuth{
				AccountId:      accountID,
				UserId:         userID,
				Domain:         testAccount.Domain,
				DomainCategory: testAccount.DomainCategory,
			},
		},

		{
			// Regression: ?account= must NOT override the AccountId derived
			// from the token. See CWE-639 fix in auth_middleware.go.
			name:       "JWT Token ignores ?account= override",
			path:       "/test?account=xyz",
			authHeader: "Bearer " + JWT,
			expectedUserAuth: &nbcontext.UserAuth{
				AccountId:      accountID,
				UserId:         userID,
				Domain:         testAccount.Domain,
				DomainCategory: testAccount.DomainCategory,
			},
		},
	}

	mockAuth := &auth.MockManager{
		ValidateAndParseTokenFunc:       mockValidateAndParseToken,
		EnsureUserAccessByJWTGroupsFunc: mockEnsureUserAccessByJWTGroups,
		MarkPATUsedFunc:                 mockMarkPATUsed,
		GetPATInfoFunc:                  mockGetAccountInfoFromPAT,
	}

	authMiddleware := NewAuthMiddleware(
		mockAuth,
		nil,
		func(ctx context.Context, userAuth nbcontext.UserAuth) (string, string, error) {
			return userAuth.AccountId, userAuth.UserId, nil
		},
		func(ctx context.Context, userAuth nbcontext.UserAuth) error {
			return nil
		},
		func(ctx context.Context, userAuth nbcontext.UserAuth) (*types.User, error) {
			return &types.User{}, nil
		},
	)

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			handlerToTest := authMiddleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				userAuth, err := nbcontext.GetUserAuthFromRequest(r)
				if tc.expectedUserAuth != nil {
					assert.NoError(t, err)
					assert.Equal(t, *tc.expectedUserAuth, userAuth)
				} else {
					assert.Error(t, err)
					assert.Empty(t, userAuth)
				}
			}))

			req := httptest.NewRequest("GET", "http://testing"+tc.path, nil)
			req.Header.Set("Authorization", tc.authHeader)
			rec := httptest.NewRecorder()

			handlerToTest.ServeHTTP(rec, req)

			result := rec.Result()
			defer result.Body.Close()

			if tc.expectedUserAuth != nil {
				assert.Equal(t, 200, result.StatusCode)
			} else {
				assert.Equal(t, 401, result.StatusCode)
			}
		})
	}
}

func TestAuthMiddleware_SessionCookie(t *testing.T) {
	// Build a real SessionService against a random key so the
	// cookie path runs end-to-end (Issue + Verify) — no mocks for
	// the JWT signing piece.
	keyRaw := make([]byte, 32)
	for i := range keyRaw {
		keyRaw[i] = byte(i + 1)
	}
	keyB64 := base64Std(keyRaw)
	sessions, err := authHandlerPkg.NewSessionService(keyB64)
	assert.NoError(t, err)

	mockAuth := &auth.MockManager{
		ValidateAndParseTokenFunc:       mockValidateAndParseToken,
		EnsureUserAccessByJWTGroupsFunc: mockEnsureUserAccessByJWTGroups,
		MarkPATUsedFunc:                 mockMarkPATUsed,
		GetPATInfoFunc:                  mockGetAccountInfoFromPAT,
	}

	var ensureCalls int
	mw := NewAuthMiddleware(
		mockAuth,
		sessions,
		func(_ context.Context, ua nbcontext.UserAuth) (string, string, error) {
			ensureCalls++
			return accountID, ua.UserId, nil
		},
		func(_ context.Context, _ nbcontext.UserAuth) error { return nil },
		func(_ context.Context, _ nbcontext.UserAuth) (*types.User, error) { return &types.User{}, nil },
	)

	mintCookie := func(t *testing.T, ttl time.Duration, sub string) string {
		t.Helper()
		raw, err := sessions.Issue(authHandlerPkg.SessionClaims{
			ProviderID:  7,
			UpstreamIss: "https://idp.example.com",
			UpstreamSub: sub,
		}, ttl)
		assert.NoError(t, err)
		return raw
	}

	t.Run("valid cookie reaches the inner handler", func(t *testing.T) {
		ensureCalls = 0
		raw := mintCookie(t, time.Hour, "user-1")
		var saw nbcontext.UserAuth
		h := mw.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			saw, _ = nbcontext.GetUserAuthFromRequest(r)
		}))
		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: authHandlerPkg.SessionCookieName, Value: raw})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, 200, rec.Result().StatusCode)
		assert.Equal(t, "user-1", saw.UserId)
		assert.Equal(t, accountID, saw.AccountId)
		assert.Equal(t, 1, ensureCalls,
			"ensureAccount must run for cookie-authenticated requests")
	})

	t.Run("missing cookie 401s", func(t *testing.T) {
		h := mw.Handler(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatalf("inner handler must not run when no auth supplied")
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/test", nil))
		assert.Equal(t, 401, rec.Result().StatusCode)
	})

	t.Run("expired cookie 401s", func(t *testing.T) {
		raw := mintCookie(t, -time.Second, "user-1")
		h := mw.Handler(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatalf("inner handler must not run for expired cookie")
		}))
		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: authHandlerPkg.SessionCookieName, Value: raw})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, 401, rec.Result().StatusCode)
	})

	t.Run("cookie not consulted when Bearer header present", func(t *testing.T) {
		// Setting a malformed cookie alongside a valid Bearer must
		// not 401 — the Bearer path wins.
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+JWT)
		req.AddCookie(&http.Cookie{Name: authHandlerPkg.SessionCookieName, Value: "garbage"})
		rec := httptest.NewRecorder()
		mw.Handler(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})).ServeHTTP(rec, req)
		assert.Equal(t, 200, rec.Result().StatusCode)
	})

	t.Run("cookie path off when sessions is nil", func(t *testing.T) {
		mwNoCookies := NewAuthMiddleware(
			mockAuth, nil,
			func(_ context.Context, ua nbcontext.UserAuth) (string, string, error) {
				return ua.AccountId, ua.UserId, nil
			},
			func(_ context.Context, _ nbcontext.UserAuth) error { return nil },
			func(_ context.Context, _ nbcontext.UserAuth) (*types.User, error) { return &types.User{}, nil },
		)
		raw := mintCookie(t, time.Hour, "user-1")
		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: authHandlerPkg.SessionCookieName, Value: raw})
		rec := httptest.NewRecorder()
		mwNoCookies.Handler(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatalf("nil sessions must not unlock the cookie path")
		})).ServeHTTP(rec, req)
		assert.Equal(t, 401, rec.Result().StatusCode)
	})
}

// base64Std mirrors what flow_exports.NewFieldEncrypt expects —
// no padding stripping, RFC 4648 std alphabet.
func base64Std(b []byte) string {
	return base64StdEncoder.EncodeToString(b)
}
