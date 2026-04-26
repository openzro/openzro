package scim

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/types"
)

// withAuth installs a synthetic UserAuth on every request so the
// handlers can read accountId/userId out of context as they would
// behind the real auth middleware.
func withAuth(accountID, userID string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := nbcontext.SetUserAuthInContext(r.Context(), nbcontext.UserAuth{
			AccountId: accountID,
			UserId:    userID,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newTestServer(am *mock_server.MockAccountManager, accountID, userID string) http.Handler {
	router := mux.NewRouter()
	sub := router.PathPrefix("/scim/v2").Subrouter()
	AddEndpoints(am, sub)
	return withAuth(accountID, userID, router)
}

func TestServiceProviderConfig_AdvertisesReadOnly(t *testing.T) {
	srv := newTestServer(&mock_server.MockAccountManager{}, "acct", "u1")

	req := httptest.NewRequest(http.MethodGet, "/scim/v2/ServiceProviderConfig", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, ContentType, rec.Header().Get("Content-Type"))

	var got ServiceProviderConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Contains(t, got.Schemas, SchemaServiceProvider)
	// Phase 1A is read-only: every mutation flag must be false so IdPs
	// don't try PATCH/bulk and get a 405.
	assert.False(t, got.Patch.Supported, "patch must be off until Phase 1B")
	assert.False(t, got.Bulk.Supported)
	assert.False(t, got.Filter.Supported)
	require.NotEmpty(t, got.AuthenticationSchemes)
	assert.Equal(t, "oauthbearertoken", got.AuthenticationSchemes[0].Type)
}

func TestResourceTypes_ListsUsers(t *testing.T) {
	srv := newTestServer(&mock_server.MockAccountManager{}, "acct", "u1")

	req := httptest.NewRequest(http.MethodGet, "/scim/v2/ResourceTypes", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var got ListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, 1, got.TotalResults)
}

func TestListUsers_ReturnsAccountUsersInSCIMShape(t *testing.T) {
	am := &mock_server.MockAccountManager{
		GetUsersFromAccountFunc: func(_ context.Context, accountID, userID string) (map[string]*types.UserInfo, error) {
			assert.Equal(t, "acct", accountID, "must scope by caller's account")
			return map[string]*types.UserInfo{
				"u-alice": {
					ID:         "u-alice",
					Email:      "alice@example.com",
					Name:       "Alice",
					AutoGroups: []string{"g-eng"},
				},
				"u-bob": {
					ID:        "u-bob",
					Email:     "bob@example.com",
					Name:      "Bob",
					IsBlocked: true,
				},
				"u-internal": {
					ID:           "u-internal",
					NonDeletable: true, // service user that must NOT leak through SCIM
				},
			}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")

	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var got ListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, 2, got.TotalResults, "non-deletable system user must be filtered")

	// Body shape — ListResponse.Resources is []any after JSON decode,
	// re-encode + decode to typed Users for clean assertions.
	jsonBytes, _ := json.Marshal(got.Resources)
	var users []User
	require.NoError(t, json.Unmarshal(jsonBytes, &users))

	byID := map[string]User{}
	for _, u := range users {
		byID[u.ID] = u
	}
	require.Contains(t, byID, "u-alice")
	require.Contains(t, byID, "u-bob")

	alice := byID["u-alice"]
	assert.Equal(t, "alice@example.com", alice.UserName)
	assert.True(t, alice.Active, "non-blocked user must be Active=true")
	require.NotNil(t, alice.Name)
	assert.Equal(t, "Alice", alice.Name.Formatted)
	require.Len(t, alice.Emails, 1)
	assert.Equal(t, "alice@example.com", alice.Emails[0].Value)
	require.Len(t, alice.Groups, 1)
	assert.Equal(t, "g-eng", alice.Groups[0].Value)

	assert.False(t, byID["u-bob"].Active, "blocked user must surface as Active=false")
}

func TestGetUser_FoundAndNotFound(t *testing.T) {
	am := &mock_server.MockAccountManager{
		GetUsersFromAccountFunc: func(_ context.Context, _, _ string) (map[string]*types.UserInfo, error) {
			return map[string]*types.UserInfo{
				"u-1": {ID: "u-1", Email: "alice@example.com", Name: "Alice"},
			}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")

	// Found
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users/u-1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var got User
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "u-1", got.ID)
	assert.Equal(t, "alice@example.com", got.UserName)

	// Not found — must NOT leak details about other accounts.
	req = httptest.NewRequest(http.MethodGet, "/scim/v2/Users/does-not-exist", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServiceUserWithoutEmail_FallsBackToID(t *testing.T) {
	am := &mock_server.MockAccountManager{
		GetUsersFromAccountFunc: func(_ context.Context, _, _ string) (map[string]*types.UserInfo, error) {
			return map[string]*types.UserInfo{
				"sa-1": {ID: "sa-1", IsServiceUser: true, Email: ""},
			}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")

	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users/sa-1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var got User
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "sa-1", got.UserName,
		"a service user without an email must fall back to ID — empty userName violates SCIM")
	assert.Empty(t, got.Emails)
}
