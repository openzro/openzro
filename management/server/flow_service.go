package server

import (
	"io"

	log "github.com/sirupsen/logrus"

	flowProto "github.com/openzro/openzro/flow/proto"
)

// FlowService is the management-side endpoint of the bidirectional
// stream of network flow events from peers. Today it accepts events
// and acknowledges them so that clients with FlowEnabled in their
// account settings do not error or back-pressure on the wire — but it
// does not persist them.
//
// Persistence, querying, and the dashboard surface land in a follow-up
// PR (see roadmap M2 "traffic events"). The split exists because:
//
//   - The schema decision (relational vs columnar; retention strategy;
//     partitioning) is non-trivial and merits a dedicated ADR.
//   - Without an ack-only handler in place, clients hit
//     "Unimplemented" gRPC errors and may misbehave.
//
// Once persistence exists, the implementation in this file is
// replaced; the wiring at management.go does not change.
type FlowService struct {
	flowProto.UnimplementedFlowServiceServer
}

// NewFlowService returns an ack-only FlowService implementation.
func NewFlowService() *FlowService {
	return &FlowService{}
}

// Events receives a stream of FlowEvents from a peer and returns an
// ack for each one. The stream is half-duplex from the client's
// perspective: it sends events, and for every event we reply with an
// ack message so its in-memory buffer can be drained.
//
// Errors from the wire (client disconnect, deadline) are surfaced to
// the gRPC layer; everything else is best-effort and logged.
func (s *FlowService) Events(stream flowProto.FlowService_EventsServer) error {
	ctx := stream.Context()
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			log.WithContext(ctx).Debugf("flow stream recv: %v", err)
			return err
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
