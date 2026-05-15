package http

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rs/cors"

	"github.com/openzro/openzro/management/integrations/integrations"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/settings"

	"github.com/openzro/openzro/management/server/integrations/port_forwarding"
	"github.com/openzro/openzro/management/server/permissions"

	activityExporters "github.com/openzro/openzro/management/server/activity_exporters"
	"github.com/openzro/openzro/management/server/admission"
	"github.com/openzro/openzro/management/server/auth"
	"github.com/openzro/openzro/management/server/dex_proxy"
	"github.com/openzro/openzro/management/server/geolocation"
	nbgroups "github.com/openzro/openzro/management/server/groups"
	"github.com/openzro/openzro/management/server/http/handlers/accounts"
	activityExportersHandler "github.com/openzro/openzro/management/server/http/handlers/activity_exporters"
	admissionBypassHandler "github.com/openzro/openzro/management/server/http/handlers/admission_bypass"
	authProvidersHandler "github.com/openzro/openzro/management/server/http/handlers/auth_providers"
	"github.com/openzro/openzro/management/server/http/handlers/dns"
	"github.com/openzro/openzro/management/server/http/handlers/events"
	flowExportsHandler "github.com/openzro/openzro/management/server/http/handlers/flow_exports"
	"github.com/openzro/openzro/management/server/http/handlers/groups"
	mdmProvidersHandler "github.com/openzro/openzro/management/server/http/handlers/mdm_providers"
	"github.com/openzro/openzro/management/server/http/handlers/network_events"
	"github.com/openzro/openzro/management/server/http/handlers/networks"
	"github.com/openzro/openzro/management/server/http/handlers/peers"

	flowstore "github.com/openzro/openzro/flow/store"
	flowExports "github.com/openzro/openzro/management/server/flow_exports"
	"github.com/openzro/openzro/management/server/http/handlers/policies"
	"github.com/openzro/openzro/management/server/http/handlers/routes"
	"github.com/openzro/openzro/management/server/http/handlers/scim"
	"github.com/openzro/openzro/management/server/http/handlers/setup_keys"
	"github.com/openzro/openzro/management/server/http/handlers/users"
	"github.com/openzro/openzro/management/server/http/middleware"
	"github.com/openzro/openzro/management/server/integrations/integrated_validator"
	"github.com/openzro/openzro/management/server/mdm"
	nbnetworks "github.com/openzro/openzro/management/server/networks"
	"github.com/openzro/openzro/management/server/networks/resources"
	"github.com/openzro/openzro/management/server/networks/routers"
	nbpeers "github.com/openzro/openzro/management/server/peers"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/telemetry"
)

const apiPrefix = "/api"

// NewAPIHandler creates the Management service HTTP API handler registering all the available endpoints.
func NewAPIHandler(
	ctx context.Context,
	accountManager account.Manager,
	networksManager nbnetworks.Manager,
	resourceManager resources.Manager,
	routerManager routers.Manager,
	groupsManager nbgroups.Manager,
	LocationManager geolocation.Geolocation,
	authManager auth.Manager,
	appMetrics telemetry.AppMetrics,
	integratedValidator integrated_validator.IntegratedValidator,
	proxyController port_forwarding.Controller,
	permissionsManager permissions.Manager,
	peersManager nbpeers.Manager,
	settingsManager settings.Manager,
	flowEventsStore flowstore.Store,
	flowExportsStore *flowExports.Store,
	flowExportsManager *flowExports.Manager,
	mdmStore *mdm.Store,
	mdmManager *mdm.Manager,
	activityExportersStore *activityExporters.Store,
	activityExportersManager *activityExporters.Manager,
	admissionBypassStore *admission.Store,
	admissionBypassEmitter admissionBypassHandler.EventEmitter,
	dexClient *dex_proxy.Client,
	postureEvalStore posture.EvalStore,
) (http.Handler, error) {

	authMiddleware := middleware.NewAuthMiddleware(
		authManager,
		accountManager.GetAccountIDFromUserAuth,
		accountManager.SyncUserJWTGroups,
		accountManager.GetUserFromUserAuth,
	)

	corsMiddleware := cors.AllowAll()

	rootRouter := mux.NewRouter()
	metricsMiddleware := appMetrics.HTTPMiddleware()

	prefix := apiPrefix
	router := rootRouter.PathPrefix(prefix).Subrouter()

	router.Use(metricsMiddleware.Handler, corsMiddleware.Handler, authMiddleware.Handler)

	if _, err := integrations.RegisterHandlers(ctx, prefix, router, accountManager, integratedValidator, appMetrics.GetMeter(), permissionsManager, peersManager, proxyController, settingsManager); err != nil {
		return nil, fmt.Errorf("register integrations endpoints: %w", err)
	}

	accounts.AddEndpoints(accountManager, settingsManager, router)
	peers.AddEndpoints(accountManager, postureEvalStore, router)
	users.AddEndpoints(accountManager, router)
	setup_keys.AddEndpoints(accountManager, router)
	policies.AddEndpoints(accountManager, LocationManager, router)
	policies.AddPostureCheckEndpoints(accountManager, LocationManager, router)
	policies.AddLocationsEndpoints(accountManager, LocationManager, permissionsManager, router)
	groups.AddEndpoints(accountManager, router)
	routes.AddEndpoints(accountManager, router)
	dns.AddEndpoints(accountManager, router)
	events.AddEndpoints(accountManager, router)
	networks.AddEndpoints(networksManager, resourceManager, routerManager, groupsManager, accountManager, flowEventsStore, router)
	network_events.AddEndpoints(permissionsManager, flowEventsStore, router)
	flowExportsHandler.AddEndpoints(permissionsManager, flowExportsStore, flowExportsManager, router)
	mdmProvidersHandler.AddEndpoints(permissionsManager, mdmStore, mdmManager, router)
	activityExportersHandler.AddEndpoints(permissionsManager, activityExportersStore, activityExportersManager, router)
	admissionBypassHandler.AddEndpoints(permissionsManager, admissionBypassStore, accountManager, admissionBypassEmitter, router)
	authProvidersHandler.AddEndpoints(permissionsManager, dexClient, router)

	// SCIM 2.0 lives at /scim/v2 per RFC 7644 — separate from /api so
	// the path matches what every IdP expects out of the box. Same
	// auth middleware: SCIM clients authenticate with a PAT issued to
	// a service user (`Authorization: Bearer nbp_*`).
	scimRouter := rootRouter.PathPrefix("/scim/v2").Subrouter()
	scimRouter.Use(metricsMiddleware.Handler, corsMiddleware.Handler, authMiddleware.Handler)
	scim.AddEndpoints(accountManager, scimRouter)

	return rootRouter, nil
}
