package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/status"
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

// ADR-0024 D6: a password reset is an isolated operation. PATCH qualifies
// only when its single operation replaces the password; PUT carries the
// rest of the representation, which the manager checks is unchanged.
func TestUpdateUser_PasswordReset(t *testing.T) {
	var resets []string
	var replaceInputs []*account.SCIMUserInput
	updateCalled := false
	am := &mock_server.MockAccountManager{
		SCIMResetLocalPasswordFunc: func(_ context.Context, _, _, userID, password string, replace *account.SCIMUserInput) (*types.User, error) {
			resets = append(resets, password)
			replaceInputs = append(replaceInputs, replace)
			return &types.User{Id: userID}, nil
		},
		SCIMReplaceUserFunc: func(_ context.Context, _, _, _ string, _ account.SCIMUserInput) (*types.User, error) {
			updateCalled = true
			return &types.User{Id: "u"}, nil
		},
		SCIMPatchUserFunc: func(_ context.Context, _, _, _ string, _ account.SCIMUserPatch) (*types.User, error) {
			updateCalled = true
			return &types.User{Id: "u"}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")
	secret := "correct-horse-battery"

	ok := map[string]*httptest.ResponseRecorder{
		"PATCH path": doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", map[string]any{
			"Operations": []map[string]any{{"op": "replace", "path": "password", "value": secret}}}),
		"PATCH bag": doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", map[string]any{
			"Operations": []map[string]any{{"op": "replace", "value": map[string]any{"password": secret}}}}),
		"PUT": doSCIM(t, srv, http.MethodPut, "/scim/v2/Users/u",
			map[string]any{"userName": "ana@example.com", "displayName": "Ana", "password": secret}),
	}
	for name, rec := range ok {
		assert.Equal(t, http.StatusOK, rec.Code, name)
		assert.NotContains(t, rec.Body.String(), secret, "%s echoed the password", name)
	}
	require.Equal(t, []string{secret, secret, secret}, resets)
	assert.Nil(t, replaceInputs[0], "PATCH carries no representation to compare")
	require.NotNil(t, replaceInputs[2], "PUT passes its representation for the unchanged check")
	assert.Equal(t, "Ana", replaceInputs[2].DisplayName)

	refused := map[string]*httptest.ResponseRecorder{
		"PATCH password + other op": doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", map[string]any{
			"Operations": []map[string]any{
				{"op": "replace", "path": "password", "value": secret},
				{"op": "replace", "path": "displayName", "value": "Other"},
			}}),
		"PATCH bag password + other": doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", map[string]any{
			"Operations": []map[string]any{{"op": "replace", "value": map[string]any{"password": secret, "active": false}}}}),
		"PATCH add password": doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", map[string]any{
			"Operations": []map[string]any{{"op": "add", "path": "password", "value": secret}}}),
		"PATCH non-string": doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", map[string]any{
			"Operations": []map[string]any{{"op": "replace", "path": "password", "value": 123}}}),
	}
	for name, rec := range refused {
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.NotContains(t, rec.Body.String(), secret, "%s echoed the password", name)
	}
	assert.Len(t, resets, 3, "a refused request must not reset the password")
	assert.False(t, updateCalled, "no request carrying a password may reach the generic update")
}

// #216 review: a request that mentions the credential but cannot be
// executed must not fall through to the legacy behavior. *string could not
// tell an omitted password from "password": null, and the PATCH op filter
// dropped "remove" before the password check.
func TestPasswordPresenceIsNeverSilentlyIgnored(t *testing.T) {
	calls := 0
	am := &mock_server.MockAccountManager{
		SCIMCreateUserFunc: func(_ context.Context, _, _ string, _ account.SCIMUserInput) (*types.User, error) {
			calls++
			return &types.User{Id: "plain"}, nil
		},
		SCIMCreateLocalUserFunc: func(_ context.Context, _, _ string, _ account.SCIMUserInput, _ string) (*types.User, error) {
			calls++
			return &types.User{Id: "local"}, nil
		},
		SCIMReplaceUserFunc: func(_ context.Context, _, _, _ string, _ account.SCIMUserInput) (*types.User, error) {
			calls++
			return &types.User{Id: "u"}, nil
		},
		SCIMPatchUserFunc: func(_ context.Context, _, _, _ string, _ account.SCIMUserPatch) (*types.User, error) {
			calls++
			return &types.User{Id: "u"}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")

	cases := map[string]*httptest.ResponseRecorder{
		"POST null": doSCIM(t, srv, http.MethodPost, "/scim/v2/Users",
			map[string]any{"userName": "ana@example.com", "password": nil}),
		"POST number": doSCIM(t, srv, http.MethodPost, "/scim/v2/Users",
			map[string]any{"userName": "ana@example.com", "password": 12345}),
		"PUT null": doSCIM(t, srv, http.MethodPut, "/scim/v2/Users/u",
			map[string]any{"userName": "ana@example.com", "password": nil}),
		"PATCH remove": doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", map[string]any{
			"Operations": []map[string]any{{"op": "remove", "path": "password"}}}),
	}
	for name, rec := range cases {
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
	}
	assert.Zero(t, calls, "no request mentioning a password may reach the legacy paths")
}

// #217 review: a recognized attribute with a malformed value must reject
// the whole PATCH. Dropping it silently let "active:false + userName:123"
// reach the manager as a plain deactivation, which is irreversible for a
// local user, and return 200.
func TestPatchUser_MalformedRecognizedValueIsRejected(t *testing.T) {
	var got []account.SCIMUserPatch
	am := &mock_server.MockAccountManager{
		SCIMPatchUserFunc: func(_ context.Context, _, _, _ string, p account.SCIMUserPatch) (*types.User, error) {
			got = append(got, p)
			return &types.User{Id: "u"}, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")
	ops := func(o ...map[string]any) map[string]any { return map[string]any{"Operations": o} }

	rejected := map[string]map[string]any{
		"userName number": ops(
			map[string]any{"op": "replace", "path": "active", "value": false},
			map[string]any{"op": "replace", "path": "userName", "value": 123}),
		"bag userName number": ops(map[string]any{"op": "replace", "value": map[string]any{"active": false, "userName": 123}}),
		"active number":       ops(map[string]any{"op": "replace", "path": "active", "value": 5}),
		"active bad string":   ops(map[string]any{"op": "replace", "path": "active", "value": "maybe"}),
		"displayName object":  ops(map[string]any{"op": "replace", "path": "displayName", "value": map[string]any{}}),
	}
	for name, body := range rejected {
		rec := doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
	}
	assert.Empty(t, got, "a malformed PATCH must not reach the manager")

	// Microsoft Entra ID sends active as a string; that keeps working.
	rec := doSCIM(t, srv, http.MethodPatch, "/scim/v2/Users/u",
		ops(map[string]any{"op": "replace", "path": "active", "value": "False"}))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Active)
	assert.False(t, *got[0].Active)
}

// #217 review: group member operations must report failures. Adding a
// hidden or unknown user returned 200 with the group unchanged, so an IGA
// believed it succeeded.
func TestPatchGroup_MemberErrorsArePropagated(t *testing.T) {
	notFound := status.NewUserNotFoundError("hidden")
	am := &mock_server.MockAccountManager{
		SCIMAddGroupMemberFunc: func(_ context.Context, _, _, _, userID string) error {
			if userID == "hidden" {
				return notFound
			}
			return nil
		},
		SCIMRemoveGroupMemberFunc: func(_ context.Context, _, _, _, _ string) error { return notFound },
		SCIMGetGroupFunc: func(_ context.Context, _, _, groupID string) (*types.Group, []string, error) {
			return &types.Group{ID: groupID, Name: "g"}, nil, nil
		},
	}
	srv := newTestServer(am, "acct", "caller")
	patch := func(op map[string]any) *httptest.ResponseRecorder {
		return doSCIM(t, srv, http.MethodPatch, "/scim/v2/Groups/g", map[string]any{"Operations": []map[string]any{op}})
	}

	rec := patch(map[string]any{"op": "add", "path": "members", "value": []map[string]any{{"value": "hidden"}}})
	assert.Equal(t, http.StatusNotFound, rec.Code, "adding a hidden user must not report success")

	rec = patch(map[string]any{"op": "add", "path": "members", "value": "not-a-list"})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = patch(map[string]any{"op": "replace", "path": "displayName", "value": 42})
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Removing a member that no longer exists is idempotent, so an IGA
	// cleaning up after a delete does not retry forever.
	rec = patch(map[string]any{"op": "remove", "path": `members[value eq "gone"]`})
	assert.Equal(t, http.StatusOK, rec.Code)
}
