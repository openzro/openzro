// Package config replaces the upstream
// github.com/netbirdio/management-integrations/integrations/config (GPL-3)
// with a clean-room BSD-3 stub. See ../integrations.go for the rationale.
package config

import (
	"github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server/types"
)

// ExtendOpenzroConfig is the seam the management server uses when sending
// the config blob to a peer; richer integrations may inject extra fields
// based on the peer or its account-level extra settings. The stub passes
// the input through unchanged.
func ExtendOpenzroConfig(_ string, config *proto.OpenzroConfig, _ *types.ExtraSettings) *proto.OpenzroConfig {
	return config
}
