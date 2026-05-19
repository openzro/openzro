package server

import (
	"context"

	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/store"
)

// scheduleLoader adapts the management Store to the
// posture.ScheduleLoader interface used by the per-account scheduler
// goroutines. It owns no state beyond the underlying store handle.
//
// Defined in package server (not posture) so the dependency arrow goes
// server → posture, not the other way around — keeps the posture
// package importable from any side without circular paths.
type scheduleLoader struct {
	store store.Store
}

// NewScheduleLoader is wired in cmd/management.go right after the
// store is constructed and handed to the posture.Scheduler.
func NewScheduleLoader(s store.Store) posture.ScheduleLoader {
	return &scheduleLoader{store: s}
}

// LoadActiveSchedules returns every posture.ScheduleCheck attached to
// the account regardless of policy attachment. The scheduler is
// conservative: a wake for a detached check is a cheap no-op
// downstream (UpdateAccountPeers diffs network maps and skips updates
// when nothing changed), and re-attach is the more frequent case.
func (l *scheduleLoader) LoadActiveSchedules(ctx context.Context, accountID string) ([]posture.ScheduleCheck, error) {
	checks, err := l.store.GetAccountPostureChecks(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]posture.ScheduleCheck, 0, len(checks))
	for _, c := range checks {
		if c == nil || c.Checks.ScheduleCheck == nil {
			continue
		}
		out = append(out, *c.Checks.ScheduleCheck)
	}
	return out, nil
}

// AccountsWithActiveSchedules returns the IDs of every account that
// currently owns at least one posture-check record containing a
// ScheduleCheck.
//
// One query (GetAllPostureChecks) instead of the previous N+1
// (GetAllAccounts followed by a GetAccountPostureChecks per account).
// At thousands of accounts the old shape meant thousands of
// sequential round-trips on every scheduler discovery pass; this is
// a single round-trip plus an in-Go filter.
//
// Filtering stays in Go rather than a SQL `WHERE checks LIKE
// '%ScheduleCheck%'` because the checks column is GORM-JSON-
// serialized — substring matching is brittle and not portable across
// the SQLite/Postgres/MySQL dialects the store supports. The
// cluster-wide posture-check row count is small, so deserialise +
// scan in memory is the right trade.
func (l *scheduleLoader) AccountsWithActiveSchedules(ctx context.Context) ([]string, error) {
	checks, err := l.store.GetAllPostureChecks(ctx, store.LockingStrengthShare)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0)
	seen := make(map[string]struct{})
	for _, c := range checks {
		if c == nil || c.Checks.ScheduleCheck == nil {
			continue
		}
		if c.AccountID == "" {
			continue
		}
		if _, dup := seen[c.AccountID]; dup {
			continue
		}
		seen[c.AccountID] = struct{}{}
		out = append(out, c.AccountID)
	}
	return out, nil
}
