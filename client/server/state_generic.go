//go:build !linux || android

package server

import (
	"github.com/openzro/openzro/client/internal/dns"
	"github.com/openzro/openzro/client/internal/routemanager/systemops"
	"github.com/openzro/openzro/client/internal/statemanager"
)

func registerStates(mgr *statemanager.Manager) {
	mgr.RegisterState(&dns.ShutdownState{})
	mgr.RegisterState(&systemops.ShutdownState{})
}
