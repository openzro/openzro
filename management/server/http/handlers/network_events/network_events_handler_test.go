package network_events

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/permissions/roles"
	"github.com/openzro/openzro/management/server/types"
)

// fakeStore captures every Query call for assertions and returns a
// canned set of events back. A new instance per test keeps state
// hermetic.
type fakeStore struct {
	gotFilter store.Filter
	out       []*store.Event
	err       error
}

func (f *fakeStore) Save(context.Context, []*store.Event) error      { return nil }
func (f *fakeStore) Purge(context.Context, time.Time) (int64, error) { return 0, nil }
func (f *fakeStore) Close() error                                    { return nil }
func (f *fakeStore) Query(_ context.Context, fl store.Filter) ([]*store.Event, error) {
	f.gotFilter = fl
	return f.out, f.err
}
func (f *fakeStore) ResolvedAddressesForResources(context.Context, string, []string, time.Time) (map[string][]string, error) {
	return map[string][]string{}, nil
}

// fakePermissions implements permissions.Manager with a single-knob
// "allow everything" / "deny everything" flag. The other interface
// methods exist only to satisfy the type — the handler never calls
// them.
type fakePermissions struct {
	allow bool
}

func (f fakePermissions) ValidateUserPermissions(context.Context, string, string, modules.Module, operations.Operation) (bool, error) {
	return f.allow, nil
}
func (fakePermissions) ValidateRoleModuleAccess(context.Context, string, roles.RolePermissions, modules.Module, operations.Operation) bool {
	return false
}
func (fakePermissions) ValidateAccountAccess(context.Context, string, *types.User, bool) error {
	return nil
}
func (fakePermissions) GetPermissionsByRole(context.Context, types.UserRole) (roles.Permissions, error) {
	return roles.Permissions{}, nil
}

func newServer(allow bool, fs store.Store) http.Handler {
	router := mux.NewRouter()
	AddEndpoints(fakePermissions{allow: allow}, fs, router)
	return withAuth("acct-1", "user-1", router)
}

func withAuth(accountID, userID string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := nbcontext.SetUserAuthInContext(r.Context(), nbcontext.UserAuth{
			AccountId: accountID,
			UserId:    userID,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestList_NoStore_ReturnsEmpty200(t *testing.T) {
	srv := newServer(true,nil)

	req := httptest.NewRequest(http.MethodGet, "/network-traffic-events", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code,
		"engine=none must surface as empty 200, not 503 or 404")
	var got response
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Empty(t, got.Events)
}

func TestList_PermissionDenied(t *testing.T) {
	fs := &fakeStore{}
	srv := newServer(false,fs)

	req := httptest.NewRequest(http.MethodGet, "/network-traffic-events", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, store.Filter{}, fs.gotFilter,
		"store must not be hit when permission check fails")
}

func TestList_AccountIDComesFromAuthContextOnly(t *testing.T) {
	fs := &fakeStore{}
	srv := newServer(true,fs)

	// Even if a malicious caller passes account_id in the URL, it is
	// ignored — AccountID always wins from the auth context. This is
	// the same lesson as CWE-639 we already mitigated in the JWT
	// middleware (commit baseline pre-fork).
	req := httptest.NewRequest(http.MethodGet,
		"/network-traffic-events?account_id=other-acct", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "acct-1", fs.gotFilter.AccountID,
		"account isolation: filter must use auth-context account, not URL param")
}

func TestList_ParsesAllEightFilters(t *testing.T) {
	fs := &fakeStore{}
	srv := newServer(true,fs)

	q := "?peer_id=p-1" +
		"&source_ip=10.0.0.1" +
		"&dest_ip=10.0.0.2" +
		"&source_port=49152" +
		"&dest_port=443" +
		"&protocol=6" +
		"&type=start" +
		"&direction=egress" +
		"&rule_id=" + hex.EncodeToString([]byte("rule-A")) +
		"&since=2026-04-01T00:00:00Z" +
		"&until=2026-04-30T00:00:00Z" +
		"&limit=250&offset=10"

	req := httptest.NewRequest(http.MethodGet, "/network-traffic-events"+q, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	got := fs.gotFilter
	assert.Equal(t, "p-1", got.PeerID)
	assert.Equal(t, "10.0.0.1", got.SourceIP)
	assert.Equal(t, "10.0.0.2", got.DestIP)
	require.NotNil(t, got.SourcePort)
	assert.Equal(t, uint32(49152), *got.SourcePort)
	require.NotNil(t, got.DestPort)
	assert.Equal(t, uint32(443), *got.DestPort)
	require.NotNil(t, got.Protocol)
	assert.Equal(t, uint16(6), *got.Protocol)
	require.NotNil(t, got.Type)
	assert.Equal(t, store.EventTypeStart, *got.Type)
	require.NotNil(t, got.Direction)
	assert.Equal(t, store.DirectionEgress, *got.Direction)
	assert.Equal(t, []byte("rule-A"), got.RuleID)
	assert.Equal(t, "2026-04-01T00:00:00Z", got.Since.Format(time.RFC3339))
	assert.Equal(t, "2026-04-30T00:00:00Z", got.Until.Format(time.RFC3339))
	assert.Equal(t, 250, got.Limit)
	assert.Equal(t, 10, got.Offset)
}

func TestList_LimitCappedAtMax(t *testing.T) {
	fs := &fakeStore{}
	srv := newServer(true,fs)

	req := httptest.NewRequest(http.MethodGet,
		"/network-traffic-events?limit=999999", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, maxLimit, fs.gotFilter.Limit,
		"limit must be capped at %d (mirrors NetBird upstream)", maxLimit)
}

func TestList_RejectsInvalidQueryParams(t *testing.T) {
	fs := &fakeStore{}
	srv := newServer(true,fs)

	cases := []string{
		"?protocol=not-a-number",
		"?type=garbage",
		"?direction=garbage",
		"?source_port=-1",
		"?since=not-a-date",
		"?rule_id=zz-not-hex",
		"?limit=-1",
	}
	for _, q := range cases {
		req := httptest.NewRequest(http.MethodGet, "/network-traffic-events"+q, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code,
			"query %q must be rejected with 400", q)
	}
}

func TestList_RendersEventDTO(t *testing.T) {
	fs := &fakeStore{
		out: []*store.Event{
			{
				EventID:     []byte{0xde, 0xad, 0xbe, 0xef},
				PeerID:      "p-1",
				IsInitiator: true,
				OccurredAt:  time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
				ReceivedAt:  time.Date(2026, 4, 26, 12, 0, 1, 0, time.UTC),
				Type:        store.EventTypeStart,
				Direction:   store.DirectionEgress,
				Protocol:    6,
				SourceIP:    "10.0.0.1",
				DestIP:      "10.0.0.2",
				SourcePort:  49152,
				DestPort:    443,
				RxBytes:     100,
				TxBytes:     200,
				RuleID:      []byte("rule-allow"),
			},
		},
	}
	srv := newServer(true,fs)

	req := httptest.NewRequest(http.MethodGet, "/network-traffic-events", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var got response
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got.Events, 1)

	e := got.Events[0]
	assert.Equal(t, "deadbeef", e.EventID, "binary byte fields must be hex-encoded on the wire")
	assert.Equal(t, "start", e.Type)
	assert.Equal(t, "egress", e.Direction)
	assert.Equal(t, uint16(6), e.Protocol)
	assert.Equal(t, "10.0.0.1", e.SourceIP)
	assert.Equal(t, uint32(49152), e.SourcePort)
	// RuleID carries an xid string in printable ASCII bytes — decodeIDBytes
	// returns it as UTF-8 (not hex) so the dashboard's policyByID lookup
	// keys the same xid the API exposes elsewhere. Binary RuleID values
	// (older agents) round-trip via hex; that path is exercised by other
	// cases.
	assert.Equal(t, "rule-allow", e.RuleID)
}
