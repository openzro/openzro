package mdm

import (
	"context"
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

// CachedProvider wraps any Provider with a TTL cache. Callers
// instantiate this around the raw provider in the Manager.
type CachedProvider struct {
	inner Provider
	cache *statusCache
}

// NewCachedProvider wraps p with a status cache. ttl=0 → default 5m.
func NewCachedProvider(p Provider, ttl time.Duration) *CachedProvider {
	return &CachedProvider{inner: p, cache: newStatusCache(ttl)}
}

func (c *CachedProvider) Type() ProviderType { return c.inner.Type() }
func (c *CachedProvider) Close() error       { return c.inner.Close() }

func (c *CachedProvider) GetDeviceStatus(ctx context.Context, lookup DeviceLookup) (DeviceStatus, error) {
	// Cache key includes UserEmail so that two operators with the same
	// hostname under different users (rare but possible — a laptop
	// reassigned during the cache window) don't read each other's
	// status. UserEmail is usually empty for vendors that don't use it,
	// in which case the key collapses to the hostname.
	key := lookup.Hostname
	if lookup.UserEmail != "" {
		key = lookup.Hostname + "\x00" + lookup.UserEmail
	}
	if cached, ok := c.cache.get(key); ok {
		return cached, nil
	}
	status, err := c.inner.GetDeviceStatus(ctx, lookup)
	if err != nil {
		return status, err
	}
	c.cache.put(key, status)
	return status, nil
}
