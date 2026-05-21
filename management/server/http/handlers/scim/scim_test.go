package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/types"
)

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

func TestServiceProviderConfig_AdvertisesPatchAndFilter(t *testing.T) {
	srv := newTestServer(&mock_server.MockAccountManager{}, "acct", "u1")

	req := httptest.NewRequest(http.MethodGet, "/scim/v2/ServiceProviderConfig", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, ContentType, rec.Header().Get("Content-Type"))

	var got ServiceProviderConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Contains(t, got.Schemas, SchemaServiceProvider)
	assert.True(t, got.Patch.Supported, "PATCH supported as of G7-B")
	assert.True(t, got.Filter.Supported, "userName eq filter supported")
	assert.False(t, got.Bulk.Supported, "bulk still off")
	require.NotEmpty(t, got.AuthenticationSchemes)
	assert.Equal(t, "oauthbearertoken", got.AuthenticationSchemes[0].Type)
}

func TestResourceTypes_ListsUsersAndGroups(t *testing.T) {
	srv := newTestServer(&mock_server.MockAccountManager{}, "acct", "u1")

	req := httptest.NewRequest(http.MethodGet, "/scim/v2/ResourceTypes", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var got ListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, 2, got.TotalResults, "Users and Groups exposed as resource types")
}

func TestListUsers_ScopedAndShaped(t *testing.T) {
	am := &mock_server.MockAccountManager{
		SCIMListUsersFunc: func(_ context.Context, accountID, callerID, filter string, startIndex, count int) ([]*types.User, int, error) {
			assert.Equal(t, "acct", accountID, "scoped by caller's account")
			assert.Equal(t, 1, startIndex)
			assert.Equal(t, 100, count)
			users := []*types.User{
				{
					Id:              "u-alice",
					AccountID:       accountID,
					Issued:          types.UserIssuedIntegration,
					SCIMUserName:    "alice@example.com",
					SCIMDisplayName: "Alice",
					AutoGroups:      []string{"g-eng"},
				},
				{
					Id:              "u-bob",
					AccountID:       accountID,
					Issued:          types.UserIssuedIntegration,
					SCIMUserName:    "bob@example.com",
					SCIMDisplayName: "Bob",
					Blocked:         true,
				},
			}
			return users, len(users), nil
		},
	}
	srv := newTestServer(am, "acct", "caller")

	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var got ListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, 2, got.TotalResults)

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
	assert.True(t, alice.Active)
	require.NotNil(t, alice.Name)
	assert.Equal(t, "Alice", alice.Name.Formatted)
	require.Len(t, alice.Emails, 1)
	require.Len(t, alice.Groups, 1)
	assert.Equal(t, "g-eng", alice.Groups[0].Value)

	assert.False(t, byID["u-bob"].Active)
}

func TestListUsers_FilterParsedAndPropagated(t *testing.T) {
	var capturedFilter string
	am := &mock_server.MockAccountManager{
		SCIMListUsersFunc: func(_ context.Context, _, _, filter string, _, _ int) ([]*types.User, int, error) {
			capturedFilter = filter
			return nil, 0, nil
		},
	}
	srv := newTestServer(am, "acct", "u1")

	cases := []struct {
		query, want string
	}{
		{`?filter=userName+eq+%22alice%40example.com%22`, "alice@example.com"},
		{`?filter=username+eq+%22Bob%22`, "Bob"}, // case-insensitive attribute
		{`?filter=garbage`, ""},                  // unparseable → empty
	}
	for _, c := range cases {
		capturedFilter = "<unset>"
		req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users"+c.query, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		assert.Equal(t, c.want, capturedFilter, "query=%s", c.query)
	}
}

func TestListUsers_PagingPropagated(t *testing.T) {
	var gotStart, gotCount int
	am := &mock_server.MockAccountManager{
		SCIMListUsersFunc: func(_ context.Context, _, _, _ string, startIndex, count int) ([]*types.User, int, error) {
			gotStart = startIndex
			gotCount = count
			return nil, 0, nil
		},
	}
	srv := newTestServer(am, "acct", "u1")

	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users?startIndex=11&count=25", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, 11, gotStart)
	assert.Equal(t, 25, gotCount)
}

func TestGetUser_FoundAndNotFound(t *testing.T) {
	am := &mock_server.MockAccountManager{
		SCIMGetUserFunc: func(_ context.Context, _, _, id string) (*types.User, error) {
			if id == "u-1" {
				return &types.User{
					Id:              "u-1",
					AccountID:       "acct",
					SCIMUserName:    "alice@example.com",
					SCIMDisplayName: "Alice",
				}, nil
			}
			return nil, errSCIMUserNotFound
		},
	}
	srv := newTestServer(am, "acct", "caller")

	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users/u-1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/scim/v2/Users/does-not-exist", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateUser_201WithLocationHeader(t *testing.T) {
	am := &mock_server.MockAccountManager{
		SCIMCreateUserFunc: func(_ context.Context, _, _ string, in account.SCIMUserInput) (*types.User, error) {
			assert.Equal(t, "alice@example.com", in.UserName)
			assert.Equal(t, "Alice", in.DisplayName)
			assert.True(t, in.Active)
			return &types.User{
				Id:              "new-id",
				AccountID:       "acct",
				SCIMUserName:    in.UserName,
				SCIMDisplayName: in.DisplayName,
			}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")

	body, _ := json.Marshal(map[string]any{
		"schemas":     []string{SchemaUserURN},
		"userName":    "alice@example.com",
		"displayName": "Alice",
		"active":      true,
	})
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "/scim/v2/Users/new-id", rec.Header().Get("Location"))
}

func TestCreateUser_409OnConflict(t *testing.T) {
	am := &mock_server.MockAccountManager{
		SCIMCreateUserFunc: func(_ context.Context, _, _ string, _ account.SCIMUserInput) (*types.User, error) {
			return nil, errAlreadyExists
		},
	}
	srv := newTestServer(am, "acct", "caller")

	body, _ := json.Marshal(map[string]any{"userName": "x@x"})
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code,
		"duplicate userName must be 409 — IdPs back off on 409 but treat 500 as outage")
}

func TestPatchUser_ReplaceActiveFlag(t *testing.T) {
	var got account.SCIMUserPatch
	am := &mock_server.MockAccountManager{
		SCIMPatchUserFunc: func(_ context.Context, _, _, _ string, p account.SCIMUserPatch) (*types.User, error) {
			got = p
			return &types.User{Id: "u-1", AccountID: "acct"}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")

	body, _ := json.Marshal(map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{
			{"op": "replace", "path": "active", "value": false},
		},
	})
	req := httptest.NewRequest(http.MethodPatch, "/scim/v2/Users/u-1", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, got.Active)
	assert.False(t, *got.Active, "replace active=false must produce Patch.Active=&false")
}

func TestPatchUser_NoPathBag(t *testing.T) {
	var got account.SCIMUserPatch
	am := &mock_server.MockAccountManager{
		SCIMPatchUserFunc: func(_ context.Context, _, _, _ string, p account.SCIMUserPatch) (*types.User, error) {
			got = p
			return &types.User{Id: "u-1", AccountID: "acct"}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")

	body, _ := json.Marshal(map[string]any{
		"Operations": []map[string]any{
			{"op": "Replace", "value": map[string]any{
				"active":      false,
				"displayName": "Alice Renamed",
			}},
		},
	})
	req := httptest.NewRequest(http.MethodPatch, "/scim/v2/Users/u-1", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, got.Active)
	assert.False(t, *got.Active)
	require.NotNil(t, got.DisplayName)
	assert.Equal(t, "Alice Renamed", *got.DisplayName)
}

func TestDeleteUser_204(t *testing.T) {
	am := &mock_server.MockAccountManager{
		SCIMDeactivateUserFunc: func(_ context.Context, _, _, _ string) error { return nil },
	}
	srv := newTestServer(am, "acct", "caller")

	req := httptest.NewRequest(http.MethodDelete, "/scim/v2/Users/u-1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// Sentinel errors used by the test mocks. These mimic the strings
// the manager layer returns; mapErrorStatus pattern-matches them to
// HTTP codes.
var (
	errAlreadyExists    = scimErr("scim: user with this userName already exists")
	errSCIMUserNotFound = scimErr("scim: user not found")
)

type scimErr string

func (e scimErr) Error() string { return string(e) }
