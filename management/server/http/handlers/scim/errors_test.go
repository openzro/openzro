package scim

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/openzro/openzro/management/server/status"
)

// Regression: typed status errors from the account manager were mapped by
// message text only, so InvalidArgument (e.g. an empty userName) surfaced
// as 500 — which IdPs treat as an outage and retry forever.
func TestMapErrorStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil", err: nil, want: http.StatusOK},
		{name: "invalid argument", err: status.Errorf(status.InvalidArgument, "userName is required"), want: http.StatusBadRequest},
		{name: "bad request", err: status.Errorf(status.BadRequest, "bad"), want: http.StatusBadRequest},
		{name: "precondition failed", err: status.Errorf(status.PreconditionFailed, "x"), want: http.StatusBadRequest},
		{name: "permission denied", err: status.NewPermissionDeniedError(), want: http.StatusForbidden},
		{name: "not found", err: status.Errorf(status.NotFound, "user x"), want: http.StatusNotFound},
		{name: "already exists", err: status.Errorf(status.AlreadyExists, "x"), want: http.StatusConflict},
		{name: "user already exists", err: status.Errorf(status.UserAlreadyExists, "x"), want: http.StatusConflict},
		{name: "unauthenticated", err: status.Errorf(status.Unauthenticated, "x"), want: http.StatusUnauthorized},
		{name: "internal", err: status.Errorf(status.Internal, "boom"), want: http.StatusInternalServerError},
		{name: "wrapped status error", err: fmt.Errorf("op: %w", status.Errorf(status.InvalidArgument, "x")), want: http.StatusBadRequest},
		// Plain sentinels from the server package keep their mapping.
		{name: "user sentinel conflict", err: errors.New("scim: user with this userName already exists"), want: http.StatusConflict},
		{name: "user sentinel not found", err: errors.New("scim: user not found"), want: http.StatusNotFound},
		{name: "unknown plain error", err: errors.New("boom"), want: http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapErrorStatus(tc.err); got != tc.want {
				t.Fatalf("mapErrorStatus(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}
