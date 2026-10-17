package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/types"
)

func doSCIM(t *testing.T, srv http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(method, path, bytes.NewReader(raw)))
	return rec
}

// ADR-0024 D1: POST /Users with the RFC 7643 password attribute creates a
// local user; without it the existing SCIM create is unchanged.
func TestCreateUser_PasswordRoutesToLocalCreate(t *testing.T) {
	var gotPassword string
	var localCalls, plainCalls int
	am := &mock_server.MockAccountManager{
		SCIMCreateLocalUserFunc: func(_ context.Context, _, _ string, in account.SCIMUserInput, password string) (*types.User, error) {
			localCalls++
			gotPassword = password
			return &types.User{Id: "local-id", SCIMUserName: in.UserName}, nil
		},
		SCIMCreateUserFunc: func(_ context.Context, _, _ string, in account.SCIMUserInput) (*types.User, error) {
			plainCalls++
			return &types.User{Id: "plain-id", SCIMUserName: in.UserName}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")

	rec := doSCIM(t, srv, http.MethodPost, "/scim/v2/Users",
		map[string]any{"userName": "ana@example.com", "password": "correct-horse-battery", "active": true})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "correct-horse-battery", gotPassword)
	assert.NotContains(t, rec.Body.String(), "correct-horse-battery", "password is writeOnly / returned never")

	rec = doSCIM(t, srv, http.MethodPost, "/scim/v2/Users", map[string]any{"userName": "bea@example.com"})
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, 1, localCalls)
	assert.Equal(t, 1, plainCalls)
}

// Password changes are a separate, isolated operation that is not wired
// yet. A password in PUT or PATCH must be refused, never silently dropped.
func TestUpdateUser_PasswordIsRefused(t *testing.T) {
	called := false
	am := &mock_server.MockAccountManager{
		SCIMReplaceUserFunc: func(_ context.Context, _, _, _ string, _ account.SCIMUserInput) (*types.User, error) {
			called = true
			return &types.User{Id: "u"}, nil
		},
		SCIMPatchUserFunc: func(_ context.Context, _, _, _ string, _ account.SCIMUserPatch) (*types.User, error) {
			called = true
			return &types.User{Id: "u"}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")
	secret := "correct-horse-battery"

	cases := map[string]*httptest.ResponseRecorder{
		"PUT": doSCIM(t, srv, http.MethodPut, "/scim/v2/Users/u",
			map[string]any{"userName": "ana@example.com", "password": secret}),
		"PATCH path": doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", map[string]any{
			"Operations": []map[string]any{{"op": "replace", "path": "password", "value": secret}}}),
		"PATCH bag": doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", map[string]any{
			"Operations": []map[string]any{{"op": "replace", "value": map[string]any{"password": secret}}}}),
	}
	for name, rec := range cases {
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.False(t, strings.Contains(rec.Body.String(), secret), "%s echoed the password", name)
	}
	assert.False(t, called, "a request carrying a password must not reach the update")
}
