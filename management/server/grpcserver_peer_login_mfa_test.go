package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/encryption"
	"github.com/openzro/openzro/management/client/peerloginmfa"
	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/auth"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/mock_server"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/types"
)

// Each gate refusal reaches the client as its reason, with the link built
// from the dashboard URL; the token is never in the message text.
func TestPeerLoginMFAStatus(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	const token = "secret-approval-token"
	const dashboard = "https://dash.example.com"

	tests := []struct {
		name       string
		err        error
		code       codes.Code
		want       peerloginmfa.Error
		managedURL string
	}{
		{
			name: "required",
			err: fmt.Errorf("login: %w", &PeerLoginMFARequiredError{Challenge: &PeerLoginMFAChallenge{
				ApprovalID: "a1", Token: token, ExpiresAt: now.Add(5 * time.Minute),
			}}),
			code: codes.FailedPrecondition,
			want: peerloginmfa.Error{Reason: peerloginmfa.ReasonRequired,
				URL: dashboard + "/mfa/peer-login#t=" + token, ExpiresIn: 5 * time.Minute},
		},
		{
			name: "denied",
			err:  &PeerLoginMFADeniedError{RetryAfter: 90 * time.Second},
			code: codes.FailedPrecondition,
			want: peerloginmfa.Error{Reason: peerloginmfa.ReasonDenied, RetryAfter: 90 * time.Second},
		},
		{
			name: "rate limited",
			err:  &PeerLoginMFARateLimitedError{RetryAfter: 61 * time.Second},
			code: codes.ResourceExhausted,
			want: peerloginmfa.Error{Reason: peerloginmfa.ReasonRateLimited, RetryAfter: 61 * time.Second},
		},
		{
			name: "enrollment required",
			err:  &PeerLoginMFAEnrollmentRequiredError{},
			code: codes.FailedPrecondition,
			want: peerloginmfa.Error{Reason: peerloginmfa.ReasonEnrollmentRequired, URL: dashboard + "/"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := peerLoginMFAStatus(tc.err, dashboard, now)
			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, st.Code())
			assert.NotContains(t, st.Message(), token)
			assert.Contains(t, st.Message(), "update openZro", "older clients learn they must update")

			got, ok := peerloginmfa.Parse(err, "https://mgmt.example.com:443")
			require.True(t, ok)
			assert.Equal(t, tc.want, *got)
		})
	}

	t.Run("no dashboard URL leaves the client its management host", func(t *testing.T) {
		err := peerLoginMFAStatus(&PeerLoginMFAEnrollmentRequiredError{}, "", now)
		got, ok := peerloginmfa.Parse(err, "https://mgmt.example.com:443")
		require.True(t, ok)
		assert.Equal(t, "https://mgmt.example.com/", got.URL)
	})

	t.Run("other errors are not refusals", func(t *testing.T) {
		assert.Nil(t, peerLoginMFAStatus(errors.New("boom"), dashboard, now))
		assert.Nil(t, peerLoginMFAStatus(nil, dashboard, now))
	})
}

// Login hands the token's connector to the gate and returns the gate's
// refusal with its details, not the generic error mapping.
func TestLogin_PeerLoginMFARefusal(t *testing.T) {
	serverKey, err := wgtypes.GeneratePrivateKey()
	require.NoError(t, err)
	peerKey, err := wgtypes.GeneratePrivateKey()
	require.NoError(t, err)

	var got types.PeerLogin
	server := &GRPCServer{
		wgKey:  serverKey,
		config: &types.Config{HttpConfig: &types.HttpServerConfig{DashboardURL: "https://dash.example.com"}},
		accountManager: &mock_server.MockAccountManager{
			GetAccountIDForPeerKeyFunc: func(context.Context, string) (string, error) { return "account", nil },
			GetAccountIDFromUserAuthFunc: func(_ context.Context, ua nbcontext.UserAuth) (string, string, error) {
				return "account", ua.UserId, nil
			},
			LoginPeerFunc: func(_ context.Context, login types.PeerLogin) (*nbpeer.Peer, *types.NetworkMap,
				[]*posture.Checks, error) {
				got = login
				return nil, nil, nil, &PeerLoginMFARequiredError{Challenge: &PeerLoginMFAChallenge{
					ApprovalID: "a1", Token: "tok", ExpiresAt: time.Now().Add(5 * time.Minute),
				}}
			},
		},
		authManager: &auth.MockManager{
			ValidateAndParseTokenFunc: func(context.Context, string) (nbcontext.UserAuth, *jwt.Token, error) {
				return nbcontext.UserAuth{UserId: "u", ConnectorID: "local"}, &jwt.Token{}, nil
			},
			EnsureUserAccessByJWTGroupsFunc: func(_ context.Context, ua nbcontext.UserAuth,
				_ *jwt.Token) (nbcontext.UserAuth, error) {
				return ua, nil
			},
		},
	}
	body, err := encryption.EncryptMessage(serverKey.PublicKey(), peerKey, &proto.LoginRequest{
		JwtToken: "jwt", Meta: &proto.PeerSystemMeta{Hostname: "ana-laptop", GoOS: "linux"},
	})
	require.NoError(t, err)

	_, err = server.Login(context.Background(), &proto.EncryptedMessage{
		WgPubKey: peerKey.PublicKey().String(), Body: body,
	})
	assert.Equal(t, "local", got.ConnectorID)
	refusal, ok := peerloginmfa.Parse(err, "https://mgmt.example.com")
	require.True(t, ok, "want a 2FA refusal, got %v", err)
	assert.Equal(t, peerloginmfa.ReasonRequired, refusal.Reason)
	assert.Equal(t, "https://dash.example.com/mfa/peer-login#t=tok", refusal.URL)
}
