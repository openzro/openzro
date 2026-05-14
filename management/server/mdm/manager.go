package mdm

import (
	"context"
	"errors"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/cluster"
)

// Manager is the in-process orchestrator that:
//
//   - reads provider rows from the Store
//   - constructs concrete drivers (Intune / SentinelOne / Huntress /
//     CrowdStrike)
//   - wraps them in a CachedProvider with the per-provider configured
//     TTL (MDMProvider.RefreshIntervalMinutes)
//   - serves status lookups to posture checks via Lookup()
//   - keeps a per-provider RefreshWorker warming the cache so big
//     tenants don't thunder-herd the vendor API on rollout
//   - in HA deployments, broadcasts every fresh fetch over the
//     cluster pub/sub so sibling replicas populate their caches
//     without re-paying the vendor latency (per ADR-0006-ish
//     thinking — replica fan-out is the dominant cost in busy
//     tenants, especially against rate-limited APIs like Graph)
//
// One Manager per process. Hot-reload happens via Refresh() — called
// after the admin API mutates a provider. Refresh swaps the provider
// map atomically and tears down stale workers + subscriptions before
// starting new ones, so an interval change on the form takes effect
// on the next admin save without a process restart.
type Manager struct {
	store *Store
	// coord is the optional cluster coordinator used to publish fresh
	// fetches and subscribe to peer replicas' fetches. nil in
	// single-instance mode — the Manager falls back to a self-
	// contained cache + worker setup with no cross-replica chatter.
	coord cluster.Coordinator

	// baseCtx is the process-lifetime context the Manager owns for
	// long-running goroutines (cluster subscriptions). It MUST be
	// independent of the request-scoped ctx the Refresh() admin path
	// uses — otherwise every API save would cancel every subscription
	// the instant the handler returned. baseCancel is wired to fire
	// on Close so shutdown actually unwinds the subscribers.
	baseCtx    context.Context
	baseCancel context.CancelFunc

	mu        sync.RWMutex
	providers map[uint64]*CachedProvider // by row ID
	workers   map[uint64]*refreshWorker  // by row ID, one per CachedProvider
	subs      map[uint64]context.CancelFunc
}

// NewManager builds a Manager from the store and (optionally) the
// cluster coordinator. coord may be nil — that's the single-instance
// case; everything still works, just without cross-replica fan-out.
func NewManager(ctx context.Context, store *Store, coord cluster.Coordinator) (*Manager, error) {
	if store == nil {
		return nil, errors.New("mdm: store is required")
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())
	m := &Manager{
		store:      store,
		coord:      coord,
		baseCtx:    baseCtx,
		baseCancel: baseCancel,
		providers:  map[uint64]*CachedProvider{},
		workers:    map[uint64]*refreshWorker{},
		subs:       map[uint64]context.CancelFunc{},
	}
	if err := m.Refresh(ctx); err != nil {
		baseCancel()
		return nil, err
	}
	return m, nil
}

// Refresh re-reads the Store and reconstructs the provider map.
// Existing providers' Close() and worker Stop() run after the swap.
// Errors building individual providers are logged loud but do not
// fail the refresh — a misconfigured row never takes the whole
// posture pipeline down.
func (m *Manager) Refresh(ctx context.Context) error {
	rows, err := m.store.List(ctx)
	if err != nil {
		return fmt.Errorf("mdm: list providers: %w", err)
	}

	nextProviders := map[uint64]*CachedProvider{}
	nextWorkers := map[uint64]*refreshWorker{}
	nextSubs := map[uint64]context.CancelFunc{}
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		p, err := m.buildProvider(&row)
		if err != nil {
			log.WithContext(ctx).Errorf("mdm: skipping provider #%d (%s/%s): %v",
				row.ID, row.Type, row.Name, err)
			continue
		}
		ttl := row.ResolvedRefreshInterval()
		cached := NewCachedProvider(p, ttl)

		// Wire the publish hook BEFORE starting the subscriber so a
		// fresh fetch can immediately fan out. The closure captures
		// providerID + coord; nil-coord short-circuits in publishStatus.
		rowID := row.ID
		cached.setBroadcaster(func(lookup DeviceLookup, status DeviceStatus) {
			// Detached ctx — the broadcast is best-effort and a parent
			// ctx cancellation must not strand the publish on
			// in-flight fetches that already succeeded.
			publishStatus(context.Background(), m.coord, rowID, lookup, status)
		})

		// Subscriptions are bound to the Manager's process-lifetime ctx
		// (NOT the request-scoped ctx Refresh runs under) — otherwise
		// every admin save would tear the sub down the instant the
		// HTTP handler returned. Cancel is captured per-provider so
		// hot-reload can stop only the affected subs.
		cancel, err := subscribeStatus(m.baseCtx, m.coord, rowID, cached)
		if err != nil {
			log.WithContext(ctx).Warnf("mdm: subscribe failed for provider #%d (%s): %v — cross-replica fan-out disabled for this provider",
				rowID, row.Type, err)
			cancel = func() {}
		}

		nextProviders[row.ID] = cached
		nextWorkers[row.ID] = startRefreshWorker(cached, ttl, row.ID, row.Type)
		nextSubs[row.ID] = cancel
	}

	m.mu.Lock()
	oldProviders := m.providers
	oldWorkers := m.workers
	oldSubs := m.subs
	m.providers = nextProviders
	m.workers = nextWorkers
	m.subs = nextSubs
	m.mu.Unlock()

	for _, cancel := range oldSubs {
		cancel()
	}
	for _, w := range oldWorkers {
		w.Stop()
	}
	for _, p := range oldProviders {
		_ = p.Close()
	}
	return nil
}

func (m *Manager) buildProvider(row *MDMProvider) (Provider, error) {
	plain, err := m.store.Decrypt(row)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	switch row.Type {
	case TypeIntune:
		return NewIntune(*plain.(*IntuneConfig))
	case TypeSentinelOne:
		return NewSentinelOne(*plain.(*SentinelOneConfig))
	case TypeHuntress:
		return NewHuntress(*plain.(*HuntressConfig))
	case TypeCrowdStrike:
		return NewCrowdStrike(*plain.(*CrowdStrikeConfig))
	}
	return nil, fmt.Errorf("unknown provider type %q", row.Type)
}

// Lookup returns the device status from the named provider.
// Returns ErrNotConfigured if the provider ID is unknown — the
// posture check translates that into "configuration error, peer
// non-compliant".
func (m *Manager) Lookup(ctx context.Context, providerID uint64, lookup DeviceLookup) (DeviceStatus, error) {
	m.mu.RLock()
	p, ok := m.providers[providerID]
	m.mu.RUnlock()
	if !ok {
		return DeviceStatus{}, ErrNotConfigured
	}
	return p.GetDeviceStatus(ctx, lookup)
}

// Close shuts the manager down — cancels every cluster subscription,
// stops every refresh worker and closes every active provider.
// Idempotent.
func (m *Manager) Close() error {
	m.mu.Lock()
	subs, workers, providers := m.subs, m.workers, m.providers
	m.subs = map[uint64]context.CancelFunc{}
	m.workers = map[uint64]*refreshWorker{}
	m.providers = map[uint64]*CachedProvider{}
	m.mu.Unlock()

	// Cancel the process-lifetime ctx first so any in-flight worker
	// refresh and subscriber goroutine see the cancellation and bail
	// fast instead of holding our lock while we wait on a 30s vendor
	// timeout in Stop().
	if m.baseCancel != nil {
		m.baseCancel()
	}
	for _, cancel := range subs {
		cancel()
	}
	for _, w := range workers {
		w.Stop()
	}
	for _, p := range providers {
		_ = p.Close()
	}
	return nil
}
