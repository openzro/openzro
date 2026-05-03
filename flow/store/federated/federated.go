// Package federated wraps a hot store and an optional cold-archive
// store and routes queries by their time window. See ADR-0012 for
// the design that motivates this layer.
//
// Write side (Save) goes exclusively to the hot store: archive
// population is the sink fan-out's responsibility (see
// flow/sinks/{s3,gcs}.go). Wiring writes through here would either
// duplicate the events on the archive or fight with the sinks for
// ordering.
//
// Purge() also targets hot only — archive retention is the
// operator's bucket lifecycle policy, not management's.
//
// Query routes by date window relative to the hot store's retention
// boundary `now - retention`:
//
//   * window fully inside retention  → hot only
//   * window fully outside retention → archive only (or empty when
//                                       no archive is configured)
//   * window crosses the boundary    → both, with each side's window
//                                       trimmed to its half so the
//                                       result has no duplicates
package federated

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/flow/store"
)

// Federated implements store.Store by composing a hot store with an
// optional archive store. Construct with New().
type Federated struct {
	hot       store.Store
	archive   store.Store // nil when not configured / not built (archive_duckdb tag off)
	retention time.Duration
	now       func() time.Time // override-able for tests
}

// New returns a Federated store wrapping the given hot store. When
// archive is nil, the result behaves exactly like the hot store
// (all queries go to hot, archive split is short-circuited). The
// retention argument is the hot store's retention window — it
// defines the boundary that splits queries between hot and archive.
//
// The hot store is required; nil hot returns an error because the
// dashboard's API would have nowhere to send fresh events.
func New(hot store.Store, archive store.Store, retention time.Duration) (*Federated, error) {
	if hot == nil {
		return nil, errors.New("federated store: hot store is required")
	}
	if retention <= 0 {
		// Treat zero retention as "hot keeps forever". The boundary
		// becomes time.Time{} so every query goes to hot.
		retention = 0
	}
	return &Federated{
		hot:       hot,
		archive:   archive,
		retention: retention,
		now:       time.Now,
	}, nil
}

// Save writes to the hot store. Archive population happens via the
// FlowService sink fan-out; double-writing here would race the sink
// and produce duplicate rows on archive.
func (f *Federated) Save(ctx context.Context, events []*store.Event) error {
	return f.hot.Save(ctx, events)
}

// Purge proxies to the hot store. Archive retention is the bucket's
// lifecycle policy.
func (f *Federated) Purge(ctx context.Context, olderThan time.Time) (int64, error) {
	return f.hot.Purge(ctx, olderThan)
}

// Close closes both backends. Errors from either are wrapped so the
// caller can see which side failed.
func (f *Federated) Close() error {
	var firstErr error
	if err := f.hot.Close(); err != nil {
		firstErr = err
	}
	if f.archive != nil {
		if err := f.archive.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Query routes the filter according to its time window vs the hot
// retention boundary, runs the slice(s) in parallel, and merges
// results by ReceivedAt descending. Limit / Offset are applied AFTER
// the merge so the caller gets the requested page across both
// backends.
func (f *Federated) Query(ctx context.Context, filter store.Filter) ([]*store.Event, error) {
	// No archive → hot-only path. This is also the path on a binary
	// built without `archive_duckdb` (archive store comes back as
	// nil from the federated factory).
	if f.archive == nil {
		return f.hot.Query(ctx, filter)
	}

	// Zero retention → hot keeps forever, archive is unreachable.
	if f.retention == 0 {
		return f.hot.Query(ctx, filter)
	}

	boundary := f.now().Add(-f.retention)
	hotFilter, archFilter, queryHot, queryArch := splitByBoundary(filter, boundary)

	if queryHot && !queryArch {
		return f.hot.Query(ctx, hotFilter)
	}
	if queryArch && !queryHot {
		return f.archive.Query(ctx, archFilter)
	}
	return f.queryBoth(ctx, filter, hotFilter, archFilter)
}

// queryBoth fans out to hot + archive in parallel, merges by
// ReceivedAt desc, and applies the caller's Limit / Offset on the
// merged stream. A failure on either side is logged but does not
// fail the whole call — operators get partial data with a warning,
// which is better than an empty result during a bucket outage.
func (f *Federated) queryBoth(
	ctx context.Context,
	original, hotFilter, archFilter store.Filter,
) ([]*store.Event, error) {
	var (
		wg       sync.WaitGroup
		hotEv    []*store.Event
		archEv   []*store.Event
		hotErr   error
		archErr  error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		hotEv, hotErr = f.hot.Query(ctx, hotFilter)
	}()
	go func() {
		defer wg.Done()
		archEv, archErr = f.archive.Query(ctx, archFilter)
	}()
	wg.Wait()

	if hotErr != nil {
		log.WithContext(ctx).Warnf("federated store: hot side failed: %v", hotErr)
	}
	if archErr != nil {
		log.WithContext(ctx).Warnf("federated store: archive side failed: %v", archErr)
	}
	// If both sides errored, propagate so the caller knows it has no
	// data at all.
	if hotErr != nil && archErr != nil {
		return nil, hotErr
	}

	merged := mergeByReceivedAtDesc(hotEv, archEv)
	return applyPaging(merged, original.Limit, original.Offset), nil
}

// splitByBoundary chops the filter's time window so the hot side
// queries [max(since, boundary), until] and the archive side
// queries [since, min(until, boundary)]. The booleans report which
// side has any rows to look at — when the original window is
// entirely on one side of the boundary, the other side is skipped.
func splitByBoundary(
	filter store.Filter,
	boundary time.Time,
) (hot, arch store.Filter, queryHot, queryArch bool) {
	hot = filter
	arch = filter

	// Hot serves everything from `boundary` onward. If the caller's
	// window does not extend that far, hot has nothing.
	hotEnd := filter.Until
	if hotEnd.IsZero() {
		hotEnd = time.Time{}
	}
	queryHot = hotEnd.IsZero() || hotEnd.After(boundary)
	if queryHot && !filter.Since.IsZero() && filter.Since.Before(boundary) {
		hot.Since = boundary
	} else if queryHot && filter.Since.IsZero() {
		hot.Since = boundary
	}

	// Archive serves up to `boundary`. If the caller's window starts
	// after the boundary, archive has nothing.
	queryArch = filter.Since.IsZero() || filter.Since.Before(boundary)
	if queryArch {
		if filter.Until.IsZero() || filter.Until.After(boundary) {
			arch.Until = boundary
		}
	}
	return
}

// mergeByReceivedAtDesc merges two ReceivedAt-desc sorted slices in
// O(n+m). Both inputs come from stores that already sort that way
// per the Store interface contract; we re-sort defensively because
// concatenating them otherwise would interleave incorrectly.
func mergeByReceivedAtDesc(a, b []*store.Event) []*store.Event {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]*store.Event, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ReceivedAt.After(out[j].ReceivedAt)
	})
	return out
}

// applyPaging slices the merged result to honour the caller's Limit
// and Offset. Limit ≤ 0 means "no limit"; Offset > len(slice) returns
// an empty slice (not an error).
func applyPaging(events []*store.Event, limit, offset int) []*store.Event {
	if offset > 0 {
		if offset >= len(events) {
			return nil
		}
		events = events[offset:]
	}
	if limit > 0 && limit < len(events) {
		events = events[:limit]
	}
	return events
}

// Compile-time check that Federated satisfies store.Store.
var _ store.Store = (*Federated)(nil)
