// Package config replaces the upstream
// github.com/netbirdio/management-integrations/integrations/config (GPL-3)
// with a clean-room BSD-3 stub. See ../integrations.go for the rationale.
package config

import (
	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/types"
)

// ExtendOpenzroConfig is the seam the management server uses when sending
// the config blob to a peer. Richer integrations may inject extra fields
// based on the peer or its account-level extra settings; we use it here
// to pass the operator's traffic-event group filter through to the peer
// so it can self-gate capture without round-tripping every event for
// management to drop.
//
// The peer compares FlowConfig.groups against its own group memberships
// and only enables the netflow Manager when the intersection is non-empty
// (or the list is empty, the "all peers report" default).
func ExtendOpenzroConfig(_ string, config *proto.OpenzroConfig, extra *types.ExtraSettings) *proto.OpenzroConfig {
	if extra == nil || config == nil {
		return config
	}
	if config.Flow != nil && len(extra.FlowEventsGroups) > 0 {
		config.Flow.Groups = append([]string(nil), extra.FlowEventsGroups...)
	}
	return config
}
