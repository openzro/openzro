package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/client/internal"
	mgm "github.com/openzro/openzro/management/client/common"
)

func TestPromptLogin(t *testing.T) {
	const (
		promptLogin = "prompt=login"
		maxAge0     = "max_age=0"
	)

	tt := []struct {
		name               string
		loginFlag          mgm.LoginFlag
		disablePromptLogin bool
		expect             string
	}{
		{
			name:      "Prompt login",
			loginFlag: mgm.LoginFlagPrompt,
			expect:    promptLogin,
		},
		{
			name:      "Max age 0 login",
			loginFlag: mgm.LoginFlagMaxAge0,
			expect:    maxAge0,
		},
		{
			name:               "Disable prompt login",
			loginFlag:          mgm.LoginFlagPrompt,
			disablePromptLogin: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			config := internal.PKCEAuthProviderConfig{
				ClientID:              "test-client-id",
				Audience:              "test-audience",
				TokenEndpoint:         "https://test-token-endpoint.com/token",
				Scope:                 "openid email profile",
				AuthorizationEndpoint: "https://test-auth-endpoint.com/authorize",
				RedirectURLs:          []string{"http://127.0.0.1:33992/"},
				UseIDToken:            true,
				LoginFlag:             tc.loginFlag,
			}
			pkce, err := NewPKCEAuthorizationFlow(config)
			if err != nil {
				t.Fatalf("Failed to create PKCEAuthorizationFlow: %v", err)
			}
			authInfo, err := pkce.RequestAuthInfo(context.Background())
			if err != nil {
				t.Fatalf("Failed to request auth info: %v", err)
			}

			if !tc.disablePromptLogin {
				require.Contains(t, authInfo.VerificationURIComplete, tc.expect)
			} else {
				require.Contains(t, authInfo.VerificationURIComplete, promptLogin)
				require.NotContains(t, authInfo.VerificationURIComplete, maxAge0)
			}
		})
	}
}

// TestTokenExchangeClientCert covers the back-channel leg of the PKCE flow.
// The browser presents its own certificate on the authorize leg, so a gate that
// requires a client certificate on every path only rejects the token POST the
// client makes itself — the leg exercised here.
func TestTokenExchangeClientCert(t *testing.T) {
	t.Run("certificate configured", func(t *testing.T) {
		clientCert := generateTestClientCert(t)

		var (
			mu   sync.Mutex
			seen []*x509.Certificate
		)
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = r.TLS.PeerCertificates
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer"}`))
		}))
		srv.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12}
		srv.StartTLS()
		defer srv.Close()

		flow := newTestPKCEFlow(t, srv.URL, &clientCert)

		transport, ok := flow.tokenHTTPClient.Transport.(*http.Transport)
		require.True(t, ok, "token exchange transport should be an *http.Transport")
		// the default transport's proxy resolution has to survive the clone,
		// gated deployments commonly sit behind one
		require.NotNil(t, transport.Proxy)
		require.Equal(t, []tls.Certificate{clientCert}, transport.TLSClientConfig.Certificates)

		// only the trust anchor is test-specific; the throwaway server is not in
		// the system pool the production transport uses
		pool := x509.NewCertPool()
		pool.AddCert(srv.Certificate())
		transport.TLSClientConfig.RootCAs = pool

		token, err := flow.handleRequest(callbackRequest(t, flow))
		require.NoError(t, err)
		require.Equal(t, "test-access-token", token.AccessToken)

		mu.Lock()
		defer mu.Unlock()
		require.Len(t, seen, 1, "the token exchange did not present a client certificate")
		require.Equal(t, clientCert.Leaf.Raw, seen[0].Raw)
	})

	t.Run("no certificate configured", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer"}`))
		}))
		defer srv.Close()

		flow := newTestPKCEFlow(t, srv.URL, nil)
		require.Nil(t, flow.tokenHTTPClient, "no certificate configured must leave the oauth2 default client in place")

		token, err := flow.handleRequest(callbackRequest(t, flow))
		require.NoError(t, err)
		require.Equal(t, "test-access-token", token.AccessToken)
	})
}

// newTestPKCEFlow returns a flow pointed at tokenEndpoint that has already
// issued its state and code verifier.
func newTestPKCEFlow(t *testing.T, tokenEndpoint string, cert *tls.Certificate) *PKCEAuthorizationFlow {
	t.Helper()

	flow, err := NewPKCEAuthorizationFlow(internal.PKCEAuthProviderConfig{
		ClientID:              "test-client-id",
		Audience:              "test-audience",
		TokenEndpoint:         tokenEndpoint,
		AuthorizationEndpoint: "https://test-auth-endpoint.com/authorize",
		Scope:                 "openid email profile",
		RedirectURLs:          []string{"http://127.0.0.1:33992/"},
		ClientCertPair:        cert,
	})
	require.NoError(t, err)

	_, err = flow.RequestAuthInfo(context.Background())
	require.NoError(t, err)

	return flow
}

// callbackRequest builds the redirect the IdP sends back to the local callback
// server once the browser has completed the authorize leg.
func callbackRequest(t *testing.T, flow *PKCEAuthorizationFlow) *http.Request {
	t.Helper()

	query := url.Values{}
	query.Set(queryState, flow.state)
	query.Set(queryCode, "test-authorization-code")

	return httptest.NewRequest(http.MethodGet, "/?"+query.Encode(), nil)
}

// generateTestClientCert returns a throwaway self-signed client certificate.
func generateTestClientCert(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "openzro-test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}
