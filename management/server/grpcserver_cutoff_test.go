package server

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/auth"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/mock_server"
)

// ADR-0027 D5: the peer login path must carry the JWT iat to LoginPeer,
// which compares it with the user's credentials cutoff. Dropping it would
// make every JWT look like one without iat.
func TestProcessJWTToken_ReturnsIssuedAt(t *testing.T) {
	iat := time.Date(2026, 10, 18, 12, 0, 5, 0, time.UTC)
	server := &GRPCServer{
		accountManager: &mock_server.MockAccountManager{
			GetAccountIDFromUserAuthFunc: func(_ context.Context, ua nbcontext.UserAuth) (string, string, error) {
				return "account", ua.UserId, nil
			},
		},
		authManager: &auth.MockManager{
			ValidateAndParseTokenFunc: func(context.Context, string) (nbcontext.UserAuth, *jwt.Token, error) {
				return nbcontext.UserAuth{UserId: "u", IssuedAt: iat}, &jwt.Token{}, nil
			},
			EnsureUserAccessByJWTGroupsFunc: func(_ context.Context, ua nbcontext.UserAuth, _ *jwt.Token) (nbcontext.UserAuth, error) {
				return ua, nil
			},
		},
	}

	userID, issuedAt, err := server.processJwtToken(context.Background(), &proto.LoginRequest{JwtToken: "token"}, wgtypes.Key{})
	require.NoError(t, err)
	require.Equal(t, "u", userID)
	require.True(t, iat.Equal(issuedAt), "iat: want %s, got %s", iat, issuedAt)

	// No JWT at all (setup key or already-registered peer): no user, no iat.
	userID, issuedAt, err = server.processJwtToken(context.Background(), &proto.LoginRequest{}, wgtypes.Key{})
	require.NoError(t, err)
	require.Empty(t, userID)
	require.True(t, issuedAt.IsZero())
}
