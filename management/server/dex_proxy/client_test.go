package dex_proxy

import (
	"context"
	"net"
	"sync"
	"testing"

	apiv2 "github.com/dexidp/dex/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeDex stands in for the upstream Dex server. Tests register
// per-method behavior by setting the fields below; defaults are
// "succeed quietly" so each test only sets what it cares about.
type fakeDex struct {
	apiv2.UnimplementedDexServer

	mu          sync.Mutex
	connectors  []*apiv2.Connector
	createErr   error
	updateErr   error
	deleteErr   error
	listErr     error
	createResp  *apiv2.CreateConnectorResp
	updateResp  *apiv2.UpdateConnectorResp
	deleteResp  *apiv2.DeleteConnectorResp
	versionResp *apiv2.VersionResp
}

func (f *fakeDex) ListConnectors(_ context.Context, _ *apiv2.ListConnectorReq) (*apiv2.ListConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &apiv2.ListConnectorResp{Connectors: append([]*apiv2.Connector(nil), f.connectors...)}, nil
}

func (f *fakeDex) CreateConnector(_ context.Context, req *apiv2.CreateConnectorReq) (*apiv2.CreateConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.createResp != nil {
		return f.createResp, nil
	}
	f.connectors = append(f.connectors, req.GetConnector())
	return &apiv2.CreateConnectorResp{}, nil
}

func (f *fakeDex) UpdateConnector(_ context.Context, req *apiv2.UpdateConnectorReq) (*apiv2.UpdateConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if f.updateResp != nil {
		return f.updateResp, nil
	}
	for _, p := range f.connectors {
		if p.GetId() == req.GetId() {
			p.Type = req.GetNewType()
			p.Name = req.GetNewName()
			p.Config = req.GetNewConfig()
			return &apiv2.UpdateConnectorResp{}, nil
		}
	}
	return &apiv2.UpdateConnectorResp{NotFound: true}, nil
}

func (f *fakeDex) DeleteConnector(_ context.Context, req *apiv2.DeleteConnectorReq) (*apiv2.DeleteConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	if f.deleteResp != nil {
		return f.deleteResp, nil
	}
	for i, p := range f.connectors {
		if p.GetId() == req.GetId() {
			f.connectors = append(f.connectors[:i], f.connectors[i+1:]...)
			return &apiv2.DeleteConnectorResp{}, nil
		}
	}
	return &apiv2.DeleteConnectorResp{NotFound: true}, nil
}

func (f *fakeDex) GetVersion(_ context.Context, _ *apiv2.VersionReq) (*apiv2.VersionResp, error) {
	if f.versionResp != nil {
		return f.versionResp, nil
	}
	return &apiv2.VersionResp{Server: "fake"}, nil
}

// newFakeClient boots an in-memory gRPC server, dials it, and
// returns a (*Client, *fakeDex) pair. Cleanup runs at t end.
func newFakeClient(t *testing.T) (*Client, *fakeDex) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	fake := &fakeDex{}
	apiv2.RegisterDexServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return &Client{conn: conn, dex: apiv2.NewDexClient(conn)}, fake
}

func TestClient_ListConnectorsEmpty(t *testing.T) {
	c, _ := newFakeClient(t)
	got, err := c.ListConnectors(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestClient_CreateAndList(t *testing.T) {
	c, _ := newFakeClient(t)
	in := Connector{
		ID:     "google-acme",
		Type:   "google",
		Name:   "Acme Google",
		Config: []byte(`{"clientID":"x","clientSecret":"y"}`),
	}
	require.NoError(t, c.CreateConnector(context.Background(), in))

	got, err := c.ListConnectors(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, in, got[0])
}

func TestClient_CreateRejectsEmpty(t *testing.T) {
	c, _ := newFakeClient(t)
	err := c.CreateConnector(context.Background(), Connector{})
	require.Error(t, err)
	err = c.CreateConnector(context.Background(), Connector{ID: "x"})
	require.Error(t, err)
}

func TestClient_CreateAlreadyExistsViaResp(t *testing.T) {
	// Some Dex versions report duplicates via the resp flag,
	// some via the gRPC AlreadyExists code. Cover both.
	c, fake := newFakeClient(t)
	fake.mu.Lock()
	fake.createResp = &apiv2.CreateConnectorResp{AlreadyExists: true}
	fake.mu.Unlock()
	err := c.CreateConnector(context.Background(), Connector{ID: "g", Type: "google"})
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

func TestClient_CreateAlreadyExistsViaCode(t *testing.T) {
	c, fake := newFakeClient(t)
	fake.mu.Lock()
	fake.createErr = status.Error(codes.AlreadyExists, "duplicate id")
	fake.mu.Unlock()
	err := c.CreateConnector(context.Background(), Connector{ID: "g", Type: "google"})
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

func TestClient_UpdateRoundTrip(t *testing.T) {
	c, _ := newFakeClient(t)
	require.NoError(t, c.CreateConnector(context.Background(), Connector{
		ID: "g", Type: "google", Name: "old", Config: []byte(`{}`),
	}))
	require.NoError(t, c.UpdateConnector(context.Background(), Connector{
		ID: "g", Type: "google", Name: "new", Config: []byte(`{"x":1}`),
	}))
	got, err := c.ListConnectors(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "new", got[0].Name)
	assert.JSONEq(t, `{"x":1}`, string(got[0].Config))
}

func TestClient_UpdateNotFound(t *testing.T) {
	c, _ := newFakeClient(t)
	err := c.UpdateConnector(context.Background(), Connector{ID: "ghost", Type: "google"})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestClient_UpdateNotFoundViaCode(t *testing.T) {
	c, fake := newFakeClient(t)
	fake.mu.Lock()
	fake.updateErr = status.Error(codes.NotFound, "no such connector")
	fake.mu.Unlock()
	err := c.UpdateConnector(context.Background(), Connector{ID: "x", Type: "google"})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestClient_DeleteRoundTrip(t *testing.T) {
	c, _ := newFakeClient(t)
	require.NoError(t, c.CreateConnector(context.Background(), Connector{
		ID: "g", Type: "google",
	}))
	require.NoError(t, c.DeleteConnector(context.Background(), "g"))
	got, err := c.ListConnectors(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestClient_DeleteNotFound(t *testing.T) {
	c, _ := newFakeClient(t)
	err := c.DeleteConnector(context.Background(), "ghost")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestClient_HealthCheck(t *testing.T) {
	c, _ := newFakeClient(t)
	assert.NoError(t, c.HealthCheck(context.Background()))
}

func TestFromEnv_NoAddr(t *testing.T) {
	t.Setenv("OPENZRO_DEX_GRPC_ADDR", "")
	cfg, err := FromEnv()
	assert.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestFromEnv_NonLoopbackRequiresMTLS(t *testing.T) {
	t.Setenv("OPENZRO_DEX_GRPC_ADDR", "dex:5557")
	t.Setenv("OPENZRO_DEX_GRPC_CA_CERT", "")
	t.Setenv("OPENZRO_DEX_GRPC_CLIENT_CERT", "")
	t.Setenv("OPENZRO_DEX_GRPC_CLIENT_KEY", "")
	_, err := FromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mTLS")
}

func TestFromEnv_LoopbackAllowsPlaintext(t *testing.T) {
	t.Setenv("OPENZRO_DEX_GRPC_ADDR", "localhost:5557")
	t.Setenv("OPENZRO_DEX_GRPC_CA_CERT", "")
	t.Setenv("OPENZRO_DEX_GRPC_CLIENT_CERT", "")
	t.Setenv("OPENZRO_DEX_GRPC_CLIENT_KEY", "")
	cfg, err := FromEnv()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.InsecureNoTLS)
}

// TestFromEnv_InsecureOptInOverridesLoopbackCheck — operator-explicit
// opt-in to plaintext for non-loopback Dex (lab/smoke clusters where
// management + Dex live in the same namespace and mTLS is overkill).
func TestFromEnv_InsecureOptInOverridesLoopbackCheck(t *testing.T) {
	t.Setenv("OPENZRO_DEX_GRPC_ADDR", "openzro-dex:5557")
	t.Setenv("OPENZRO_DEX_GRPC_CA_CERT", "")
	t.Setenv("OPENZRO_DEX_GRPC_CLIENT_CERT", "")
	t.Setenv("OPENZRO_DEX_GRPC_CLIENT_KEY", "")
	t.Setenv("OPENZRO_DEX_GRPC_INSECURE", "true")
	cfg, err := FromEnv()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.InsecureNoTLS)
}

// TestFromEnv_InsecureOptInIsCaseInsensitive guards against operators
// setting OPENZRO_DEX_GRPC_INSECURE=True / TRUE in their helm values.
func TestFromEnv_InsecureOptInIsCaseInsensitive(t *testing.T) {
	for _, v := range []string{"true", "True", "TRUE"} {
		t.Setenv("OPENZRO_DEX_GRPC_ADDR", "openzro-dex:5557")
		t.Setenv("OPENZRO_DEX_GRPC_CA_CERT", "")
		t.Setenv("OPENZRO_DEX_GRPC_CLIENT_CERT", "")
		t.Setenv("OPENZRO_DEX_GRPC_CLIENT_KEY", "")
		t.Setenv("OPENZRO_DEX_GRPC_INSECURE", v)
		cfg, err := FromEnv()
		require.NoError(t, err, "value %q should be accepted", v)
		require.NotNil(t, cfg)
		assert.True(t, cfg.InsecureNoTLS)
	}
}

func TestServerNameFromAddr(t *testing.T) {
	cases := []struct{ in, want string }{
		{"dex:5557", "dex"},
		{"localhost:5557", "localhost"},
		{"127.0.0.1:5557", "127.0.0.1"},
		{"hostonly", "hostonly"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, serverNameFromAddr(c.in))
	}
}
