package integrations

import (
	"context"

	"github.com/openzro/openzro/management/server/integrations/port_forwarding"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// portForwardingStub is the no-op port_forwarding.Controller. Holds the
// store reference so future, real implementations can swap in without
// changing call sites.
type portForwardingStub struct {
	store store.Store
}

// NewController returns a stub Controller. Real port-forwarding logic
// belongs in a richer integration that operators provide themselves.
func NewController(s store.Store) port_forwarding.Controller {
	return &portForwardingStub{store: s}
}

func (*portForwardingStub) SendUpdate(_ context.Context, _ string, _ string, _ []string) {
	// no-op
}

func (*portForwardingStub) GetProxyNetworkMaps(_ context.Context, _ string) (map[string]*types.NetworkMap, error) {
	return map[string]*types.NetworkMap{}, nil
}

func (*portForwardingStub) IsPeerInIngressPorts(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
