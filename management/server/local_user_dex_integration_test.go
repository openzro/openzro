package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/dex_proxy"
	"github.com/openzro/openzro/management/server/store"
)

// dexTestImage is pinned to the Dex release whose storage/API semantics
// ADR-0024 was reviewed against.
const dexTestImage = "ghcr.io/dexidp/dex:v2.45.0"

const dexTestConfig = `issuer: http://127.0.0.1:5556/dex
storage:
  type: memory
web:
  http: 0.0.0.0:5556
grpc:
  addr: 0.0.0.0:5557
enablePasswordDB: true
oauth2:
  passwordConnector: local
  skipApprovalScreen: true
staticClients:
  - id: openzro-test
    secret: openzro-test-secret
    name: openzro-test
    redirectURIs:
      - http://127.0.0.1/callback
`

// startDex runs a real Dex with its password DB and gRPC API, and returns
// a dex_proxy client plus the base URL of its HTTP endpoints.
func startDex(t *testing.T) (*dex_proxy.Client, string) {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        dexTestImage,
			ExposedPorts: []string{"5556/tcp", "5557/tcp"},
			Cmd:          []string{"dex", "serve", "/etc/dex/openzro-test.yaml"},
			Files: []testcontainers.ContainerFile{{
				Reader:            strings.NewReader(dexTestConfig),
				ContainerFilePath: "/etc/dex/openzro-test.yaml",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForHTTP("/dex/.well-known/openid-configuration").
				WithPort("5556/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	host, err := c.Host(ctx)
	require.NoError(t, err)
	httpPort, err := c.MappedPort(ctx, "5556/tcp")
	require.NoError(t, err)
	grpcPort, err := c.MappedPort(ctx, "5557/tcp")
	require.NoError(t, err)

	conn, err := grpc.NewClient(fmt.Sprintf("%s:%s", host, grpcPort.Port()),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return dex_proxy.NewWithConn(conn), fmt.Sprintf("http://%s:%s/dex", host, httpPort.Port())
}

// passwordGrant logs in through Dex's resource-owner password grant and
// returns the ID token claims, or the HTTP status on failure.
func passwordGrant(t *testing.T, baseURL, email, password string) (map[string]any, int) {
	t.Helper()
	form := url.Values{
		"grant_type": {"password"},
		"username":   {email},
		"password":   {password},
		"scope":      {"openid email profile"},
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/token", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("openzro-test", "openzro-test-secret")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode
	}

	var body struct {
		IDToken string `json:"id_token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	parts := strings.Split(body.IDToken, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims, resp.StatusCode
}

// ADR-0024 verification: a local user created through SCIM logs in at a
// real Dex, the token's sub is exactly the openZro user ID, and that login
// lands on the pre-provisioned user instead of creating a second one.
func TestSCIMCreateLocalUser_RealDexLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test; needs a container runtime")
	}
	if runtime.GOOS != "linux" && os.Getenv("CI") == "true" {
		t.Skip("container tests run on Linux CI only")
	}
	dex, baseURL := startDex(t)

	manager, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := manager.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)
	manager.EnableLocalUsers(dex)

	user, err := manager.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
	require.NoError(t, err)

	claims, code := passwordGrant(t, baseURL, "ana@example.com", goodPassword)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, user.Id, claims["sub"], "Dex must issue exactly the sub openZro stored")
	require.Equal(t, true, claims["email_verified"], "local credentials are verified (ADR-0025)")

	_, code = passwordGrant(t, baseURL, "ana@example.com", "not-the-password")
	require.NotEqual(t, http.StatusOK, code)

	before, err := manager.Store.GetAccountUsers(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	gotAccount, gotUser, err := manager.GetAccountIDFromUserAuth(ctx, nbcontext.UserAuth{
		UserId: claims["sub"].(string), Email: "ana@example.com", EmailVerified: true,
	})
	require.NoError(t, err)
	require.Equal(t, accountID, gotAccount)
	require.Equal(t, user.Id, gotUser)
	after, err := manager.Store.GetAccountUsers(ctx, store.LockingStrengthShare, accountID)
	require.NoError(t, err)
	require.Len(t, after, len(before), "the first login must not create a second (JIT) user")
}
