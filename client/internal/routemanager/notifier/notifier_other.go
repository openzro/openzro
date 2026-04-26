//go:build !android && !ios

package notifier

import (
	"net/netip"

	"github.com/openzro/openzro/client/internal/listener"
	"github.com/openzro/openzro/route"
)

type Notifier struct{}

func NewNotifier() *Notifier {
	return &Notifier{}
}

func (n *Notifier) SetListener(listener listener.NetworkChangeListener) {
	// Not used on non-mobile platforms
}

func (n *Notifier) SetInitialClientRoutes([]*route.Route, []*route.Route) {
	// Not used on non-mobile platforms
}

func (n *Notifier) OnNewRoutes(idMap route.HAMap) {
	// Not used on non-mobile platforms
}

func (n *Notifier) OnNewPrefixes(prefixes []netip.Prefix) {
	// Not used on non-mobile platforms
}

func (n *Notifier) GetInitialRouteRanges() []string {
	return []string{}
}
