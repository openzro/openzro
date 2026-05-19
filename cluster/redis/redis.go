// Package redis implements cluster.Coordinator on top of a Redis-compatible
// server (Redis, Valkey, Dragonfly).
//
// Locks: SET <key> <token> NX EX <ttl> — token is a random UUID identifying
// this acquirer. Release runs an EVAL script that DELs the key only when
// the stored value matches the token, so a slow holder whose TTL has
// already expired cannot accidentally release the new holder's lock.
// A heartbeat goroutine renews the TTL while the lock is held.
//
// Pub/Sub: standard Redis pub/sub. At-most-once delivery; broker outages
// drop in-flight messages.
//
// References:
//   - https://redis.io/docs/latest/develop/use/patterns/distributed-locks/ (token+EVAL pattern)
//   - https://redis.io/docs/latest/develop/pubsub/
//
// No upstream openzro/netbird post-AGPL code was consulted.
package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/cluster"
)

const (
	lockKeyPrefix = "oz:cluster:lock:"

	// DefaultLockTTL is how long a lock's Redis key lives without
	// renewal. Heartbeat refreshes it at TTL/3.
	DefaultLockTTL = 30 * time.Second

	// DefaultLockAcquireBackoff is the initial sleep between TryLock
	// retries when the lock is held by someone else. Doubles up to
	// DefaultLockAcquireBackoffMax.
	DefaultLockAcquireBackoff    = 50 * time.Millisecond
	DefaultLockAcquireBackoffMax = 1 * time.Second
)

// releaseScript: DEL the key only if its value equals the provided token.
// Avoids the "expired-then-released-by-someone-else" race.
const releaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
else
  return 0
end
`

// Config tunes the coordinator. All fields are optional; zero values fall
// back to the Default* constants.
type Config struct {
	Client  *redis.Client
	LockTTL time.Duration
}

// Coordinator is a Redis-backed cluster.Coordinator.
type Coordinator struct {
	rdb     *redis.Client
	lockTTL time.Duration
	release *redis.Script

	closedMu sync.RWMutex
	closed   bool
	parent   context.Context
	cancel   context.CancelFunc
}

// New constructs a Redis coordinator and verifies connectivity. The
// caller owns the *redis.Client; New does not Close() it on Close.
func New(ctx context.Context, cfg Config) (*Coordinator, error) {
	if cfg.Client == nil {
		return nil, errors.New("redis coordinator: Client is required")
	}
	ttl := cfg.LockTTL
	if ttl <= 0 {
		ttl = DefaultLockTTL
	}

	parentCtx, cancel := context.WithCancel(ctx)
	if err := cfg.Client.Ping(parentCtx).Err(); err != nil {
		cancel()
		return nil, fmt.Errorf("redis coordinator: ping: %w", err)
	}

	return &Coordinator{
		rdb:     cfg.Client,
		lockTTL: ttl,
		release: redis.NewScript(releaseScript),
		parent:  parentCtx,
		cancel:  cancel,
	}, nil
}

// Close cancels all in-flight Locks and Subscribes.
func (c *Coordinator) Close() error {
	c.closedMu.Lock()
	defer c.closedMu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	c.cancel()
	return nil
}

func (c *Coordinator) isClosed() bool {
	c.closedMu.RLock()
	defer c.closedMu.RUnlock()
	return c.closed
}

// Lock blocks (with backoff) until it acquires the named lock or ctx is
// canceled. The returned release func is safe to call multiple times.
func (c *Coordinator) Lock(ctx context.Context, name string) (func(), error) {
	if c.isClosed() {
		return nil, cluster.ErrClosed
	}
	key := lockKeyPrefix + name
	token := uuid.NewString()

	backoff := DefaultLockAcquireBackoff
	for {
		ok, err := c.rdb.SetNX(ctx, key, token, c.lockTTL).Result()
		if err != nil {
			return nil, fmt.Errorf("redis coordinator: acquire %s: %w", name, err)
		}
		if ok {
			break
		}
		// Held by someone else — wait and retry.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.parent.Done():
			return nil, cluster.ErrClosed
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > DefaultLockAcquireBackoffMax {
			backoff = DefaultLockAcquireBackoffMax
		}
	}

	heartbeatCtx, stopHeartbeat := context.WithCancel(c.parent)
	go c.heartbeat(heartbeatCtx, key, token)

	var releaseOnce sync.Once
	return func() {
		releaseOnce.Do(func() {
			stopHeartbeat()
			// Best-effort: if Redis is unreachable, the TTL takes over.
			delCtx, cancel := context.WithTimeout(c.parent, 2*time.Second)
			defer cancel()
			if _, err := c.release.Run(delCtx, c.rdb, []string{key}, token).Result(); err != nil {
				log.Debugf("redis coordinator: release %s: %v (TTL will clean up)", name, err)
			}
		})
	}, nil
}

func (c *Coordinator) heartbeat(ctx context.Context, key, token string) {
	interval := c.lockTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Only refresh if we still own the lock.
			res, err := c.rdb.Eval(c.parent, `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
  return 0
end`, []string{key}, token, c.lockTTL.Milliseconds()).Int64()
			if err != nil {
				log.Warnf("redis coordinator: heartbeat %s: %v", key, err)
				continue
			}
			if res == 0 {
				// We no longer own the lock (TTL expired and someone else took it).
				log.Warnf("redis coordinator: lost lock %s during heartbeat", key)
				return
			}
		}
	}
}

// Publish sends payload to topic. Returns when Redis has accepted the
// PUBLISH command.
func (c *Coordinator) Publish(ctx context.Context, topic string, payload []byte) error {
	if c.isClosed() {
		return cluster.ErrClosed
	}
	return c.rdb.Publish(ctx, topic, payload).Err()
}

// Subscribe returns a channel of events for topic. The channel closes when
// ctx is canceled or the coordinator is Closed.
func (c *Coordinator) Subscribe(ctx context.Context, topic string) (<-chan cluster.Event, error) {
	if c.isClosed() {
		return nil, cluster.ErrClosed
	}
	pubsub := c.rdb.Subscribe(ctx, topic)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("redis coordinator: subscribe %s: %w", topic, err)
	}

	out := make(chan cluster.Event, 64)
	go func() {
		defer close(out)
		defer pubsub.Close()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.parent.Done():
				return
			case m, ok := <-ch:
				if !ok {
					return
				}
				select {
				case out <- cluster.Event{Topic: m.Channel, Payload: []byte(m.Payload)}:
				default:
					log.Warnf("redis coordinator: subscriber for %s is slow; dropping event", topic)
				}
			}
		}
	}()
	return out, nil
}
