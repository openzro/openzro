package posture

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextBoundary_NoChecks(t *testing.T) {
	t.Parallel()

	_, ok := NextBoundary(time.Now(), nil)
	assert.False(t, ok, "no checks → no boundary")

	_, ok = NextBoundary(time.Now(), []ScheduleCheck{})
	assert.False(t, ok, "empty slice → no boundary")
}

func TestNextBoundary_SameDayWindow(t *testing.T) {
	t.Parallel()

	const layout = "2006-01-02 15:04"
	utc, _ := time.LoadLocation("UTC")

	check := ScheduleCheck{
		Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
		Action: CheckActionAllow,
	}

	tests := []struct {
		name     string
		now      string
		wantHHMM string // expected boundary time in HH:MM UTC
	}{
		{
			name:     "before opens → opens at 09:00 today",
			now:      "2026-05-13 07:00",
			wantHHMM: "09:00",
		},
		{
			name:     "in window → next boundary is 18:00 today",
			now:      "2026-05-13 12:00",
			wantHHMM: "18:00",
		},
		{
			name:     "after close → opens at 09:00 tomorrow",
			now:      "2026-05-13 19:00",
			wantHHMM: "09:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := mustTime(t, layout, tt.now, "UTC")
			b, ok := NextBoundary(now, []ScheduleCheck{check})
			require.True(t, ok)
			require.Equal(t, utc, b.Location())
			assert.True(t, b.After(now), "boundary must be in the future")
			gotHHMM := b.Format("15:04")
			assert.Equal(t, tt.wantHHMM, gotHHMM)
		})
	}
}

func TestNextBoundary_DayFilter(t *testing.T) {
	t.Parallel()

	const layout = "2006-01-02 15:04"

	// Mon-Fri 09:00-18:00. From Saturday afternoon the next start
	// boundary is Monday 09:00.
	check := ScheduleCheck{
		Window: TimeWindow{DaysOfWeek: []int{1, 2, 3, 4, 5}, StartTime: "09:00", EndTime: "18:00"},
		Action: CheckActionAllow,
	}

	now := mustTime(t, layout, "2026-05-16 15:00", "UTC") // Saturday
	b, ok := NextBoundary(now, []ScheduleCheck{check})
	require.True(t, ok)
	assert.Equal(t, "2026-05-18 09:00", b.Format(layout), "next boundary should be Monday 09:00")
}

func TestNextBoundary_WrapMidnight(t *testing.T) {
	t.Parallel()

	const layout = "2006-01-02 15:04"

	check := ScheduleCheck{
		Window: TimeWindow{StartTime: "22:00", EndTime: "06:00"},
		Action: CheckActionDeny,
	}

	// At 23:00 we're inside the wrapping window; the next boundary
	// is the close at 06:00 tomorrow.
	now := mustTime(t, layout, "2026-05-13 23:00", "UTC")
	b, ok := NextBoundary(now, []ScheduleCheck{check})
	require.True(t, ok)
	assert.Equal(t, "2026-05-14 06:00", b.Format(layout))

	// At 03:00 we're still in the previous-day's wrap. Next boundary
	// is 06:00 today.
	now2 := mustTime(t, layout, "2026-05-13 03:00", "UTC")
	b2, ok := NextBoundary(now2, []ScheduleCheck{check})
	require.True(t, ok)
	assert.Equal(t, "2026-05-13 06:00", b2.Format(layout))

	// At 12:00 (between cycles) the next boundary is the next start
	// at 22:00 today.
	now3 := mustTime(t, layout, "2026-05-13 12:00", "UTC")
	b3, ok := NextBoundary(now3, []ScheduleCheck{check})
	require.True(t, ok)
	assert.Equal(t, "2026-05-13 22:00", b3.Format(layout))
}

func TestNextBoundary_MultipleChecks_SoonestWins(t *testing.T) {
	t.Parallel()

	const layout = "2006-01-02 15:04"

	// One check fires at 12:00; another fires at 14:00. Soonest must
	// win regardless of slice order.
	checks := []ScheduleCheck{
		{Window: TimeWindow{StartTime: "14:00", EndTime: "18:00"}, Action: CheckActionAllow},
		{Window: TimeWindow{StartTime: "12:00", EndTime: "13:00"}, Action: CheckActionDeny},
	}

	now := mustTime(t, layout, "2026-05-13 10:00", "UTC")
	b, ok := NextBoundary(now, checks)
	require.True(t, ok)
	assert.Equal(t, "2026-05-13 12:00", b.Format(layout), "12:00 boundary must win over 14:00")
}

func TestNextBoundary_TimezoneRespected(t *testing.T) {
	t.Parallel()

	const layout = "2006-01-02 15:04"

	// Same window in São Paulo (UTC-3) means 09:00 local == 12:00 UTC.
	check := ScheduleCheck{
		Window:   TimeWindow{StartTime: "09:00", EndTime: "18:00"},
		Timezone: "America/Sao_Paulo",
		Action:   CheckActionAllow,
	}

	// 10:00 UTC = 07:00 São Paulo. Next boundary is 12:00 UTC
	// (09:00 SP).
	now := mustTime(t, layout, "2026-05-13 10:00", "UTC")
	b, ok := NextBoundary(now, []ScheduleCheck{check})
	require.True(t, ok)
	assert.Equal(t, "2026-05-13 12:00", b.In(time.UTC).Format(layout))
}

func TestNextBoundary_MalformedCheckSkipped(t *testing.T) {
	t.Parallel()

	const layout = "2006-01-02 15:04"

	// A check with invalid timezone is skipped; the valid check still
	// produces a boundary. Validate would have rejected this at save
	// time, but the scheduler must not panic when reading hand-edited
	// DB rows.
	checks := []ScheduleCheck{
		{Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"}, Timezone: "Mars/Olympus_Mons", Action: CheckActionAllow},
		{Window: TimeWindow{StartTime: "14:00", EndTime: "17:00"}, Action: CheckActionAllow},
	}

	now := mustTime(t, layout, "2026-05-13 10:00", "UTC")
	b, ok := NextBoundary(now, checks)
	require.True(t, ok)
	assert.Equal(t, "2026-05-13 14:00", b.Format(layout))
}

func TestNextBoundary_BoundaryAlwaysInFuture(t *testing.T) {
	t.Parallel()

	const layout = "2006-01-02 15:04"

	// At the exact start of a window — the boundary returned must be
	// the next one (the close), never the same instant. Equal times
	// would cause the scheduler loop to busy-spin without sleeping.
	check := ScheduleCheck{
		Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
		Action: CheckActionAllow,
	}
	now := mustTime(t, layout, "2026-05-13 09:00", "UTC")
	b, ok := NextBoundary(now, []ScheduleCheck{check})
	require.True(t, ok)
	assert.True(t, b.After(now), "boundary must be strictly after now")
	assert.Equal(t, "2026-05-13 18:00", b.Format(layout))
}

// TestNextBoundary_DST pins the boundary math across Daylight Saving
// transitions. Brazil dropped DST in 2019 but tenants in US/EU
// timezones still observe it, and a wrong boundary means a peer's
// network access flips at the wrong wall-clock time — a real,
// security-relevant correctness bug.
//
// Reference transitions for America/New_York in 2026:
//   - spring-forward: Sun 2026-03-08, 02:00 EST → 03:00 EDT.
//     Local wall-clock [02:00, 03:00) does NOT exist that day.
//   - fall-back:       Sun 2026-11-01, 02:00 EDT → 01:00 EST.
//     Local wall-clock [01:00, 02:00) occurs TWICE that day.
//
// Two assertion classes:
//   - boundaries OUTSIDE the gap/ambiguous hour (09:00, 18:00, 00:00)
//     must land at EXACTLY that wall-clock time on the expected
//     calendar day — a 09:00 window opens at 09:00 local even on a
//     DST day, never 08:00 or 10:00 (the bug class we guard against).
//   - boundaries INSIDE the gap / repeated hour are normalised by
//     Go's time.Date deterministically; we don't pin the exact
//     instant (that's tzdata's contract, not ours) but we DO pin the
//     safety contract: ok==true, strictly After(from), same calendar
//     day, loop terminates (no past boundary, no infinite scan).
func TestNextBoundary_DST(t *testing.T) {
	t.Parallel()

	const layout = "2006-01-02 15:04"
	const tz = "America/New_York"
	nyc, err := time.LoadLocation(tz)
	require.NoError(t, err)

	daily := func(start, end string) ScheduleCheck {
		return ScheduleCheck{
			Window:   TimeWindow{StartTime: start, EndTime: end},
			Timezone: tz,
			Action:   CheckActionAllow,
		}
	}

	t.Run("spring-forward: 09:00 open lands exactly at 09:00 local", func(t *testing.T) {
		// Evening before the transition; next boundary is the 09:00
		// open on the transition day itself.
		now := mustTime(t, layout, "2026-03-07 20:00", tz)
		b, ok := NextBoundary(now, []ScheduleCheck{daily("09:00", "18:00")})
		require.True(t, ok)
		assert.True(t, b.After(now))
		got := b.In(nyc).Format(layout)
		assert.Equal(t, "2026-03-08 09:00", got,
			"09:00 is well clear of the 02:00-03:00 gap; must open at exactly 09:00 local")
	})

	t.Run("spring-forward: in-window mid-day, next boundary is 18:00 close", func(t *testing.T) {
		now := mustTime(t, layout, "2026-03-08 12:00", tz)
		b, ok := NextBoundary(now, []ScheduleCheck{daily("09:00", "18:00")})
		require.True(t, ok)
		assert.Equal(t, "2026-03-08 18:00", b.In(nyc).Format(layout))
	})

	t.Run("spring-forward: window START inside the non-existent hour", func(t *testing.T) {
		// 02:30 does not exist on 2026-03-08. time.Date normalises it
		// (rolls into EDT). Contract: a sane, strictly-future boundary
		// on the right day — never a past instant, never a skipped
		// day that would starve the scheduler.
		now := mustTime(t, layout, "2026-03-08 00:30", tz)
		b, ok := NextBoundary(now, []ScheduleCheck{daily("02:30", "10:00")})
		require.True(t, ok)
		assert.True(t, b.After(now), "boundary must be strictly in the future")
		assert.Equal(t, "2026-03-08", b.In(nyc).Format("2006-01-02"),
			"boundary must stay on the transition day, not skip it")
	})

	t.Run("fall-back: 09:00 open lands exactly at 09:00 local", func(t *testing.T) {
		now := mustTime(t, layout, "2026-10-31 20:00", tz)
		b, ok := NextBoundary(now, []ScheduleCheck{daily("09:00", "18:00")})
		require.True(t, ok)
		assert.True(t, b.After(now))
		assert.Equal(t, "2026-11-01 09:00", b.In(nyc).Format(layout),
			"09:00 is clear of the 01:00-02:00 repeated hour; exact local time")
	})

	t.Run("fall-back: midnight-to-02:00 window, 02:00 close is unambiguous", func(t *testing.T) {
		// 02:00 occurs once on fall-back day (the repeat is 01:00-02:00).
		// Just before midnight → next boundary is the 00:00 open.
		now := mustTime(t, layout, "2026-10-31 23:30", tz)
		b, ok := NextBoundary(now, []ScheduleCheck{daily("00:00", "02:00")})
		require.True(t, ok)
		assert.True(t, b.After(now))
		assert.Equal(t, "2026-11-01 00:00", b.In(nyc).Format(layout))
	})

	t.Run("fall-back: window END inside the repeated hour", func(t *testing.T) {
		// 01:30 occurs twice on 2026-11-01. time.Date picks one offset
		// deterministically. Contract: strictly-future, same day, ok.
		now := mustTime(t, layout, "2026-11-01 00:15", tz)
		b, ok := NextBoundary(now, []ScheduleCheck{daily("00:00", "01:30")})
		require.True(t, ok)
		assert.True(t, b.After(now), "boundary must be strictly in the future")
		assert.Equal(t, "2026-11-01", b.In(nyc).Format("2006-01-02"))
	})

	t.Run("transition day never yields a past or zero boundary", func(t *testing.T) {
		// Sweep both transition days at several `from` instants and a
		// window that brackets the special hour. The invariant under
		// test is purely the loop-safety contract NextBoundary must
		// uphold regardless of DST: a found boundary is always
		// strictly in the future.
		windows := []ScheduleCheck{daily("01:00", "03:00"), daily("02:00", "04:00")}
		fromTimes := []string{
			"2026-03-08 00:00", "2026-03-08 01:30", "2026-03-08 03:30",
			"2026-11-01 00:00", "2026-11-01 01:30", "2026-11-01 02:30",
		}
		for _, ft := range fromTimes {
			now := mustTime(t, layout, ft, tz)
			b, ok := NextBoundary(now, windows)
			require.True(t, ok, "from=%s should yield a boundary", ft)
			assert.True(t, b.After(now),
				"from=%s: boundary %s must be strictly after now", ft, b)
		}
	})
}
