package server

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/auth"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/mock_server"
	internalstatus "github.com/openzro/openzro/management/server/status"
)

func TestProcessJWTToken_DomainDenyIsPermissionDeniedWithoutRetry(t *testing.T) {
	accountCalls := 0
	accountManager := &mock_server.MockAccountManager{
		GetAccountIDFromUserAuthFunc: func(context.Context, nbcontext.UserAuth) (string, string, error) {
			accountCalls++
			return "", "", internalstatus.Errorf(internalstatus.PermissionDenied, errEmailDomainNotAllowed)
		},
	}
	authManager := &auth.MockManager{
		ValidateAndParseTokenFunc: func(context.Context, string) (nbcontext.UserAuth, *jwt.Token, error) {
			return nbcontext.UserAuth{UserId: "denied-user"}, &jwt.Token{}, nil
		},
	}
	server := &GRPCServer{accountManager: accountManager, authManager: authManager}

	_, _, err := server.processJwtToken(context.Background(), &proto.LoginRequest{JwtToken: "token"}, wgtypes.Key{})
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, grpcstatus.Code(err))
	require.Equal(t, 1, accountCalls, "a policy denial must not be retried or audited repeatedly")
}

// Regression: the agent-login retry exists for the IdP cache race (1bfb26cc),
// where redeemInvite reports NotFound until the cache catches up. Narrowing
// the retry to codes.Internal stopped retrying that case; deterministic
// rejections must still fail fast.
func TestProcessJWTToken_RetryOnlyTransientErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  codes.Code
		wantCalls int
	}{
		{name: "cache race NotFound is retried", err: internalstatus.Errorf(internalstatus.NotFound, "user x not found in the IdP"), wantCode: codes.NotFound, wantCalls: 3},
		{name: "unclassified error is retried", err: errors.New("boom"), wantCode: codes.Internal, wantCalls: 3},
		{name: "permission denied fails fast", err: internalstatus.NewPermissionDeniedError(), wantCode: codes.PermissionDenied, wantCalls: 1},
		{name: "precondition failed fails fast", err: internalstatus.Errorf(internalstatus.PreconditionFailed, "x"), wantCode: codes.FailedPrecondition, wantCalls: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := &GRPCServer{
				accountManager: &mock_server.MockAccountManager{
					GetAccountIDFromUserAuthFunc: func(context.Context, nbcontext.UserAuth) (string, string, error) {
						calls++
						return "", "", tc.err
					},
				},
				authManager: &auth.MockManager{
					ValidateAndParseTokenFunc: func(context.Context, string) (nbcontext.UserAuth, *jwt.Token, error) {
						return nbcontext.UserAuth{UserId: "u"}, &jwt.Token{}, nil
					},
				},
			}

			_, _, err := server.processJwtToken(context.Background(), &proto.LoginRequest{JwtToken: "token"}, wgtypes.Key{})
			require.Error(t, err)
			require.Equal(t, tc.wantCode, grpcstatus.Code(err))
			require.Equal(t, tc.wantCalls, calls)
		})
	}
}

// An invalid JWT cannot become valid in 400 ms; it must not be retried.
func TestProcessJWTToken_InvalidTokenNotRetried(t *testing.T) {
	parses := 0
	server := &GRPCServer{
		accountManager: &mock_server.MockAccountManager{},
		authManager: &auth.MockManager{
			ValidateAndParseTokenFunc: func(context.Context, string) (nbcontext.UserAuth, *jwt.Token, error) {
				parses++
				return nbcontext.UserAuth{}, nil, errors.New("token expired")
			},
		},
	}

	_, _, err := server.processJwtToken(context.Background(), &proto.LoginRequest{JwtToken: "token"}, wgtypes.Key{})
	require.Equal(t, codes.InvalidArgument, grpcstatus.Code(err))
	require.Equal(t, 1, parses)
}

func TestProcessJWTToken_CanceledWhileWaitingToRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	server := &GRPCServer{
		accountManager: &mock_server.MockAccountManager{
			GetAccountIDFromUserAuthFunc: func(context.Context, nbcontext.UserAuth) (string, string, error) {
				calls++
				cancel()
				return "", "", internalstatus.Errorf(internalstatus.NotFound, "user not yet in IdP cache")
			},
		},
		authManager: &auth.MockManager{
			ValidateAndParseTokenFunc: func(context.Context, string) (nbcontext.UserAuth, *jwt.Token, error) {
				return nbcontext.UserAuth{UserId: "u"}, &jwt.Token{}, nil
			},
		},
	}

	_, _, err := server.processJwtToken(ctx, &proto.LoginRequest{JwtToken: "token"}, wgtypes.Key{})
	require.Equal(t, codes.Canceled, grpcstatus.Code(err))
	require.Equal(t, 1, calls)
}
