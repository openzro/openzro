//go:build archive_duckdb

package archive

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

// The handler accepts type and direction and the hot store applies them,
// but the archive reader dropped both. The filter therefore worked for
// the retention window and quietly stopped working past it: same UI,
// same request, and the cold half of the answer ignored the filter.
//
// Silently returning too much is the bad half. A "drop" filter that also
// returns "start" events past the retention boundary is a wrong answer
// presented as a filtered one.
func TestQueryFiltersTypeAndDirection(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	seedTyped(t, db, partitionPath(root, "2026", "06", "10", "a.parquet"),
		"2026-06-10 10:00:00", "start", "ingress")
	seedTyped(t, db, partitionPath(root, "2026", "06", "10", "b.parquet"),
		"2026-06-10 11:00:00", "drop", "egress")

	window := store.Filter{
		AccountID: testAccount,
		Since:     time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		Until:     time.Date(2026, 6, 10, 23, 59, 59, 0, time.UTC),
	}

	drop := store.EventTypeDrop
	egress := store.DirectionEgress
	start := store.EventTypeStart
	ingress := store.DirectionIngress

	for _, tc := range []struct {
		name string
		typ  *store.EventType
		dir  *store.Direction
		want int
	}{
		{"no type or direction returns both", nil, nil, 2},
		{"type alone", &drop, nil, 1},
		{"direction alone", nil, &egress, 1},
		{"both, agreeing", &drop, &egress, 1},
		{"both, disagreeing, returns nothing", &drop, &ingress, 0},
		{"the other row", &start, &ingress, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := window
			f.Type = tc.typ
			f.Direction = tc.dir
			q, args := buildQuery(partitionGlob(root), f, 1)
			n, err := countRows(t, db, q, args)
			require.NoError(t, err)
			require.Equal(t, tc.want, n)
		})
	}
}

// The archive stores these as strings, so the filter has to be encoded
// with the same spelling the writer used. A disagreement does not fail:
// it matches nothing, and an empty result page looks exactly like an
// account with no traffic. Pinned against the reader's own decoder, so
// the pair cannot drift apart on one side only.
func TestTypeAndDirectionStringsRoundTrip(t *testing.T) {
	for _, v := range []store.EventType{
		store.EventTypeStart, store.EventTypeEnd, store.EventTypeDrop,
	} {
		require.Equal(t, v, parseEventType(typeString(v)))
	}
	for _, v := range []store.Direction{
		store.DirectionIngress, store.DirectionEgress,
	} {
		require.Equal(t, v, parseDirection(dirString(v)))
	}

	// The literals themselves, because a round trip through two
	// functions that agree with each other and disagree with
	// flow/sinks/parquet.go would still pass.
	require.Equal(t, "start", typeString(store.EventTypeStart))
	require.Equal(t, "end", typeString(store.EventTypeEnd))
	require.Equal(t, "drop", typeString(store.EventTypeDrop))
	require.Equal(t, "ingress", dirString(store.DirectionIngress))
	require.Equal(t, "egress", dirString(store.DirectionEgress))
}
