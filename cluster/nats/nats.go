// Package nats implements cluster.Coordinator on top of NATS, using
// JetStream's KV bucket for distributed locks and core NATS pub/sub for
// fanout. Works against any NATS deployment with JetStream enabled —
// embedded (cluster/embedded turns JetStream on by default) or external.
//
// Locks: KV bucket entry with our holder token. Atomicity comes from
// JetStream KV's Create (only-if-absent), Update with revision (CAS), and
// per-key TTLs. A heartbeat goroutine renews the entry's lifetime while
// the lock is held; on release the entry is Purged. If a holder dies the
// TTL eventually expires and another acquirer can succeed.
//
// Pub/Sub: standard NATS subjects. The coordinator namespaces every topic
// under "oz.cluster." so collisions with other NATS subjects (e.g. the
// signal dispatcher's oz.signal.*) are impossible.
//
// References:
//   - https://docs.nats.io/nats-concepts/jetstream/key-value-store
//   - https://docs.nats.io/nats-concepts/subjects
//
// No upstream openzro/netbird post-AGPL code was consulted.
package nats

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	natsclient "github.com/nats-io/nats.go"
	jetstream "github.com/nats-io/nats.go/jetstream"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/cluster"
)

const (
	// LocksBucket is the JetStream KV bucket name used for distributed
	// locks. Created by New if it doesn't exist.
	LocksBucket = "oz_cluster_locks"

	// PubSubSubjectPrefix namespaces all coordinator pub/sub topics so
	// they cannot collide with other NATS subjects in the deployment.
	PubSubSubjectPrefix = "oz.cluster."

	DefaultLockTTL               = 30 * time.Second
	DefaultLockAcquireBackoff    = 50 * time.Millisecond
	DefaultLockAcquireBackoffMax = 1 * time.Second
)

// Config tunes the coordinator. All fields are optional; zero values
// fall back to the Default* constants.
type Config struct {
	Conn    *natsclient.Conn
	LockTTL time.Duration
}

// Coordinator is a NATS+JetStream-KV-backed cluster.Coordinator.
type Coordinator struct {
	nc      *natsclient.Conn
	js      jetstream.JetStream
	kv      jetstream.KeyValue
	lockTTL time.Duration

	closedMu sync.RWMutex
	closed   bool
	parent   context.Context
	cancel   context.CancelFunc
}

// New constructs a NATS coordinator. The caller owns the *nats.Conn; New
// does not Close() it. Requires JetStream to be enabled on the target
// NATS deployment.
func New(ctx context.Context, cfg Config) (*Coordinator, error) {
	if cfg.Conn == nil {
		return nil, errors.New("nats coordinator: Conn is required")
	}
	if !cfg.Conn.IsConnected() {
		return nil, errors.New("nats coordinator: connection is not in connected state")
	}
	ttl := cfg.LockTTL
	if ttl <= 0 {
		ttl = DefaultLockTTL
	}

	js, err := jetstream.New(cfg.Conn)
	if err != nil {
		return nil, fmt.Errorf("nats coordinator: jetstream context: %w", err)
	}

	parentCtx, cancel := context.WithCancel(ctx)

	// The JetStream cluster needs an elected meta-leader before the KV
	// bucket can be created. On fresh HA deploys (3 pods booting
	// together) the election takes a few seconds; a management boot in
	// that window misses it and the pod exits with an error, creating
	// a restart loop that never converges. Retry with backoff up to
	// 60s covers the common case.
	var kv jetstream.KeyValue
	const (
		bucketRetryTimeout = 60 * time.Second
		bucketRetryBackoff = 2 * time.Second
	)
	deadline := time.Now().Add(bucketRetryTimeout)
	for {
		kv, err = js.CreateOrUpdateKeyValue(parentCtx, jetstream.KeyValueConfig{
			Bucket:      LocksBucket,
			Description: "openzro distributed locks",
			TTL:         ttl,
			// Locks are TTL-bound and inherently ephemeral — they expire
			// in seconds and re-acquire on restart. Memory storage is the
			// right semantic match (and avoids needing JetStream file
			// store + PVCs in HA deployments). The KV API requests file
			// storage by default, which fails on NATS deployments that
			// only allow memory streams (e.g. our chart's nats subchart
			// with fileStore.enabled=false). Pinning Storage here makes
			// the coordinator portable across both layouts.
			Storage: jetstream.MemoryStorage,
		})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			return nil, fmt.Errorf("nats coordinator: locks bucket (after %s): %w", bucketRetryTimeout, err)
		}
		log.WithContext(parentCtx).Warnf("nats coordinator: locks bucket creation failed, retrying in %s: %v", bucketRetryBackoff, err)
		select {
		case <-parentCtx.Done():
			cancel()
			return nil, fmt.Errorf("nats coordinator: locks bucket cancelled: %w", parentCtx.Err())
		case <-time.After(bucketRetryBackoff):
		}
	}

	return &Coordinator{
		nc:      cfg.Conn,
		js:      js,
		kv:      kv,
		lockTTL: ttl,
		parent:  parentCtx,
		cancel:  cancel,
	}, nil
}

// Close cancels in-flight operations.
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

// Lock acquires the named lock, blocking with exponential backoff until
// success or ctx cancellation.
func (c *Coordinator) Lock(ctx context.Context, name string) (func(), error) {
	if c.isClosed() {
		return nil, cluster.ErrClosed
	}
	token := uuid.NewString()
	tokenBytes := []byte(token)

	var rev uint64
	backoff := DefaultLockAcquireBackoff
	for {
		// Create only succeeds when the key does not exist — atomic
		// compare-and-swap against absence.
		r, err := c.kv.Create(ctx, name, tokenBytes)
		if err == nil {
			rev = r
			break
		}
		// jetstream returns a typed error here (already-exists) but it's
		// not exported as a sentinel — fall through to retry on any err.
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

	hbCtx, stopHB := context.WithCancel(c.parent)
	go c.heartbeat(hbCtx, name, tokenBytes, rev)

	var releaseOnce sync.Once
	return func() {
		releaseOnce.Do(func() {
			stopHB()
			delCtx, cancel := context.WithTimeout(c.parent, 2*time.Second)
			defer cancel()
			// Purge unconditionally — TTL is the safety net if this fails.
			if err := c.kv.Purge(delCtx, name); err != nil {
				log.Debugf("nats coordinator: release %s: %v (TTL will clean up)", name, err)
			}
		})
	}, nil
}

func (c *Coordinator) heartbeat(ctx context.Context, name string, token []byte, rev uint64) {
	interval := c.lockTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	currentRev := rev
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Update with the current revision = CAS. If another holder
			// acquired the lock (because our entry's TTL expired), the
			// revision won't match and we step away.
			newRev, err := c.kv.Update(c.parent, name, token, currentRev)
			if err != nil {
				log.Warnf("nats coordinator: lost lock %s during heartbeat: %v", name, err)
				return
			}
			currentRev = newRev
		}
	}
}

// Publish sends payload to topic, namespaced under oz.cluster.
func (c *Coordinator) Publish(_ context.Context, topic string, payload []byte) error {
	if c.isClosed() {
		return cluster.ErrClosed
	}
	return c.nc.Publish(PubSubSubjectPrefix+topic, payload)
}

// Subscribe returns events on topic until ctx is cancelled.
func (c *Coordinator) Subscribe(ctx context.Context, topic string) (<-chan cluster.Event, error) {
	if c.isClosed() {
		return nil, cluster.ErrClosed
	}
	subject := PubSubSubjectPrefix + topic
	out := make(chan cluster.Event, 64)

	sub, err := c.nc.Subscribe(subject, func(m *natsclient.Msg) {
		select {
		case out <- cluster.Event{Topic: topic, Payload: m.Data}:
		default:
			log.Warnf("nats coordinator: subscriber for %s is slow; dropping event", topic)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("nats coordinator: subscribe %s: %w", subject, err)
	}

	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
		close(out)
	}()
	return out, nil
}
