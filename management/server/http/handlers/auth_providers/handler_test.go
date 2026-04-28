package auth_providers

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	apiv2 "github.com/dexidp/dex/api/v2"
	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/dex_proxy"
	"github.com/openzro/openzro/management/server/permissions"
)

// fakeDex is a minimal in-memory Dex for handler tests. Lives
// here rather than importing dex_proxy's test fake so each
// package's tests stay independent.
type fakeDex struct {
	apiv2.UnimplementedDexServer
	mu         sync.Mutex
	connectors []*apiv2.Connector
}

func (f *fakeDex) ListConnectors(_ context.Context, _ *apiv2.ListConnectorReq) (*apiv2.ListConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &apiv2.ListConnectorResp{Connectors: append([]*apiv2.Connector(nil), f.connectors...)}, nil
}

func (f *fakeDex) CreateConnector(_ context.Context, req *apiv2.CreateConnectorReq) (*apiv2.CreateConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.connectors {
		if c.GetId() == req.GetConnector().GetId() {
			return &apiv2.CreateConnectorResp{AlreadyExists: true}, nil
		}
	}
	f.connectors = append(f.connectors, req.GetConnector())
	return &apiv2.CreateConnectorResp{}, nil
}

func (f *fakeDex) UpdateConnector(_ context.Context, req *apiv2.UpdateConnectorReq) (*apiv2.UpdateConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.connectors {
		if c.GetId() == req.GetId() {
			c.Type = req.GetNewType()
			c.Name = req.GetNewName()
			c.Config = req.GetNewConfig()
			return &apiv2.UpdateConnectorResp{}, nil
		}
	}
	return &apiv2.UpdateConnectorResp{NotFound: true}, nil
}

func (f *fakeDex) DeleteConnector(_ context.Context, req *apiv2.DeleteConnectorReq) (*apiv2.DeleteConnectorResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.connectors {
		if c.GetId() == req.GetId() {
			f.connectors = append(f.connectors[:i], f.connectors[i+1:]...)
			return &apiv2.DeleteConnectorResp{}, nil
		}
	}
	return &apiv2.DeleteConnectorResp{NotFound: true}, nil
}

type fixture struct {
	router *mux.Router
	dex    *fakeDex
}

func newFixture(t *testing.T, allowAdmin bool, withDex bool) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	perms := permissions.NewMockManager(ctrl)
	perms.EXPECT().
		ValidateUserPermissions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(allowAdmin, nil).
		AnyTimes()

	var client *dex_proxy.Client
	var fake *fakeDex
	if withDex {
		lis := bufconn.Listen(1024 * 1024)
		srv := grpc.NewServer()
		fake = &fakeDex{}
		apiv2.RegisterDexServer(srv, fake)
		go func() { _ = srv.Serve(lis) }()
		t.Cleanup(srv.Stop)

		conn, err := grpc.NewClient("passthrough://bufnet",
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		client = dex_proxy.NewWithConn(conn)
	}

	r := mux.NewRouter()
	AddEndpoints(perms, client, r)
	return &fixture{router: r, dex: fake}
}

func withAuth(req *http.Request) *http.Request {
	return nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
		AccountId: "test-acct", UserId: "test-user",
	})
}

func encodeBody(t *testing.T, b requestBody) *bytes.Buffer {
	t.Helper()
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	return bytes.NewBuffer(raw)
}

func validBody() requestBody {
	return requestBody{
		ID:     "google-acme",
		Type:   "google",
		Name:   "Acme Google",
		Config: json.RawMessage(`{"clientID":"x","clientSecret":"y","redirectURI":"https://app.example.com/dex/callback"}`),
	}
}

func TestList(t *testing.T) {
	f := newFixture(t, true, true)
	for _, name := range []string{"a", "b"} {
		body := validBody()
		body.ID = name
		req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encodeBody(t, body)))
		f.router.ServeHTTP(httptest.NewRecorder(), req)
	}
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodGet, "/admin/auth-providers", nil)))
	require.Equal(t, http.StatusOK, rr.Code)
	var rows []responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&rows))
	assert.Len(t, rows, 2)
}

func TestCreate(t *testing.T) {
	f := newFixture(t, true, true)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodPost,
		"/admin/auth-providers", encodeBody(t, validBody()))))
	require.Equal(t, http.StatusCreated, rr.Code, "body=%s", rr.Body.String())
	var got responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, "google-acme", got.ID)
	assert.Equal(t, "google", got.Type)
}

func TestCreate_DuplicateIDReturns409(t *testing.T) {
	f := newFixture(t, true, true)
	body := validBody()
	first := httptest.NewRecorder()
	f.router.ServeHTTP(first, withAuth(httptest.NewRequest(http.MethodPost,
		"/admin/auth-providers", encodeBody(t, body))))
	require.Equal(t, http.StatusCreated, first.Code)
	second := httptest.NewRecorder()
	f.router.ServeHTTP(second, withAuth(httptest.NewRequest(http.MethodPost,
		"/admin/auth-providers", encodeBody(t, body))))
	assert.Equal(t, http.StatusConflict, second.Code)
}

func TestCreate_RequiresAdmin(t *testing.T) {
	f := newFixture(t, false, true)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodPost,
		"/admin/auth-providers", encodeBody(t, validBody()))))
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestCreate_RejectsMissingFields(t *testing.T) {
	f := newFixture(t, true, true)
	cases := []struct {
		name string
		raw  string
	}{
		{"missing-id", `{"type":"google","config":{"x":1}}`},
		{"missing-type", `{"id":"g","config":{"x":1}}`},
		{"missing-config", `{"id":"g","type":"google"}`},
		{"null-config", `{"id":"g","type":"google","config":null}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			f.router.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodPost,
				"/admin/auth-providers", bytes.NewBufferString(c.raw))))
			assert.Equal(t, http.StatusBadRequest, rr.Code, "body=%s", rr.Body.String())
		})
	}
}

func TestUpdate(t *testing.T) {
	f := newFixture(t, true, true)
	create := httptest.NewRecorder()
	f.router.ServeHTTP(create, withAuth(httptest.NewRequest(http.MethodPost,
		"/admin/auth-providers", encodeBody(t, validBody()))))
	require.Equal(t, http.StatusCreated, create.Code)

	updated := validBody()
	updated.Name = "renamed"
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodPut,
		"/admin/auth-providers/google-acme", encodeBody(t, updated))))
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
	var got responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, "renamed", got.Name)
}

func TestUpdate_NotFound(t *testing.T) {
	f := newFixture(t, true, true)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodPut,
		"/admin/auth-providers/ghost", encodeBody(t, validBody()))))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDelete(t *testing.T) {
	f := newFixture(t, true, true)
	create := httptest.NewRecorder()
	f.router.ServeHTTP(create, withAuth(httptest.NewRequest(http.MethodPost,
		"/admin/auth-providers", encodeBody(t, validBody()))))
	require.Equal(t, http.StatusCreated, create.Code)

	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodDelete,
		"/admin/auth-providers/google-acme", nil)))
	assert.Equal(t, http.StatusNoContent, rr.Code)

	list := httptest.NewRecorder()
	f.router.ServeHTTP(list, withAuth(httptest.NewRequest(http.MethodGet, "/admin/auth-providers", nil)))
	var rows []responseBody
	require.NoError(t, json.NewDecoder(list.Body).Decode(&rows))
	assert.Empty(t, rows)
}

func TestDelete_NotFound(t *testing.T) {
	f := newFixture(t, true, true)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodDelete,
		"/admin/auth-providers/ghost", nil)))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestNoDex_Returns503(t *testing.T) {
	// Routes are registered but the handler returns 503 when
	// the dex client is nil. The dashboard renders an "IdP not
	// configured" empty state from this signal.
	f := newFixture(t, true, false)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodGet, "/admin/auth-providers", nil)))
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}
