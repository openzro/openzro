package netflow

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/openzro/openzro/client/firewall/policymark"
	"github.com/openzro/openzro/client/internal/netflow/conntrack"
	"github.com/openzro/openzro/client/internal/netflow/filter"
	"github.com/openzro/openzro/client/internal/netflow/logger"
	nftypes "github.com/openzro/openzro/client/internal/netflow/types"
	"github.com/openzro/openzro/client/internal/peer"
	"github.com/openzro/openzro/flow/client"
	"github.com/openzro/openzro/flow/proto"
)

// Manager handles netflow tracking and logging
type Manager struct {
	mux sync.Mutex
	// shutdownWg tracks the sender + receiveACKs goroutines so Close()
	// can wait for them to exit before returning. Without this, a
	// Close was fire-and-forget — the daemon could exit while the
	// gRPC stream still had pending Send calls, leaking buffered
	// events at every restart and producing flaky teardown in tests.
	shutdownWg     sync.WaitGroup
	logger         nftypes.FlowLogger
	flowConfig     *nftypes.FlowConfig
	conntrack      nftypes.ConnTracker
	receiverClient *client.GRPCClient
	publicKey      []byte
	cancel         context.CancelFunc
}

// NewManager creates a new netflow manager
func NewManager(iface nftypes.IFaceMapper, publicKey []byte, statusRecorder *peer.Status) *Manager {
	var prefix netip.Prefix
	if iface != nil {
		prefix = iface.Address().Network
	}
	flowLogger := logger.New(statusRecorder, prefix)

	var ct nftypes.ConnTracker
	if runtime.GOOS == "linux" && iface != nil && !iface.IsUserspaceBind() {
		// ADR-0013: hand the netlink collector a reference to the
		// process-wide policymark indexer so it can translate the
		// rule_index that nftables/iptables stamped on the ct mark
		// back into a PolicyID inside the FlowEvent. Both writer
		// and reader share the same Default() singleton.
		ct = conntrack.New(flowLogger, iface, policymark.Default())
	}

	return &Manager{
		logger:    flowLogger,
		conntrack: ct,
		publicKey: publicKey,
	}
}

// Update applies new flow configuration settings
// needsNewClient checks if a new client needs to be created
func (m *Manager) needsNewClient(previous *nftypes.FlowConfig) bool {
	current := m.flowConfig
	return previous == nil ||
		!previous.Enabled ||
		previous.TokenPayload != current.TokenPayload ||
		previous.TokenSignature != current.TokenSignature ||
		previous.URL != current.URL
}

// enableFlow starts components for flow tracking
func (m *Manager) enableFlow(previous *nftypes.FlowConfig) error {
	// first make sender ready so events don't pile up
	if m.needsNewClient(previous) {
		if err := m.resetClient(); err != nil {
			return fmt.Errorf("reset client: %w", err)
		}
	}

	m.logger.Enable()

	if m.conntrack != nil {
		if err := m.conntrack.Start(m.flowConfig.Counters); err != nil {
			return fmt.Errorf("start conntrack: %w", err)
		}
	}

	return nil
}

func (m *Manager) resetClient() error {
	if m.receiverClient != nil {
		if err := m.receiverClient.Close(); err != nil {
			log.Warnf("error closing previous flow client: %v", err)
		}
	}

	flowClient, err := client.NewClient(m.flowConfig.URL, m.flowConfig.TokenPayload, m.flowConfig.TokenSignature, m.flowConfig.Interval)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	log.Infof("flow client configured to connect to %s", m.flowConfig.URL)

	m.receiverClient = flowClient

	if m.cancel != nil {
		m.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	m.shutdownWg.Add(2)
	go func() {
		defer m.shutdownWg.Done()
		m.receiveACKs(ctx, flowClient)
	}()
	go func() {
		defer m.shutdownWg.Done()
		m.startSender(ctx)
	}()

	return nil
}

// disableFlow stops components for flow tracking
func (m *Manager) disableFlow() error {
	if m.cancel != nil {
		m.cancel()
	}

	if m.conntrack != nil {
		m.conntrack.Stop()
	}

	m.logger.Close()

	if m.receiverClient == nil {
		return nil
	}

	err := m.receiverClient.Close()
	m.receiverClient = nil
	if err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

// Update applies new flow configuration settings
func (m *Manager) Update(update *nftypes.FlowConfig) error {
	if update == nil {
		log.Debug("no update provided; skipping update")
		return nil
	}

	log.Tracef("updating flow configuration with new settings: url -> %s, interval -> %s, enabled? %t", update.URL, update.Interval, update.Enabled)

	m.mux.Lock()
	defer m.mux.Unlock()

	previous := m.flowConfig
	m.flowConfig = update

	// Preserve TokenPayload and TokenSignature if they were set previously
	if previous != nil && previous.TokenPayload != "" && m.flowConfig != nil && m.flowConfig.TokenPayload == "" {
		m.flowConfig.TokenPayload = previous.TokenPayload
		m.flowConfig.TokenSignature = previous.TokenSignature
	}

	m.logger.UpdateConfig(update.DNSCollection, update.ExitNodeCollection,
		filter.New(update.DisableDefaultPortFilter, update.ExcludedPorts))

	changed := previous != nil && update.Enabled != previous.Enabled
	if update.Enabled {
		if changed {
			log.Infof("netflow manager enabled; starting netflow manager")
		}
		return m.enableFlow(previous)
	}

	if changed {
		log.Infof("netflow manager disabled; stopping netflow manager")
	}
	return m.disableFlow()
}

// Close cleans up all resources. Unlocks the mux before waiting on
// shutdownWg so the in-flight sender/receiver goroutines can finish
// their teardown — both call back into methods that take the mux,
// and holding it through Wait would deadlock.
func (m *Manager) Close() {
	m.mux.Lock()
	if err := m.disableFlow(); err != nil {
		log.Warnf("failed to disable flow manager: %v", err)
	}
	m.mux.Unlock()

	m.shutdownWg.Wait()
}

// GetLogger returns the flow logger
func (m *Manager) GetLogger() nftypes.FlowLogger {
	return m.logger
}

func (m *Manager) startSender(ctx context.Context) {
	ticker := time.NewTicker(m.flowConfig.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events := m.logger.GetEvents()
			for _, event := range events {
				if err := m.send(event); err != nil {
					log.Errorf("failed to send flow event to server: %v", err)
					continue
				}
				log.Tracef("sent flow event: %s", event.ID)
			}
		}
	}
}

func (m *Manager) receiveACKs(ctx context.Context, client *client.GRPCClient) {
	err := client.Receive(ctx, m.flowConfig.Interval, func(ack *proto.FlowEventAck) error {
		id, err := uuid.FromBytes(ack.EventId)
		if err != nil {
			log.Warnf("failed to convert ack event id to uuid: %v", err)
			return nil
		}
		log.Tracef("received flow event ack: %s", id)
		m.logger.DeleteEvents([]uuid.UUID{id})
		return nil
	})

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Errorf("failed to receive flow event ack: %v", err)
	}
}

func (m *Manager) send(event *nftypes.Event) error {
	m.mux.Lock()
	client := m.receiverClient
	m.mux.Unlock()

	if client == nil {
		return nil
	}

	return client.Send(toProtoEvent(m.publicKey, event))
}

func toProtoEvent(publicKey []byte, event *nftypes.Event) *proto.FlowEvent {
	protoEvent := &proto.FlowEvent{
		EventId:   event.ID[:],
		Timestamp: timestamppb.New(event.Timestamp),
		PublicKey: publicKey,
		FlowFields: &proto.FlowFields{
			FlowId:           event.FlowID[:],
			RuleId:           event.RuleID,
			Type:             proto.Type(event.Type),
			Direction:        proto.Direction(event.Direction),
			Protocol:         uint32(event.Protocol),
			SourceIp:         event.SourceIP.AsSlice(),
			DestIp:           event.DestIP.AsSlice(),
			RxPackets:        event.RxPackets,
			TxPackets:        event.TxPackets,
			RxBytes:          event.RxBytes,
			TxBytes:          event.TxBytes,
			SourceResourceId: event.SourceResourceID,
			DestResourceId:   event.DestResourceID,
		},
	}

	if event.Protocol == nftypes.ICMP {
		protoEvent.FlowFields.ConnectionInfo = &proto.FlowFields_IcmpInfo{
			IcmpInfo: &proto.ICMPInfo{
				IcmpType: uint32(event.ICMPType),
				IcmpCode: uint32(event.ICMPCode),
			},
		}
		return protoEvent
	}

	protoEvent.FlowFields.ConnectionInfo = &proto.FlowFields_PortInfo{
		PortInfo: &proto.PortInfo{
			SourcePort: uint32(event.SourcePort),
			DestPort:   uint32(event.DestPort),
		},
	}

	return protoEvent
}
