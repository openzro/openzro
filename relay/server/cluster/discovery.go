package cluster

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// DefaultDiscoveryInterval is the period between DNS reconciles.
// 10 s strikes a balance: K8s endpoint changes propagate to DNS
// within a few seconds, and the cost of one A-record lookup per
// interval is negligible. Operators can tune via Discovery.Interval.
const DefaultDiscoveryInterval = 10 * time.Second

// Resolver looks up the A records of a Headless Service hostname.
// Real callers wire net.DefaultResolver through; tests inject a
// fake to drive the watcher deterministically.
type Resolver interface {
	LookupHost(ctx context.Context, host string) (addrs []string, err error)
}

// netResolver adapts *net.Resolver to the Resolver interface.
type netResolver struct{ r *net.Resolver }

// LookupHost implements Resolver via net.Resolver.LookupHost.
func (n netResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return n.r.LookupHost(ctx, host)
}

// DefaultResolver returns a Resolver backed by net.DefaultResolver
// — the right choice for any process running inside K8s.
func DefaultResolver() Resolver {
	return netResolver{r: net.DefaultResolver}
}

// Discovery watches a Headless Service hostname and keeps the
// Transport's outbound stream set in sync with the live pod set.
//
// On each tick:
//   - resolve `headless` to a list of A records (one per peer pod);
//   - filter out self (selfIP, learned via the K8s downward API);
//   - for each new IP, Dial through the transport (HELLO handshake
//     happens inside Dial);
//   - for any IP that disappeared from DNS, no explicit teardown —
//     the underlying stream's read loop hits EOF when the peer pod
//     terminates and the stream drops itself out of the map.
//
// Discovery deliberately does NOT manage stream lifecycle beyond
// dialing. The transport already handles dedup, accept-side
// streams, and orderly close.
type Discovery struct {
	headless string // K8s Headless Service hostname (e.g. relay-internal)
	port     int    // pod port to dial (e.g. 7090)
	selfIP   string // POD_IP we filter out so we don't dial ourselves
	interval time.Duration

	transport *Transport
	resolver  Resolver

	// known caches the most recent IP set so logs only mention
	// changes, not every tick. Read-only outside the watch loop.
	knownMu sync.Mutex
	known   map[string]struct{}

	cancel context.CancelFunc
	doneCh chan struct{}

	// onTickedHook is set in tests to observe each reconcile pass.
	// Production callers leave it nil.
	onTickedHook func(added, removed []string)
}

// DiscoveryConfig configures a Discovery loop. All fields except
// SelfIP are required; SelfIP defaults to empty (no self-filter).
type DiscoveryConfig struct {
	// Headless is the FQDN of the K8s Headless Service that
	// resolves to every peer pod. In-cluster DNS handles the rest.
	Headless string

	// Port is the inter-pod port to connect to on each peer pod.
	// Discovery dials `<resolved-ip>:<Port>`.
	Port int

	// SelfIP is this pod's IP, learned from the K8s downward API
	// (`POD_IP`). DNS resolution returns every pod including this
	// one; SelfIP filters it so the transport never dials itself.
	SelfIP string

	// Interval is the reconcile period. Defaults to
	// DefaultDiscoveryInterval when zero.
	Interval time.Duration

	// Resolver overrides the DNS resolver (for tests). Production
	// passes nil to use DefaultResolver.
	Resolver Resolver
}

// NewDiscovery wires a watcher that reconciles the transport's
// outbound stream set against the headless service's A records.
// Call Start to begin watching.
func NewDiscovery(transport *Transport, cfg DiscoveryConfig) (*Discovery, error) {
	if transport == nil {
		return nil, fmt.Errorf("cluster discovery: transport is required")
	}
	if cfg.Headless == "" {
		return nil, fmt.Errorf("cluster discovery: Headless hostname is required")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("cluster discovery: invalid Port %d", cfg.Port)
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultDiscoveryInterval
	}
	if cfg.Resolver == nil {
		cfg.Resolver = DefaultResolver()
	}
	return &Discovery{
		headless:  cfg.Headless,
		port:      cfg.Port,
		selfIP:    cfg.SelfIP,
		interval:  cfg.Interval,
		transport: transport,
		resolver:  cfg.Resolver,
		known:     make(map[string]struct{}),
		doneCh:    make(chan struct{}),
	}, nil
}

// Start begins reconciling. The first tick runs immediately so we
// don't wait an interval before discovering peers on a cold start.
// Stop with the returned cancel func or by calling Stop().
func (d *Discovery) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	go d.watch(ctx)
}

// Stop terminates the watch loop and waits for it to exit.
// Safe to call more than once.
func (d *Discovery) Stop() {
	if d.cancel == nil {
		return
	}
	d.cancel()
	<-d.doneCh
}

// Known returns a snapshot of the IPs currently known to be peer
// pods. For diagnostics / tests.
func (d *Discovery) Known() []string {
	d.knownMu.Lock()
	defer d.knownMu.Unlock()
	out := make([]string, 0, len(d.known))
	for ip := range d.known {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

func (d *Discovery) watch(ctx context.Context) {
	defer close(d.doneCh)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	// First reconcile immediately rather than waiting for a tick.
	d.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reconcile(ctx)
		}
	}
}

func (d *Discovery) reconcile(ctx context.Context) {
	resolveCtx, cancel := context.WithTimeout(ctx, d.interval/2)
	defer cancel()

	addrs, err := d.resolver.LookupHost(resolveCtx, d.headless)
	if err != nil {
		// DNS hiccups happen — usually transient; log at debug
		// rather than warn so logs aren't noisy on rolling
		// restart of the kube-dns / coredns deployment.
		log.Debugf("cluster discovery: lookup %s failed: %v", d.headless, err)
		return
	}

	current := make(map[string]struct{}, len(addrs))
	for _, a := range addrs {
		if a == d.selfIP || a == "" {
			continue
		}
		current[a] = struct{}{}
	}

	d.knownMu.Lock()
	previous := d.known
	d.known = current
	d.knownMu.Unlock()

	added := diffKeys(current, previous)
	removed := diffKeys(previous, current)

	for _, ip := range added {
		remote := net.JoinHostPort(ip, strconv.Itoa(d.port))
		// Dial in its own goroutine so a slow-handshaking pod
		// doesn't stall the rest of the reconcile.
		go func(remote string) {
			dialCtx, dialCancel := context.WithTimeout(ctx, d.interval)
			defer dialCancel()
			if _, err := d.transport.Dial(dialCtx, remote); err != nil {
				log.Debugf("cluster discovery: dial %s failed: %v", remote, err)
			}
		}(remote)
	}

	if len(added)+len(removed) > 0 {
		log.Infof("cluster discovery: %s reconciled — +%d / -%d (now %d peers)",
			d.headless, len(added), len(removed), len(current))
	}

	if d.onTickedHook != nil {
		d.onTickedHook(added, removed)
	}
}

// diffKeys returns the keys in a that are not in b. Result is
// sorted for stable logs.
func diffKeys(a, b map[string]struct{}) []string {
	out := make([]string, 0)
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
