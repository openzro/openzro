// Package factory builds a cluster.Coordinator from environment variables.
// Lives in its own subpackage to avoid an import cycle: the concrete
// backends (cluster/redis, cluster/nats) import the cluster package for
// the Coordinator interface and the Event type, so the cluster package
// itself cannot reach back into the backends.
package factory

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	natsclient "github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/cluster"
	"github.com/openzro/openzro/cluster/embedded"
	clusternats "github.com/openzro/openzro/cluster/nats"
	clusterredis "github.com/openzro/openzro/cluster/redis"
)

// Env vars consumed by NewFromEnv. Same broker-selection logic as the
// signal dispatcher factory in signal/cmd, kept here as a public seam
// so management/cmd can use it too.
const (
	EnvBroker            = "OPENZRO_BROKER"
	EnvRedisURL          = "OPENZRO_REDIS_URL"
	EnvNatsURL           = "OPENZRO_NATS_URL"
	EnvClusterPeers      = "OPENZRO_CLUSTER_PEERS"
	EnvEmbeddedNatsPort  = "OPENZRO_EMBEDDED_NATS_CLIENT_PORT"
	EnvEmbeddedNatsClust = "OPENZRO_EMBEDDED_NATS_CLUSTER_PORT"
	EnvJetStreamDir      = "OPENZRO_EMBEDDED_NATS_JETSTREAM_DIR"

	BrokerEmbedded = "embedded"
)

// NewFromEnv constructs a Coordinator according to the operator's
// environment. Selection rules (in this order):
//
//  1. OPENZRO_BROKER=embedded — start an embedded NATS+JetStream server
//     and return a NATS-backed coordinator pointing at localhost.
//  2. OPENZRO_NATS_URL=... — connect to that external NATS.
//  3. OPENZRO_REDIS_URL=... — connect to that Redis-compatible server.
//  4. None of the above — return (nil, nil). The caller must treat that
//     as "single-instance mode; no coordinator needed".
//
// The embedded NATS server (mode 1) is bootstrapped once and outlives
// the Coordinator's Close — it is intended to run for the process'
// lifetime.
func NewFromEnv(ctx context.Context) (cluster.Coordinator, error) {
	broker := strings.ToLower(os.Getenv(EnvBroker))

	switch {
	case broker == BrokerEmbedded:
		srv, err := startEmbedded()
		if err != nil {
			return nil, fmt.Errorf("cluster: start embedded NATS: %w", err)
		}
		nc, err := natsclient.Connect(srv.ClientURL(),
			natsclient.NoEcho(),
			natsclient.Name("openzro-coordinator-embedded"),
		)
		if err != nil {
			return nil, fmt.Errorf("cluster: connect embedded NATS: %w", err)
		}
		log.Infof("cluster coordinator: NATS (embedded; client=%s)", srv.ClientURL())
		return clusternats.New(ctx, clusternats.Config{Conn: nc})

	case os.Getenv(EnvNatsURL) != "":
		url := os.Getenv(EnvNatsURL)
		nc, err := natsclient.Connect(url,
			natsclient.NoEcho(),
			natsclient.Name("openzro-coordinator"),
		)
		if err != nil {
			return nil, fmt.Errorf("cluster: connect NATS at %s: %w", url, err)
		}
		log.Infof("cluster coordinator: NATS (external; %s)", url)
		return clusternats.New(ctx, clusternats.Config{Conn: nc})

	case os.Getenv(EnvRedisURL) != "":
		url := os.Getenv(EnvRedisURL)
		opts, err := goredis.ParseURL(url)
		if err != nil {
			return nil, fmt.Errorf("cluster: parse %s: %w", EnvRedisURL, err)
		}
		client := goredis.NewClient(opts)
		log.Infof("cluster coordinator: Redis-compatible (%s)", opts.Addr)
		return clusterredis.New(ctx, clusterredis.Config{Client: client})

	default:
		log.Info("cluster coordinator: none (single-instance mode)")
		return nil, nil
	}
}

func startEmbedded() (*embedded.Server, error) {
	cfg := embedded.Config{
		// JetStream is on by default; the NATS coordinator needs the KV
		// bucket for distributed locks.
		JetStreamStoreDir: os.Getenv(EnvJetStreamDir),
	}

	if v := os.Getenv(EnvEmbeddedNatsPort); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid %s=%q", EnvEmbeddedNatsPort, v)
		}
		cfg.ClientPort = p
	}
	if v := os.Getenv(EnvEmbeddedNatsClust); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid %s=%q", EnvEmbeddedNatsClust, v)
		}
		cfg.ClusterPort = p
	}
	if peers := os.Getenv(EnvClusterPeers); peers != "" {
		for _, p := range strings.Split(peers, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.ClusterPeers = append(cfg.ClusterPeers, p)
			}
		}
	}
	return embedded.Start(cfg)
}
