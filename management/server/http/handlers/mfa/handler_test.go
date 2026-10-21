package mfa

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/types"
)

const (
	testAccountID = "account-1"
	testAdminID   = "admin-1"
	testMemberID  = "member-1"
)

type disenrollCall struct{ accountID, initiator, target string }

func newDisenrollTestManager(adminEnrolled, sessionValid bool, calls *[]disenrollCall) *mock_server.MockAccountManager {
	users := map[string]*types.User{
		testAdminID:  {Id: testAdminID, AccountID: testAccountID, Role: types.UserRoleAdmin},
		testMemberID: {Id: testMemberID, AccountID: testAccountID, Role: types.UserRoleUser},
	}
	return &mock_server.MockAccountManager{
		GetUserByIDFunc: func(_ context.Context, id string) (*types.User, error) { return users[id], nil },
		MFAStatusFunc: func(_ context.Context, userID string) (*account.MFAStatus, error) {
			return &account.MFAStatus{Enrolled: userID == testAdminID && adminEnrolled}, nil
		},
		MFASessionValidFunc:        func(_, _, _ string) bool { return sessionValid },
		MFAIssueChallengeTokenFunc: func(_, _ string) (string, error) { return "challenge-token", nil },
		MFADisenrollFunc: func(_ context.Context, accountID, initiator, target string) error {
			*calls = append(*calls, disenrollCall{accountID, initiator, target})
			return nil
		},
	}
}

func deleteMFA(t *testing.T, am account.Manager, callerID, targetID string) (int, map[string]any) {
	t.Helper()
	h := &handler{am: am}
	router := mux.NewRouter()
	router.HandleFunc("/users/{userId}/mfa", h.disenroll).Methods(http.MethodDelete)
	req := httptest.NewRequest(http.MethodDelete, "/users/"+targetID+"/mfa", nil)
	req.Header.Set("Authorization", "Bearer jwt")
	req = nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{UserId: callerID, AccountId: testAccountID})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return rec.Code, body
}

// An admin with a verified 2FA session resets another user's 2FA, and the
// manager gets the account, the admin and the target.
func TestDisenroll_AdminResetsMember(t *testing.T) {
	var calls []disenrollCall
	code, _ := deleteMFA(t, newDisenrollTestManager(true, true, &calls), testAdminID, testMemberID)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []disenrollCall{{testAccountID, testAdminID, testMemberID}}, calls)
}

// An admin who has 2FA but no verified session is sent to the challenge,
// as before.
func TestDisenroll_EnrolledAdminWithoutSessionIsChallenged(t *testing.T) {
	var calls []disenrollCall
	code, body := deleteMFA(t, newDisenrollTestManager(true, false, &calls), testAdminID, testMemberID)
	require.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, true, body["mfa_required"])
	assert.Equal(t, "challenge-token", body["token"])
	assert.Empty(t, calls)
}

// An admin without 2FA cannot pass a challenge, so a challenge token would
// strand them on the challenge page. They get a refusal that says what to
// do, with no token and no redirect.
func TestDisenroll_AdminWithoutMFAIsToldToSetItUp(t *testing.T) {
	var calls []disenrollCall
	code, body := deleteMFA(t, newDisenrollTestManager(false, false, &calls), testAdminID, testMemberID)
	require.Equal(t, http.StatusForbidden, code)
	assert.NotContains(t, body, "token")
	assert.NotContains(t, body, "mfa_required")
	assert.Contains(t, body["message"], "set up two-factor authentication on your own account first")
	assert.Empty(t, calls)
}

// A regular user cannot reset someone else's 2FA.
func TestDisenroll_MemberCannotResetOthers(t *testing.T) {
	var calls []disenrollCall
	code, _ := deleteMFA(t, newDisenrollTestManager(true, true, &calls), testMemberID, testAdminID)
	require.Equal(t, http.StatusForbidden, code)
	assert.Empty(t, calls)
}

// A right code whose step was already used is reported as such, so the
// page can ask for the next code instead of saying it does not match.
func TestChallenge_ReportsReusedCode(t *testing.T) {
	am := &mock_server.MockAccountManager{
		MFAVerifyTokenFunc: func(_ string, _ account.MFATokenPurpose) (*account.MFATokenVerifyResult, error) {
			return &account.MFATokenVerifyResult{UserID: testMemberID, JTIBinding: "jti"}, nil
		},
		MFAChallengeFunc: func(_ context.Context, _, _, _ string) (*account.MFAChallengeOutcome, error) {
			return &account.MFAChallengeOutcome{CodeReused: true}, nil
		},
	}
	h := &handler{am: am}
	req := httptest.NewRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(`{"code":"123456"}`))
	req.Header.Set("Authorization", "Bearer challenge-token")
	rec := httptest.NewRecorder()
	h.challenge(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, false, body["ok"])
	assert.Equal(t, true, body["code_reused"])
	assert.NotContains(t, body, "mfa_session_token")
}
