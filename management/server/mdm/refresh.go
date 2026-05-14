package mdm

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// refreshWorker keeps a CachedProvider's entries warm. On each tick
// it walks the current cache and calls GetDeviceStatus on every
// entry — entries still within their TTL serve from cache (no
// vendor API call), expired entries refetch and re-cache. This lets
// us ride a fixed schedule without flogging the vendor on every
// tick: the worker only "spends" API calls on entries that need
// them, but spends them on the worker's goroutine rather than on a
// peer Sync where the latency would be visible.
//
// One worker per CachedProvider, started/stopped by the Manager on
// Refresh(). The first fire is staggered by a half-jitter so a
// cluster reboot doesn't line every replica's workers up on the
// same wall-clock second.
type refreshWorker struct {
	provider *CachedProvider
	interval time.Duration
	rowID    uint64
	rowType  ProviderType

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func startRefreshWorker(p *CachedProvider, interval time.Duration, rowID uint64, rowType ProviderType) *refreshWorker {
	w := &refreshWorker{
		provider: p,
		interval: interval,
		rowID:    rowID,
		rowType:  rowType,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *refreshWorker) run() {
	defer close(w.done)

	// Stagger the first fire over (0, interval) so a fleet of replicas
	// that just rolled doesn't hit the vendor in lockstep. UnixNano
	// mod interval gives a cheap, unseeded jitter that's good enough
	// for de-correlation; we don't need cryptographic randomness here.
	jitter := time.Duration(time.Now().UnixNano() % int64(w.interval))
	select {
	case <-time.After(jitter):
	case <-w.stop:
		return
	}

	w.refresh()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.refresh()
		case <-w.stop:
			return
		}
	}
}

func (w *refreshWorker) refresh() {
	lookups := w.provider.cache.snapshot()
	if len(lookups) == 0 {
		return
	}
	// Detached ctx — this is a background loop, not a request, and
	// must not inherit a cancelled parent. Per-tick timeout caps how
	// long the worker can spend on the vendor when it's slow.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var served, errored int
	for _, l := range lookups {
		// Go through the cached path on purpose: entries still inside
		// their TTL return without an API call, expired entries
		// refetch and re-cache. The worker exists to take that
		// refetch latency on its own goroutine so peer Sync never
		// pays it.
		if _, err := w.provider.GetDeviceStatus(ctx, l); err != nil {
			errored++
			continue
		}
		served++
	}
	log.Debugf("mdm refresh worker: provider=%d (%s) cache=%d served=%d errored=%d",
		w.rowID, w.rowType, len(lookups), served, errored)
}

// Stop is idempotent — multiple callers (Manager.Refresh on a hot
// reload, then Manager.Close on shutdown) can race without panicking
// on a double channel-close.
func (w *refreshWorker) Stop() {
	w.closeOnce.Do(func() {
		close(w.stop)
		<-w.done
	})
}
