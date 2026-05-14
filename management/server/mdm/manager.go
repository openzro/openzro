package mdm

import (
	"context"
	"errors"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
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
//
// One Manager per process. Hot-reload happens via Refresh() — called
// after the admin API mutates a provider. Refresh swaps the provider
// map atomically and tears down stale workers before starting new
// ones, so an interval change on the form takes effect on the next
// admin save without a process restart.
type Manager struct {
	store *Store

	mu        sync.RWMutex
	providers map[uint64]*CachedProvider // by row ID
	workers   map[uint64]*refreshWorker  // by row ID, one per CachedProvider
}

func NewManager(ctx context.Context, store *Store) (*Manager, error) {
	if store == nil {
		return nil, errors.New("mdm: store is required")
	}
	m := &Manager{
		store:     store,
		providers: map[uint64]*CachedProvider{},
		workers:   map[uint64]*refreshWorker{},
	}
	if err := m.Refresh(ctx); err != nil {
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
		nextProviders[row.ID] = cached
		nextWorkers[row.ID] = startRefreshWorker(cached, ttl, row.ID, row.Type)
	}

	m.mu.Lock()
	oldProviders := m.providers
	oldWorkers := m.workers
	m.providers = nextProviders
	m.workers = nextWorkers
	m.mu.Unlock()

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

// Close shuts the manager down — stops every refresh worker and
// closes every active provider. Idempotent.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.workers {
		w.Stop()
	}
	for _, p := range m.providers {
		_ = p.Close()
	}
	m.workers = map[uint64]*refreshWorker{}
	m.providers = map[uint64]*CachedProvider{}
	return nil
}
