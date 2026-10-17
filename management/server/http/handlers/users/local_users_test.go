package users

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server"
	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

const localSecret = "correct-horse-battery"

func localUsersRouter(am *mock_server.MockAccountManager) http.Handler {
	router := mux.NewRouter()
	AddEndpoints(am, router)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = nbcontext.SetUserAuthInRequest(r, nbcontext.UserAuth{UserId: "admin", AccountId: "acct"})
		router.ServeHTTP(w, r)
	})
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	return rec
}

func localUser(id string, state types.LocalCredentialState) *types.User {
	return &types.User{Id: id, AccountID: "acct", SCIMUserName: id + "@example.com", Role: types.UserRoleUser,
		LocalCredentialState: state, Blocked: state == types.LocalCredentialDeactivated}
}

// ADR-0026 D1: the console create maps onto the ADR-0024 create saga and
// never echoes the password.
func TestCreateLocalUser(t *testing.T) {
	var gotInput account.SCIMUserInput
	var gotPassword string
	am := &mock_server.MockAccountManager{
		SCIMCreateLocalUserFunc: func(_ context.Context, accountID, callerID string, in account.SCIMUserInput, password string) (*types.User, error) {
			assert.Equal(t, "acct", accountID)
			assert.Equal(t, "admin", callerID)
			gotInput, gotPassword = in, password
			return localUser("ana", types.LocalCredentialActive), nil
		},
	}
	rec := doJSON(t, localUsersRouter(am), http.MethodPost, "/users/local", map[string]any{
		"email": "ana@example.com", "name": "Ana", "password": localSecret, "auto_groups": []string{"g1"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, account.SCIMUserInput{UserName: "ana@example.com", DisplayName: "Ana", Active: true, AutoGroups: []string{"g1"}}, gotInput)
	assert.Equal(t, localSecret, gotPassword)
	assert.NotContains(t, rec.Body.String(), localSecret)

	var got api.User
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.LocalCredentialState)
	assert.Equal(t, api.UserLocalCredentialStateActive, *got.LocalCredentialState)
}

func TestCreateLocalUser_ErrorsMapToStatus(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"policy":   {status.Errorf(status.BadRequest, "password policy"), http.StatusBadRequest},
		"conflict": {status.Errorf(status.AlreadyExists, "exists"), http.StatusConflict},
		"disabled": {status.Errorf(status.BadRequest, "creating local users is not enabled"), http.StatusBadRequest},
		// The sentinel the manager really returns for a used email, not a
		// stand-in, so the mapping is tested end to end.
		"used email": {server.ErrSCIMUserAlreadyExists, http.StatusConflict},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			am := &mock_server.MockAccountManager{
				SCIMCreateLocalUserFunc: func(context.Context, string, string, account.SCIMUserInput, string) (*types.User, error) {
					return nil, tc.err
				},
			}
			rec := doJSON(t, localUsersRouter(am), http.MethodPost, "/users/local",
				map[string]any{"email": "ana@example.com", "password": localSecret, "auto_groups": []string{}})
			assert.Equal(t, tc.want, rec.Code)
			assert.NotContains(t, rec.Body.String(), localSecret)
		})
	}

	// A malformed body never reaches the manager.
	called := false
	am := &mock_server.MockAccountManager{
		SCIMCreateLocalUserFunc: func(context.Context, string, string, account.SCIMUserInput, string) (*types.User, error) {
			called = true
			return nil, errors.New("unreachable")
		},
	}
	rec := doJSON(t, localUsersRouter(am), http.MethodPost, "/users/local", "not-an-object")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.False(t, called)
}

// ADR-0026 D1: reset and deactivate call the reviewed sagas.
func TestResetAndDeactivateLocalUser(t *testing.T) {
	var resetUser, resetPassword, deactivated string
	am := &mock_server.MockAccountManager{
		SCIMResetLocalPasswordFunc: func(_ context.Context, _, _, userID, password string, replace *account.SCIMUserInput) (*types.User, error) {
			assert.Nil(t, replace, "the console reset carries no representation")
			resetUser, resetPassword = userID, password
			return localUser(userID, types.LocalCredentialActive), nil
		},
		DeactivateLocalUserFunc: func(_ context.Context, _, _, userID string) (*types.User, error) {
			deactivated = userID
			return localUser(userID, types.LocalCredentialDeactivated), nil
		},
	}
	h := localUsersRouter(am)

	rec := doJSON(t, h, http.MethodPost, "/users/u1/password", map[string]any{"password": localSecret})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "u1", resetUser)
	assert.Equal(t, localSecret, resetPassword)
	assert.NotContains(t, rec.Body.String(), localSecret)

	rec = doJSON(t, h, http.MethodPost, "/users/u2/deactivate", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "u2", deactivated)
	var got api.User
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.LocalCredentialState)
	assert.Equal(t, api.UserLocalCredentialStateDeactivated, *got.LocalCredentialState)
	assert.True(t, got.IsBlocked)

	rec = doJSON(t, h, http.MethodPost, "/users/u1/password", map[string]any{"password": nil})
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a null password is malformed")
}

func TestLocalUserConfig(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		am := &mock_server.MockAccountManager{
			LocalUserCreationEnabledFunc: func() bool { return enabled },
		}
		rec := doJSON(t, localUsersRouter(am), http.MethodGet, "/users/local/config", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var got api.LocalUserConfig
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, enabled, got.CreationEnabled)
	}
}

// ADR-0026 D2: pending reservations and tombstones are not users for an
// operator, and local users carry their state.
func TestGetUsers_LocalUsers(t *testing.T) {
	am := &mock_server.MockAccountManager{
		GetUsersFromAccountFunc: func(context.Context, string, string) (map[string]*types.UserInfo, error) {
			return map[string]*types.UserInfo{
				"plain":   {ID: "plain", Role: "user"},
				"active":  {ID: "active", Role: "user", LocalCredentialState: string(types.LocalCredentialActive)},
				"deact":   {ID: "deact", Role: "user", IsBlocked: true, LocalCredentialState: string(types.LocalCredentialDeactivated)},
				"pending": {ID: "pending", Role: "user", IsBlocked: true, LocalCredentialState: string(types.LocalCredentialPending)},
				"tomb":    {ID: "tomb", Role: "user", IsBlocked: true, LocalCredentialState: string(types.LocalCredentialTombstoned)},
			}, nil
		},
	}
	rec := doJSON(t, localUsersRouter(am), http.MethodGet, "/users", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var got []api.User
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	states := map[string]*api.UserLocalCredentialState{}
	for _, u := range got {
		states[u.Id] = u.LocalCredentialState
	}
	assert.Len(t, states, 3, "pending and tombstoned users are omitted")
	assert.Nil(t, states["plain"])
	require.NotNil(t, states["active"])
	assert.Equal(t, api.UserLocalCredentialStateActive, *states["active"])
	require.NotNil(t, states["deact"])
	assert.Equal(t, api.UserLocalCredentialStateDeactivated, *states["deact"])
}
