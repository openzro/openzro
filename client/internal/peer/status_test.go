package peer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAddPeer(t *testing.T) {
	key := "abc"
	ip := "100.108.254.1"
	status := NewRecorder("https://mgm")
	err := status.AddPeer(key, "abc.openzro", ip)
	assert.NoError(t, err, "shouldn't return error")

	_, exists := status.peers[key]
	assert.True(t, exists, "value was found")

	err = status.AddPeer(key, "abc.openzro", ip)

	assert.Error(t, err, "should return error on duplicate")
}

func TestGetPeer(t *testing.T) {
	key := "abc"
	ip := "100.108.254.1"
	status := NewRecorder("https://mgm")
	err := status.AddPeer(key, "abc.openzro", ip)
	assert.NoError(t, err, "shouldn't return error")

	peerStatus, err := status.GetPeer(key)
	assert.NoError(t, err, "shouldn't return error on getting peer")

	assert.Equal(t, key, peerStatus.PubKey, "retrieved public key should match")

	_, err = status.GetPeer("non_existing_key")
	assert.Error(t, err, "should return error when peer doesn't exist")
}

func TestUpdatePeerState(t *testing.T) {
	key := "abc"
	ip := "10.10.10.10"
	fqdn := "peer-a.openzro.local"
	status := NewRecorder("https://mgm")
	_ = status.AddPeer(key, fqdn, ip)

	peerState := State{
		PubKey:           key,
		ConnStatusUpdate: time.Now(),
		ConnStatus:       StatusConnecting,
	}

	err := status.UpdatePeerState(peerState)
	assert.NoError(t, err, "shouldn't return error")

	state, exists := status.peers[key]
	assert.True(t, exists, "state should be found")
	assert.Equal(t, ip, state.IP, "ip should be equal")
}

func TestUpdateDNSState(t *testing.T) {
	const (
		groupA = "group-a"
		groupB = "group-b"
	)
	failed := errors.New("all nameservers timed out")

	// recorderWithNSGroups returns a recorder holding two healthy nameserver
	// groups serving one zone — the shape a pooled zone reports through.
	recorderWithNSGroups := func() *Status {
		status := NewRecorder("https://mgm")
		status.UpdateDNSStates([]NSGroupState{
			{ID: groupA, Servers: []string{"10.0.0.1:53"}, Domains: []string{"eu.example.com"}, Enabled: true},
			{ID: groupB, Servers: []string{"10.0.0.2:53"}, Domains: []string{"eu.example.com"}, Enabled: true},
		})
		return status
	}

	stateOf := func(t *testing.T, status *Status, id string) NSGroupState {
		t.Helper()

		for _, state := range status.GetDNSStates() {
			if state.ID == id {
				return state
			}
		}
		t.Fatalf("no state for nameserver group %s", id)
		return NSGroupState{}
	}

	t.Run("a failing group is disabled with its error", func(t *testing.T) {
		status := recorderWithNSGroups()

		status.UpdateDNSState(groupA, failed, false)

		failing := stateOf(t, status, groupA)
		assert.False(t, failing.Enabled, "the failing group should be disabled")
		assert.Equal(t, failed, failing.Error, "the failing group should carry its error")
		assert.Equal(t, []string{"10.0.0.1:53"}, failing.Servers, "the rest of the state should be untouched")

		healthy := stateOf(t, status, groupB)
		assert.True(t, healthy.Enabled, "the other group should stay enabled")
		assert.NoError(t, healthy.Error, "the other group should stay error-free")
	})

	t.Run("a recovered group is enabled and its error cleared", func(t *testing.T) {
		status := recorderWithNSGroups()

		status.UpdateDNSState(groupA, failed, false)
		status.UpdateDNSState(groupA, nil, true)

		recovered := stateOf(t, status, groupA)
		assert.True(t, recovered.Enabled, "the recovered group should be enabled")
		assert.NoError(t, recovered.Error, "the recovered group's error should be cleared")
	})

	t.Run("an unknown ID changes nothing", func(t *testing.T) {
		status := recorderWithNSGroups()
		before := status.GetDNSStates()

		status.UpdateDNSState("no-such-group", failed, false)

		assert.Equal(t, before, status.GetDNSStates(), "an unknown group should leave every state alone")
	})

	t.Run("groups failing at the same time do not overwrite each other", func(t *testing.T) {
		// This is what the method exists for: pooled nameserver groups report their
		// health without holding the DNS server's lock, so two of them can flip at
		// once. Reading every state, editing one and writing them all back would
		// lose one of the two updates. Repeated because that loss depends on the
		// schedule: a single round hits the losing interleaving too rarely to
		// notice a regression, a few hundred hit it reliably.
		for range 500 {
			status := recorderWithNSGroups()

			start := make(chan struct{})
			var wg sync.WaitGroup
			for _, id := range []string{groupA, groupB} {
				wg.Add(1)
				go func() {
					defer wg.Done()

					<-start
					status.UpdateDNSState(id, failed, false)
				}()
			}
			close(start)
			wg.Wait()

			for _, id := range []string{groupA, groupB} {
				state := stateOf(t, status, id)
				assert.False(t, state.Enabled, "%s should be disabled", id)
				assert.Equal(t, failed, state.Error, "%s should carry its error", id)
			}
		}
	})
}

func TestStatus_UpdatePeerFQDN(t *testing.T) {
	key := "abc"
	fqdn := "peer-a.openzro.local"
	status := NewRecorder("https://mgm")
	peerState := State{
		PubKey: key,
		Mux:    new(sync.RWMutex),
	}

	status.peers[key] = peerState

	err := status.UpdatePeerFQDN(key, fqdn)
	assert.NoError(t, err, "shouldn't return error")

	state, exists := status.peers[key]
	assert.True(t, exists, "state should be found")
	assert.Equal(t, fqdn, state.FQDN, "fqdn should be equal")
}

func TestGetPeerStateChangeNotifierLogic(t *testing.T) {
	key := "abc"
	ip := "10.10.10.10"
	status := NewRecorder("https://mgm")
	_ = status.AddPeer(key, "abc.openzro", ip)

	sub := status.SubscribeToPeerStateChanges(context.Background(), key)
	assert.NotNil(t, sub, "channel shouldn't be nil")

	peerState := State{
		PubKey:           key,
		ConnStatus:       StatusConnecting,
		Relayed:          false,
		ConnStatusUpdate: time.Now(),
	}

	err := status.UpdatePeerRelayedStateToDisconnected(peerState)
	assert.NoError(t, err, "shouldn't return error")

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	select {
	case <-sub.eventsChan:
	case <-timeoutCtx.Done():
		t.Errorf("timed out waiting for event")
	}
}

func TestRemovePeer(t *testing.T) {
	key := "abc"
	status := NewRecorder("https://mgm")
	peerState := State{
		PubKey: key,
		Mux:    new(sync.RWMutex),
	}

	status.peers[key] = peerState

	err := status.RemovePeer(key)
	assert.NoError(t, err, "shouldn't return error")

	_, exists := status.peers[key]
	assert.False(t, exists, "state value shouldn't be found")

	err = status.RemovePeer("not existing")
	assert.Error(t, err, "should return error when peer doesn't exist")
}

func TestUpdateLocalPeerState(t *testing.T) {
	localPeerState := LocalPeerState{
		IP:              "10.10.10.10",
		PubKey:          "abc",
		KernelInterface: false,
	}
	status := NewRecorder("https://mgm")

	status.UpdateLocalPeerState(localPeerState)

	assert.Equal(t, localPeerState, status.localPeer, "local peer status should be equal")
}

func TestCleanLocalPeerState(t *testing.T) {
	emptyLocalPeerState := LocalPeerState{}
	localPeerState := LocalPeerState{
		IP:              "10.10.10.10",
		PubKey:          "abc",
		KernelInterface: false,
	}
	status := NewRecorder("https://mgm")

	status.localPeer = localPeerState

	status.CleanLocalPeerState()

	assert.Equal(t, emptyLocalPeerState, status.localPeer, "local peer status should be empty")
}

func TestUpdateSignalState(t *testing.T) {
	url := "https://signal"
	var tests = []struct {
		name      string
		connected bool
		want      bool
		err       error
	}{
		{"should mark as connected", true, true, nil},
		{"should mark as disconnected", false, false, errors.New("test")},
	}

	status := NewRecorder("https://mgm")
	status.UpdateSignalAddress(url)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.connected {
				status.MarkSignalConnected()
			} else {
				status.MarkSignalDisconnected(test.err)
			}
			assert.Equal(t, test.want, status.signalState, "signal status should be equal")
			assert.Equal(t, test.err, status.signalError)
		})
	}
}

func TestUpdateManagementState(t *testing.T) {
	url := "https://management"
	var tests = []struct {
		name      string
		connected bool
		want      bool
		err       error
	}{
		{"should mark as connected", true, true, nil},
		{"should mark as disconnected", false, false, errors.New("test")},
	}

	status := NewRecorder(url)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.connected {
				status.MarkManagementConnected()
			} else {
				status.MarkManagementDisconnected(test.err)
			}
			assert.Equal(t, test.want, status.managementState, "signalState status should be equal")
			assert.Equal(t, test.err, status.managementError)
		})
	}
}

func TestGetFullStatus(t *testing.T) {
	key1 := "abc"
	key2 := "def"
	signalAddr := "https://signal"
	managementState := ManagementState{
		URL:       "https://mgm",
		Connected: true,
	}
	signalState := SignalState{
		URL:       signalAddr,
		Connected: true,
	}
	peerState1 := State{
		PubKey: key1,
	}

	peerState2 := State{
		PubKey: key2,
	}

	status := NewRecorder("https://mgm")
	status.UpdateSignalAddress(signalAddr)

	status.managementState = managementState.Connected
	status.signalState = signalState.Connected
	status.peers[key1] = peerState1
	status.peers[key2] = peerState2

	fullStatus := status.GetFullStatus()

	assert.Equal(t, managementState, fullStatus.ManagementState, "management status should be equal")
	assert.Equal(t, signalState, fullStatus.SignalState, "signal status should be equal")
	assert.ElementsMatch(t, []State{peerState1, peerState2}, fullStatus.Peers, "peers states should match")
}
