package cmd

import (
	"context"
	"fmt"
	"os"

	goredis "github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/metric"

	"github.com/openzro/openzro/signal/dispatcher"
	"github.com/openzro/openzro/signal/dispatcher/inmem"
	"github.com/openzro/openzro/signal/dispatcher/redis"
)

const (
	envSignalDispatcher = "OPENZRO_SIGNAL_DISPATCHER"
	envRedisURL         = "OPENZRO_REDIS_URL"

	dispatcherInMem = "inmem"
	dispatcherRedis = "redis"
)

// buildDispatcher returns the Dispatcher implementation selected by
// OPENZRO_SIGNAL_DISPATCHER. Defaults to "inmem" (single-instance).
//
// To run multiple signal-server instances behind a load balancer, set:
//
//	OPENZRO_SIGNAL_DISPATCHER=redis
//	OPENZRO_REDIS_URL=redis://<host>:<port>/<db>   (or rediss://... for TLS)
//
// All instances must point to the same Redis.
func buildDispatcher(ctx context.Context, meter metric.Meter) (dispatcher.Dispatcher, error) {
	kind := os.Getenv(envSignalDispatcher)
	if kind == "" {
		kind = dispatcherInMem
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
		log.Infof("signal dispatcher: redis at %s (HA mode)", opts.Addr)
		return redis.New(ctx, redis.Config{Client: client}, meter)
	default:
		return nil, fmt.Errorf("unknown %s=%q (expected %q or %q)", envSignalDispatcher, kind, dispatcherInMem, dispatcherRedis)
	}
}
