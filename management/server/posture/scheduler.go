package posture

import (
	"sort"
	"time"
)

// AccountBoundary describes the soonest moment the verdict of any
// ScheduleCheck attached to `AccountID` is going to flip. The
// management server wakes a per-account timer at exactly `At` and
// triggers a network-map recomputation; without this nudge a peer
// that stays connected across a window boundary would keep its
// pre-boundary permissions until its next Sync poll arrives. The
// struct is the caller's contract — the scheduler-loop code lives
// in `management/server/posture_scheduler.go` so it can reach the
// account manager without creating an import cycle from this
// package.
type AccountBoundary struct {
	AccountID string
	At        time.Time
}

// NextBoundary computes the soonest future instant after `from` at
// which the inWindow() result of any ScheduleCheck in `checks` would
// flip. Returns the zero time and false when no checks are active or
// none of them have a reachable boundary within the search horizon
// (one week — far more than any real schedule needs, but bounded so
// the loop is guaranteed to terminate even on a pathological all-day
// every-day window).
//
// The function is the pure-math heart of the per-account scheduler;
// it has no I/O, no state, no clock side-effects, which makes it
// trivially testable. The caller drives `from` (typically time.Now())
// and re-invokes on every wakeup.
func NextBoundary(from time.Time, checks []ScheduleCheck) (time.Time, bool) {
	var boundaries []time.Time

	for i := range checks {
		c := checks[i]
		loc, err := c.location()
		if err != nil {
			// Validate already rejected malformed timezones at policy
			// save time, so reaching this branch means the schedule
			// definition was hand-written into the DB. Skip the
			// offending check rather than abort the whole boundary
			// scan — other valid checks still need a wakeup.
			continue
		}
		startMins, err := parseMinutesOfDay(c.Window.StartTime)
		if err != nil {
			continue
		}
		endMins, err := parseMinutesOfDay(c.Window.EndTime)
		if err != nil {
			continue
		}

		fromLocal := from.In(loc)
		// Probe yesterday through the next seven days. The
		// offset=-1 case picks up wrap-midnight windows whose
		// closing boundary is still in the future (e.g. now is
		// 03:00 and yesterday's 22:00–06:00 window closes at
		// today's 06:00). offset=7 lets a Mon-only window be
		// scheduled even when invoked late on Saturday.
		for offset := -1; offset < 8; offset++ {
			day := fromLocal.AddDate(0, 0, offset)
			weekday := int(day.Weekday())
			if !c.dayMatches(weekday) {
				continue
			}

			start := dateAtMinute(day, startMins, loc)
			if start.After(from) {
				boundaries = append(boundaries, start)
			}

			endDay := day
			if endMins <= startMins {
				// Wrap-midnight: the window that opens on `day` at
				// startMins closes the following calendar day.
				endDay = day.AddDate(0, 0, 1)
			}
			end := dateAtMinute(endDay, endMins, loc)
			if end.After(from) {
				boundaries = append(boundaries, end)
			}
		}
	}

	if len(boundaries) == 0 {
		return time.Time{}, false
	}

	sort.Slice(boundaries, func(i, j int) bool {
		return boundaries[i].Before(boundaries[j])
	})
	return boundaries[0], true
}

// dateAtMinute returns the instant at minute `mins` since midnight on
// the calendar date of `day`, expressed in `loc`. Wraps the otherwise
// verbose time.Date call to keep NextBoundary readable.
func dateAtMinute(day time.Time, mins int, loc *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), mins/60, mins%60, 0, 0, loc)
}
