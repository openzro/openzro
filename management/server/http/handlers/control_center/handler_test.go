package control_center

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/controlcenter"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/permissions"
)

func newFixture(t *testing.T, allowAdmin bool, graphFn func(ctx context.Context, accountID, view, focusID string) (*controlcenter.GraphDTO, error)) *mux.Router {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	perms := permissions.NewMockManager(ctrl)
	perms.EXPECT().
		ValidateUserPermissions(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(allowAdmin, nil).
		AnyTimes()

	am := &mock_server.MockAccountManager{GetAccessGraphFunc: graphFn}

	r := mux.NewRouter()
	AddEndpoints(am, perms, r)
	return r
}

func withAuth(req *http.Request) *http.Request {
	return nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
		AccountId: "acct1", UserId: "u1",
	})
}

func TestControlCenter_HappyPath(t *testing.T) {
	var gotAccount, gotView, gotID string
	r := newFixture(t, true, func(_ context.Context, accountID, view, focusID string) (*controlcenter.GraphDTO, error) {
		gotAccount, gotView, gotID = accountID, view, focusID
		return &controlcenter.GraphDTO{
			Focus: controlcenter.Focus{Type: controlcenter.FocusPeer, ID: "p1"},
			Nodes: []controlcenter.Node{{ID: "p1", Kind: controlcenter.NodeFocus, Label: "alice"}},
		}, nil
	})

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodGet, "/control-center/peer/p1", nil)))

	require.Equal(t, http.StatusOK, rr.Code)
	// accountID is the caller's auth account, not a path param (tenant scoping).
	require.Equal(t, "acct1", gotAccount)
	require.Equal(t, "peer", gotView)
	require.Equal(t, "p1", gotID)

	var g controlcenter.GraphDTO
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &g))
	require.Equal(t, controlcenter.FocusPeer, g.Focus.Type)
}

func TestControlCenter_ForbiddenWhenNotAdmin(t *testing.T) {
	r := newFixture(t, false, func(context.Context, string, string, string) (*controlcenter.GraphDTO, error) {
		t.Fatal("manager must not be reached when RBAC denies")
		return nil, nil //nolint:nilnil // unreachable: t.Fatal above stops the goroutine; the return only satisfies the signature.
	})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodGet, "/control-center/peer/p1", nil)))
	require.Equal(t, http.StatusForbidden, rr.Code)
}

func TestControlCenter_BadView(t *testing.T) {
	r := newFixture(t, true, func(context.Context, string, string, string) (*controlcenter.GraphDTO, error) {
		t.Fatal("manager must not be reached for an invalid view")
		return nil, nil //nolint:nilnil // unreachable: t.Fatal above stops the goroutine; the return only satisfies the signature.
	})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodGet, "/control-center/bogus/x", nil)))
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestControlCenter_UnknownFocusIs404(t *testing.T) {
	r := newFixture(t, true, func(context.Context, string, string, string) (*controlcenter.GraphDTO, error) {
		return nil, fmt.Errorf("focus peer %q: %w", "ghost", controlcenter.ErrFocusNotFound)
	})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodGet, "/control-center/peer/ghost", nil)))
	require.Equal(t, http.StatusNotFound, rr.Code)
}

// Finding 5: a generic (non-typed, non-status) error must NOT be
// silently reported as 404 — that trap is now closed.
func TestControlCenter_GenericErrorIsNot404(t *testing.T) {
	r := newFixture(t, true, func(context.Context, string, string, string) (*controlcenter.GraphDTO, error) {
		return nil, fmt.Errorf("database exploded")
	})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, withAuth(httptest.NewRequest(http.MethodGet, "/control-center/peer/p1", nil)))
	require.NotEqual(t, http.StatusNotFound, rr.Code)
	require.GreaterOrEqual(t, rr.Code, 500)
}

func TestControlCenter_Unauthenticated(t *testing.T) {
	r := newFixture(t, true, func(context.Context, string, string, string) (*controlcenter.GraphDTO, error) {
		// Handler must short-circuit on missing UserAuth before reaching
		// the manager; this stub return is here only to satisfy the
		// signature.
		return nil, nil //nolint:nilnil // see comment above.
	})
	rr := httptest.NewRecorder()
	// no withAuth → no UserAuth in context.
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/control-center/peer/p1", nil))
	require.NotEqual(t, http.StatusOK, rr.Code)
}
