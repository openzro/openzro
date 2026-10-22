package cmd

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

// Without TLS, gRPC and the HTTP API share one port: gRPC clients speak
// HTTP/2 with prior knowledge, browsers and API clients HTTP/1.1.
func TestPlaintextServer_ServesHTTP2AndHTTP1(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := newPlaintextServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Proto)
	}))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	url := "http://" + listener.Addr().String() + "/"

	h2 := &http.Client{Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}}
	for name, client := range map[string]*http.Client{"HTTP/2.0": h2, "HTTP/1.1": http.DefaultClient} {
		resp, err := client.Get(url)
		require.NoError(t, err, name)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, name, string(body))
	}
}
