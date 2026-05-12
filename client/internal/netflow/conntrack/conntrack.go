//go:build linux && !android

package conntrack

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	nfct "github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"

	nftypes "github.com/openzro/openzro/client/internal/netflow/types"
	nbnet "github.com/openzro/openzro/util/net"
)

const defaultChannelSize = 100

// Backoff parameters for the conntrack listener reconnect loop.
// Exposed as vars so unit tests can shorten the cycle (tests assign
// reconnectInitInterval = a few ms before constructing a ConnTrack).
// Production values land within the netlink "transient EPERM after
// module reload" recovery window: ~5s first try, capped at 5 minutes.
var (
	reconnectInitInterval  = 5 * time.Second
	reconnectMaxInterval   = 5 * time.Minute
	reconnectRandomization = 0.5
)

// PolicyResolver maps the agent-local rule_index that the firewall
// backend stamped on the conntrack mark (per ADR-0013) back to the
// management-issued PolicyID. Returns ok=false when the index is
// unknown — the caller emits an empty RuleId in that case, matching
// the behaviour before ADR-0013.
type PolicyResolver interface {
	LookupPolicyID(ruleIndex uint32) ([]byte, bool)
}

// listener abstracts the netlink conntrack connection so the receiver
// loop (and especially the upcoming reconnect-with-backoff path) can
// be unit-tested without a real kernel. *nfct.Conn satisfies this
// interface as-is.
type listener interface {
	Listen(evChan chan<- nfct.Event, numWorkers uint8, groups []netfilter.NetlinkGroup) (chan error, error)
	Close() error
}

// DialFunc opens a fresh netlink conntrack connection. Tests inject
// a mock via WithDialer to avoid touching the kernel.
type DialFunc func() (listener, error)

// Option configures a ConnTrack instance at construction time.
type Option func(*ConnTrack)

// WithDialer overrides the default netlink dialer. Production code
// leaves the default in place; tests use this to feed a mock
// listener that simulates kernel events, errors, and reconnects.
func WithDialer(d DialFunc) Option {
	return func(c *ConnTrack) { c.dial = d }
}

// defaultDial is the production netlink conntrack dialer. Wrapped
// behind DialFunc so tests can swap it out.
func defaultDial() (listener, error) {
	return nfct.Dial(nil)
}

// ConnTrack manages kernel-based conntrack events
type ConnTrack struct {
	flowLogger nftypes.FlowLogger
	iface      nftypes.IFaceMapper
	resolver   PolicyResolver

	conn listener
	mux  sync.Mutex

	dial           DialFunc
	instanceID     uuid.UUID
	started        bool
	done           chan struct{}
	sysctlModified bool
}

// New creates a new connection tracker that interfaces with the kernel's conntrack system.
// If resolver is nil the collector emits events without RuleId, matching the
// behaviour from before the ADR-0013 mark layout.
func New(flowLogger nftypes.FlowLogger, iface nftypes.IFaceMapper, resolver PolicyResolver, opts ...Option) *ConnTrack {
	c := &ConnTrack{
		flowLogger: flowLogger,
		iface:      iface,
		resolver:   resolver,
		instanceID: uuid.New(),
		started:    false,
		dial:       defaultDial,
		done:       make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Start begins tracking connections by listening for conntrack events. This method is idempotent.
func (c *ConnTrack) Start(enableCounters bool) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.started {
		return nil
	}

	log.Info("Starting conntrack event listening")

	if enableCounters {
		c.EnableAccounting()
	}

	conn, err := c.dial()
	if err != nil {
		return fmt.Errorf("dial conntrack: %w", err)
	}
	c.conn = conn

	events := make(chan nfct.Event, defaultChannelSize)
	errChan, err := conn.Listen(events, 1, []netfilter.NetlinkGroup{
		netfilter.GroupCTNew,
		netfilter.GroupCTDestroy,
	})

	if err != nil {
		if err := c.conn.Close(); err != nil {
			log.Errorf("Error closing conntrack connection: %v", err)
		}
		c.conn = nil
		return fmt.Errorf("start conntrack listener: %w", err)
	}

	// Drain any stale stop sentinel left over from a previous Stop().
	// c.done is buffered at capacity 1; without this drain the new
	// receiver goroutine would read the leftover signal on its first
	// iteration and exit immediately, leaving kernel conntrack events
	// pouring into a dead channel until the daemon was restarted.
	select {
	case <-c.done:
	default:
	}

	c.started = true

	go c.receiverRoutine(events, errChan)

	return nil
}

func (c *ConnTrack) receiverRoutine(events chan nfct.Event, errChan chan error) {
	for {
		select {
		case event := <-events:
			c.handleEvent(event)
		case err := <-errChan:
			// Listener errored — kernel module reloaded, EPERM on a
			// transient permission flap, netlink dispatcher restart.
			// Attempt to recover via exponential backoff instead of
			// dying for good. handleListenerError returns nil channels
			// when Stop() ran during the reconnect window.
			if events, errChan = c.handleListenerError(err); events == nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

// handleListenerError closes the failed connection and attempts to
// reconnect with exponential backoff. Returns the new event channels
// on success, or (nil, nil) when shutdown was requested mid-reconnect
// (caller exits the receiver loop in that case).
func (c *ConnTrack) handleListenerError(err error) (chan nfct.Event, chan error) {
	log.Warnf("conntrack event listener failed: %v", err)
	c.closeConn()
	return c.reconnect()
}

// closeConn tears down the current netlink listener if any. Safe to
// call when c.conn is already nil (the reconnect race path).
func (c *ConnTrack) closeConn() {
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			log.Debugf("close conntrack connection: %v", err)
		}
		c.conn = nil
	}
}

// reconnect loops on backoff.NextBackOff() until either a fresh
// listener comes up or Stop()/Close() set started=false. Returns the
// new channels on success and (nil, nil) when shutdown wins the
// race. Logs each attempt at Info so operators can see "we're
// trying" without enabling debug.
func (c *ConnTrack) reconnect() (chan nfct.Event, chan error) {
	bo := &backoff.ExponentialBackOff{
		InitialInterval:     reconnectInitInterval,
		RandomizationFactor: reconnectRandomization,
		Multiplier:          backoff.DefaultMultiplier,
		MaxInterval:         reconnectMaxInterval,
		MaxElapsedTime:      0, // retry indefinitely — only Stop/Close exit
		Clock:               backoff.SystemClock,
	}
	bo.Reset()

	for {
		delay := bo.NextBackOff()
		log.Infof("reconnecting conntrack listener in %s", delay)

		select {
		case <-c.done:
			c.mux.Lock()
			c.started = false
			c.mux.Unlock()
			return nil, nil
		case <-time.After(delay):
		}

		conn, err := c.dial()
		if err != nil {
			log.Warnf("reconnect conntrack dial: %v", err)
			continue
		}

		events := make(chan nfct.Event, defaultChannelSize)
		errChan, err := conn.Listen(events, 1, []netfilter.NetlinkGroup{
			netfilter.GroupCTNew,
			netfilter.GroupCTDestroy,
		})
		if err != nil {
			log.Warnf("reconnect conntrack listen: %v", err)
			if closeErr := conn.Close(); closeErr != nil {
				log.Debugf("close conntrack connection: %v", closeErr)
			}
			continue
		}

		c.mux.Lock()
		if !c.started {
			// Stop()/Close() landed while we were dialing or
			// listening; close the orphan and bail.
			c.mux.Unlock()
			if closeErr := conn.Close(); closeErr != nil {
				log.Debugf("close conntrack connection: %v", closeErr)
			}
			return nil, nil
		}
		c.conn = conn
		c.mux.Unlock()

		log.Infof("conntrack listener reconnected successfully")
		return events, errChan
	}
}

// Stop stops the connection tracking. This method is idempotent.
func (c *ConnTrack) Stop() {
	c.mux.Lock()
	defer c.mux.Unlock()

	if !c.started {
		return
	}

	log.Info("Stopping conntrack event listening")

	select {
	case c.done <- struct{}{}:
	default:
	}

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			log.Errorf("Error closing conntrack connection: %v", err)
		}
		c.conn = nil
	}

	c.started = false

	c.RestoreAccounting()
}

// Close stops listening for events and cleans up resources
func (c *ConnTrack) Close() error {
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.started {
		select {
		case c.done <- struct{}{}:
		default:
		}
	}

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.started = false

		c.RestoreAccounting()

		if err != nil {
			return fmt.Errorf("close conntrack: %w", err)
		}
	}

	return nil
}

// handleEvent processes incoming conntrack events
func (c *ConnTrack) handleEvent(event nfct.Event) {
	if event.Flow == nil {
		return
	}

	if event.Type != nfct.EventNew && event.Type != nfct.EventDestroy {
		return
	}

	flow := *event.Flow

	proto := nftypes.Protocol(flow.TupleOrig.Proto.Protocol)
	if proto == nftypes.ProtocolUnknown {
		return
	}
	srcIP := flow.TupleOrig.IP.SourceAddress
	dstIP := flow.TupleOrig.IP.DestinationAddress

	if !c.relevantFlow(flow.Mark, srcIP, dstIP) {
		return
	}

	var srcPort, dstPort uint16
	var icmpType, icmpCode uint8

	switch proto {
	case nftypes.TCP, nftypes.UDP, nftypes.SCTP:
		srcPort = flow.TupleOrig.Proto.SourcePort
		dstPort = flow.TupleOrig.Proto.DestinationPort
	case nftypes.ICMP:
		icmpType = flow.TupleOrig.Proto.ICMPType
		icmpCode = flow.TupleOrig.Proto.ICMPCode
	}

	flowID := c.getFlowID(flow.ID)
	direction := c.inferDirection(flow.Mark, srcIP, dstIP)

	eventType := nftypes.TypeStart
	eventStr := "New"

	if event.Type == nfct.EventDestroy {
		eventType = nftypes.TypeEnd
		eventStr = "Ended"
	}

	log.Tracef("%s %s %s connection: %s:%d → %s:%d", eventStr, direction, proto, srcIP, srcPort, dstIP, dstPort)

	// ADR-0013: pull the rule_index that the firewall backend
	// stamped onto the high bits of the ct mark and resolve it
	// back to the originating PolicyID. The legacy 17-bit fwmark
	// space stays on the low bits; the resolver handles unknown
	// indices (returning ok=false) by emitting an empty RuleId,
	// matching pre-ADR behaviour.
	var ruleID []byte
	if c.resolver != nil {
		if ruleIndex := nbnet.MarkRuleIndex(flow.Mark); ruleIndex != 0 {
			if pid, ok := c.resolver.LookupPolicyID(ruleIndex); ok {
				ruleID = pid
			}
		}
	}

	c.flowLogger.StoreEvent(nftypes.EventFields{
		FlowID:     flowID,
		Type:       eventType,
		RuleID:     ruleID,
		Direction:  direction,
		Protocol:   proto,
		SourceIP:   srcIP,
		DestIP:     dstIP,
		SourcePort: srcPort,
		DestPort:   dstPort,
		ICMPType:   icmpType,
		ICMPCode:   icmpCode,
		RxPackets:  c.mapRxPackets(flow, direction),
		TxPackets:  c.mapTxPackets(flow, direction),
		RxBytes:    c.mapRxBytes(flow, direction),
		TxBytes:    c.mapTxBytes(flow, direction),
	})
}

// relevantFlow decides whether a conntrack flow should reach the
// dashboard's traffic log. The data-plane mark is the canonical
// signal — every accept rule the agent installs (peer-ACL chain in
// acl_linux.go and routing-peer forward chain in router_linux.go)
// stamps it via the policymark indexer (ADR-0013). A flow without
// the mark never matched a mesh-installed rule, which means it
// either bypassed the agent entirely or was a kernel-side
// connection unrelated to mesh traffic; in both cases it does not
// belong in the operator's flow log.
//
// The pre-ADR-0013 implementation also accepted flows whose src or
// dst landed in the WG network range as a transitional fallback.
// That fallback drove a class of false positives (kernel-side
// bind() to the wgaddr emitting outbound to the public internet,
// stale conntrack from before the agent restarted, etc.) without
// ever providing useful audit signal — those flows have no
// rule_id, so the dashboard renders them with an empty Policy line
// and "external" destination labels. Removed in
// ADR-0015's diagnostic follow-up.
func (c *ConnTrack) relevantFlow(mark uint32, _, _ netip.Addr) bool {
	return nbnet.IsDataPlaneMark(mark)
}

// mapRxPackets maps packet counts to RX based on flow direction
func (c *ConnTrack) mapRxPackets(flow nfct.Flow, direction nftypes.Direction) uint64 {
	// For Ingress: CountersOrig is from external to us (RX)
	// For Egress: CountersReply is from external to us (RX)
	if direction == nftypes.Ingress {
		return flow.CountersOrig.Packets
	}
	return flow.CountersReply.Packets
}

// mapTxPackets maps packet counts to TX based on flow direction
func (c *ConnTrack) mapTxPackets(flow nfct.Flow, direction nftypes.Direction) uint64 {
	// For Ingress: CountersReply is from us to external (TX)
	// For Egress: CountersOrig is from us to external (TX)
	if direction == nftypes.Ingress {
		return flow.CountersReply.Packets
	}
	return flow.CountersOrig.Packets
}

// mapRxBytes maps byte counts to RX based on flow direction
func (c *ConnTrack) mapRxBytes(flow nfct.Flow, direction nftypes.Direction) uint64 {
	// For Ingress: CountersOrig is from external to us (RX)
	// For Egress: CountersReply is from external to us (RX)
	if direction == nftypes.Ingress {
		return flow.CountersOrig.Bytes
	}
	return flow.CountersReply.Bytes
}

// mapTxBytes maps byte counts to TX based on flow direction
func (c *ConnTrack) mapTxBytes(flow nfct.Flow, direction nftypes.Direction) uint64 {
	// For Ingress: CountersReply is from us to external (TX)
	// For Egress: CountersOrig is from us to external (TX)
	if direction == nftypes.Ingress {
		return flow.CountersReply.Bytes
	}
	return flow.CountersOrig.Bytes
}

// getFlowID creates a unique UUID based on the conntrack ID and instance ID
func (c *ConnTrack) getFlowID(conntrackID uint32) uuid.UUID {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], conntrackID)
	return uuid.NewSHA1(c.instanceID, buf[:])
}

func (c *ConnTrack) inferDirection(mark uint32, srcIP, dstIP netip.Addr) nftypes.Direction {
	// ADR-0013: peer ACL rules now stamp a rule_index on the
	// upper 15 bits of the ct mark, so an exact match on
	// `DataPlaneMark{In,Out}` no longer fits. Strip the index
	// before comparing.
	switch nbnet.MarkValue(mark) {
	case nbnet.DataPlaneMarkIn:
		return nftypes.Ingress
	case nbnet.DataPlaneMarkOut:
		return nftypes.Egress
	}

	// fallback if marks are not set
	wgaddr := c.iface.Address().IP
	wgnetwork := c.iface.Address().Network
	switch {
	case wgaddr == srcIP:
		return nftypes.Egress
	case wgaddr == dstIP:
		return nftypes.Ingress
	case wgnetwork.Contains(srcIP):
		// openzro network -> resource network
		return nftypes.Ingress
	case wgnetwork.Contains(dstIP):
		// resource network -> openzro network
		return nftypes.Egress
	}

	return nftypes.DirectionUnknown
}
