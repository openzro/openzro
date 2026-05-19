package posture

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"time"

	// Embed Go's tzdata so time.LoadLocation works without the host
	// /usr/share/zoneinfo. The management container today is a slim
	// ubuntu:24.04 base that does NOT install the tzdata apt package
	// — without this import every non-UTC schedule check would fail
	// in production while passing Validate locally on a dev machine
	// that does have tzdata. ~450 KB binary-size cost; tiny price for
	// eliminating a silent prod-only regression.
	_ "time/tzdata"

	nbpeer "github.com/openzro/openzro/management/server/peer"
)

// timeOfDayRegex matches HH:MM in 24-hour notation (00:00 through 23:59).
var timeOfDayRegex = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// TimeWindow expresses a window in local time. When EndTime is less than
// or equal to StartTime, the window wraps midnight — e.g. 22:00–06:00
// covers 22:00 today through 06:00 the following day.
type TimeWindow struct {
	// DaysOfWeek the window applies on. 0=Sunday..6=Saturday. Empty
	// slice means every day. Validate rejects duplicates and values
	// outside [0..6].
	DaysOfWeek []int

	// StartTime in HH:MM (24h). Inclusive bound.
	StartTime string

	// EndTime in HH:MM (24h). Exclusive bound. EndTime <= StartTime
	// expresses a midnight-wrapping window.
	EndTime string
}

var _ Check = (*ScheduleCheck)(nil)

// ScheduleCheck gates peer access by wall-clock time. Operators express
// "office hours only" with Action=allow + the business window, and
// "after-hours lockout" with Action=deny + the off-hours window.
type ScheduleCheck struct {
	// Window of time the policy applies in.
	Window TimeWindow

	// Timezone IANA name (e.g. "America/Sao_Paulo", "Europe/Berlin").
	// Empty defaults to UTC. "Local" is rejected explicitly so the
	// rule never silently depends on the management process host.
	Timezone string

	// Action on match. CheckActionAllow ⇒ peer passes only inside
	// the window; CheckActionDeny ⇒ peer passes only outside the
	// window.
	Action string
}

func (s *ScheduleCheck) Check(_ context.Context, _ nbpeer.Peer) (bool, error) {
	if s == nil {
		// Defensive: GetChecks() only appends non-nil pointers today,
		// but a future refactor or a direct caller could pass nil and
		// would otherwise panic on receiver method dispatch. Treat as
		// an invalid configuration rather than crashing the worker.
		return false, fmt.Errorf("nil ScheduleCheck")
	}
	return s.checkAt(time.Now())
}

// checkAt evaluates the schedule against an explicit instant. Split out
// from Check so tests can drive the clock without monkey-patching
// time.Now.
func (s *ScheduleCheck) checkAt(now time.Time) (bool, error) {
	loc, err := s.location()
	if err != nil {
		return false, err
	}
	inWindow, err := s.inWindow(now.In(loc))
	if err != nil {
		return false, err
	}
	switch s.Action {
	case CheckActionAllow:
		return inWindow, nil
	case CheckActionDeny:
		return !inWindow, nil
	default:
		return false, fmt.Errorf("invalid %s action: %s", s.Name(), s.Action)
	}
}

// inWindow returns whether t (already localized to the check's tz) sits
// inside the configured window. The midnight-wrap branch checks both
// the "evening of today" and "morning of yesterday" portions of the
// wrapped window so a 22:00–06:00 rule scoped to "Mon" admits both
// late-Monday and early-Tuesday traffic.
func (s *ScheduleCheck) inWindow(t time.Time) (bool, error) {
	startMins, err := parseMinutesOfDay(s.Window.StartTime)
	if err != nil {
		return false, fmt.Errorf("startTime: %w", err)
	}
	endMins, err := parseMinutesOfDay(s.Window.EndTime)
	if err != nil {
		return false, fmt.Errorf("endTime: %w", err)
	}
	nowMins := t.Hour()*60 + t.Minute()
	weekday := int(t.Weekday()) // 0=Sun..6=Sat
	if startMins < endMins {
		// Same-day window.
		if !s.dayMatches(weekday) {
			return false, nil
		}
		return nowMins >= startMins && nowMins < endMins, nil
	}
	// Wrap-midnight window. Equality (startMins == endMins) is
	// rejected by Validate, so we only reach this branch with a
	// strictly wrapping range.
	yesterday := (weekday + 6) % 7
	return (s.dayMatches(weekday) && nowMins >= startMins) ||
		(s.dayMatches(yesterday) && nowMins < endMins), nil
}

func (s *ScheduleCheck) dayMatches(weekday int) bool {
	if len(s.Window.DaysOfWeek) == 0 {
		return true
	}
	return slices.Contains(s.Window.DaysOfWeek, weekday)
}

func (s *ScheduleCheck) location() (*time.Location, error) {
	tz := s.Timezone
	if tz == "" {
		tz = "UTC"
	}
	if tz == "Local" {
		return nil, fmt.Errorf("timezone %q not allowed; use UTC or an IANA name such as America/Sao_Paulo", tz)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	return loc, nil
}

// parseMinutesOfDay returns the count of minutes since midnight for an
// HH:MM string. The caller must have already regex-matched the input.
func parseMinutesOfDay(s string) (int, error) {
	if !timeOfDayRegex.MatchString(s) {
		return 0, fmt.Errorf("expected HH:MM 24h format, got %q", s)
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

func (s *ScheduleCheck) Name() string {
	return ScheduleCheckName
}

func (s *ScheduleCheck) Validate() error {
	if s == nil {
		return fmt.Errorf("nil ScheduleCheck")
	}
	if s.Action == "" {
		return fmt.Errorf("%s action shouldn't be empty", s.Name())
	}
	if !slices.Contains([]string{CheckActionAllow, CheckActionDeny}, s.Action) {
		return fmt.Errorf("%s action is not valid: %s", s.Name(), s.Action)
	}
	if s.Window.StartTime == "" || s.Window.EndTime == "" {
		return fmt.Errorf("%s window startTime and endTime are required", s.Name())
	}
	if !timeOfDayRegex.MatchString(s.Window.StartTime) {
		return fmt.Errorf("%s startTime must match HH:MM 24h: got %q", s.Name(), s.Window.StartTime)
	}
	if !timeOfDayRegex.MatchString(s.Window.EndTime) {
		return fmt.Errorf("%s endTime must match HH:MM 24h: got %q", s.Name(), s.Window.EndTime)
	}
	if s.Window.StartTime == s.Window.EndTime {
		return fmt.Errorf("%s startTime and endTime must differ — use 00:00–23:59 for an all-day window", s.Name())
	}
	seen := make(map[int]struct{}, len(s.Window.DaysOfWeek))
	for _, d := range s.Window.DaysOfWeek {
		if d < 0 || d > 6 {
			return fmt.Errorf("%s daysOfWeek must be in [0..6] (Sunday..Saturday): got %d", s.Name(), d)
		}
		if _, dup := seen[d]; dup {
			return fmt.Errorf("%s daysOfWeek has duplicate value %d", s.Name(), d)
		}
		seen[d] = struct{}{}
	}
	if _, err := s.location(); err != nil {
		return fmt.Errorf("%s: %w", s.Name(), err)
	}
	return nil
}
