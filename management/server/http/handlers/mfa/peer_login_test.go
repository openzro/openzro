package mfa

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/status"
)

var testView = &account.PeerLoginMFAView{
	State: account.PeerLoginMFAPending, Hostname: "ana-laptop", OS: "linux", IP: "203.0.113.7",
	KeyFingerprint: "Oxc1yl…CVQ=", MaskedEmail: "a***@example.com", AccountDomain: "example.com",
	CreatedAt: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 10, 21, 12, 5, 0, 0, time.UTC),
}

func peerLoginCall(t *testing.T, fn http.HandlerFunc, method, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer page-token")
	rec := httptest.NewRecorder()
	fn(rec, req)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), "never cached")
	assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"), "never sent as a referrer")
	var out map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	}
	return rec, out
}

func TestPeerLogin_Get(t *testing.T) {
	var gotToken string
	h := &handler{am: &mock_server.MockAccountManager{
		GetPeerLoginMFAApprovalFunc: func(_ context.Context, token string) (*account.PeerLoginMFAView, error) {
			gotToken = token
			return testView, nil
		},
	}}
	rec, body := peerLoginCall(t, h.peerLoginGet, http.MethodGet, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "page-token", gotToken, "the token is read from the bearer header")
	assert.Equal(t, "pending", body["state"])
	assert.Equal(t, "a***@example.com", body["masked_email"])
	assert.Equal(t, "Oxc1yl…CVQ=", body["key_fingerprint"])

	h.am = &mock_server.MockAccountManager{
		GetPeerLoginMFAApprovalFunc: func(_ context.Context, _ string) (*account.PeerLoginMFAView, error) {
			return nil, status.Errorf(status.PreconditionFailed, "this device login has expired")
		},
	}
	rec, _ = peerLoginCall(t, h.peerLoginGet, http.MethodGet, "")
	assert.Equal(t, http.StatusPreconditionFailed, rec.Code)
}

func TestPeerLogin_GetWithoutToken(t *testing.T) {
	h := &handler{am: &mock_server.MockAccountManager{}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.peerLoginGet(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestPeerLogin_Approve(t *testing.T) {
	lockedUntil := time.Date(2026, 10, 21, 12, 15, 0, 0, time.UTC)
	tests := []struct {
		name       string
		outcome    *account.PeerLoginMFAApproveOutcome
		err        error
		wantStatus int
		wantReason string
	}{
		{"approved", &account.PeerLoginMFAApproveOutcome{Approved: true}, nil, http.StatusOK, ""},
		{"wrong code", &account.PeerLoginMFAApproveOutcome{}, nil, http.StatusUnprocessableEntity, "wrong_code"},
		{"reused code", &account.PeerLoginMFAApproveOutcome{CodeReused: true}, nil, http.StatusUnprocessableEntity, "code_reused"},
		{"locked", &account.PeerLoginMFAApproveOutcome{Locked: true, LockedUntil: &lockedUntil}, nil, http.StatusLocked, "locked"},
		{"final state", nil, status.Errorf(status.PreconditionFailed, "gone"), http.StatusPreconditionFailed, ""},
		{"bad link", nil, status.Errorf(status.Unauthorized, "invalid approval link"), http.StatusUnauthorized, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotCode string
			h := &handler{am: &mock_server.MockAccountManager{
				ApprovePeerLoginMFAFunc: func(_ context.Context, _, code string) (*account.PeerLoginMFAApproveOutcome, error) {
					gotCode = code
					return tc.outcome, tc.err
				},
			}}
			rec, body := peerLoginCall(t, h.peerLoginApprove, http.MethodPost, `{"code":"123456"}`)
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.err != nil {
				return
			}
			assert.Equal(t, "123456", gotCode)
			assert.Equal(t, tc.outcome.Approved, body["approved"])
			if tc.wantReason != "" {
				assert.Equal(t, tc.wantReason, body["reason"])
			} else {
				assert.NotContains(t, body, "reason")
			}
			if tc.outcome.Locked {
				assert.Equal(t, "2026-10-21T12:15:00Z", body["locked_until"])
			}
		})
	}
}

func TestPeerLogin_Deny(t *testing.T) {
	denied := *testView
	denied.State = account.PeerLoginMFADenied
	h := &handler{am: &mock_server.MockAccountManager{
		DenyPeerLoginMFAFunc: func(_ context.Context, _ string) (*account.PeerLoginMFAView, error) {
			return &denied, nil
		},
	}}
	rec, body := peerLoginCall(t, h.peerLoginDeny, http.MethodPost, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "denied", body["state"])
}

// The three routes skip the JWT middleware: the approval link's token is
// their only credential.
func TestPeerLogin_RoutesBypassJWT(t *testing.T) {
	for _, route := range []string{"/api/mfa/peer-login", "/api/mfa/peer-login/approve", "/api/mfa/peer-login/deny"} {
		assert.Contains(t, mfaBypassRoutes, route)
	}
}

// The approve body is one short code. Anything larger, or anything after
// the JSON object, is refused before the manager is reached; an empty body
// is the retry of an approval that already happened.
func TestPeerLogin_ApproveBody(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCalled bool
		wantCode   string
	}{
		{"oversized", `{"code":"` + strings.Repeat("1", 64<<10) + `"}`, http.StatusRequestEntityTooLarge, false, ""},
		{"trailing data", `{"code":"123456"}{"code":"654321"}`, http.StatusBadRequest, false, ""},
		{"not JSON", `code=123456`, http.StatusBadRequest, false, ""},
		{"null", `null`, http.StatusBadRequest, false, ""},
		{"array", `[]`, http.StatusBadRequest, false, ""},
		{"string", `"123456"`, http.StatusBadRequest, false, ""},
		{"number", `123456`, http.StatusBadRequest, false, ""},
		{"null code", `{"code":null}`, http.StatusOK, true, ""},
		{"object with surrounding space", " \n {\"code\":\"123456\"} \n", http.StatusOK, true, "123456"},
		{"empty body retries", ``, http.StatusOK, true, ""},
		{"empty object retries", `{}`, http.StatusOK, true, ""},
		{"code", `{"code":" 123456 "}`, http.StatusOK, true, "123456"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called, gotCode := false, "unset"
			h := &handler{am: &mock_server.MockAccountManager{
				ApprovePeerLoginMFAFunc: func(_ context.Context, _, code string) (*account.PeerLoginMFAApproveOutcome, error) {
					called, gotCode = true, code
					return &account.PeerLoginMFAApproveOutcome{Approved: true}, nil
				},
			}}
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer page-token")
			rec := httptest.NewRecorder()
			h.peerLoginApprove(rec, req)
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, tc.wantCalled, called, "manager reached")
			if tc.wantCalled {
				assert.Equal(t, tc.wantCode, gotCode)
			}
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		})
	}
}
