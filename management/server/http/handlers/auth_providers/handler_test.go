package auth_providers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/auth/providers"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/permissions"
)

// recordingEmitter captures every emit call so tests assert the
// audit-stream side-effect.
type recordingEmitter struct {
	mu     sync.Mutex
	events []emittedEvent
}

type emittedEvent struct {
	initiator, target, account string
	code                       activity.Activity
	meta                       map[string]any
}

func (r *recordingEmitter) emit(_ context.Context, initiator, target, account string,
	code activity.Activity, meta map[string]any,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, emittedEvent{initiator, target, account, code, meta})
}

type fixture struct {
	router   *mux.Router
	store    *providers.Store
	manager  *providers.Manager
	perms    *permissions.MockManager
	recorder *recordingEmitter
}

func newFixture(t *testing.T, allowAdmin bool) *fixture {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/test.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	store, err := providers.NewStore(db, base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)

	mgr := providers.NewManager(store, "http://127.0.0.1/auth/callback")
	_, err = mgr.Refresh(context.Background())
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mockPerms := permissions.NewMockManager(ctrl)
	mockPerms.EXPECT().
		ValidateUserPermissions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(allowAdmin, nil).
		AnyTimes()

	rec := &recordingEmitter{}
	router := mux.NewRouter()
	AddEndpoints(mockPerms, store, mgr, rec.emit, router)
	return &fixture{router: router, store: store, manager: mgr, perms: mockPerms, recorder: rec}
}

// withAuth attaches a UserAuth to the request context so the
// handler's auth() check passes once permissions has approved.
func withAuth(req *http.Request) *http.Request {
	return nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
		AccountId: "test-acct", UserId: "test-user",
	})
}

func validBody() requestBody {
	enabled := true
	return requestBody{
		Name:    "prod-zitadel",
		Type:    providers.TypeZitadel,
		Enabled: &enabled,
		Config: providers.Config{
			IssuerURL:             "http://127.0.0.1:1/never-listening",
			ClientID:              "client-uuid",
			ClientSecret:          "super-secret",
			AuthorizationEndpoint: "https://example.com/oauth/authorize",
			TokenEndpoint:         "https://example.com/oauth/token",
		},
		BrandLabel: "Acme SSO",
	}
}

func encode(t *testing.T, b requestBody) *bytes.Buffer {
	t.Helper()
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	return bytes.NewBuffer(raw)
}

// TestCreate exercises the happy path: admin posts a new
// provider, server persists it, refreshes the manager, and
// returns 201 with the redacted projection.
func TestCreate(t *testing.T) {
	f := newFixture(t, true)

	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, validBody())))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code, "body=%s", rr.Body.String())
	var got responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.NotZero(t, got.ID)
	assert.Equal(t, "prod-zitadel", got.Name)
	assert.True(t, got.Enabled)
	assert.NotContains(t, string(got.Config), "super-secret",
		"client_secret must not appear in the response")
}

func TestCreate_RequiresAdmin(t *testing.T) {
	f := newFixture(t, false)

	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, validBody())))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestCreate_RejectsInvalidJSON(t *testing.T) {
	f := newFixture(t, true)

	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers",
		bytes.NewBufferString("{ not json")))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreate_RejectsMissingClientSecret(t *testing.T) {
	f := newFixture(t, true)

	bad := validBody()
	bad.Config.ClientSecret = ""
	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, bad)))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestList(t *testing.T) {
	f := newFixture(t, true)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		body := validBody()
		body.Name = name
		req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, body)))
		f.router.ServeHTTP(httptest.NewRecorder(), req)
	}

	req := withAuth(httptest.NewRequest(http.MethodGet, "/admin/auth-providers", nil))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var rows []responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&rows))
	assert.Len(t, rows, 3)
}

func TestGet(t *testing.T) {
	f := newFixture(t, true)

	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, validBody())))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var created responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))

	req = withAuth(httptest.NewRequest(http.MethodGet, "/admin/auth-providers/"+strconv.FormatUint(created.ID, 10), nil))
	rr = httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var got responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "prod-zitadel", got.Name)
}

func TestGet_NotFound(t *testing.T) {
	f := newFixture(t, true)
	req := withAuth(httptest.NewRequest(http.MethodGet, "/admin/auth-providers/9999", nil))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGet_InvalidID(t *testing.T) {
	f := newFixture(t, true)
	req := withAuth(httptest.NewRequest(http.MethodGet, "/admin/auth-providers/not-a-number", nil))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUpdate(t *testing.T) {
	f := newFixture(t, true)

	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, validBody())))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var created responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))

	updated := validBody()
	updated.Name = "renamed"
	disabled := false
	updated.Enabled = &disabled

	req = withAuth(httptest.NewRequest(http.MethodPut,
		"/admin/auth-providers/"+strconv.FormatUint(created.ID, 10),
		encode(t, updated)))
	rr = httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "body=%s", rr.Body.String())
	var got responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "renamed", got.Name)
	assert.False(t, got.Enabled)
	assert.Equal(t, created.CreatedAt, got.CreatedAt,
		"update must not move CreatedAt")
}

func TestUpdate_NotFound(t *testing.T) {
	f := newFixture(t, true)
	req := withAuth(httptest.NewRequest(http.MethodPut, "/admin/auth-providers/9999",
		encode(t, validBody())))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDelete(t *testing.T) {
	f := newFixture(t, true)

	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, validBody())))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var created responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))

	req = withAuth(httptest.NewRequest(http.MethodDelete,
		"/admin/auth-providers/"+strconv.FormatUint(created.ID, 10), nil))
	rr = httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)

	// Confirm the row is gone.
	req = withAuth(httptest.NewRequest(http.MethodGet,
		"/admin/auth-providers/"+strconv.FormatUint(created.ID, 10), nil))
	rr = httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDelete_NotFound(t *testing.T) {
	f := newFixture(t, true)
	req := withAuth(httptest.NewRequest(http.MethodDelete, "/admin/auth-providers/9999", nil))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestList_RequiresAuth(t *testing.T) {
	// Request without UserAuth in context — handler's auth()
	// returns the "user auth not in context" error before the
	// permissions stub is consulted.
	f := newFixture(t, true)
	req := httptest.NewRequest(http.MethodGet, "/admin/auth-providers", nil)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	assert.NotEqual(t, http.StatusOK, rr.Code,
		"missing UserAuth must not produce a 200")
}

func TestEmits_CreateUpdateDelete(t *testing.T) {
	f := newFixture(t, true)

	// Create -> AuthProviderCreated
	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, validBody())))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var created responseBody
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))
	require.Len(t, f.recorder.events, 1)
	assert.Equal(t, activity.AuthProviderCreated, f.recorder.events[0].code)
	assert.Equal(t, "test-user", f.recorder.events[0].initiator)
	assert.Equal(t, "test-acct", f.recorder.events[0].account)
	assert.Equal(t, strconv.FormatUint(created.ID, 10), f.recorder.events[0].target)
	assert.Equal(t, created.Name, f.recorder.events[0].meta["name"])
	assert.Equal(t, string(created.Type), f.recorder.events[0].meta["type"])

	// Update -> AuthProviderUpdated
	updated := validBody()
	updated.Name = "renamed"
	req = withAuth(httptest.NewRequest(http.MethodPut,
		"/admin/auth-providers/"+strconv.FormatUint(created.ID, 10), encode(t, updated)))
	rr = httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, f.recorder.events, 2)
	assert.Equal(t, activity.AuthProviderUpdated, f.recorder.events[1].code)
	assert.Equal(t, "renamed", f.recorder.events[1].meta["name"])

	// Delete -> AuthProviderDeleted (meta from pre-delete snapshot)
	req = withAuth(httptest.NewRequest(http.MethodDelete,
		"/admin/auth-providers/"+strconv.FormatUint(created.ID, 10), nil))
	rr = httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Len(t, f.recorder.events, 3)
	assert.Equal(t, activity.AuthProviderDeleted, f.recorder.events[2].code)
	assert.Equal(t, "renamed", f.recorder.events[2].meta["name"],
		"delete must capture pre-delete name in audit meta")
}

func TestEmits_SkippedOnPermissionDenied(t *testing.T) {
	f := newFixture(t, false)

	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, validBody())))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)
	assert.Empty(t, f.recorder.events,
		"forbidden requests must not produce audit events")
}

func TestEmits_NilEmitter(t *testing.T) {
	// Wiring with nil emitter must not panic.
	dsn := "file:" + t.TempDir() + "/test.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	store, err := providers.NewStore(db, base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	mgr := providers.NewManager(store, "http://127.0.0.1/auth/callback")
	_, err = mgr.Refresh(context.Background())
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	perms := permissions.NewMockManager(ctrl)
	perms.EXPECT().ValidateUserPermissions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(true, nil).AnyTimes()

	router := mux.NewRouter()
	AddEndpoints(perms, store, mgr, nil, router)

	req := withAuth(httptest.NewRequest(http.MethodPost, "/admin/auth-providers", encode(t, validBody())))
	rr := httptest.NewRecorder()
	require.NotPanics(t, func() { router.ServeHTTP(rr, req) })
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestAddEndpoints_NilStore(t *testing.T) {
	// Defensive: AddEndpoints should be a no-op when wired with
	// nil store/manager — allows callers to keep the auth
	// providers feature off without a panic.
	router := mux.NewRouter()
	AddEndpoints(nil, nil, nil, nil, router)
	req := httptest.NewRequest(http.MethodGet, "/admin/auth-providers", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code,
		"nil store/manager should leave the route unregistered")
}
