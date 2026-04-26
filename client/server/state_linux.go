//go:build !android

package server

import (
	"github.com/openzro/openzro/client/firewall/iptables"
	"github.com/openzro/openzro/client/firewall/nftables"
	"github.com/openzro/openzro/client/internal/dns"
	"github.com/openzro/openzro/client/internal/routemanager/systemops"
	"github.com/openzro/openzro/client/internal/statemanager"
)

func registerStates(mgr *statemanager.Manager) {
	mgr.RegisterState(&dns.ShutdownState{})
	mgr.RegisterState(&systemops.ShutdownState{})
	mgr.RegisterState(&nftables.ShutdownState{})
	mgr.RegisterState(&iptables.ShutdownState{})
}
