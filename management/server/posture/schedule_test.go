package posture

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
)

func TestScheduleCheck_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		check   ScheduleCheck
		wantErr string // substring; empty == expect success
	}{
		{
			name: "happy path — office hours allow",
			check: ScheduleCheck{
				Window: TimeWindow{
					DaysOfWeek: []int{1, 2, 3, 4, 5},
					StartTime:  "09:00",
					EndTime:    "18:00",
				},
				Timezone: "America/Sao_Paulo",
				Action:   CheckActionAllow,
			},
		},
		{
			name: "happy path — wrap midnight deny",
			check: ScheduleCheck{
				Window: TimeWindow{
					StartTime: "22:00",
					EndTime:   "06:00",
				},
				Timezone: "UTC",
				Action:   CheckActionDeny,
			},
		},
		{
			name: "happy path — empty days means every day",
			check: ScheduleCheck{
				Window: TimeWindow{
					StartTime: "00:00",
					EndTime:   "23:59",
				},
				Action: CheckActionAllow,
			},
		},
		{
			name: "happy path — empty timezone defaults to UTC",
			check: ScheduleCheck{
				Window: TimeWindow{
					StartTime: "08:00",
					EndTime:   "17:00",
				},
				Action: CheckActionAllow,
			},
		},
		{
			name: "missing action",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
			},
			wantErr: "action shouldn't be empty",
		},
		{
			name: "invalid action",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
				Action: "drop",
			},
			wantErr: "action is not valid",
		},
		{
			name: "missing startTime",
			check: ScheduleCheck{
				Window: TimeWindow{EndTime: "18:00"},
				Action: CheckActionAllow,
			},
			wantErr: "startTime and endTime are required",
		},
		{
			name: "missing endTime",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "09:00"},
				Action: CheckActionAllow,
			},
			wantErr: "startTime and endTime are required",
		},
		{
			name: "malformed startTime — single digit hour",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "9:00", EndTime: "18:00"},
				Action: CheckActionAllow,
			},
			wantErr: "startTime must match HH:MM",
		},
		{
			name: "malformed startTime — out of range",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "24:00", EndTime: "23:00"},
				Action: CheckActionAllow,
			},
			wantErr: "startTime must match HH:MM",
		},
		{
			name: "malformed endTime — minutes out of range",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "09:00", EndTime: "18:60"},
				Action: CheckActionAllow,
			},
			wantErr: "endTime must match HH:MM",
		},
		{
			name: "equal start and end times",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "09:00", EndTime: "09:00"},
				Action: CheckActionAllow,
			},
			wantErr: "startTime and endTime must differ",
		},
		{
			name: "daysOfWeek out of range — negative",
			check: ScheduleCheck{
				Window: TimeWindow{
					DaysOfWeek: []int{-1, 0},
					StartTime:  "09:00",
					EndTime:    "18:00",
				},
				Action: CheckActionAllow,
			},
			wantErr: "daysOfWeek must be in [0..6]",
		},
		{
			name: "daysOfWeek out of range — too high",
			check: ScheduleCheck{
				Window: TimeWindow{
					DaysOfWeek: []int{0, 7},
					StartTime:  "09:00",
					EndTime:    "18:00",
				},
				Action: CheckActionAllow,
			},
			wantErr: "daysOfWeek must be in [0..6]",
		},
		{
			name: "daysOfWeek duplicate value",
			check: ScheduleCheck{
				Window: TimeWindow{
					DaysOfWeek: []int{1, 2, 2, 3},
					StartTime:  "09:00",
					EndTime:    "18:00",
				},
				Action: CheckActionAllow,
			},
			wantErr: "duplicate value",
		},
		{
			name: "invalid timezone",
			check: ScheduleCheck{
				Window:   TimeWindow{StartTime: "09:00", EndTime: "18:00"},
				Timezone: "Mars/Olympus_Mons",
				Action:   CheckActionAllow,
			},
			wantErr: "invalid timezone",
		},
		{
			name: "Local timezone explicitly rejected",
			check: ScheduleCheck{
				Window:   TimeWindow{StartTime: "09:00", EndTime: "18:00"},
				Timezone: "Local",
				Action:   CheckActionAllow,
			},
			wantErr: "not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.check.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// mustTime parses "2006-01-02 15:04 MST" in the given location.
func mustTime(t *testing.T, layout, value, locName string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(locName)
	require.NoError(t, err, "loading timezone")
	parsed, err := time.ParseInLocation(layout, value, loc)
	require.NoError(t, err, "parsing %q", value)
	return parsed
}

func TestScheduleCheck_checkAt(t *testing.T) {
	t.Parallel()

	// Reference clock anchors for readability.
	// 2026-05-13 is a Wednesday in any reasonable timezone we test
	// (no nasty calendar wrap).
	const layout = "2006-01-02 15:04"

	tests := []struct {
		name  string
		check ScheduleCheck
		now   time.Time
		want  bool
	}{
		// Same-day window, allow action
		{
			name: "allow + inside window + day matches",
			check: ScheduleCheck{
				Window: TimeWindow{DaysOfWeek: []int{3}, StartTime: "09:00", EndTime: "18:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-13 10:30", "UTC"),
			want: true,
		},
		{
			name: "allow + before window opens",
			check: ScheduleCheck{
				Window: TimeWindow{DaysOfWeek: []int{3}, StartTime: "09:00", EndTime: "18:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-13 08:59", "UTC"),
			want: false,
		},
		{
			name: "allow + exactly at start",
			check: ScheduleCheck{
				Window: TimeWindow{DaysOfWeek: []int{3}, StartTime: "09:00", EndTime: "18:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-13 09:00", "UTC"),
			want: true,
		},
		{
			name: "allow + exactly at end is exclusive",
			check: ScheduleCheck{
				Window: TimeWindow{DaysOfWeek: []int{3}, StartTime: "09:00", EndTime: "18:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-13 18:00", "UTC"),
			want: false,
		},
		{
			name: "allow + day doesn't match (weekend, Mon-Fri rule)",
			check: ScheduleCheck{
				Window: TimeWindow{DaysOfWeek: []int{1, 2, 3, 4, 5}, StartTime: "09:00", EndTime: "18:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-16 10:30", "UTC"), // Saturday
			want: false,
		},
		{
			name: "allow + empty days = every day",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-17 12:00", "UTC"), // Sunday
			want: true,
		},

		// Same-day window, deny action
		{
			name: "deny + inside window",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "12:00", EndTime: "14:00"},
				Action: CheckActionDeny,
			},
			now:  mustTime(t, layout, "2026-05-13 13:00", "UTC"),
			want: false,
		},
		{
			name: "deny + outside window",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "12:00", EndTime: "14:00"},
				Action: CheckActionDeny,
			},
			now:  mustTime(t, layout, "2026-05-13 15:00", "UTC"),
			want: true,
		},

		// Wrap midnight
		{
			name: "wrap midnight allow + late evening (today portion)",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "22:00", EndTime: "06:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-13 23:30", "UTC"),
			want: true,
		},
		{
			name: "wrap midnight allow + early morning (yesterday portion)",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "22:00", EndTime: "06:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-13 03:30", "UTC"),
			want: true,
		},
		{
			name: "wrap midnight allow + outside (mid afternoon)",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "22:00", EndTime: "06:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-13 12:00", "UTC"),
			want: false,
		},
		{
			name: "wrap midnight + day filter; today matches but yesterday doesn't",
			check: ScheduleCheck{
				// Block starts Wed 22:00 → Thu 06:00. DaysOfWeek lists Wed only.
				Window: TimeWindow{DaysOfWeek: []int{3}, StartTime: "22:00", EndTime: "06:00"},
				Action: CheckActionDeny,
			},
			// Wed 23:00 → inside today portion → deny semantics fail = false
			now:  mustTime(t, layout, "2026-05-13 23:00", "UTC"),
			want: false,
		},
		{
			name: "wrap midnight + day filter; yesterday matched, today doesn't",
			check: ScheduleCheck{
				Window: TimeWindow{DaysOfWeek: []int{3}, StartTime: "22:00", EndTime: "06:00"},
				Action: CheckActionDeny,
			},
			// Thu 03:00 → checking yesterday (Wed) portion of the wrap window
			now:  mustTime(t, layout, "2026-05-14 03:00", "UTC"),
			want: false,
		},
		{
			name: "wrap midnight + day filter; outside any matching day",
			check: ScheduleCheck{
				Window: TimeWindow{DaysOfWeek: []int{3}, StartTime: "22:00", EndTime: "06:00"},
				Action: CheckActionDeny,
			},
			// Fri 03:00 → yesterday is Thu (not in days), today Fri 03:00 < startMins
			now:  mustTime(t, layout, "2026-05-15 03:00", "UTC"),
			want: true,
		},

		// Timezone interpretation
		{
			name: "America/Sao_Paulo allow + inside in local time",
			check: ScheduleCheck{
				Window:   TimeWindow{StartTime: "09:00", EndTime: "18:00"},
				Timezone: "America/Sao_Paulo",
				Action:   CheckActionAllow,
			},
			// 12:00 UTC == 09:00 in São Paulo (UTC-3, no DST today)
			now:  mustTime(t, layout, "2026-05-13 12:00", "UTC"),
			want: true,
		},
		{
			name: "America/Sao_Paulo allow + outside in local time",
			check: ScheduleCheck{
				Window:   TimeWindow{StartTime: "09:00", EndTime: "18:00"},
				Timezone: "America/Sao_Paulo",
				Action:   CheckActionAllow,
			},
			// 11:00 UTC == 08:00 in São Paulo — before window opens
			now:  mustTime(t, layout, "2026-05-13 11:00", "UTC"),
			want: false,
		},

		// Action allow with empty timezone defaults to UTC
		{
			name: "empty tz defaults to UTC + inside window",
			check: ScheduleCheck{
				Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
				Action: CheckActionAllow,
			},
			now:  mustTime(t, layout, "2026-05-13 10:00", "UTC"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.check.checkAt(tt.now)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got, "check at %s", tt.now.Format(time.RFC3339))
		})
	}
}

// TestScheduleCheck_checkAt_invalidAction makes sure runtime evaluation
// surfaces misconfigured actions, since Validate guards configuration
// time but Check can still be called directly on partially-populated
// structs (e.g. database manual inserts, fuzz testing).
func TestScheduleCheck_checkAt_invalidAction(t *testing.T) {
	t.Parallel()

	check := ScheduleCheck{
		Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
		Action: "panic",
	}
	_, err := check.checkAt(mustTime(t, "2006-01-02 15:04", "2026-05-13 10:00", "UTC"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestScheduleCheck_Name(t *testing.T) {
	t.Parallel()
	s := &ScheduleCheck{}
	assert.Equal(t, ScheduleCheckName, s.Name())
}

// TestScheduleCheck_NilReceiver guards against future regressions where
// the posture engine, a fuzz harness or a direct caller hands a nil
// *ScheduleCheck to Check/Validate. The receiver methods must return an
// error instead of panicking on receiver method dispatch.
func TestScheduleCheck_NilReceiver(t *testing.T) {
	t.Parallel()

	var s *ScheduleCheck

	ok, err := s.Check(context.Background(), nbpeer.Peer{})
	require.Error(t, err)
	assert.False(t, ok)

	err = s.Validate()
	require.Error(t, err)
}

// TestScheduleCheck_BoundaryMinutes covers the inclusive-start /
// exclusive-end boundary semantics at both ends of the day so the
// "00:00–23:59" all-day pattern documented in the operator guide
// behaves as advertised.
func TestScheduleCheck_BoundaryMinutes(t *testing.T) {
	t.Parallel()

	const layout = "2006-01-02 15:04"

	tests := []struct {
		name     string
		now      string
		window   TimeWindow
		action   string
		want     bool
	}{
		{
			name:   "all-day window — start of day passes",
			window: TimeWindow{StartTime: "00:00", EndTime: "23:59"},
			action: CheckActionAllow,
			now:    "2026-05-13 00:00",
			want:   true,
		},
		{
			name:   "all-day window — 23:58 still inside",
			window: TimeWindow{StartTime: "00:00", EndTime: "23:59"},
			action: CheckActionAllow,
			now:    "2026-05-13 23:58",
			want:   true,
		},
		{
			name:   "all-day window — 23:59 is the exclusive end",
			window: TimeWindow{StartTime: "00:00", EndTime: "23:59"},
			action: CheckActionAllow,
			now:    "2026-05-13 23:59",
			want:   false,
		},
		{
			name:   "midnight start, evening end — 00:00 inclusive",
			window: TimeWindow{StartTime: "00:00", EndTime: "08:00"},
			action: CheckActionAllow,
			now:    "2026-05-13 00:00",
			want:   true,
		},
		{
			name:   "midnight start, evening end — 07:59 still inside",
			window: TimeWindow{StartTime: "00:00", EndTime: "08:00"},
			action: CheckActionAllow,
			now:    "2026-05-13 07:59",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ScheduleCheck{Window: tt.window, Action: tt.action}
			got, err := s.checkAt(mustTime(t, layout, tt.now, "UTC"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestScheduleCheck_ZeroValueTime makes sure passing a zero time.Time
// (epoch 0001-01-01) doesn't crash the predicate. Zero-time inputs are
// improbable in production but cheap to defend against.
func TestScheduleCheck_ZeroValueTime(t *testing.T) {
	t.Parallel()

	s := ScheduleCheck{
		Window: TimeWindow{StartTime: "09:00", EndTime: "18:00"},
		Action: CheckActionAllow,
	}
	// 0001-01-01 00:00:00 UTC. Hour=0, Minute=0, weekday=Monday(1).
	// Falls outside the 09:00–18:00 window → allow returns false.
	got, err := s.checkAt(time.Time{})
	require.NoError(t, err)
	assert.False(t, got)
}

// TestScheduleCheck_DSTTransitions exercises the wall-clock semantics
// across both pathologies (spring-forward gap and fall-back duplicate)
// to ensure the predicate does not accidentally double-fire during
// fall-back or skip a valid window during spring-forward.
//
// We use America/New_York because Brazil dropped DST in 2019 and so
// would not exercise the transitions. The two transition Sundays in
// 2026 are:
//   - 2026-03-08: 02:00 EST → 03:00 EDT (spring-forward, 02:30 doesn't exist)
//   - 2026-11-01: 02:00 EDT → 01:00 EST (fall-back, 01:30 happens twice)
func TestScheduleCheck_DSTTransitions(t *testing.T) {
	t.Parallel()

	tz := "America/New_York"
	loc, err := time.LoadLocation(tz)
	require.NoError(t, err, "tzdata must be embedded for DST tests")

	// Window that brackets the transition: 01:00–04:00 local on the
	// transition Sunday.
	s := ScheduleCheck{
		Window: TimeWindow{
			DaysOfWeek: []int{0}, // Sunday
			StartTime:  "01:00",
			EndTime:    "04:00",
		},
		Timezone: tz,
		Action:   CheckActionAllow,
	}

	// Spring-forward day: 02:00 EST does not exist (wall clock jumps
	// to 03:00 EDT). Probing a real UTC instant that translates to
	// 03:30 EDT — inside the window — should still pass.
	springForwardNoon := time.Date(2026, 3, 8, 3, 30, 0, 0, loc)
	got, err := s.checkAt(springForwardNoon)
	require.NoError(t, err)
	assert.True(t, got, "spring-forward 03:30 EDT is inside the 01:00–04:00 window")

	// Fall-back day: 01:30 local happens twice. Both instances should
	// pass the check identically — the predicate operates on wall
	// time, not on absolute time, so it has no way to (and shouldn't)
	// distinguish the two.
	fallBackFirst := time.Date(2026, 11, 1, 1, 30, 0, 0, loc) // first 01:30 (EDT)
	got, err = s.checkAt(fallBackFirst)
	require.NoError(t, err)
	assert.True(t, got, "fall-back first 01:30 inside window")

	// Add an hour to land on the second 01:30 (now EST).
	fallBackSecond := fallBackFirst.Add(1 * time.Hour)
	got, err = s.checkAt(fallBackSecond)
	require.NoError(t, err)
	assert.True(t, got, "fall-back second 01:30 also inside window")
}

// TestScheduleCheck_TzDataEmbedded asserts the tzdata embed (the
// blank import in schedule.go) is in effect by loading a zone that is
// not part of the POSIX-minimum set most slim base images ship with.
// If this regresses, the assertion fires before any production deploy.
func TestScheduleCheck_TzDataEmbedded(t *testing.T) {
	t.Parallel()
	for _, zone := range []string{
		"America/Sao_Paulo",
		"America/Manaus",
		"Europe/Berlin",
		"Asia/Tokyo",
		"Pacific/Auckland",
	} {
		_, err := time.LoadLocation(zone)
		assert.NoErrorf(t, err, "loading %s — tzdata embed missing?", zone)
	}
}
