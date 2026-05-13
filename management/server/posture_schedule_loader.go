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
// ScheduleCheck. Filtering happens in Go rather than via a SQL
// `WHERE checks LIKE '%ScheduleCheck%'` because the checks column is
// GORM-JSON-serialised — substring matching is brittle, and per-row
// inspection is cheap at expected fleet sizes (typically < 1k
// posture-check records cluster-wide).
func (l *scheduleLoader) AccountsWithActiveSchedules(ctx context.Context) ([]string, error) {
	accounts := l.store.GetAllAccounts(ctx)
	out := make([]string, 0)
	seen := make(map[string]struct{}, len(accounts))
	for _, acc := range accounts {
		if acc == nil {
			continue
		}
		checks, err := l.store.GetAccountPostureChecks(ctx, store.LockingStrengthShare, acc.Id)
		if err != nil {
			// A single account's load failure shouldn't poison the
			// whole startup — log at the call site (cmd/management),
			// keep going.
			continue
		}
		for _, c := range checks {
			if c == nil || c.Checks.ScheduleCheck == nil {
				continue
			}
			if _, dup := seen[acc.Id]; dup {
				break
			}
			seen[acc.Id] = struct{}{}
			out = append(out, acc.Id)
			break
		}
	}
	return out, nil
}
