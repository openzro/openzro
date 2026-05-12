package store

import (
	"container/list"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/client/internal/netflow/types"
)

const (
	// defaultMaxEvents caps the in-memory queue of un-ACK'd flow
	// events. Hit during management stream outages: the gRPC
	// reconnect loop keeps trying, conntrack keeps generating, the
	// peer keeps buffering — without this cap the daemon would OOM
	// on the host after a multi-hour outage. 50000 events at ~200B
	// each is ~10MB of resident memory, the right balance between
	// "survive a network blip without losing data" and "do not
	// crash the host".
	defaultMaxEvents = 50000
	envMaxEvents     = "OPENZRO_FLOW_LOGGER_MAX_EVENTS"
)

// maxEvents is resolved once at package init. Operators flip the
// env var when their event rate or expected outage window calls for
// a different ceiling.
var maxEvents = resolveMaxEvents()

func resolveMaxEvents() int {
	raw := os.Getenv(envMaxEvents)
	if raw == "" {
		return defaultMaxEvents
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		log.Warnf("ignoring invalid %s=%q, using default %d", envMaxEvents, raw, defaultMaxEvents)
		return defaultMaxEvents
	}
	log.Infof("%s=%d (overriding default %d)", envMaxEvents, n, defaultMaxEvents)
	return n
}

// entry pairs the event with its position in the order list so
// DeleteEvents can pop the linked-list node in O(1).
type entry struct {
	event *types.Event
	elem  *list.Element // value: event.ID
}

func NewMemoryStore() *Memory {
	return &Memory{
		events: make(map[uuid.UUID]*entry),
		order:  list.New(),
	}
}

// Memory is a bounded in-memory FIFO of flow events awaiting an ACK
// from management. New events go on the tail; when the cap is hit
// the oldest entry is evicted with a loud log (rate-limited so a
// long outage does not flood the daemon log).
type Memory struct {
	mux     sync.Mutex
	events  map[uuid.UUID]*entry
	order   *list.List // oldest at Front
	dropped atomic.Uint64
}

func (m *Memory) StoreEvent(event *types.Event) {
	m.mux.Lock()
	defer m.mux.Unlock()

	if existing, ok := m.events[event.ID]; ok {
		// Same uuid arriving again is not expected in production
		// (logger.go assigns uuid.New() per Store), but if it
		// happens, overwrite the payload without disturbing FIFO
		// position.
		existing.event = event
		return
	}

	if m.order.Len() >= maxEvents {
		m.evictOldest()
	}

	elem := m.order.PushBack(event.ID)
	m.events[event.ID] = &entry{event: event, elem: elem}
}

// evictOldest drops the front-of-list entry. Caller holds mux.
func (m *Memory) evictOldest() {
	oldest := m.order.Front()
	if oldest == nil {
		return
	}
	id := oldest.Value.(uuid.UUID)
	m.order.Remove(oldest)
	delete(m.events, id)

	// Rate-limited drop log: emit only at powers of two so a
	// multi-hour outage does not produce one log line per event.
	// Operators see the first drop, then drops at 2, 4, 8, 16, ...
	// — enough signal to alert without flooding journals.
	n := m.dropped.Add(1)
	if n&(n-1) == 0 {
		log.Errorf(
			"flow logger dropped %d event(s): in-memory cap %d reached. "+
				"Management stream likely unreachable; oldest events are being "+
				"shed to bound peer memory.",
			n, maxEvents,
		)
	}
}

func (m *Memory) Close() {
	m.mux.Lock()
	defer m.mux.Unlock()
	m.events = make(map[uuid.UUID]*entry)
	m.order.Init()
}

func (m *Memory) GetEvents() []*types.Event {
	m.mux.Lock()
	defer m.mux.Unlock()
	events := make([]*types.Event, 0, len(m.events))
	// Iterate in FIFO order so the sender ships oldest first —
	// preserves chronological ordering on the management side.
	for elem := m.order.Front(); elem != nil; elem = elem.Next() {
		id := elem.Value.(uuid.UUID)
		if e, ok := m.events[id]; ok {
			events = append(events, e.event)
		}
	}
	return events
}

func (m *Memory) DeleteEvents(ids []uuid.UUID) {
	m.mux.Lock()
	defer m.mux.Unlock()
	for _, id := range ids {
		if e, ok := m.events[id]; ok {
			m.order.Remove(e.elem)
			delete(m.events, id)
		}
	}
}

// DroppedCount returns the cumulative number of events the cap has
// shed since process start. Exposed for tests and for telemetry that
// wants to surface "agent is shedding events" as an alertable
// signal — atomic load so callers do not need to hold mux.
func (m *Memory) DroppedCount() uint64 {
	return m.dropped.Load()
}
