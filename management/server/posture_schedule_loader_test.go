package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/store"
)

// seedAccount persists a bare parent account so SavePostureChecks
// satisfies the posture_checks → accounts FK
// (fk_accounts_posture_checks). These loader tests start from an
// empty store and used raw account-id strings that were never
// created — that only "worked" on SQLite (FKs off by default); on
// Postgres/MySQL it violated the constraint (SQLSTATE 23503) and
// failed CI. Same root-cause class as the store-layer fix in #66.
func seedAccount(t testing.TB, st store.Store, id string) {
	t.Helper()
	require.NoError(t, st.SaveAccount(context.Background(),
		newAccountWithId(context.Background(), id, id+"-user", "", false)))
}

// TestScheduleLoader_AccountsWithActiveSchedules covers the N+1 fix:
// AccountsWithActiveSchedules must return exactly the set of accounts
// owning at least one posture check with a ScheduleCheck, deduped,
// using the single GetAllPostureChecks query rather than a per-account
// loop.
func TestScheduleLoader_AccountsWithActiveSchedules(t *testing.T) {
	st, err := createStore(t)
	require.NoError(t, err)
	ctx := context.Background()

	accountSched1 := "acct-sched-1"
	accountSched2 := "acct-sched-2"
	accountNoSched := "acct-no-sched"
	seedAccount(t, st, accountSched1)
	seedAccount(t, st, accountSched2)
	seedAccount(t, st, accountNoSched)

	seed := []*posture.Checks{
		{
			ID:        "pc-1",
			AccountID: accountSched1,
			Checks: posture.ChecksDefinition{
				ScheduleCheck: &posture.ScheduleCheck{
					Window:   posture.TimeWindow{StartTime: "09:00", EndTime: "17:00"},
					Timezone: "America/New_York",
				},
			},
		},
		{
			// Second schedule-bearing check for the SAME account —
			// must not produce a duplicate account ID.
			ID:        "pc-2",
			AccountID: accountSched1,
			Checks: posture.ChecksDefinition{
				ScheduleCheck: &posture.ScheduleCheck{
					Window:   posture.TimeWindow{StartTime: "20:00", EndTime: "23:00"},
					Timezone: "America/New_York",
				},
			},
		},
		{
			ID:        "pc-3",
			AccountID: accountSched2,
			Checks: posture.ChecksDefinition{
				ScheduleCheck: &posture.ScheduleCheck{
					Window:   posture.TimeWindow{StartTime: "00:00", EndTime: "06:00"},
					Timezone: "Europe/Berlin",
				},
			},
		},
		{
			// No ScheduleCheck → account must NOT appear.
			ID:        "pc-4",
			AccountID: accountNoSched,
			Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "0.31.0"},
			},
		},
	}
	for _, c := range seed {
		require.NoError(t, st.SavePostureChecks(ctx, store.LockingStrengthUpdate, c))
	}

	loader := NewScheduleLoader(st)
	accounts, err := loader.AccountsWithActiveSchedules(ctx)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{accountSched1, accountSched2}, accounts,
		"only accounts with a ScheduleCheck, deduped, no account-without-schedule")
}

func TestScheduleLoader_AccountsWithActiveSchedules_EmptyStore(t *testing.T) {
	st, err := createStore(t)
	require.NoError(t, err)

	loader := NewScheduleLoader(st)
	accounts, err := loader.AccountsWithActiveSchedules(context.Background())
	require.NoError(t, err)
	assert.Empty(t, accounts, "no posture checks → no accounts, not an error")
}

// TestScheduleLoader_LoadActiveSchedules sanity-checks the sibling
// per-account loader still works alongside the rewritten
// AccountsWithActiveSchedules (they share the same store).
func TestScheduleLoader_LoadActiveSchedules(t *testing.T) {
	st, err := createStore(t)
	require.NoError(t, err)
	ctx := context.Background()

	const accountID = "acct-x"
	seedAccount(t, st, accountID)
	require.NoError(t, st.SavePostureChecks(ctx, store.LockingStrengthUpdate, &posture.Checks{
		ID:        "pc-sched",
		AccountID: accountID,
		Checks: posture.ChecksDefinition{
			ScheduleCheck: &posture.ScheduleCheck{
				Window:   posture.TimeWindow{StartTime: "08:00", EndTime: "12:00"},
				Timezone: "America/New_York",
			},
		},
	}))
	require.NoError(t, st.SavePostureChecks(ctx, store.LockingStrengthUpdate, &posture.Checks{
		ID:        "pc-nbver",
		AccountID: accountID,
		Checks: posture.ChecksDefinition{
			NBVersionCheck: &posture.NBVersionCheck{MinVersion: "0.31.0"},
		},
	}))

	loader := NewScheduleLoader(st)
	scheds, err := loader.LoadActiveSchedules(ctx, accountID)
	require.NoError(t, err)
	require.Len(t, scheds, 1, "only the ScheduleCheck-bearing record yields a schedule")
	assert.Equal(t, "America/New_York", scheds[0].Timezone)
}
