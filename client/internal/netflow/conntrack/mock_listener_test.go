//go:build linux && !android

package conntrack

import (
	"sync/atomic"

	nfct "github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"
)

// mockListener satisfies the listener interface for unit tests so the
// receiver loop and lifecycle code can be exercised without touching
// the kernel. errChan is operator-controlled: tests push errors to
// drive the receiver into its error branch (reconnect or exit
// depending on what's implemented).
type mockListener struct {
	errChan  chan error
	closed   atomic.Bool
	closedCh chan struct{}
}

func newMockListener() *mockListener {
	return &mockListener{
		errChan:  make(chan error, 1),
		closedCh: make(chan struct{}),
	}
}

func (m *mockListener) Listen(_ chan<- nfct.Event, _ uint8, _ []netfilter.NetlinkGroup) (chan error, error) {
	return m.errChan, nil
}

func (m *mockListener) Close() error {
	if m.closed.CompareAndSwap(false, true) {
		close(m.closedCh)
	}
	return nil
}
