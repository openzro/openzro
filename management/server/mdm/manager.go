package mdm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Manager is the in-process orchestrator that:
//
//   - reads provider rows from the Store
//   - constructs concrete drivers (Intune / SentinelOne / Huntress /
//     CrowdStrike)
//   - wraps them in a CachedProvider with the configured TTL
//   - serves status lookups to posture checks via Lookup()
//
// One Manager per process. Hot-reload happens via Refresh() — called
// after the admin API mutates a provider.
type Manager struct {
	store    *Store
	cacheTTL time.Duration

	mu        sync.RWMutex
	providers map[uint64]Provider // by row ID
}

func NewManager(ctx context.Context, store *Store, cacheTTL time.Duration) (*Manager, error) {
	if store == nil {
		return nil, errors.New("mdm: store is required")
	}
	m := &Manager{store: store, cacheTTL: cacheTTL, providers: map[uint64]Provider{}}
	if err := m.Refresh(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// Refresh re-reads the Store and reconstructs the provider map.
// Existing providers' Close() runs after the swap. Errors building
// individual providers are logged loud but do not fail the refresh —
// a misconfigured row never takes the whole posture pipeline down.
func (m *Manager) Refresh(ctx context.Context) error {
	rows, err := m.store.List(ctx)
	if err != nil {
		return fmt.Errorf("mdm: list providers: %w", err)
	}

	next := map[uint64]Provider{}
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
		next[row.ID] = NewCachedProvider(p, m.cacheTTL)
	}

	m.mu.Lock()
	old := m.providers
	m.providers = next
	m.mu.Unlock()

	for _, p := range old {
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

// Close shuts the manager down — closes every active provider.
// Idempotent.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.providers {
		_ = p.Close()
	}
	m.providers = map[uint64]Provider{}
	return nil
}
