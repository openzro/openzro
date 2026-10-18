package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/auth"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/mock_server"
	internalStatus "github.com/openzro/openzro/management/server/status"
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
			EnsureUserAccessByJWTGroupsFunc: func(_ context.Context, ua nbcontext.UserAuth,
				_ *jwt.Token) (nbcontext.UserAuth, error) {
				return ua, nil
			},
		},
	}

	userID, issuedAt, err := server.processJwtToken(context.Background(),
		&proto.LoginRequest{JwtToken: "token"}, wgtypes.Key{})
	require.NoError(t, err)
	require.Equal(t, "u", userID)
	require.True(t, iat.Equal(issuedAt), "iat: want %s, got %s", iat, issuedAt)

	// No JWT at all (setup key or already-registered peer): no user, no iat.
	userID, issuedAt, err = server.processJwtToken(context.Background(), &proto.LoginRequest{}, wgtypes.Key{})
	require.NoError(t, err)
	require.Empty(t, userID)
	require.True(t, issuedAt.IsZero())
}

// A JWT revoked by a reset must reach the client as PermissionDenied: its
// reconnect loop switches to needs-login only on that code, and backs off
// forever on InvalidArgument. A store failure while checking the cutoff is
// an outage, not a bad token: Internal, which the login retries.
func TestProcessJWTToken_MapsValidationFailures(t *testing.T) {
	cases := map[string]struct {
		err  error
		want codes.Code
	}{
		"revoked":       {fmt.Errorf("user u: %w", auth.ErrJWTRevoked), codes.PermissionDenied},
		"store failure": {fmt.Errorf("read user: %w", internalStatus.Errorf(internalStatus.Internal, "db down")), codes.Internal},
		"bad signature": {errors.New("token signature is invalid"), codes.InvalidArgument},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := &GRPCServer{
				accountManager: &mock_server.MockAccountManager{},
				authManager: &auth.MockManager{
					ValidateAndParseTokenFunc: func(context.Context, string) (nbcontext.UserAuth, *jwt.Token, error) {
						calls++
						return nbcontext.UserAuth{}, nil, tc.err
					},
				},
			}
			_, _, err := server.processJwtToken(context.Background(),
				&proto.LoginRequest{JwtToken: "token"}, wgtypes.Key{})
			require.Equal(t, tc.want, status.Code(err), "error: %v", err)
			if tc.want == codes.Internal {
				require.Equal(t, 3, calls, "an outage is retried")
			} else {
				require.Equal(t, 1, calls, "a rejection fails fast")
			}
		})
	}
}
