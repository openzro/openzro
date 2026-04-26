package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	natsclient "github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/metric"

	"github.com/openzro/openzro/cluster/embedded"
	"github.com/openzro/openzro/signal/dispatcher"
	"github.com/openzro/openzro/signal/dispatcher/inmem"
	"github.com/openzro/openzro/signal/dispatcher/nats"
	"github.com/openzro/openzro/signal/dispatcher/redis"
)

const (
	envSignalDispatcher = "OPENZRO_SIGNAL_DISPATCHER"
	envRedisURL         = "OPENZRO_REDIS_URL"
	envNatsURL          = "OPENZRO_NATS_URL"

	envBroker            = "OPENZRO_BROKER"
	envClusterPeers      = "OPENZRO_CLUSTER_PEERS"
	envEmbeddedNatsPort  = "OPENZRO_EMBEDDED_NATS_CLIENT_PORT"
	envEmbeddedNatsClust = "OPENZRO_EMBEDDED_NATS_CLUSTER_PORT"

	dispatcherInMem    = "inmem"
	dispatcherRedis    = "redis"
	dispatcherNats     = "nats"
	brokerEmbeddedNats = "embedded"
)

// buildDispatcher selects the signal Dispatcher implementation according to
// the operator's configuration. Three families are supported:
//
//  1. In-memory (default): single signal-server instance, no broker.
//
//  2. External broker: a Redis-compatible server (Redis / Valkey / Dragonfly)
//     OR a NATS server reachable at a network URL. Multiple openzro
//     instances point at the same broker and form an HA group.
//
//     OPENZRO_SIGNAL_DISPATCHER=redis  + OPENZRO_REDIS_URL=redis://...
//     OPENZRO_SIGNAL_DISPATCHER=nats   + OPENZRO_NATS_URL=nats://...
//
//  3. Embedded NATS cluster: each openzro instance starts an in-process
//     NATS server and routes to its peers. Zero infra outside the
//     openzro binary.
//
//     OPENZRO_BROKER=embedded
//     OPENZRO_CLUSTER_PEERS=nats://node2:6222,nats://node3:6222
//
// In auto-detect mode (env vars present, OPENZRO_SIGNAL_DISPATCHER unset),
// OPENZRO_REDIS_URL implies redis, OPENZRO_NATS_URL implies nats, and
// OPENZRO_BROKER=embedded implies embedded NATS.
func buildDispatcher(ctx context.Context, meter metric.Meter) (dispatcher.Dispatcher, error) {
	kind := strings.ToLower(os.Getenv(envSignalDispatcher))
	if kind == "" {
		// Auto-detect from URL/broker env vars; fall back to inmem.
		switch {
		case os.Getenv(envBroker) == brokerEmbeddedNats:
			kind = dispatcherNats // embedded path uses the NATS dispatcher
		case os.Getenv(envNatsURL) != "":
			kind = dispatcherNats
		case os.Getenv(envRedisURL) != "":
			kind = dispatcherRedis
		default:
			kind = dispatcherInMem
		}
	}

	switch kind {
	case dispatcherInMem:
		log.Infof("signal dispatcher: in-memory (single-instance)")
		return inmem.New(ctx, meter)

	case dispatcherRedis:
		url := os.Getenv(envRedisURL)
		if url == "" {
			return nil, fmt.Errorf("%s=%s requires %s to be set", envSignalDispatcher, dispatcherRedis, envRedisURL)
		}
		opts, err := goredis.ParseURL(url)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", envRedisURL, err)
		}
		client := goredis.NewClient(opts)
		log.Infof("signal dispatcher: redis-compatible at %s (HA mode)", opts.Addr)
		return redis.New(ctx, redis.Config{Client: client}, meter)

	case dispatcherNats:
		url, err := resolveNatsURL(ctx)
		if err != nil {
			return nil, err
		}
		nc, err := natsclient.Connect(url, natsclient.NoEcho(), natsclient.Name("openzro-signal"))
		if err != nil {
			return nil, fmt.Errorf("connect nats at %s: %w", url, err)
		}
		log.Infof("signal dispatcher: nats at %s (HA mode)", url)
		return nats.New(ctx, nc, meter)

	default:
		return nil, fmt.Errorf("unknown %s=%q (expected %q, %q, or %q)",
			envSignalDispatcher, kind, dispatcherInMem, dispatcherRedis, dispatcherNats)
	}
}

// resolveNatsURL returns the URL the local NATS client should connect to.
// In embedded mode it bootstraps an in-process NATS server first and
// returns its loopback URL.
func resolveNatsURL(_ context.Context) (string, error) {
	if os.Getenv(envBroker) == brokerEmbeddedNats {
		srv, err := startEmbeddedNats()
		if err != nil {
			return "", fmt.Errorf("start embedded nats: %w", err)
		}
		// Note: we intentionally do not retain a reference to srv here.
		// Shutdown of the embedded server happens at process exit; an
		// HA deploy is expected to be long-lived.
		return srv.ClientURL(), nil
	}

	url := os.Getenv(envNatsURL)
	if url == "" {
		return "", fmt.Errorf("%s=%s requires either %s or %s=%s",
			envSignalDispatcher, dispatcherNats, envNatsURL, envBroker, brokerEmbeddedNats)
	}
	return url, nil
}

func startEmbeddedNats() (*embedded.Server, error) {
	cfg := embedded.Config{}

	if v := os.Getenv(envEmbeddedNatsPort); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid %s=%q", envEmbeddedNatsPort, v)
		}
		cfg.ClientPort = p
	}
	if v := os.Getenv(envEmbeddedNatsClust); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid %s=%q", envEmbeddedNatsClust, v)
		}
		cfg.ClusterPort = p
	}

	if peers := os.Getenv(envClusterPeers); peers != "" {
		for _, p := range strings.Split(peers, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.ClusterPeers = append(cfg.ClusterPeers, p)
			}
		}
	}

	return embedded.Start(cfg)
}
