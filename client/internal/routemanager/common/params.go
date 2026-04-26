package common

import (
	"time"

	"github.com/openzro/openzro/client/firewall/manager"
	"github.com/openzro/openzro/client/internal/dns"
	"github.com/openzro/openzro/client/internal/peer"
	"github.com/openzro/openzro/client/internal/peerstore"
	"github.com/openzro/openzro/client/internal/routemanager/fakeip"
	"github.com/openzro/openzro/client/internal/routemanager/iface"
	"github.com/openzro/openzro/client/internal/routemanager/refcounter"
	"github.com/openzro/openzro/route"
)

type HandlerParams struct {
	Route                *route.Route
	RouteRefCounter      *refcounter.RouteRefCounter
	AllowedIPsRefCounter *refcounter.AllowedIPsRefCounter
	DnsRouterInterval    time.Duration
	StatusRecorder       *peer.Status
	WgInterface          iface.WGIface
	DnsServer            dns.Server
	PeerStore            *peerstore.Store
	UseNewDNSRoute       bool
	Firewall             manager.Manager
	FakeIPManager        *fakeip.Manager
}
