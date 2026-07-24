package server

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	nbAccount "github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/store"
)

const (
	ephemeralLifeTime = 10 * time.Minute
	// cleanupWindow is the time window to wait after nearest peer deadline to start the cleanup procedure.
	cleanupWindow = 1 * time.Minute
)

var (
	timeNow = time.Now
)

type ephemeralPeer struct {
	id        string
	accountID string
	deadline  time.Time
	next      *ephemeralPeer
}

// todo: consider to remove peer from ephemeral list when the peer has been deleted via API. If we do not do it
// in worst case we will get invalid error message in this manager.

// EphemeralManager keep a list of ephemeral peers. After ephemeralLifeTime inactivity the peer will be deleted
// automatically. Inactivity means the peer disconnected from the Management server.
type EphemeralManager struct {
	store          store.Store
	accountManager nbAccount.Manager

	headPeer  *ephemeralPeer
	tailPeer  *ephemeralPeer
	peersLock sync.Mutex
	timer     *time.Timer

	lifeTime      time.Duration
	cleanupWindow time.Duration
}

// NewEphemeralManager instantiate new EphemeralManager
func NewEphemeralManager(store store.Store, accountManager nbAccount.Manager) *EphemeralManager {
	return &EphemeralManager{
		store:          store,
		accountManager: accountManager,

		lifeTime:      ephemeralLifeTime,
		cleanupWindow: cleanupWindow,
	}
}

// LoadInitialPeers load from the database the ephemeral type of peers and schedule a cleanup procedure to the head
// of the linked list (to the most deprecated peer). At the end of cleanup it schedules the next cleanup to the new
// head.
func (e *EphemeralManager) LoadInitialPeers(ctx context.Context) {
	e.peersLock.Lock()
	defer e.peersLock.Unlock()

	e.loadEphemeralPeers(ctx)
	if e.headPeer != nil {
		e.timer = time.AfterFunc(e.lifeTime, func() {
			e.cleanup(ctx)
		})
	}
}

// Stop timer
func (e *EphemeralManager) Stop() {
	e.peersLock.Lock()
	defer e.peersLock.Unlock()

	if e.timer != nil {
		e.timer.Stop()
	}
}

// OnPeerConnected remove the peer from the linked list of ephemeral peers. Because it has been called when the peer
// is active the manager will not delete it while it is active.
func (e *EphemeralManager) OnPeerConnected(ctx context.Context, peer *nbpeer.Peer) {
	if !peer.Ephemeral {
		return
	}

	log.WithContext(ctx).Tracef("remove peer from ephemeral list: %s", peer.ID)

	e.peersLock.Lock()
	defer e.peersLock.Unlock()

	e.removePeer(peer.ID)

	// stop the unnecessary timer
	if e.headPeer == nil && e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
}

// OnPeerDisconnected add the peer to the linked list of ephemeral peers. Because of the peer
// is inactive it will be deleted after the ephemeralLifeTime period.
func (e *EphemeralManager) OnPeerDisconnected(ctx context.Context, peer *nbpeer.Peer) {
	if !peer.Ephemeral {
		return
	}

	log.WithContext(ctx).Tracef("add peer to ephemeral list: %s", peer.ID)

	e.peersLock.Lock()
	defer e.peersLock.Unlock()

	if e.isPeerOnList(peer.ID) {
		return
	}

	e.addPeer(peer.AccountID, peer.ID, e.newDeadLine())
	if e.timer == nil {
		delay := e.headPeer.deadline.Sub(timeNow()) + e.cleanupWindow
		if delay < 0 {
			delay = 0
		}
		e.timer = time.AfterFunc(delay, func() {
			e.cleanup(ctx)
		})
	}
}

func (e *EphemeralManager) loadEphemeralPeers(ctx context.Context) {
	peers, err := e.store.GetAllEphemeralPeers(ctx, store.LockingStrengthShare)
	if err != nil {
		log.WithContext(ctx).Debugf("failed to load ephemeral peers: %s", err)
		return
	}

	t := e.newDeadLine()
	for _, p := range peers {
		e.addPeer(p.AccountID, p.ID, t)
	}

	log.WithContext(ctx).Debugf("loaded ephemeral peer(s): %d", len(peers))
}

func (e *EphemeralManager) cleanup(ctx context.Context) {
	log.Tracef("on ephemeral cleanup")
	deletePeers := make(map[string]*ephemeralPeer)

	e.peersLock.Lock()
	now := timeNow()
	for p := e.headPeer; p != nil; p = p.next {
		if now.Before(p.deadline) {
			break
		}

		deletePeers[p.id] = p
		e.headPeer = p.next
		if p.next == nil {
			e.tailPeer = nil
		}
	}

	if e.headPeer != nil {
		delay := e.headPeer.deadline.Sub(timeNow()) + e.cleanupWindow
		if delay < 0 {
			delay = 0
		}
		e.timer = time.AfterFunc(delay, func() {
			e.cleanup(ctx)
		})
	} else {
		e.timer = nil
	}

	e.peersLock.Unlock()

	// Cluster-wide liveness gate. This manager keeps per-process, in-memory
	// state, but the store is shared across every management replica. A
	// replica that does not own a peer's Sync stream still loaded that peer
	// at startup and scheduled its cleanup here — deleting it now would rip
	// a peer that is continuously connected on another replica out of the
	// shared store. Before deleting, re-read the authoritative Connected
	// flag (written by the stream-owning replica and guarded against stale
	// disconnects, see updatePeerStatusAndLocation) and skip any peer that
	// is connected anywhere in the cluster. The owning replica deletes it
	// once the peer actually disconnects.
	//
	// The read is one bulk query per account (not one per peer): cleanup is
	// a batched, low-frequency path and the delete it guards is far heavier,
	// but a peer storm that expires many peers at once should still not fan
	// out into N individual SELECTs.
	//
	// Peers whose state could not be verified (a transient store error) are
	// returned rather than deleted, and re-tracked below so a running
	// instance retries them — the expired peers were already popped off the
	// in-memory list above, and LoadInitialPeers only runs once at startup,
	// so without this they would be orphaned until the process restarts.
	requeue := e.dropStillConnected(ctx, deletePeers)
	if len(requeue) > 0 {
		e.rescheduleAfterError(ctx, requeue)
	}

	bufferAccountCall := make(map[string]struct{})

	for id, p := range deletePeers {
		log.WithContext(ctx).Debugf("delete ephemeral peer: %s", id)
		err := e.accountManager.DeletePeer(ctx, p.accountID, id, activity.SystemInitiator)
		if err != nil {
			log.WithContext(ctx).Errorf("failed to delete ephemeral peer: %s", err)
		} else {
			bufferAccountCall[p.accountID] = struct{}{}
		}
	}
	for accountID := range bufferAccountCall {
		e.accountManager.BufferUpdateAccountPeers(ctx, accountID)
	}
}

// dropStillConnected removes from the delete set every peer that must not
// be hard-deleted and returns the peers that need to be re-tracked in
// process. Three cases are dropped from the set: peers still connected
// somewhere in the cluster and peers already gone from the store (both
// drop-and-forget — the owning replica handles the connected one, the gone
// one needs nothing), and — on a store read error — every peer of the
// affected account (never delete on an unverified state). Only the last
// case is returned: those peers could not be verified, so a running
// instance must retry them rather than forget them until a restart.
// It groups the staged peers by account so the shared store is hit once per
// account instead of once per peer.
func (e *EphemeralManager) dropStillConnected(ctx context.Context, deletePeers map[string]*ephemeralPeer) []*ephemeralPeer {
	idsByAccount := make(map[string][]string)
	for id, p := range deletePeers {
		idsByAccount[p.accountID] = append(idsByAccount[p.accountID], id)
	}

	var requeue []*ephemeralPeer
	for accountID, ids := range idsByAccount {
		current, err := e.store.GetPeersByIDs(ctx, store.LockingStrengthShare, accountID, ids)
		if err != nil {
			log.WithContext(ctx).Warnf("defer ephemeral cleanup for account %s: cannot verify connection state: %s", accountID, err)
			for _, id := range ids {
				requeue = append(requeue, deletePeers[id])
				delete(deletePeers, id)
			}
			continue
		}

		for _, id := range ids {
			peer, ok := current[id]
			if !ok {
				// Already removed from the store (e.g. deleted via API) —
				// nothing left to delete.
				delete(deletePeers, id)
				continue
			}
			if peer.Status != nil && peer.Status.Connected {
				log.WithContext(ctx).Debugf("skip ephemeral cleanup for peer %s: still connected on the cluster", id)
				delete(deletePeers, id)
			}
		}
	}
	return requeue
}

// rescheduleAfterError re-tracks peers whose connection state could not be
// verified during cleanup (a transient store error) and re-arms the cleanup
// timer if it is idle, so a running instance retries them after another
// lifeTime rather than forgetting them until the next restart. A fresh
// deadline is used deliberately: the retry is a backstop for a transient
// store blip, not a tight spin.
func (e *EphemeralManager) rescheduleAfterError(ctx context.Context, peers []*ephemeralPeer) {
	e.peersLock.Lock()
	defer e.peersLock.Unlock()

	for _, p := range peers {
		e.addPeer(p.accountID, p.id, e.newDeadLine())
	}

	if e.timer == nil && e.headPeer != nil {
		delay := e.headPeer.deadline.Sub(timeNow()) + e.cleanupWindow
		if delay < 0 {
			delay = 0
		}
		e.timer = time.AfterFunc(delay, func() {
			e.cleanup(ctx)
		})
	}
}

func (e *EphemeralManager) addPeer(accountID string, peerID string, deadline time.Time) {
	ep := &ephemeralPeer{
		id:        peerID,
		accountID: accountID,
		deadline:  deadline,
	}

	if e.headPeer == nil {
		e.headPeer = ep
	}
	if e.tailPeer != nil {
		e.tailPeer.next = ep
	}
	e.tailPeer = ep
}

func (e *EphemeralManager) removePeer(id string) {
	if e.headPeer == nil {
		return
	}

	if e.headPeer.id == id {
		e.headPeer = e.headPeer.next
		if e.tailPeer.id == id {
			e.tailPeer = nil
		}
		return
	}

	for p := e.headPeer; p.next != nil; p = p.next {
		if p.next.id == id {
			// if we remove the last element from the chain then set the last-1 as tail
			if e.tailPeer.id == id {
				e.tailPeer = p
			}
			p.next = p.next.next
			return
		}
	}
}

func (e *EphemeralManager) isPeerOnList(id string) bool {
	for p := e.headPeer; p != nil; p = p.next {
		if p.id == id {
			return true
		}
	}
	return false
}

func (e *EphemeralManager) newDeadLine() time.Time {
	return timeNow().Add(e.lifeTime)
}
