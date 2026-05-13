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
		name    string
		now     string
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
