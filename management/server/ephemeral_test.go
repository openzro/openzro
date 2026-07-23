package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	nbAccount "github.com/openzro/openzro/management/server/account"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

type MockStore struct {
	store.Store
	account *types.Account
}

func (s *MockStore) GetAllEphemeralPeers(_ context.Context, _ store.LockingStrength) ([]*nbpeer.Peer, error) {
	var peers []*nbpeer.Peer
	for _, v := range s.account.Peers {
		if v.Ephemeral {
			peers = append(peers, v)
		}
	}
	return peers, nil
}

func (s *MockStore) GetPeersByIDs(_ context.Context, _ store.LockingStrength, _ string, peerIDs []string) (map[string]*nbpeer.Peer, error) {
	res := make(map[string]*nbpeer.Peer)
	for _, id := range peerIDs {
		if p, ok := s.account.Peers[id]; ok {
			res[id] = p
		}
	}
	return res, nil
}

type MockAccountManager struct {
	mu sync.Mutex
	nbAccount.Manager
	store             *MockStore
	deletePeerCalls   int
	bufferUpdateCalls map[string]int
	wg                *sync.WaitGroup
}

func (a *MockAccountManager) DeletePeer(_ context.Context, accountID, peerID, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deletePeerCalls++
	// Apply the side effect (peer removal from the store) BEFORE
	// signaling the WaitGroup. Otherwise the test's wg.Wait() can
	// return as soon as the 10th Done() lands, while the 10th
	// goroutine is still parked one instruction short of the delete()
	// call. The test then reads mockStore.account.Peers and asserts
	// len == 0 — but peer-N is still in the map, so the assertion
	// fails intermittently (always on the last peer because the race
	// window is the gap between Done and delete on the LAST goroutine).
	delete(a.store.account.Peers, peerID)
	if a.wg != nil {
		a.wg.Done()
	}
	return nil
}

func (a *MockAccountManager) GetDeletePeerCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deletePeerCalls
}

func (a *MockAccountManager) BufferUpdateAccountPeers(ctx context.Context, accountID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bufferUpdateCalls == nil {
		a.bufferUpdateCalls = make(map[string]int)
	}
	a.bufferUpdateCalls[accountID]++
}

func (a *MockAccountManager) GetBufferUpdateCalls(accountID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bufferUpdateCalls == nil {
		return 0
	}
	return a.bufferUpdateCalls[accountID]
}

func (a *MockAccountManager) GetStore() store.Store {
	return a.store
}

func TestNewManager(t *testing.T) {
	t.Cleanup(func() {
		timeNow = time.Now
	})
	startTime := time.Now()
	timeNow = func() time.Time {
		return startTime
	}

	store := &MockStore{}
	am := MockAccountManager{
		store: store,
	}

	numberOfPeers := 5
	numberOfEphemeralPeers := 3
	seedPeers(store, numberOfPeers, numberOfEphemeralPeers)

	mgr := NewEphemeralManager(store, &am)
	mgr.loadEphemeralPeers(context.Background())
	startTime = startTime.Add(ephemeralLifeTime + 1)
	mgr.cleanup(context.Background())

	if len(store.account.Peers) != numberOfPeers {
		t.Errorf("failed to cleanup ephemeral peers, expected: %d, result: %d", numberOfPeers, len(store.account.Peers))
	}
}

func TestNewManagerPeerConnected(t *testing.T) {
	t.Cleanup(func() {
		timeNow = time.Now
	})
	startTime := time.Now()
	timeNow = func() time.Time {
		return startTime
	}

	store := &MockStore{}
	am := MockAccountManager{
		store: store,
	}

	numberOfPeers := 5
	numberOfEphemeralPeers := 3
	seedPeers(store, numberOfPeers, numberOfEphemeralPeers)

	mgr := NewEphemeralManager(store, &am)
	mgr.loadEphemeralPeers(context.Background())
	mgr.OnPeerConnected(context.Background(), store.account.Peers["ephemeral_peer_0"])

	startTime = startTime.Add(ephemeralLifeTime + 1)
	mgr.cleanup(context.Background())

	expected := numberOfPeers + 1
	if len(store.account.Peers) != expected {
		t.Errorf("failed to cleanup ephemeral peers, expected: %d, result: %d", expected, len(store.account.Peers))
	}
}

func TestNewManagerPeerDisconnected(t *testing.T) {
	t.Cleanup(func() {
		timeNow = time.Now
	})
	startTime := time.Now()
	timeNow = func() time.Time {
		return startTime
	}

	store := &MockStore{}
	am := MockAccountManager{
		store: store,
	}

	numberOfPeers := 5
	numberOfEphemeralPeers := 3
	seedPeers(store, numberOfPeers, numberOfEphemeralPeers)

	mgr := NewEphemeralManager(store, &am)
	mgr.loadEphemeralPeers(context.Background())
	for _, v := range store.account.Peers {
		mgr.OnPeerConnected(context.Background(), v)

	}
	mgr.OnPeerDisconnected(context.Background(), store.account.Peers["ephemeral_peer_0"])

	startTime = startTime.Add(ephemeralLifeTime + 1)
	mgr.cleanup(context.Background())

	expected := numberOfPeers + numberOfEphemeralPeers - 1
	if len(store.account.Peers) != expected {
		t.Errorf("failed to cleanup ephemeral peers, expected: %d, result: %d", expected, len(store.account.Peers))
	}
}

// TestCleanupSkipsClusterConnectedPeer is the regression test for the
// multi-replica ephemeral-peer bug: a replica that does NOT own a peer's
// Sync stream still loads that peer at startup and schedules its cleanup.
// Before the fix, that non-owning replica hard-deleted the peer from the
// shared store ~lifeTime after its own start, even though the peer was
// continuously connected on another replica. cleanup must now re-check the
// cluster-shared Status.Connected flag (written by the stream-owning
// replica) and skip the delete while the peer is connected anywhere.
func TestCleanupSkipsClusterConnectedPeer(t *testing.T) {
	t.Cleanup(func() {
		timeNow = time.Now
	})
	startTime := time.Now()
	timeNow = func() time.Time {
		return startTime
	}

	store := &MockStore{}
	am := MockAccountManager{
		store: store,
	}

	store.account = newAccountWithId(context.Background(), "account", "", "", false)
	// connected is owned by another replica's Sync stream — its shared
	// status says Connected=true, so this replica must not delete it.
	connected := &nbpeer.Peer{
		ID: "ephemeral_connected", AccountID: store.account.Id, Ephemeral: true,
		Status: &nbpeer.PeerStatus{Connected: true, OwnerStreamID: "stream-on-replica-a"},
	}
	// disconnected is genuinely gone — it must still be cleaned up.
	disconnected := &nbpeer.Peer{
		ID: "ephemeral_disconnected", AccountID: store.account.Id, Ephemeral: true,
		Status: &nbpeer.PeerStatus{Connected: false},
	}
	store.account.Peers[connected.ID] = connected
	store.account.Peers[disconnected.ID] = disconnected

	mgr := NewEphemeralManager(store, &am)
	mgr.loadEphemeralPeers(context.Background())
	startTime = startTime.Add(ephemeralLifeTime + 1)
	mgr.cleanup(context.Background())

	if _, ok := store.account.Peers[connected.ID]; !ok {
		t.Errorf("cluster-connected ephemeral peer must not be deleted by a non-owning replica")
	}
	if _, ok := store.account.Peers[disconnected.ID]; ok {
		t.Errorf("disconnected ephemeral peer should have been cleaned up")
	}
	if calls := am.GetDeletePeerCalls(); calls != 1 {
		t.Errorf("expected exactly 1 DeletePeer call, got %d", calls)
	}
}

func TestCleanupSchedulingBehaviorIsBatched(t *testing.T) {
	// testCleanupWindow needs to be wide enough that the loop below
	// stages every OnPeerDisconnected BEFORE the first cleanup timer
	// fires. The original 100ms window left ~1s of margin, which a
	// contended Linux CI runner could still eat (scheduler starves
	// the test goroutine, peer-N lands AFTER cleanup #1 finishes,
	// peer-N triggers a new timer → cleanup #2 → BufferUpdate==2).
	// 1s window × 3s lifetime gives ~3s of headroom after the loop
	// completes — more than enough for any GH-Actions Linux runner
	// we measure. Test runtime grows ~3s; the alternative is the
	// recurring flake observed on the v0.53.1-alpha.83 tag push.
	const (
		ephemeralPeers    = 10
		testLifeTime      = 3 * time.Second
		testCleanupWindow = 1 * time.Second
	)
	mockStore := &MockStore{}
	mockAM := &MockAccountManager{
		store: mockStore,
	}
	mockAM.wg = &sync.WaitGroup{}
	mockAM.wg.Add(ephemeralPeers)
	mgr := NewEphemeralManager(mockStore, mockAM)
	mgr.lifeTime = testLifeTime
	mgr.cleanupWindow = testCleanupWindow

	account := newAccountWithId(context.Background(), "account", "", "", false)
	mockStore.account = account
	for i := range ephemeralPeers {
		p := &nbpeer.Peer{ID: fmt.Sprintf("peer-%d", i), AccountID: account.Id, Ephemeral: true}
		mockStore.account.Peers[p.ID] = p
		time.Sleep(testCleanupWindow / ephemeralPeers)
		mgr.OnPeerDisconnected(context.Background(), p)
	}
	mockAM.wg.Wait()
	assert.Len(t, mockStore.account.Peers, 0, "all ephemeral peers should be cleaned up after the lifetime")
	assert.Equal(t, 1, mockAM.GetBufferUpdateCalls(account.Id), "buffer update should be called once")
	assert.Equal(t, ephemeralPeers, mockAM.GetDeletePeerCalls(), "should have deleted all peers")
}

func seedPeers(store *MockStore, numberOfPeers int, numberOfEphemeralPeers int) {
	store.account = newAccountWithId(context.Background(), "my account", "", "", false)

	for i := 0; i < numberOfPeers; i++ {
		peerId := fmt.Sprintf("peer_%d", i)
		p := &nbpeer.Peer{
			ID:        peerId,
			Ephemeral: false,
		}
		store.account.Peers[p.ID] = p
	}

	for i := 0; i < numberOfEphemeralPeers; i++ {
		peerId := fmt.Sprintf("ephemeral_peer_%d", i)
		p := &nbpeer.Peer{
			ID:        peerId,
			Ephemeral: true,
		}
		store.account.Peers[p.ID] = p
	}
}
