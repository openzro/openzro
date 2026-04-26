// Package integrations is openzro's clean-room replacement for the
// upstream stub package github.com/netbirdio/management-integrations/integrations.
//
// The upstream "integrations" repository is GPL-3.0-licensed. Importing GPL
// code into our BSD-3-Clause base would force the entire openzro binary
// to inherit GPL obligations, which is incompatible with the project's
// license posture (see ADR-0001 §3.3 and §3.1). Although the code in
// that upstream repository is all stubs (every method is a no-op or a
// trivial pass-through; the "real" implementations live in upstream's
// commercial cloud), we cannot simply vendor it.
//
// This package therefore implements the same surface area from scratch.
// The interfaces this package satisfies live in our own BSD code:
//   - management/server/integrations/integrated_validator/interface.go
//   - management/server/integrations/extra_settings/manager.go
//   - management/server/integrations/port_forwarding/controller.go
//
// All implementations are no-ops with the same conservative semantics
// the upstream stub had: validators accept everything, port forwarding
// reports nothing, integration handlers register no extra HTTP routes.
// Operators who want richer behavior wire their own implementations
// rather than relying on a closed-source default.
package integrations

import (
	"context"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/metric"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	activitystore "github.com/openzro/openzro/management/server/activity/store"
	"github.com/openzro/openzro/management/server/integrations/integrated_validator"
	"github.com/openzro/openzro/management/server/integrations/port_forwarding"
	"github.com/openzro/openzro/management/server/peers"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/settings"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/telemetry"
)

// Metrics is the wrapper passed around so future, richer integrations
// can record their own measurements without churning every signature.
// In this stub it is a thin envelope over the existing AppMetrics.
type Metrics struct {
	telemetry.AppMetrics
}

// RegisterHandlers is the extension point a richer integrations package
// would use to mount additional HTTP routes under prefix on router. The
// no-op stub simply returns the router untouched.
//
// Argument list mirrors the upstream stub so call sites do not need to
// change. Any caller that passes nils for the dependencies it does not
// use is fine.
func RegisterHandlers(
	_ context.Context,
	_ string,
	router *mux.Router,
	_ account.Manager,
	_ integrated_validator.IntegratedValidator,
	_ metric.Meter,
	_ permissions.Manager,
	_ peers.Manager,
	_ port_forwarding.Controller,
	_ settings.Manager,
) (*mux.Router, error) {
	return router, nil
}

// InitIntegrationMetrics builds the Metrics envelope. It cannot fail in
// the stub but the (*Metrics, error) signature is preserved for forward
// compatibility with implementations that may.
func InitIntegrationMetrics(_ context.Context, m telemetry.AppMetrics) (*Metrics, error) {
	return &Metrics{AppMetrics: m}, nil
}

// InitEventStore returns the SQL-backed activity store. If key is empty
// a fresh encryption key is generated and returned alongside the store
// so the caller can persist it.
func InitEventStore(ctx context.Context, dataDir string, key string, _ *Metrics) (activity.Store, string, error) {
	if key == "" {
		log.Debugf("integrations: generating new activity store encryption key")
		generated, err := activitystore.GenerateKey()
		if err != nil {
			return nil, "", err
		}
		key = generated
	}
	s, err := activitystore.NewSqlStore(ctx, dataDir, key)
	if err != nil {
		return nil, "", err
	}
	return s, key, nil
}

// InitPermissionsManager returns the standard permissions manager from
// the BSD core. Exists so call sites that already speak through the
// integrations package do not need to import permissions directly.
func InitPermissionsManager(s store.Store) permissions.Manager {
	return permissions.NewManager(s)
}
