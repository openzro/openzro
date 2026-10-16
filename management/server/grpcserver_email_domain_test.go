package server

import (
	"context"
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

	_, err := server.processJwtToken(context.Background(), &proto.LoginRequest{JwtToken: "token"}, wgtypes.Key{})
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, grpcstatus.Code(err))
	require.Equal(t, 1, accountCalls, "a policy denial must not be retried or audited repeatedly")
}
