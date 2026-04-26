package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	flowProto "github.com/openzro/openzro/flow/proto"
)

// startFlowService spins up a real in-process gRPC server with the
// FlowService registered, returns a client and a cleanup func.
func startFlowService(t *testing.T) (flowProto.FlowServiceClient, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	flowProto.RegisterFlowServiceServer(server, NewFlowService())
	go func() { _ = server.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	cleanup := func() {
		_ = conn.Close()
		server.Stop()
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
