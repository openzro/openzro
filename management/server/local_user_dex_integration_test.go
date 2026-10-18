package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	apiv2 "github.com/dexidp/dex/api/v2"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/auth"
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
	client, baseURL, _ := startDexWithAPI(t)
	return client, baseURL
}

// startDexWithAPI also returns the raw Dex API client, for assertions the
// openZro client does not expose (listing refresh tokens).
func startDexWithAPI(t *testing.T) (*dex_proxy.Client, string, apiv2.DexClient) {
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

	return dex_proxy.NewWithConn(conn), fmt.Sprintf("http://%s:%s/dex", host, httpPort.Port()), apiv2.NewDexClient(conn)
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

// tokenGrant posts a grant to Dex's token endpoint and returns the decoded
// response (id_token claims merged with refresh_token) or the status.
func tokenGrant(t *testing.T, baseURL string, form url.Values) (map[string]any, int) {
	t.Helper()
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
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body, resp.StatusCode
}

func loginWithRefresh(t *testing.T, baseURL, email, password string) (string, int) {
	t.Helper()
	body, code := tokenGrant(t, baseURL, url.Values{
		"grant_type": {"password"}, "username": {email}, "password": {password},
		"scope": {"openid email offline_access"},
	})
	if code != http.StatusOK {
		return "", code
	}
	refresh, _ := body["refresh_token"].(string)
	require.NotEmpty(t, refresh, "offline_access must yield a refresh token")
	return refresh, code
}

// Issuer and client of dexTestConfig.
const (
	dexTestIssuer   = "http://127.0.0.1:5556/dex"
	dexTestClientID = "openzro-test"
)

// accessTokenAuth logs in with the password grant and validates the
// returned access token with openZro's JWT validator and claims extractor,
// as the HTTP API and peer login do.
func accessTokenAuth(t *testing.T, validator auth.Manager, baseURL, email, password string) nbcontext.UserAuth {
	t.Helper()
	body, code := tokenGrant(t, baseURL, url.Values{
		"grant_type": {"password"}, "username": {email}, "password": {password},
		"scope": {"openid email"},
	})
	require.Equal(t, http.StatusOK, code)
	accessToken, _ := body["access_token"].(string)
	require.NotEmpty(t, accessToken)
	userAuth, _, err := validator.ValidateAndParseToken(context.Background(), accessToken)
	require.NoError(t, err)
	return userAuth
}

func refreshTokenCount(t *testing.T, api apiv2.DexClient, sub string) int {
	t.Helper()
	resp, err := api.ListRefresh(context.Background(), &apiv2.ListRefreshReq{UserId: sub})
	require.NoError(t, err)
	return len(resp.GetRefreshTokens())
}

// ADR-0024 D6/D7 against a real Dex: a reset replaces the credential and
// revokes refresh tokens; deactivation removes the credential and revokes
// them again, so the user can no longer log in or hold a session.
//
// Revocation is checked through Dex's ListRefresh API rather than by
// redeeming a refresh token: on SQLite and memory storage, Dex v2.45-v2.46
// deadlocks its token endpoint on the first refresh of a password-DB user
// (dexidp/dex#4209, fix pending in #4991), which is why local users require
// Dex on Postgres for now.
func TestLocalUserLifecycle_RealDex(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test; needs a container runtime")
	}
	if runtime.GOOS != "linux" && os.Getenv("CI") == "true" {
		t.Skip("container tests run on Linux CI only")
	}
	dex, baseURL, api := startDexWithAPI(t)

	manager, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := manager.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)
	manager.EnableLocalUsers(dex)

	user, err := manager.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
	require.NoError(t, err)
	_, code := loginWithRefresh(t, baseURL, "ana@example.com", goodPassword)
	require.Equal(t, http.StatusOK, code)
	require.Positive(t, refreshTokenCount(t, api, user.Id), "offline_access must create a refresh token")
	validator := auth.NewManager(manager.Store, dexTestIssuer, dexTestClientID, baseURL+"/keys", "sub",
		[]string{dexTestClientID}, false)
	oldAuth := accessTokenAuth(t, validator, baseURL, "ana@example.com", goodPassword)
	require.False(t, oldAuth.IssuedAt.IsZero(), "Dex access tokens carry iat")

	// Reset.
	_, err = manager.SCIMResetLocalPassword(ctx, accountID, userID, user.Id, newPassword, nil)
	require.NoError(t, err)
	require.Zero(t, refreshTokenCount(t, api, user.Id), "a reset revokes existing refresh tokens")
	// ADR-0027: the access token minted before the reset is revoked by the
	// cutoff, and one minted with the new password after it is accepted.
	stored, err := manager.Store.GetUserByUserID(ctx, store.LockingStrengthShare, user.Id)
	require.NoError(t, err)
	require.NotNil(t, stored.CredentialsValidAfter)
	require.True(t, stored.JWTRevoked(oldAuth.IssuedAt), "a Dex access token issued before the reset is revoked")
	time.Sleep(time.Until(stored.CredentialsValidAfter.Add(time.Second + 100*time.Millisecond)))
	newAuth := accessTokenAuth(t, validator, baseURL, "ana@example.com", newPassword)
	require.False(t, stored.JWTRevoked(newAuth.IssuedAt), "a Dex access token issued after the reset is accepted")
	_, code = passwordGrant(t, baseURL, "ana@example.com", goodPassword)
	require.NotEqual(t, http.StatusOK, code, "the old password must stop working")
	_, code = loginWithRefresh(t, baseURL, "ana@example.com", newPassword)
	require.Equal(t, http.StatusOK, code, "the new password must work")
	require.Positive(t, refreshTokenCount(t, api, user.Id))

	// Deactivate.
	inactive := false
	_, err = manager.SCIMPatchUser(ctx, accountID, userID, user.Id, account.SCIMUserPatch{Active: &inactive})
	require.NoError(t, err)
	require.Zero(t, refreshTokenCount(t, api, user.Id), "deactivation revokes refresh tokens")
	_, code = passwordGrant(t, baseURL, "ana@example.com", newPassword)
	require.NotEqual(t, http.StatusOK, code, "a deactivated local user cannot log in")

	entries, err := dex.ListPasswords(ctx)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotEqual(t, "ana@example.com", e.Email, "the Dex credential must be gone")
	}
}

// authCodeAfterPassword runs Dex's authorization-code login up to the code:
// it submits the password form and stops at the redirect to the client, so
// the password has been checked but no token exists yet.
func authCodeAfterPassword(t *testing.T, baseURL, email, password string) string {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if strings.HasPrefix(req.URL.String(), "http://127.0.0.1/callback") {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	authURL := baseURL + "/auth?" + url.Values{
		"client_id": {dexTestClientID}, "redirect_uri": {"http://127.0.0.1/callback"},
		"response_type": {"code"}, "scope": {"openid email offline_access"}, "state": {"in-flight"},
	}.Encode()
	resp, err := client.Get(authURL)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "the password form")

	resp, err = client.PostForm(resp.Request.URL.String(), url.Values{"login": {email}, "password": {password}})
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "the password is accepted")
	location, err := resp.Location()
	require.NoError(t, err)
	code := location.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

// ADR-0027 D6, #220 review, against real Dex: a login that passed the old
// password before the reset redeems its authorization code after it. Its
// access token is newer than the first cutoff and passes; it also gets a
// refresh token. The second pass cuts both.
func TestLocalPasswordReset_InFlightLoginCutBySecondPass_RealDex(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test; needs a container runtime")
	}
	if runtime.GOOS != "linux" && os.Getenv("CI") == "true" {
		t.Skip("container tests run on Linux CI only")
	}
	dex, baseURL, api := startDexWithAPI(t)
	manager, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()
	accountID, err := manager.GetAccountIDByUserID(ctx, userID, "")
	require.NoError(t, err)
	manager.EnableLocalUsers(dex)
	user, err := manager.SCIMCreateLocalUser(ctx, accountID, userID, localInput("ana@example.com"), goodPassword)
	require.NoError(t, err)
	validator := auth.NewManager(manager.Store, dexTestIssuer, dexTestClientID, baseURL+"/keys", "sub",
		[]string{dexTestClientID}, false)

	code := authCodeAfterPassword(t, baseURL, "ana@example.com", goodPassword)

	_, err = manager.SCIMResetLocalPassword(ctx, accountID, userID, user.Id, newPassword, nil)
	require.NoError(t, err)
	stored, err := manager.Store.GetUserByUserID(ctx, store.LockingStrengthShare, user.Id)
	require.NoError(t, err)
	require.NotNil(t, stored.CredentialsRecheckAt)
	// Redeem in a later second than the cutoff, as a real client would.
	time.Sleep(time.Until(stored.CredentialsValidAfter.Add(time.Second + 100*time.Millisecond)))

	body, status := tokenGrant(t, baseURL, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"http://127.0.0.1/callback"},
	})
	require.Equal(t, http.StatusOK, status, "Dex redeems a code issued before the reset")
	accessToken, _ := body["access_token"].(string)
	require.NotEmpty(t, accessToken)
	_, _, err = validator.ValidateAndParseToken(ctx, accessToken)
	require.NoError(t, err, "the first cutoff cannot tell this token from a new-password one")
	require.Positive(t, refreshTokenCount(t, api, user.Id), "the redemption minted a refresh token")

	manager.runDueCredentialsRechecks(ctx, *stored.CredentialsRecheckAt)

	_, _, err = validator.ValidateAndParseToken(ctx, accessToken)
	require.ErrorIs(t, err, auth.ErrJWTRevoked, "the second pass cuts the in-flight login's token")
	require.Zero(t, refreshTokenCount(t, api, user.Id), "and its refresh token")
}
