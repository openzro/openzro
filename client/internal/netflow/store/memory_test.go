package store

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/client/internal/netflow/types"
)

func newEvent() *types.Event {
	return &types.Event{ID: uuid.New()}
}

// TestMemory_BoundsAtMaxEvents asserts the memory cap prevents the
// in-memory queue from growing unbounded during a management
// outage. Pre-fix the map grew forever; a multi-hour outage at
// realistic event rates (~10/sec/peer) OOM'd peer hosts.
func TestMemory_BoundsAtMaxEvents(t *testing.T) {
	orig := maxEvents
	maxEvents = 100
	defer func() { maxEvents = orig }()

	m := NewMemoryStore()
	for i := 0; i < 250; i++ {
		m.StoreEvent(newEvent())
	}

	assert.Equal(t, 100, len(m.events), "events map must not exceed cap")
	assert.Equal(t, 100, m.order.Len(), "order list must mirror map size")
	assert.Equal(t, uint64(150), m.DroppedCount(), "must record every shed event")
}

// TestMemory_FIFOEvictionOrder confirms the cap evicts the OLDEST
// entries (FIFO), preserving the most recent events for the next
// flush. The opposite order would leave the daemon shipping stale
// history while losing fresh data — backwards from what an operator
// expects after a transient outage.
func TestMemory_FIFOEvictionOrder(t *testing.T) {
	orig := maxEvents
	maxEvents = 3
	defer func() { maxEvents = orig }()

	m := NewMemoryStore()
	a, b, c, d, e := newEvent(), newEvent(), newEvent(), newEvent(), newEvent()
	for _, ev := range []*types.Event{a, b, c, d, e} {
		m.StoreEvent(ev)
	}

	// Only the last three survived; the order returned by GetEvents
	// must be c, d, e (oldest-first inside the survivors).
	got := m.GetEvents()
	require.Len(t, got, 3)
	assert.Equal(t, c.ID, got[0].ID)
	assert.Equal(t, d.ID, got[1].ID)
	assert.Equal(t, e.ID, got[2].ID)
}

// TestMemory_DeleteRemovesFromOrderList covers the ACK-driven
// removal path. Once management ACKs an event, we drop it from both
// the map AND the order list so future evictions don't try to evict
// a stale ID and so memory actually returns to the runtime.
func TestMemory_DeleteRemovesFromOrderList(t *testing.T) {
	m := NewMemoryStore()
	a, b, c := newEvent(), newEvent(), newEvent()
	m.StoreEvent(a)
	m.StoreEvent(b)
	m.StoreEvent(c)

	m.DeleteEvents([]uuid.UUID{b.ID})

	assert.Equal(t, 2, m.order.Len(), "order list must shrink alongside the map")
	got := m.GetEvents()
	require.Len(t, got, 2)
	assert.Equal(t, a.ID, got[0].ID)
	assert.Equal(t, c.ID, got[1].ID)
}

// TestMemory_GetEventsFIFOOrder regression: before the cap, the
// store iterated the events map in Go's randomized order. With FIFO
// guarantees we want oldest-first so the gRPC sender ships events
// in the order they were captured.
func TestMemory_GetEventsFIFOOrder(t *testing.T) {
	m := NewMemoryStore()
	events := make([]*types.Event, 5)
	for i := range events {
		events[i] = newEvent()
		m.StoreEvent(events[i])
	}

	got := m.GetEvents()
	require.Len(t, got, 5)
	for i := range got {
		assert.Equal(t, events[i].ID, got[i].ID, "GetEvents must be FIFO")
	}
}

// TestMemory_DuplicateStoreUpdatesInPlace covers the (non-typical)
// case where the same uuid is stored twice. The payload updates but
// position in the FIFO does not advance — the entry keeps its
// original eviction priority.
func TestMemory_DuplicateStoreUpdatesInPlace(t *testing.T) {
	m := NewMemoryStore()
	a, b := newEvent(), newEvent()
	m.StoreEvent(a)
	m.StoreEvent(b)

	// Re-store `a` with the same ID — should not move it to the back.
	m.StoreEvent(a)

	assert.Equal(t, 2, m.order.Len(), "duplicate StoreEvent must not add a second entry")
	got := m.GetEvents()
	require.Len(t, got, 2)
	assert.Equal(t, a.ID, got[0].ID, "duplicate must keep original FIFO position")
	assert.Equal(t, b.ID, got[1].ID)
}

// TestResolveMaxEvents_DefaultAndOverride covers the env var
// plumbing without touching the package var directly. Operators
// flip OPENZRO_FLOW_LOGGER_MAX_EVENTS via the chart.
func TestResolveMaxEvents_DefaultAndOverride(t *testing.T) {
	t.Setenv(envMaxEvents, "")
	assert.Equal(t, defaultMaxEvents, resolveMaxEvents())

	t.Setenv(envMaxEvents, "12345")
	assert.Equal(t, 12345, resolveMaxEvents())

	t.Setenv(envMaxEvents, "garbage")
	assert.Equal(t, defaultMaxEvents, resolveMaxEvents(),
		"invalid value falls back to default")

	t.Setenv(envMaxEvents, "-5")
	assert.Equal(t, defaultMaxEvents, resolveMaxEvents(),
		"non-positive falls back to default")
}
