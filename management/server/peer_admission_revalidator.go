package server

import (
	"context"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/store"
)

const (
	// admissionRevalidateIntervalEnv overrides the default revalidation
	// cadence. Operators tighten this when their compliance window is
	// narrower than the default 60s — Bacen 4.893 itself does not pin a
	// specific number, but auditors typically want sub-5min revocation.
	admissionRevalidateIntervalEnv = "OPENZRO_ADMISSION_REVALIDATE_INTERVAL_SECONDS"

	// Default cadence: balances revocation latency (~1 min worst case)
	// against load on the management DB and the configured MDM/EDR
	// vendor APIs. Per-peer cost is bounded by the mdm cache TTL
	// (5 min) — the worker hits the cache for repeat lookups within a
	// window, so the actual vendor RPS is len(peers)/300s, not
	// len(peers)/60s.
	defaultAdmissionRevalidateInterval = 60 * time.Second

	// minAdmissionRevalidateInterval guards against pathologically tight
	// values (e.g. someone setting "1" by mistake). Below this we
	// silently floor — protects vendor APIs from a stampede that would
	// trigger their throttles and lock everyone out.
	minAdmissionRevalidateInterval = 10 * time.Second
)

// runAdmissionRevalidator is the long-running worker that reapplies
// the account-wide admission posture checks against every locally
// connected peer.
//
// Why this exists: SyncPeer evaluates admission only when the client
// opens a fresh gRPC Sync stream. After that, the stream stays open
// indefinitely and the peer never re-presents itself for evaluation.
// Without this worker, a peer whose Intune compliance flips to
// non-compliant *after* it connected would keep its session forever.
//
// The worker closes the update channel on denial; that terminates the
// gRPC stream cleanly, the client backs off and retries Login, and
// the Login gate (Phase 1) refuses re-entry. End-to-end revocation
// latency is bounded by:
//
//	worst_case = revalidate_interval + mdm_cache_ttl + client_backoff
//	          =       60s            +     5min      +      ~30s
//	          ≈ ~6 min
//
// HA-aware by construction: GetAllConnectedPeers is local-only, so
// each management instance handles its own set of connected peers.
// No coordination required.
func (am *DefaultAccountManager) runAdmissionRevalidator(ctx context.Context) {
	interval := admissionRevalidateInterval()
	if interval == 0 {
		log.WithContext(ctx).Infof("admission revalidator disabled (%s=0)", admissionRevalidateIntervalEnv)
		return
	}
	log.WithContext(ctx).Infof("admission revalidator running every %s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			am.revalidateAdmissionOnce(ctx)
		}
	}
}

func admissionRevalidateInterval() time.Duration {
	v := os.Getenv(admissionRevalidateIntervalEnv)
	if v == "" {
		return defaultAdmissionRevalidateInterval
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Warnf("ignoring invalid %s=%q; using default %s",
			admissionRevalidateIntervalEnv, v, defaultAdmissionRevalidateInterval)
		return defaultAdmissionRevalidateInterval
	}
	if n < 0 {
		return defaultAdmissionRevalidateInterval
	}
	if n == 0 {
		return 0 // explicitly disabled
	}
	d := time.Duration(n) * time.Second
	if d < minAdmissionRevalidateInterval {
		log.Warnf("%s=%d below minimum %s; flooring",
			admissionRevalidateIntervalEnv, n, minAdmissionRevalidateInterval)
		return minAdmissionRevalidateInterval
	}
	return d
}

// revalidateAdmissionOnce snapshots the connected-peer set and
// evaluates admission for each. Failures close the peer's channel
// and emit a PeerAdmissionDenied audit event.
func (am *DefaultAccountManager) revalidateAdmissionOnce(ctx context.Context) {
	if am.peersUpdateManager == nil {
		return
	}
	peers := am.peersUpdateManager.GetAllConnectedPeers()
	if len(peers) == 0 {
		return
	}

	for peerID := range peers {
		select {
		case <-ctx.Done():
			return
		default:
		}
		am.revalidateAdmissionForPeer(ctx, peerID)
	}
}

// revalidateAdmissionForPeer loads a single peer's account context,
// runs admission, and revokes its session on denial. Errors loading
// the peer (deleted between snapshot and now, store hiccup, etc.) are
// logged at debug — they're transient and the next tick retries.
func (am *DefaultAccountManager) revalidateAdmissionForPeer(ctx context.Context, peerID string) {
	accountID, err := am.Store.GetAccountIDByPeerID(ctx, store.LockingStrengthShare, peerID)
	if err != nil {
		log.WithContext(ctx).Debugf("admission revalidator: peer %s account lookup failed: %v", peerID, err)
		return
	}

	peer, err := am.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	if err != nil {
		log.WithContext(ctx).Debugf("admission revalidator: peer %s load failed: %v", peerID, err)
		return
	}

	// evaluateAdmission already audit-logs and returns
	// status.PermissionDenied on rejection. We translate that into a
	// channel close — the gRPC handler wakes up on its update channel
	// closing and drops the stream with the same error semantics.
	// Revalidator: peer is persisted, so the helper resolves group
	// memberships from the store. candidateGroups is nil. The
	// peerID stands as the audit initiator since this is a
	// system-initiated re-check, not a user action.
	if err := am.evaluateAdmission(ctx, am.Store, accountID, peer, nil, peerID); err != nil {
		log.WithContext(ctx).Infof("admission revalidator: revoking session for peer %s: %v", peerID, err)
		am.peersUpdateManager.CloseChannel(ctx, peerID)
	}
}
