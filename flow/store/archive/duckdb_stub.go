//go:build !archive_duckdb

package archive

import (
	"context"
	"time"

	"github.com/openzro/openzro/flow/store"
)

// New is the stub returned when the binary is compiled without the
// archive_duckdb build tag. The federated wrapper checks for
// ErrUnavailable and falls back to hot-only behavior — same UX as
// the v0.53.x line before ADR-0012.
func New(_ Config) (store.Store, error) {
	return nil, ErrUnavailable
}

// stub is exported only so the test files compile in both build
// modes. It is never returned from New() in this build.
type stub struct{}

func (s *stub) Save(_ context.Context, _ []*store.Event) error { return nil }
func (s *stub) Query(_ context.Context, _ store.Filter) ([]*store.Event, error) {
	return nil, nil
}
func (s *stub) Purge(_ context.Context, _ time.Time) (int64, error) { return 0, nil }
func (s *stub) Close() error                                        { return nil }
