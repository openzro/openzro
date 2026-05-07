package server

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/metadata"

	flowProto "github.com/openzro/openzro/flow/proto"
	"github.com/openzro/openzro/flow/store"
)

// FlowService is the management-side endpoint of the bidirectional
// FlowService.Events stream. Peers send FlowEvents (start/end/drop of
// individual TCP/UDP/ICMP flows); the management server buffers them
// in memory and fans batches out to one or more Sinks (hot store,
// SIEM exporter, cold archive — any combination, configured by the
// operator).
//
// Hot path (Events RPC) is non-blocking: events are pushed onto a
// per-process channel and the peer is acked immediately. A
// background worker drains the channel, batches up to batchSize
// events or until flushEvery elapses, looks up peer→account from a
// cache (with the supplied resolver as the cache miss path), and
// calls Save on every Sink. This decouples peer ack latency from
// any sink's latency, which matters because flow rates can be high
// and a slow Sink (e.g., a SIEM under load) must not block peer
// reporting.
//
// If sinks is empty (engine=none and no exporters configured), the
// service degrades to ack-only — events are dropped after acking.
// This is the expected configuration for the smallest dev setups.
type FlowService struct {
	flowProto.UnimplementedFlowServiceServer

	sinksMu  sync.RWMutex
	sinks    []store.Sink
	resolver PeerResolver
	cache    *peerCache

	bufferSize int
	batchSize  int
	flushEvery time.Duration

	queue  chan *bufferedEvent
	wg     sync.WaitGroup
	stopCh chan struct{}
	closed sync.Once
}

// PeerResolver maps a WireGuard public key (raw 32 bytes from the
// proto) to the peer's openZro ID and owning account. It is the
// only dependency FlowService has on the management's data plane,
// kept narrow so the test surface stays small.
type PeerResolver func(ctx context.Context, pubKey []byte) (peerID, accountID string, err error)

// FlowServiceOption tweaks the buffering parameters. Defaults are
// sized for the small/medium tier per ADR-0002.
type FlowServiceOption func(*FlowService)

// WithBufferSize sets the in-memory queue capacity (default 10000).
// When full, Events drops events with a loud log.
func WithBufferSize(n int) FlowServiceOption {
	return func(s *FlowService) {
		if n > 0 {
			s.bufferSize = n
		}
	}
}

// WithBatchSize sets the max events per Save call (default 500).
func WithBatchSize(n int) FlowServiceOption {
	return func(s *FlowService) {
		if n > 0 {
			s.batchSize = n
		}
	}
}

// WithFlushInterval bounds buffered-event staleness (default 5s).
func WithFlushInterval(d time.Duration) FlowServiceOption {
	return func(s *FlowService) {
		if d > 0 {
			s.flushEvery = d
		}
	}
}

// NewFlowService constructs a FlowService. When sinks is empty the
// service runs in ack-only mode and the resolver is unused. The
// resolver MUST be non-nil when sinks is non-empty; the constructor
// does not check — wiring is internal and the cmd/ side is the only
// caller.
//
// nil entries in sinks are silently skipped, so callers can pass an
// optional Store as the first element without conditionals.
func NewFlowService(sinks []store.Sink, resolver PeerResolver, opts ...FlowServiceOption) *FlowService {
	active := sinks[:0]
	for _, s := range sinks {
		if s != nil {
			active = append(active, s)
		}
	}
	f := &FlowService{
		sinks:      active,
		resolver:   resolver,
		bufferSize: 10000,
		batchSize:  500,
		flushEvery: 5 * time.Second,
		stopCh:     make(chan struct{}),
		cache:      newPeerCache(time.Minute),
	}
	for _, opt := range opts {
		opt(f)
	}
	if len(active) > 0 {
		f.queue = make(chan *bufferedEvent, f.bufferSize)
		f.wg.Add(1)
		go f.runWorker()
	}
	return f
}

// Close stops the background worker and drains any buffered events.
// Safe to call multiple times. Idempotent.
func (s *FlowService) Close() error {
	s.closed.Do(func() {
		if s.queue != nil {
			close(s.stopCh)
			s.wg.Wait()
		}
	})
	return nil
}

// SetSinks atomically replaces the destination set. Sinks that exist
// only in the old set (not carried over to next) are Close()d AFTER
// the swap so the worker never observes a moment with no sinks.
//
// Sinks that appear in BOTH old and next are intentionally kept
// open — Manager.merged() always re-includes the hot flow store and
// any env-baseline sinks alongside the changing dynamic set, so a
// naive "close everything in old" would shut down the hot store on
// every Refresh and leave subsequent reads against /api/network-
// traffic-events failing with `sql: database is closed` (caught in
// the v0.53.1-alpha.26 lab smoke when flow store was wired).
//
// Used by the runtime config Manager to apply changes operators
// make through the dashboard without restarting the process. The
// caller is responsible for not feeding the same Sink instance to
// two services — Close on a Sink is idempotent but Save is not.
func (s *FlowService) SetSinks(next []store.Sink) {
	active := make([]store.Sink, 0, len(next))
	keep := make(map[store.Sink]struct{}, len(next))
	for _, sink := range next {
		if sink != nil {
			active = append(active, sink)
			keep[sink] = struct{}{}
		}
	}

	s.sinksMu.Lock()
	old := s.sinks
	s.sinks = active
	// If the service started without any sinks, spin up the worker
	// now — the queue is created lazily on first activation.
	if s.queue == nil && len(active) > 0 {
		s.queue = make(chan *bufferedEvent, s.bufferSize)
		s.wg.Add(1)
		go s.runWorker()
	}
	s.sinksMu.Unlock()

	for _, sink := range old {
		if _, stillActive := keep[sink]; stillActive {
			continue
		}
		_ = sink.Close()
	}
}

// snapshotSinks returns a pointer-stable copy of the current set so
// flush can iterate without holding the lock across Save calls.
func (s *FlowService) snapshotSinks() []store.Sink {
	s.sinksMu.RLock()
	defer s.sinksMu.RUnlock()
	out := make([]store.Sink, len(s.sinks))
	copy(out, s.sinks)
	return out
}

// bufferedEvent pairs a proto event with its server-side received
// timestamp, captured at ingest time so it is independent of clock
// skew at the peer.
type bufferedEvent struct {
	proto    *flowProto.FlowEvent
	received time.Time
}

// Events implements flow.proto FlowService.Events. Per-event work is:
// receive → enqueue (non-blocking) → ack. Persistence happens off
// the hot path in runWorker.
func (s *FlowService) Events(stream flowProto.FlowService_EventsServer) error {
	ctx := stream.Context()
	// Send initial response headers proactively so the client's
	// stream.Header() returns immediately with non-empty metadata
	// instead of blocking on the first ack. The flow client (see
	// flow/client/client.go:checkHeader) treats an empty header set
	// as "stream broken" and retries forever — which manifested as
	// "flow receiver sent no headers" + "stream not initialized" in
	// every operator deployment that turned on Network Traffic
	// Logs. Empty MD is fine; gRPC-go flushes the END_HEADERS frame
	// regardless and that's all the client checks for.
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		log.WithContext(ctx).Debugf("flow stream send header: %v", err)
	}
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			log.WithContext(ctx).Debugf("flow stream recv: %v", err)
			return err
		}

		if s.queue != nil {
			select {
			case s.queue <- &bufferedEvent{proto: event, received: time.Now().UTC()}:
				// queued
			default:
				log.WithContext(ctx).Errorf(
					"dropped flow event for buffer: channel full (size=%d)", s.bufferSize)
			}
		}

		ack := &flowProto.FlowEventAck{
			EventId:     event.GetEventId(),
			IsInitiator: event.GetIsInitiator(),
		}
		if err := stream.Send(ack); err != nil {
			log.WithContext(ctx).Debugf("flow stream send ack: %v", err)
			return err
		}
	}
}

// runWorker accumulates buffered events and persists batches.
func (s *FlowService) runWorker() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.flushEvery)
	defer ticker.Stop()

	batch := make([]*bufferedEvent, 0, s.batchSize)
	for {
		select {
		case <-s.stopCh:
			for {
				select {
				case ev := <-s.queue:
					batch = append(batch, ev)
				default:
					if len(batch) > 0 {
						s.flush(context.Background(), batch)
					}
					return
				}
			}
		case ev := <-s.queue:
			batch = append(batch, ev)
			if len(batch) >= s.batchSize {
				s.flush(context.Background(), batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				s.flush(context.Background(), batch)
				batch = batch[:0]
			}
		}
	}
}

// flush resolves peer identity for each buffered event and writes
// the batch to every configured sink. Resolution failures are logged
// loud but do NOT fail the batch — events whose peer cannot be
// resolved are dropped (a peer was deleted between sending the event
// and the flush). The successfully-resolved events still land.
//
// Sinks are independent: a failure in one (network blip to the SIEM)
// is logged and we move on to the next. Operators concerned about
// durability run multiple sinks (hot store + SIEM, hot store + S3).
func (s *FlowService) flush(ctx context.Context, batch []*bufferedEvent) {
	events := make([]*store.Event, 0, len(batch))
	for _, b := range batch {
		key := b.proto.GetPublicKey()
		peerID, accountID, ok := s.cache.get(b64key(key))
		if !ok {
			pid, aid, err := s.resolver(ctx, key)
			if err != nil {
				log.WithContext(ctx).Errorf(
					"flow ingest: peer lookup failed for %s: %v", b64key(key), err)
				continue
			}
			peerID, accountID = pid, aid
			s.cache.put(b64key(key), pid, aid)
		}
		events = append(events, fromProto(b.proto, peerID, accountID, b.received))
	}
	if len(events) == 0 {
		return
	}
	for _, sink := range s.snapshotSinks() {
		if err := sink.Save(ctx, events); err != nil {
			log.WithContext(ctx).Errorf(
				"flow sink save failed (%d events): %v", len(events), err)
		}
	}
}

// b64key encodes a raw WireGuard public key the same way the
// management store keys peers internally.
func b64key(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

// fromProto projects a FlowEvent + server-side received timestamp
// into the storage model. Defensive nil-checks on every nested
// message — peers that send malformed events should produce zero
// values, not panics.
func fromProto(p *flowProto.FlowEvent, peerID, accountID string, received time.Time) *store.Event {
	fields := p.GetFlowFields()
	if fields == nil {
		fields = &flowProto.FlowFields{}
	}

	occurred := received
	if t := p.GetTimestamp(); t != nil {
		occurred = t.AsTime()
	}

	ev := &store.Event{
		EventID:        p.GetEventId(),
		FlowID:         fields.GetFlowId(),
		PeerPublicKey:  p.GetPublicKey(),
		IsInitiator:    p.GetIsInitiator(),
		AccountID:      accountID,
		PeerID:         peerID,
		OccurredAt:     occurred,
		ReceivedAt:     received,
		Type:           store.EventType(fields.GetType()),
		Direction:      store.Direction(fields.GetDirection()),
		Protocol:       uint16(fields.GetProtocol()),
		SourceIP:       net.IP(fields.GetSourceIp()).String(),
		DestIP:         net.IP(fields.GetDestIp()).String(),
		RxPackets:      fields.GetRxPackets(),
		TxPackets:      fields.GetTxPackets(),
		RxBytes:        fields.GetRxBytes(),
		TxBytes:        fields.GetTxBytes(),
		RuleID:         fields.GetRuleId(),
		SourceResource: fields.GetSourceResourceId(),
		DestResource:   fields.GetDestResourceId(),
	}

	if portInfo := fields.GetPortInfo(); portInfo != nil {
		ev.SourcePort = portInfo.GetSourcePort()
		ev.DestPort = portInfo.GetDestPort()
	}
	if icmpInfo := fields.GetIcmpInfo(); icmpInfo != nil {
		ev.ICMPType = uint16(icmpInfo.GetIcmpType())
		ev.ICMPCode = uint16(icmpInfo.GetIcmpCode())
	}

	return ev
}

// peerCache is a small TTL cache for pubkey → (peerID, accountID).
// Entries expire after ttl so a peer re-registered under a different
// account does not leak across the boundary indefinitely. Map churn
// is bounded by peer count (small relative to flow event count).
type peerCache struct {
	mu  sync.RWMutex
	e   map[string]peerCacheEntry
	ttl time.Duration
}

type peerCacheEntry struct {
	peerID    string
	accountID string
	expiresAt time.Time
}

func newPeerCache(ttl time.Duration) *peerCache {
	return &peerCache{e: map[string]peerCacheEntry{}, ttl: ttl}
}

func (c *peerCache) get(key string) (peerID, accountID string, ok bool) {
	c.mu.RLock()
	e, found := c.e[key]
	c.mu.RUnlock()
	if !found || time.Now().After(e.expiresAt) {
		return "", "", false
	}
	return e.peerID, e.accountID, true
}

func (c *peerCache) put(key, peerID, accountID string) {
	c.mu.Lock()
	c.e[key] = peerCacheEntry{
		peerID:    peerID,
		accountID: accountID,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}
