package server

import (
	"context"
	"sync"
	"testing"

	gproto "google.golang.org/protobuf/proto"

	"github.com/openzro/openzro/cluster"
	"github.com/openzro/openzro/management/proto"
)

// The manager hands a channel to the Sync stream and closes it to signal
// teardown, while updates are sent into it from elsewhere — the account
// broadcast, the token refresher, the cluster forwarder. Sending and
// closing are therefore concurrent by design, and only the channels mutex
// can order them.
//
// Both tests below drive the manager through its public surface only.
// Reaching into peerChannels (as some existing tests do) would add
// unsynchronized access of its own and make any race report ambiguous
// about whose fault it is.
//
// The assertion vehicle is the race detector, not a panic. The window
// between releasing the lock and touching the channel is a few
// instructions wide, so waiting for an actual send-on-closed panic would
// need far more iterations and still be luck; -race reports the
// unsynchronized close/send pair on the first overlap.

const raceTestPeerID = "peer-race"

// drainUntilClosed consumes a channel the way the Sync stream does, so
// senders keep finding room and the drop path stays off the hot loop.
// It returns when the manager closes the channel.
func drainUntilClosed(wg *sync.WaitGroup, ch chan *UpdateMessage) {
	defer wg.Done()
	for range ch { //nolint:revive // draining is the point
	}
}

// TestPeersUpdateManager_SendUpdateRacesClose covers the local send path
// against every way a channel gets closed.
func TestPeersUpdateManager_SendUpdateRacesClose(t *testing.T) {
	// cycle closes the peer's current channel and leaves a fresh one in
	// place, so the next iteration has something to race against.
	cases := []struct {
		name  string
		cycle func(p *PeersUpdateManager, ctx context.Context, reopen func())
	}{
		{
			name: "CloseChannel",
			cycle: func(p *PeersUpdateManager, ctx context.Context, reopen func()) {
				p.CloseChannel(ctx, raceTestPeerID)
				reopen()
			},
		},
		{
			name: "CloseChannels",
			cycle: func(p *PeersUpdateManager, ctx context.Context, reopen func()) {
				p.CloseChannels(ctx, []string{raceTestPeerID})
				reopen()
			},
		},
		{
			// CreateChannel closes whatever channel the peer already had
			// before installing the new one — the reconnect path, and the
			// least obvious of the three closers.
			name: "CreateChannel replacing an existing channel",
			cycle: func(_ *PeersUpdateManager, _ context.Context, reopen func()) {
				reopen()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const (
				senders    = 8
				iterations = 300
			)

			ctx := context.Background()
			p := NewPeersUpdateManager(nil)

			var drains sync.WaitGroup
			reopen := func() {
				ch := p.CreateChannel(ctx, raceTestPeerID)
				drains.Add(1)
				go drainUntilClosed(&drains, ch)
			}
			reopen()

			stop := make(chan struct{})
			var senderWG sync.WaitGroup
			for i := 0; i < senders; i++ {
				senderWG.Add(1)
				go func() {
					defer senderWG.Done()
					for {
						select {
						case <-stop:
							return
						default:
							p.SendUpdate(ctx, raceTestPeerID, &UpdateMessage{
								Update: &proto.SyncResponse{},
							})
						}
					}
				}()
			}

			for i := 0; i < iterations; i++ {
				tc.cycle(p, ctx, reopen)
			}

			close(stop)
			senderWG.Wait()
			p.CloseChannel(ctx, raceTestPeerID)
			drains.Wait()
		})
	}
}

// fakeCoordinator is an in-process cluster.Coordinator good enough to
// drive forwardClusterEvents. Publish hands the payload to every live
// subscriber without blocking, matching the at-most-once contract the
// real backends document.
type fakeCoordinator struct {
	mu   sync.Mutex
	subs map[string][]chan cluster.Event
}

func newFakeCoordinator() *fakeCoordinator {
	return &fakeCoordinator{subs: make(map[string][]chan cluster.Event)}
}

func (f *fakeCoordinator) Lock(_ context.Context, _ string) (func(), error) {
	return func() {}, nil
}

func (f *fakeCoordinator) Publish(_ context.Context, topic string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.subs[topic] {
		select {
		case ch <- cluster.Event{Topic: topic, Payload: payload}:
		default:
		}
	}
	return nil
}

func (f *fakeCoordinator) Subscribe(ctx context.Context, topic string) (<-chan cluster.Event, error) {
	ch := make(chan cluster.Event, 64)

	f.mu.Lock()
	f.subs[topic] = append(f.subs[topic], ch)
	f.mu.Unlock()

	go func() {
		<-ctx.Done()
		f.mu.Lock()
		defer f.mu.Unlock()
		remaining := f.subs[topic][:0]
		for _, existing := range f.subs[topic] {
			if existing != ch {
				remaining = append(remaining, existing)
			}
		}
		f.subs[topic] = remaining
		close(ch)
	}()

	return ch, nil
}

func (f *fakeCoordinator) Close() error { return nil }

// TestPeersUpdateManager_ClusterForwardRacesClose covers the HA path.
//
// forwardClusterEvents is a second sender into the same channel, and it
// already carries a guard that re-reads peerChannels before sending —
// but it drops the read lock before touching the channel, so the guard
// narrows the window instead of closing it. Publishing straight to the
// peer's topic is what another management instance does, so this drives
// that sender without touching manager internals.
func TestPeersUpdateManager_ClusterForwardRacesClose(t *testing.T) {
	const iterations = 300

	ctx := context.Background()
	coord := newFakeCoordinator()
	p := NewPeersUpdateManagerWithCluster(nil, coord)
	t.Cleanup(p.Stop)

	payload, err := gproto.Marshal(&proto.SyncResponse{})
	if err != nil {
		t.Fatalf("marshal sync response: %v", err)
	}
	topic := peerUpdateTopicPrefix + raceTestPeerID

	var drains sync.WaitGroup
	reopen := func() {
		ch := p.CreateChannel(ctx, raceTestPeerID)
		drains.Add(1)
		go drainUntilClosed(&drains, ch)
	}
	reopen()

	stop := make(chan struct{})
	var publishers sync.WaitGroup
	for i := 0; i < 4; i++ {
		publishers.Add(1)
		go func() {
			defer publishers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if err := coord.Publish(ctx, topic, payload); err != nil {
						return
					}
				}
			}
		}()
	}

	for i := 0; i < iterations; i++ {
		p.CloseChannel(ctx, raceTestPeerID)
		reopen()
	}

	close(stop)
	publishers.Wait()
	p.CloseChannel(ctx, raceTestPeerID)
	drains.Wait()
}
