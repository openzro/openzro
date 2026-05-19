package client_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	flow "github.com/openzro/openzro/flow/client"
	"github.com/openzro/openzro/flow/proto"
)

type testServer struct {
	proto.UnimplementedFlowServiceServer
	events  chan *proto.FlowEvent
	acks    chan *proto.FlowEventAck
	grpcSrv *grpc.Server
	addr    string
}

func newTestServer(t *testing.T) *testServer {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := &testServer{
		events:  make(chan *proto.FlowEvent, 100),
		acks:    make(chan *proto.FlowEventAck, 100),
		grpcSrv: grpc.NewServer(),
		addr:    listener.Addr().String(),
	}

	proto.RegisterFlowServiceServer(s.grpcSrv, s)

	go func() {
		if err := s.grpcSrv.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Logf("server error: %v", err)
		}
	}()

	t.Cleanup(func() {
		s.grpcSrv.Stop()
	})

	return s
}

func (s *testServer) Events(stream proto.FlowService_EventsServer) error {
	err := stream.Send(&proto.FlowEventAck{IsInitiator: true})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	go func() {
		defer cancel()
		for {
			event, err := stream.Recv()
			if err != nil {
				return
			}

			if !event.IsInitiator {
				select {
				case s.events <- event:
					ack := &proto.FlowEventAck{
						EventId: event.EventId,
					}
					select {
					case s.acks <- ack:
					case <-ctx.Done():
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	for {
		select {
		case ack := <-s.acks:
			if err := stream.Send(ack); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func TestReceive(t *testing.T) {
	server := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	client, err := flow.NewClient("http://"+server.addr, "test-payload", "test-signature", 1*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := client.Close()
		assert.NoError(t, err, "failed to close flow")
	})

	receivedAcks := make(map[string]bool)
	receiveDone := make(chan struct{})

	go func() {
		err := client.Receive(ctx, 1*time.Second, func(msg *proto.FlowEventAck) error {
			if !msg.IsInitiator && len(msg.EventId) > 0 {
				id := string(msg.EventId)
				receivedAcks[id] = true

				if len(receivedAcks) >= 3 {
					close(receiveDone)
				}
			}
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("receive error: %v", err)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	for i := 0; i < 3; i++ {
		eventID := uuid.New().String()

		// Create acknowledgment and send it to the flow through our test server
		ack := &proto.FlowEventAck{
			EventId: []byte(eventID),
		}

		select {
		case server.acks <- ack:
		case <-time.After(time.Second):
			t.Fatal("timeout sending ack")
		}
	}

	select {
	case <-receiveDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for acks to be processed")
	}

	assert.Equal(t, 3, len(receivedAcks))
}

func TestReceive_ContextCancellation(t *testing.T) {
	server := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	client, err := flow.NewClient("http://"+server.addr, "test-payload", "test-signature", 1*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := client.Close()
		assert.NoError(t, err, "failed to close flow")
	})

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	handlerCalled := false
	msgHandler := func(msg *proto.FlowEventAck) error {
		if !msg.IsInitiator {
			handlerCalled = true
		}
		return nil
	}

	err = client.Receive(ctx, 1*time.Second, msgHandler)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
	assert.False(t, handlerCalled)
}

func TestSend(t *testing.T) {
	server := newTestServer(t)

	client, err := flow.NewClient("http://"+server.addr, "test-payload", "test-signature", 1*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := client.Close()
		assert.NoError(t, err, "failed to close flow")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	ackReceived := make(chan struct{})

	go func() {
		err := client.Receive(ctx, 1*time.Second, func(ack *proto.FlowEventAck) error {
			if len(ack.EventId) > 0 && !ack.IsInitiator {
				close(ackReceived)
			}
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("receive error: %v", err)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	testEvent := &proto.FlowEvent{
		EventId:   []byte("test-event-id"),
		PublicKey: []byte("test-public-key"),
		FlowFields: &proto.FlowFields{
			FlowId:   []byte("test-flow-id"),
			Protocol: 6,
			SourceIp: []byte{192, 168, 1, 1},
			DestIp:   []byte{192, 168, 1, 2},
			ConnectionInfo: &proto.FlowFields_PortInfo{
				PortInfo: &proto.PortInfo{
					SourcePort: 12345,
					DestPort:   443,
				},
			},
		},
	}

	err = client.Send(testEvent)
	require.NoError(t, err)

	var receivedEvent *proto.FlowEvent
	select {
	case receivedEvent = <-server.events:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event to be received by server")
	}

	assert.Equal(t, testEvent.EventId, receivedEvent.EventId)
	assert.Equal(t, testEvent.PublicKey, receivedEvent.PublicKey)

	select {
	case <-ackReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ack to be received by flow")
	}
}

// TestReceive_ClosedDuringRetryReturnsClientClosed verifies that
// closing the client mid-retry causes Receive to unwind with
// ErrClientClosed instead of looping forever on the dead conn. The
// recreate-on-failure path checks the closed flag under streamMu
// before dialing a fresh grpc.ClientConn; without that check, Close
// would race with a new dial and the Receive goroutine could keep
// trying past Close indefinitely on slow networks.
func TestReceive_ClosedDuringRetryReturnsClientClosed(t *testing.T) {
	server := newTestServer(t)

	// Use a context that won't expire before we get to Close — we
	// want the closure (not the context) to terminate the loop.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	client, err := flow.NewClient("http://"+server.addr, "test-payload", "test-signature", 200*time.Millisecond)
	require.NoError(t, err)

	// Force the stream into a retry cycle: stop the server, then call
	// Close on the client a moment later. The Receive loop should
	// observe the closed flag inside recreateConnection or
	// establishStream and return ErrClientClosed.
	server.grpcSrv.Stop()

	receiveDone := make(chan error, 1)
	go func() {
		receiveDone <- client.Receive(ctx, 200*time.Millisecond, func(*proto.FlowEventAck) error { return nil })
	}()

	// Give the loop one cycle to enter retry, then close.
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, client.Close())

	select {
	case err := <-receiveDone:
		require.Error(t, err)
		assert.True(t, errors.Is(err, flow.ErrClientClosed) || errors.Is(err, context.Canceled),
			"Receive should unwind with ErrClientClosed or context cancellation after Close; got %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Receive did not unwind within 5s after Close")
	}
}

// TestReceive_RejectsConcurrentCall is a regression test for the
// double-Receive bug: nothing in the API prevented two goroutines
// from each opening a fresh stream against the same GRPCClient.
// The realistic trigger is the netflow manager's resetClient path
// — when a config Update flips URL or token, a new receiver
// goroutine spawns before the previous one has unwound, and both
// race on the shared `stream` pointer + ack handling. Production
// users saw duplicate ACK processing and warnings about
// "stream not initialized".
//
// Post-fix: the second Receive returns ErrConcurrentReceive
// immediately, leaving the first stream untouched.
func TestReceive_RejectsConcurrentCall(t *testing.T) {
	server := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	client, err := flow.NewClient("http://"+server.addr, "test-payload", "test-signature", 1*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	firstActive := make(chan struct{})
	firstDone := make(chan struct{})

	go func() {
		defer close(firstDone)
		// Signal that we are inside Receive — the server-side stream
		// handler runs once the initiator handshake clears, by which
		// time the receiving flag is set.
		go func() {
			time.Sleep(200 * time.Millisecond)
			close(firstActive)
		}()
		err := client.Receive(ctx, time.Second, func(*proto.FlowEventAck) error { return nil })
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Logf("first receive returned: %v", err)
		}
	}()

	<-firstActive

	// Second concurrent call should be refused immediately.
	err = client.Receive(ctx, time.Second, func(*proto.FlowEventAck) error { return nil })
	require.ErrorIs(t, err, flow.ErrConcurrentReceive, "second concurrent Receive must be refused")

	// Tear down by canceling — first goroutine should exit cleanly.
	cancel()
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first Receive did not unwind after context cancel")
	}
}
