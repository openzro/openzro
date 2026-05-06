// Package embedded starts a NATS server inside the openzro process so that
// HA deployments can be operated without running a separate NATS broker.
// Each openzro instance becomes a node in a NATS cluster; clients (the
// signal dispatcher and the management coordinator) connect to localhost.
//
// Discovery is via a static seed list of cluster URLs (typically the
// other openzro instances themselves). Once two or more nodes have a
// route between them, the NATS gnatsd cluster protocol takes over and
// peer state propagates through gossip.
//
// This package wraps github.com/nats-io/nats-server/v2 — it is
// configuration glue, not a reimplementation. No upstream openzro/netbird
// post-AGPL code was consulted.
//
// Reference: https://docs.nats.io/running-a-nats-service/configuration/clustering
package embedded

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	log "github.com/sirupsen/logrus"
)

const (
	// DefaultClientPort is where local clients (signal dispatcher,
	// management coordinator) connect — bound to loopback unless overridden.
	DefaultClientPort = 4222

	// DefaultClusterPort is where peer openzro instances connect for
	// inter-node gossip.
	DefaultClusterPort = 6222

	// DefaultClusterName groups all openzro instances in a single cluster.
	// Servers with different cluster names refuse to route to each other.
	DefaultClusterName = "openzro"

	defaultStartupTimeout = 10 * time.Second
)

// Config configures the embedded NATS server. Zero values fall back to
// the Default* constants above.
type Config struct {
	// ClientHost is the listen address for client connections. Defaults
	// to "127.0.0.1" so only the local openzro process can connect.
	ClientHost string
	ClientPort int

	// ClusterHost is the listen address for the NATS cluster (peer
	// nodes). Must be reachable by other openzro instances; defaults to
	// "0.0.0.0".
	ClusterHost string
	ClusterPort int
	ClusterName string

	// ServerName uniquely identifies this NATS instance within the cluster.
	// Mandatory for JetStream cluster mode (NATS rejects the server with
	// "jetstream cluster requires server_name to be set" if it's empty).
	// When left empty Start() defaults to os.Hostname() — in K8s pods this
	// resolves to the pod name (StatefulSet ordinal-stable), which is the
	// right thing for HA replica sets.
	ServerName string

	// ClusterPeers is the seed list of other openzro instances to route
	// to, in nats:// URL form, e.g.
	// ["nats://node2:6222", "nats://node3:6222"].
	// A single-node cluster (empty list) is allowed and useful for
	// development / standalone deploys.
	ClusterPeers []string

	// JetStream toggles JetStream support. **Defaults to true** because
	// the management coordinator (cluster/nats) needs JetStream KV for
	// distributed locks. Set to false explicitly only for deployments
	// using cluster/redis as their coordinator and the embedded NATS
	// purely for signal pub/sub (which works on core NATS alone).
	//
	// Use *bool semantics via JetStreamDisabled if you need to set false
	// — the zero-value of a bool field can't distinguish "not set" from
	// "false". This struct chooses to flip the polarity for the same
	// reason (see DisableJetStream below).
	DisableJetStream bool

	// JetStreamStoreDir is where JetStream persists data. If empty,
	// JetStream uses an in-memory store (no persistence across restart).
	// Recommended for production: a writable directory under the
	// openzro data dir.
	JetStreamStoreDir string

	// StartupTimeout caps how long Start waits for the server to become
	// ready for connections. Defaults to 10s.
	StartupTimeout time.Duration
}

// Server is a running embedded NATS server. Stop must be called to
// release listeners and goroutines.
type Server struct {
	ns *natsserver.Server

	clientURL  string
	clusterURL string
}

// Start launches an embedded NATS server in a background goroutine and
// blocks until it is ready to accept connections (or StartupTimeout
// elapses). On error, no listeners are left open.
func Start(cfg Config) (*Server, error) {
	if cfg.ClientHost == "" {
		cfg.ClientHost = "127.0.0.1"
	}
	if cfg.ClientPort == 0 {
		cfg.ClientPort = DefaultClientPort
	}
	if cfg.ClusterHost == "" {
		cfg.ClusterHost = "0.0.0.0"
	}
	if cfg.ClusterPort == 0 {
		cfg.ClusterPort = DefaultClusterPort
	}
	if cfg.ClusterName == "" {
		cfg.ClusterName = DefaultClusterName
	}
	if cfg.ServerName == "" {
		// os.Hostname() resolves to the K8s pod name in StatefulSets,
		// which is unique per replica and stable across restarts. Falls
		// back to a UUID-shaped string only if hostname lookup fails
		// (rare — should only happen on broken /etc/hostname).
		hn, err := os.Hostname()
		if err != nil || hn == "" {
			return nil, fmt.Errorf("embedded nats: ServerName not set and os.Hostname() failed: %w", err)
		}
		cfg.ServerName = hn
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = defaultStartupTimeout
	}

	routes := make([]*url.URL, 0, len(cfg.ClusterPeers))
	for _, raw := range cfg.ClusterPeers {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("embedded nats: invalid cluster peer %q: %w", raw, err)
		}
		if u.Scheme != "nats" {
			return nil, fmt.Errorf("embedded nats: cluster peer %q must use nats:// scheme", raw)
		}
		routes = append(routes, u)
	}

	jsEnabled := !cfg.DisableJetStream

	opts := &natsserver.Options{
		ServerName: cfg.ServerName,
		Host:       cfg.ClientHost,
		Port:       cfg.ClientPort,
		Cluster: natsserver.ClusterOpts{
			Name: cfg.ClusterName,
			Host: cfg.ClusterHost,
			Port: cfg.ClusterPort,
		},
		Routes:    routes,
		JetStream: jsEnabled,
		StoreDir:  cfg.JetStreamStoreDir,
		// Logs go through the openzro logger via SetLogger below.
		NoLog: true,
	}

	ns, err := natsserver.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("embedded nats: create server: %w", err)
	}

	ns.SetLoggerV2(&logrusBridge{}, false, false, false)

	go ns.Start()

	if !ns.ReadyForConnections(cfg.StartupTimeout) {
		ns.Shutdown()
		return nil, errors.New("embedded nats: not ready for connections within startup timeout")
	}

	srv := &Server{
		ns:         ns,
		clientURL:  fmt.Sprintf("nats://%s:%d", cfg.ClientHost, cfg.ClientPort),
		clusterURL: fmt.Sprintf("nats://%s:%d", cfg.ClusterHost, cfg.ClusterPort),
	}

	log.Infof("embedded nats: running; client=%s cluster=%s peers=%v jetstream=%v",
		srv.clientURL, srv.clusterURL, cfg.ClusterPeers, jsEnabled)
	return srv, nil
}

// ClientURL returns the local nats:// URL clients (signal, management)
// should connect to.
func (s *Server) ClientURL() string { return s.clientURL }

// ClusterURL returns the nats:// URL other openzro instances should put
// in their OPENZRO_CLUSTER_PEERS list to route to this node.
func (s *Server) ClusterURL() string { return s.clusterURL }

// Shutdown stops the server and waits for goroutines to exit.
func (s *Server) Shutdown() {
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

// logrusBridge implements the natsserver.Logger interface and forwards
// every line to the openzro logrus logger so embedded-NATS log lines
// appear in the same stream as everything else.
type logrusBridge struct{}

func (logrusBridge) Noticef(format string, v ...any) { log.Infof("nats: "+format, v...) }
func (logrusBridge) Warnf(format string, v ...any)   { log.Warnf("nats: "+format, v...) }
func (logrusBridge) Fatalf(format string, v ...any)  { log.Errorf("nats: "+format, v...) } // do not actually exit
func (logrusBridge) Errorf(format string, v ...any)  { log.Errorf("nats: "+format, v...) }
func (logrusBridge) Debugf(format string, v ...any)  { log.Debugf("nats: "+format, v...) }
func (logrusBridge) Tracef(format string, v ...any)  { log.Tracef("nats: "+format, v...) }
