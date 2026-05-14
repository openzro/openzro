package mdm

import (
	"context"
	"strings"
	"sync"
	"time"
)

// statusCache is a per-provider TTL cache of device statuses. Posture
// validation runs on every peer sync; without this cache the vendor
// API would receive a request per peer per heartbeat. The 5-minute
// default keeps freshness reasonable while keeping API cost bounded.
type statusCache struct {
	ttl time.Duration
	mu  sync.RWMutex
	e   map[string]cachedStatus
}

type cachedStatus struct {
	value     DeviceStatus
	expiresAt time.Time
}

func newStatusCache(ttl time.Duration) *statusCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &statusCache{ttl: ttl, e: map[string]cachedStatus{}}
}

func (c *statusCache) get(key string) (DeviceStatus, bool) {
	c.mu.RLock()
	entry, ok := c.e[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return DeviceStatus{}, false
	}
	return entry.value, true
}

func (c *statusCache) put(key string, status DeviceStatus) {
	status.LastChecked = time.Now()
	c.mu.Lock()
	c.e[key] = cachedStatus{
		value:     status,
		expiresAt: status.LastChecked.Add(c.ttl),
	}
	c.mu.Unlock()
}

// snapshot returns a list of the lookups currently in the cache,
// decoded back from their string keys. Used by the refresh worker
// to iterate "every device we've seen for this provider" and tick
// each through the cache so entries refresh before they expire.
// The set is captured under the read lock — subsequent mutations
// are invisible to this snapshot, which is fine for the worker's
// best-effort semantics.
func (c *statusCache) snapshot() []DeviceLookup {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]DeviceLookup, 0, len(c.e))
	for key := range c.e {
		out = append(out, decodeCacheKey(key))
	}
	return out
}

// encodeCacheKey + decodeCacheKey are the single source of truth for
// the cache's key shape. Centralised so the refresh worker can
// round-trip lookups through the cache without re-implementing the
// hostname/user-email join.
func encodeCacheKey(lookup DeviceLookup) string {
	if lookup.UserEmail != "" {
		return lookup.Hostname + "\x00" + lookup.UserEmail
	}
	return lookup.Hostname
}

func decodeCacheKey(key string) DeviceLookup {
	if i := strings.IndexByte(key, 0); i >= 0 {
		return DeviceLookup{Hostname: key[:i], UserEmail: key[i+1:]}
	}
	return DeviceLookup{Hostname: key}
}

// CachedProvider wraps any Provider with a TTL cache. Callers
// instantiate this around the raw provider in the Manager.
type CachedProvider struct {
	inner Provider
	cache *statusCache
	// onFreshFetch fires after a cache miss has been resolved from
	// inner — used by the Manager to broadcast the result to other
	// replicas via cluster pub/sub. Nil in single-instance mode and
	// in tests that don't exercise cross-replica fan-out.
	onFreshFetch func(lookup DeviceLookup, status DeviceStatus)
}

// NewCachedProvider wraps p with a status cache. ttl=0 → default 5m.
func NewCachedProvider(p Provider, ttl time.Duration) *CachedProvider {
	return &CachedProvider{inner: p, cache: newStatusCache(ttl)}
}

// setBroadcaster wires a publish hook fired after every successful
// inner fetch. The Manager calls this once per provider at Refresh,
// pointing at a closure that knows the provider's topic.
func (c *CachedProvider) setBroadcaster(fn func(lookup DeviceLookup, status DeviceStatus)) {
	c.onFreshFetch = fn
}

func (c *CachedProvider) Type() ProviderType { return c.inner.Type() }
func (c *CachedProvider) Close() error       { return c.inner.Close() }

func (c *CachedProvider) GetDeviceStatus(ctx context.Context, lookup DeviceLookup) (DeviceStatus, error) {
	// Cache key includes UserEmail so that two operators with the same
	// hostname under different users (rare but possible — a laptop
	// reassigned during the cache window) don't read each other's
	// status. UserEmail is usually empty for vendors that don't use it,
	// in which case the key collapses to the hostname.
	key := encodeCacheKey(lookup)
	if cached, ok := c.cache.get(key); ok {
		return cached, nil
	}
	status, err := c.inner.GetDeviceStatus(ctx, lookup)
	if err != nil {
		return status, err
	}
	c.cache.put(key, status)
	if c.onFreshFetch != nil {
		c.onFreshFetch(lookup, status)
	}
	return status, nil
}

// putFromBroker fills the cache from an inbound cluster broadcast.
// Bypasses onFreshFetch so the inbound event doesn't echo back to
// the broker. Used only by the subscriber goroutine in pubsub.go.
func (c *CachedProvider) putFromBroker(lookup DeviceLookup, status DeviceStatus) {
	c.cache.put(encodeCacheKey(lookup), status)
}
