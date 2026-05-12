package server

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	flowProto "github.com/openzro/openzro/flow/proto"
	flowstore "github.com/openzro/openzro/flow/store"
)

// startFlowService spins up a real in-process gRPC server with the
// FlowService registered, returns a client and a cleanup func. The
// service is built in ack-only mode (no sinks) so these tests
// exercise the gRPC contract without persistence.
func startFlowService(t *testing.T) (flowProto.FlowServiceClient, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	fs := NewFlowService(nil, nil)
	flowProto.RegisterFlowServiceServer(server, fs)
	_ = fs
	go func() { _ = server.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	cleanup := func() {
		_ = conn.Close()
		server.Stop()
		_ = fs.Close()
	}
	return flowProto.NewFlowServiceClient(conn), cleanup
}

func TestFlowService_AcksEverySentEvent(t *testing.T) {
	client, cleanup := startFlowService(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Events(ctx)
	require.NoError(t, err)

	send := []*flowProto.FlowEvent{
		{EventId: []byte("e1"), IsInitiator: true},
		{EventId: []byte("e2"), IsInitiator: false},
		{EventId: []byte("e3"), IsInitiator: true},
	}

	for _, ev := range send {
		require.NoError(t, stream.Send(ev))
	}
	require.NoError(t, stream.CloseSend())

	got := []*flowProto.FlowEventAck{}
	for {
		ack, err := stream.Recv()
		if err != nil {
			break
		}
		// Drop the server's leading IsInitiator ack — the handler
		// emits one with empty EventId on stream-open to flush the
		// initial gRPC HEADERS frame past intermediaries that
		// otherwise stall the client's Header() call. The flow
		// client at flow/client/client.go:142 ignores these too.
		if ack.IsInitiator && len(ack.EventId) == 0 {
			continue
		}
		got = append(got, ack)
	}

	require.Len(t, got, len(send), "must ack one-for-one")
	for i, ack := range got {
		assert.Equal(t, send[i].EventId, ack.EventId,
			"ack must echo event_id so the client can correlate")
		assert.Equal(t, send[i].IsInitiator, ack.IsInitiator)
	}
}

func TestFlowService_DoesNotErrorOnEmptyStream(t *testing.T) {
	client, cleanup := startFlowService(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := client.Events(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.CloseSend())

	_, err = stream.Recv()
	// Server returns nil on EOF; client surfaces that as io.EOF on the
	// next Recv. Either way, no transport error.
	assert.True(t, err == nil || err.Error() == "EOF" || err.Error() == "rpc error: code = Unknown desc = EOF",
		"clean close must not surface as a transport error: %v", err)
}

// inMemoryStore is a minimal store.Store for ingestion tests; it just
// records every batch passed to Save. Real DB roundtrip is exercised
// in flow/store/sql tests.
type inMemoryStore struct {
	mu     sync.Mutex
	saved  []*flowstore.Event
	saveCh chan struct{}
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{saveCh: make(chan struct{}, 16)}
}

func (s *inMemoryStore) Save(_ context.Context, events []*flowstore.Event) error {
	s.mu.Lock()
	s.saved = append(s.saved, events...)
	s.mu.Unlock()
	select {
	case s.saveCh <- struct{}{}:
	default:
	}
	return nil
}

func (s *inMemoryStore) Query(context.Context, flowstore.Filter) ([]*flowstore.Event, error) {
	return nil, nil
}
func (s *inMemoryStore) Purge(context.Context, time.Time) (int64, error) { return 0, nil }
func (s *inMemoryStore) Close() error                                    { return nil }

func (s *inMemoryStore) all() []*flowstore.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*flowstore.Event, len(s.saved))
	copy(out, s.saved)
	return out
}

// startFlowServiceWithStore starts a service that persists events and
// resolves peer keys via the given resolver function.
func startFlowServiceWithStore(t *testing.T, store flowstore.Sink, resolver PeerResolver) (flowProto.FlowServiceClient, *FlowService, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	fs := NewFlowService([]flowstore.Sink{store}, resolver,
		WithBatchSize(2),                       // small batch so tests don't wait
		WithFlushInterval(50*time.Millisecond), // and tight flush
	)
	flowProto.RegisterFlowServiceServer(server, fs)
	go func() { _ = server.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	cleanup := func() {
		_ = conn.Close()
		server.Stop()
		_ = fs.Close()
	}
	return flowProto.NewFlowServiceClient(conn), fs, cleanup
}

func TestFlowService_PersistsResolvedEvents(t *testing.T) {
	mem := newInMemoryStore()
	pubKey := []byte("01234567890123456789012345678901") // exactly 32 bytes
	resolver := func(_ context.Context, key []byte) (string, string, error) {
		assert.Equal(t, pubKey, key, "resolver receives the raw key bytes from the proto")
		return "peer-1", "acct-1", nil
	}

	client, _, cleanup := startFlowServiceWithStore(t, mem, resolver)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Events(ctx)
	require.NoError(t, err)

	require.NoError(t, stream.Send(&flowProto.FlowEvent{
		EventId:   []byte("e1"),
		PublicKey: pubKey,
		FlowFields: &flowProto.FlowFields{
			FlowId:    []byte("f1"),
			Type:      flowProto.Type_TYPE_START,
			Direction: flowProto.Direction_EGRESS,
			Protocol:  6,
			SourceIp:  []byte{10, 0, 0, 1},
			DestIp:    []byte{10, 0, 0, 2},
			ConnectionInfo: &flowProto.FlowFields_PortInfo{
				PortInfo: &flowProto.PortInfo{SourcePort: 49152, DestPort: 443},
			},
			RxBytes: 100,
			TxBytes: 200,
		},
	}))
	require.NoError(t, stream.CloseSend())

	// Drain ack so the stream is clean.
	if _, err := stream.Recv(); err != nil && err != io.EOF {
		t.Fatalf("recv ack: %v", err)
	}

	select {
	case <-mem.saveCh:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not flush within the deadline")
	}

	all := mem.all()
	require.Len(t, all, 1)
	got := all[0]
	assert.Equal(t, "peer-1", got.PeerID, "peer ID must come from the resolver, not the wire")
	assert.Equal(t, "acct-1", got.AccountID, "account isolation derives from the resolver")
	assert.Equal(t, "10.0.0.1", got.SourceIP)
	assert.Equal(t, "10.0.0.2", got.DestIP)
	assert.Equal(t, uint32(49152), got.SourcePort)
	assert.Equal(t, uint32(443), got.DestPort)
	assert.Equal(t, flowstore.EventTypeStart, got.Type)
	assert.Equal(t, flowstore.DirectionEgress, got.Direction)
	assert.Equal(t, uint16(6), got.Protocol)
	assert.Equal(t, uint64(100), got.RxBytes)
	assert.Equal(t, uint64(200), got.TxBytes)
}

func TestFlowService_DropsEventsWithUnknownPeer(t *testing.T) {
	mem := newInMemoryStore()
	resolver := func(context.Context, []byte) (string, string, error) {
		return "", "", io.ErrUnexpectedEOF // any non-nil error
	}

	client, _, cleanup := startFlowServiceWithStore(t, mem, resolver)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := client.Events(ctx)
	require.NoError(t, err)

	require.NoError(t, stream.Send(&flowProto.FlowEvent{
		EventId:   []byte("e1"),
		PublicKey: []byte("unknown-peer-key-padding-32-byte"),
	}))
	require.NoError(t, stream.CloseSend())
	if _, err := stream.Recv(); err != nil && err != io.EOF {
		t.Fatalf("recv: %v", err)
	}

	// Wait for at least one tick — even with unresolvable events, the
	// worker should run at least once.
	time.Sleep(150 * time.Millisecond)

	assert.Empty(t, mem.all(),
		"events whose peer cannot be resolved must be dropped, never persisted with empty account_id")
}

// fakePolicyResolver lets tests inspect resolver invocations and
// optionally fill RuleID with a canned value. Mirrors the
// PolicyResolver interface flow_service expects.
type fakePolicyResolver struct {
	mu      sync.Mutex
	calls   []fakePolicyCall
	stampID string // when set, all unstamped events get this RuleID
}

type fakePolicyCall struct {
	accountID string
	ruleID    string
}

func (f *fakePolicyResolver) Resolve(accountID string, e *flowstore.Event) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakePolicyCall{accountID: accountID, ruleID: string(e.RuleID)})
	if len(e.RuleID) != 0 {
		return false
	}
	if f.stampID == "" {
		return false
	}
	e.RuleID = []byte(f.stampID)
	return true
}

func (f *fakePolicyResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// startFlowServiceWithPolicyResolver wires the service with both a
// store and an ADR-0018 policy resolver. Used by the resolver-wiring
// tests below.
func startFlowServiceWithPolicyResolver(t *testing.T, store flowstore.Sink, resolver PeerResolver, policy PolicyResolver) (flowProto.FlowServiceClient, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	fs := NewFlowService(
		[]flowstore.Sink{store}, resolver,
		WithBatchSize(2),
		WithFlushInterval(50*time.Millisecond),
		WithPolicyResolver(policy),
	)
	flowProto.RegisterFlowServiceServer(server, fs)
	go func() { _ = server.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	cleanup := func() {
		_ = conn.Close()
		server.Stop()
		_ = fs.Close()
	}
	return flowProto.NewFlowServiceClient(conn), cleanup
}

// TestFlowService_PolicyResolver_FillsEmptyRuleID covers the
// happy-path wiring (ADR-0018): an event arrives with no RuleID,
// the resolver fills it, the stored event carries the resolver's
// PolicyID.
func TestFlowService_PolicyResolver_FillsEmptyRuleID(t *testing.T) {
	mem := newInMemoryStore()
	pubKey := []byte("01234567890123456789012345678901")
	peerResolver := func(context.Context, []byte) (string, string, error) {
		return "peer-1", "acct-1", nil
	}
	policy := &fakePolicyResolver{stampID: "p-resolved-by-server"}

	client, cleanup := startFlowServiceWithPolicyResolver(t, mem, peerResolver, policy)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.Events(ctx)
	require.NoError(t, err)

	require.NoError(t, stream.Send(&flowProto.FlowEvent{
		EventId:   []byte("e-unstamped"),
		PublicKey: pubKey,
		FlowFields: &flowProto.FlowFields{
			FlowId:   []byte("f1"),
			Protocol: 6,
			SourceIp: []byte{10, 0, 0, 1},
			DestIp:   []byte{10, 0, 0, 2},
			ConnectionInfo: &flowProto.FlowFields_PortInfo{
				PortInfo: &flowProto.PortInfo{DestPort: 443},
			},
			// RuleId intentionally empty — the Linux kernel
			// outbound-initiator path can't stamp it.
		},
	}))
	require.NoError(t, stream.CloseSend())
	if _, err := stream.Recv(); err != nil && err != io.EOF {
		t.Fatalf("recv: %v", err)
	}

	select {
	case <-mem.saveCh:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not flush")
	}

	all := mem.all()
	require.Len(t, all, 1)
	assert.Equal(t, "p-resolved-by-server", string(all[0].RuleID),
		"unstamped event must carry the resolver's PolicyID after persist")
	assert.Equal(t, 1, policy.callCount(), "resolver must be consulted once per unstamped event")
}

// TestFlowService_PolicyResolver_DoesNotOverwriteAgentStamp asserts
// that events the agent already stamped (the ADR-0013 primary path:
// inbound, routing-forward, uspfilter) pass through untouched. The
// resolver is still consulted to expose the metric, but it returns
// false and leaves the event alone.
func TestFlowService_PolicyResolver_DoesNotOverwriteAgentStamp(t *testing.T) {
	mem := newInMemoryStore()
	pubKey := []byte("01234567890123456789012345678901")
	peerResolver := func(context.Context, []byte) (string, string, error) {
		return "peer-1", "acct-1", nil
	}
	policy := &fakePolicyResolver{stampID: "p-resolver-would-have-used"}

	client, cleanup := startFlowServiceWithPolicyResolver(t, mem, peerResolver, policy)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.Events(ctx)
	require.NoError(t, err)

	require.NoError(t, stream.Send(&flowProto.FlowEvent{
		EventId:   []byte("e-stamped"),
		PublicKey: pubKey,
		FlowFields: &flowProto.FlowFields{
			FlowId:   []byte("f1"),
			Protocol: 6,
			SourceIp: []byte{10, 0, 0, 1},
			DestIp:   []byte{10, 0, 0, 2},
			ConnectionInfo: &flowProto.FlowFields_PortInfo{
				PortInfo: &flowProto.PortInfo{DestPort: 443},
			},
			RuleId: []byte("agent-stamped-policy-id"),
		},
	}))
	require.NoError(t, stream.CloseSend())
	if _, err := stream.Recv(); err != nil && err != io.EOF {
		t.Fatalf("recv: %v", err)
	}

	select {
	case <-mem.saveCh:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not flush")
	}

	all := mem.all()
	require.Len(t, all, 1)
	assert.Equal(t, "agent-stamped-policy-id", string(all[0].RuleID),
		"agent-stamped RuleID must NOT be overwritten")
}
