package server

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/openzro/openzro/relay/server/cluster"
	"github.com/openzro/openzro/relay/server/store"
)

// ClusterBootstrap holds the long-lived inter-pod components a
// relay needs in multi-pod mode (per ADR-0014). The fields are
// exported so operators / tests can inspect or instrument them, but
// the lifetime is owned by the bootstrap: call Stop to tear
// everything down in the right order.
type ClusterBootstrap struct {
	Transport *cluster.Transport
	Locator   *cluster.PeerLocator
	Forwarder *cluster.Forwarder
	Discovery *cluster.Discovery
}

// ClusterBootstrapConfig is what the relay command needs to know
// about its K8s deployment to wire the inter-pod fabric. Headless,
// Port and PodIP are required; everything else has sensible
// defaults.
type ClusterBootstrapConfig struct {
	// Headless is the FQDN (or in-cluster short name) of the K8s
	// Headless Service that resolves to every relay pod's IP.
	// Example: "openzro-relay-internal".
	Headless string

	// Port is the inter-pod TCP port. Same value on every pod.
	// Defaults to cluster.DefaultInterpodPort when zero.
	Port int

	// PodIP is the address other pods will dial us on, learned via
	// the K8s downward API (POD_IP). Required — without it we'd
	// announce a useless 0.0.0.0 in our HELLO frame and sibling
	// pods could never establish back-streams.
	PodIP string

	// Interval is the discovery reconcile period. Defaults to
	// cluster.DefaultDiscoveryInterval (10 s) when zero.
	Interval time.Duration
}

func (c ClusterBootstrapConfig) validate() error {
	if c.Headless == "" {
		return fmt.Errorf("cluster bootstrap: Headless is required")
	}
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("cluster bootstrap: invalid Port %d", c.Port)
	}
	if c.PodIP == "" {
		return fmt.Errorf("cluster bootstrap: PodIP is required (set POD_IP via the K8s downward API)")
	}
	return nil
}

// StartCluster wires the cluster transport, locator, forwarder and
// discovery loop, returning the assembled bootstrap. The caller
// must hand cb.Forwarder to Server.SetCrossPodForwarder before any
// peer connections are accepted, so that local-store misses
// fall through to the cluster fabric.
//
// On any wiring failure StartCluster cleans up partially-built
// state (transport listener, etc.) before returning the error.
func StartCluster(ctx context.Context, st *store.Store, cfg ClusterBootstrapConfig) (*ClusterBootstrap, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("cluster bootstrap: store is required")
	}
	port := cfg.Port
	if port == 0 {
		port = cluster.DefaultInterpodPort
	}

	// We bind to 0.0.0.0 so the kernel doesn't care which
	// interface the inbound conn arrived on; we announce PodIP so
	// sibling pods can dial us back.
	listenAddr := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	announceAddr := net.JoinHostPort(cfg.PodIP, strconv.Itoa(port))

	transport := cluster.NewTransport(listenAddr, announceAddr, nil)

	dispatcher := NewLocalPeerDispatcher(st)
	locator := cluster.NewPeerLocator(transport, dispatcher)
	forwarder, err := cluster.NewForwarder(transport, locator, dispatcher)
	if err != nil {
		return nil, fmt.Errorf("cluster bootstrap: forwarder: %w", err)
	}

	transport.SetHandler(&clusterFrameRouter{loc: locator, fwd: forwarder})

	if err := transport.ListenAndServe(ctx); err != nil {
		return nil, fmt.Errorf("cluster bootstrap: transport listen: %w", err)
	}

	disc, err := cluster.NewDiscovery(transport, cluster.DiscoveryConfig{
		Headless: cfg.Headless,
		Port:     port,
		SelfIP:   cfg.PodIP,
		Interval: cfg.Interval,
	})
	if err != nil {
		transport.Stop()
		return nil, fmt.Errorf("cluster bootstrap: discovery: %w", err)
	}
	disc.Start(ctx)

	return &ClusterBootstrap{
		Transport: transport,
		Locator:   locator,
		Forwarder: forwarder,
		Discovery: disc,
	}, nil
}

// Stop tears down the cluster machinery in reverse construction
// order: stop discovery (no new outbound dials), then stop the
// transport (which closes every live stream and the listener).
// Safe on a nil receiver and idempotent.
func (cb *ClusterBootstrap) Stop() {
	if cb == nil {
		return
	}
	if cb.Discovery != nil {
		cb.Discovery.Stop()
	}
	if cb.Transport != nil {
		cb.Transport.Stop()
	}
}

// clusterFrameRouter dispatches an inbound inter-pod frame to the
// right component. Locator handles WHO_HAS / I_HAVE control
// frames; Forwarder handles the FWD data plane. PING/PONG and
// HELLO are intercepted by the transport itself, so we don't need
// cases for them here. Unknown types are ignored to keep the
// protocol forward-compatible.
type clusterFrameRouter struct {
	loc *cluster.PeerLocator
	fwd *cluster.Forwarder
}

func (r *clusterFrameRouter) HandleFrame(remote string, t cluster.MsgType, payload []byte) error {
	switch t {
	case cluster.MsgWhoHas:
		return r.loc.HandleWhoHas(remote, payload)
	case cluster.MsgIHave:
		return r.loc.HandleIHave(remote, payload)
	case cluster.MsgFwd:
		return r.fwd.HandleFwd(remote, payload)
	default:
		return nil
	}
}
