package cmd

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	grpcMiddleware "github.com/grpc-ecosystem/go-grpc-middleware/v2"
	"github.com/miekg/dns"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/realip"

	clusterfactory "github.com/openzro/openzro/cluster/factory"
	"github.com/openzro/openzro/management/integrations/integrations"

	"github.com/openzro/openzro/management/server/peers"
	"github.com/openzro/openzro/management/server/types"

	"github.com/openzro/openzro/encryption"
	flowProto "github.com/openzro/openzro/flow/proto"
	flowSinks "github.com/openzro/openzro/flow/sinks"
	flowstore "github.com/openzro/openzro/flow/store"
	flowArchive "github.com/openzro/openzro/flow/store/archive"
	flowFactory "github.com/openzro/openzro/flow/store/factory"
	flowFederated "github.com/openzro/openzro/flow/store/federated"
	"github.com/openzro/openzro/formatter/hook"
	mgmtProto "github.com/openzro/openzro/management/proto"
	"github.com/openzro/openzro/management/server"
	"github.com/openzro/openzro/management/server/activity"
	activityExporters "github.com/openzro/openzro/management/server/activity_exporters"
	"github.com/openzro/openzro/management/server/admission"
	"github.com/openzro/openzro/management/server/auth"
	nbContext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/dex_proxy"
	flowExports "github.com/openzro/openzro/management/server/flow_exports"
	flowPolicyResolverPkg "github.com/openzro/openzro/management/server/flow_policy_resolver"
	"github.com/openzro/openzro/management/server/geolocation"
	"github.com/openzro/openzro/management/server/groups"
	nbhttp "github.com/openzro/openzro/management/server/http"
	"github.com/openzro/openzro/management/server/idp"
	"github.com/openzro/openzro/management/server/mdm"
	"github.com/openzro/openzro/management/server/metrics"
	"github.com/openzro/openzro/management/server/networks"
	"github.com/openzro/openzro/management/server/networks/resources"
	"github.com/openzro/openzro/management/server/networks/routers"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/settings"
	mgmtStore "github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/telemetry"
	"github.com/openzro/openzro/management/server/users"
	"github.com/openzro/openzro/util"
	"github.com/openzro/openzro/version"
)

// ManagementLegacyPort is the port that was used before by the Management gRPC server.
// It is used for backward compatibility now.
const ManagementLegacyPort = 33073

var (
	mgmtPort                int
	mgmtMetricsPort         int
	mgmtLetsencryptDomain   string
	mgmtSingleAccModeDomain string
	certFile                string
	certKey                 string
	config                  *types.Config

	kaep = keepalive.EnforcementPolicy{
		MinTime:             15 * time.Second,
		PermitWithoutStream: true,
	}

	kasp = keepalive.ServerParameters{
		MaxConnectionIdle:     15 * time.Second,
		MaxConnectionAgeGrace: 5 * time.Second,
		Time:                  5 * time.Second,
		Timeout:               2 * time.Second,
	}

	mgmtCmd = &cobra.Command{
		Use:   "management",
		Short: "start Openzro Management Server",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			flag.Parse()

			//nolint
			ctx := context.WithValue(cmd.Context(), hook.ExecutionContextKey, hook.SystemSource)

			err := util.InitLog(logLevel, logFile)
			if err != nil {
				return fmt.Errorf("failed initializing log %v", err)
			}

			// detect whether user specified a port
			userPort := cmd.Flag("port").Changed

			config, err = loadMgmtConfig(ctx, types.MgmtConfigPath)
			if err != nil {
				return fmt.Errorf("failed reading provided config file: %s: %v", types.MgmtConfigPath, err)
			}

			if cmd.Flag(idpSignKeyRefreshEnabledFlagName).Changed {
				config.HttpConfig.IdpSignKeyRefreshEnabled = idpSignKeyRefreshEnabled
			}

			tlsEnabled := false
			if mgmtLetsencryptDomain != "" || (config.HttpConfig.CertFile != "" && config.HttpConfig.CertKey != "") {
				tlsEnabled = true
			}

			if !userPort {
				// different defaults for port when tls enabled/disabled
				if tlsEnabled {
					mgmtPort = 443
				} else {
					mgmtPort = 80
				}
			}

			_, valid := dns.IsDomainName(dnsDomain)
			if !valid || len(dnsDomain) > 192 {
				return fmt.Errorf("failed parsing the provided dns-domain. Valid status: %t, Length: %d", valid, len(dnsDomain))
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			flag.Parse()

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			//nolint
			ctx = context.WithValue(ctx, hook.ExecutionContextKey, hook.SystemSource)

			err := handleRebrand(cmd)
			if err != nil {
				return fmt.Errorf("failed to migrate files %v", err)
			}

			if _, err = os.Stat(config.Datadir); os.IsNotExist(err) {
				err = os.MkdirAll(config.Datadir, 0755)
				if err != nil {
					return fmt.Errorf("failed creating datadir: %s: %v", config.Datadir, err)
				}
			}
			appMetrics, err := telemetry.NewDefaultAppMetrics(cmd.Context())
			if err != nil {
				return err
			}
			err = appMetrics.Expose(ctx, mgmtMetricsPort, "/metrics")
			if err != nil {
				return err
			}

			integrationMetrics, err := integrations.InitIntegrationMetrics(ctx, appMetrics)
			if err != nil {
				return err
			}

			store, err := mgmtStore.NewStore(ctx, config.StoreConfig.Engine, config.Datadir, appMetrics, false)
			if err != nil {
				return fmt.Errorf("failed creating Store: %s: %v", config.Datadir, err)
			}

			// Build the cluster coordinator from environment. nil means
			// single-instance mode (no broker configured); HA mode is
			// triggered by setting OPENZRO_REDIS_URL, OPENZRO_NATS_URL,
			// or OPENZRO_BROKER=embedded. See cluster/factory.
			clusterCoord, err := clusterfactory.NewFromEnv(ctx)
			if err != nil {
				return fmt.Errorf("failed building cluster coordinator: %v", err)
			}

			peersUpdateManager := server.NewPeersUpdateManagerWithCluster(appMetrics, clusterCoord)

			var idpManager idp.Manager
			if config.IdpManagerConfig != nil {
				idpManager, err = idp.NewManager(ctx, *config.IdpManagerConfig, appMetrics)
				if err != nil {
					return fmt.Errorf("failed retrieving a new idp manager with err: %v", err)
				}
			}

			if disableSingleAccMode {
				mgmtSingleAccModeDomain = ""
			}
			eventStore, key, err := integrations.InitEventStore(ctx, config.Datadir, config.DataStoreEncryptionKey, integrationMetrics)
			if err != nil {
				return fmt.Errorf("failed to initialize database: %s", err)
			}

			if config.DataStoreEncryptionKey != key {
				log.WithContext(ctx).Infof("update config with activity store key")
				config.DataStoreEncryptionKey = key
				err := updateMgmtConfig(ctx, types.MgmtConfigPath, config)
				if err != nil {
					return fmt.Errorf("failed to write out store encryption key: %s", err)
				}
			}

			geoSrc := geolocation.DownloadSource{LicenseKey: maxmindLicenseKey}
			geo, err := geolocation.NewGeolocation(ctx, config.Datadir, !disableGeoliteUpdate, geoSrc)
			if err != nil {
				// Geolocation is best-effort: when auto-update is
				// disabled and no local mmdb is staged, we land here
				// with a "not configured" error. INFO instead of WARN
				// reflects the configuration choice — operator
				// disabled it deliberately or hasn't staged the DB
				// yet. The dashboard's geo posture-check modal
				// surfaces this state with a setup-instructions
				// banner.
				log.WithContext(ctx).Infof("geolocation service not initialized (running without geolocation support): %v", err)
			} else {
				log.WithContext(ctx).Infof("geolocation service has been initialized from %s (source: %s)", config.Datadir, geoSrc)
			}

			integratedPeerValidator, err := integrations.NewIntegratedValidator(ctx, eventStore)
			if err != nil {
				return fmt.Errorf("failed to initialize integrated peer validator: %v", err)
			}

			permissionsManager := integrations.InitPermissionsManager(store)
			userManager := users.NewManager(store)
			extraSettingsManager := integrations.NewManager(eventStore)
			settingsManager := settings.NewManager(store, userManager, extraSettingsManager, permissionsManager)
			peersManager := peers.NewManager(store, permissionsManager)
			proxyController := integrations.NewController(store)
			accountManager, err := server.BuildManager(ctx, store, peersUpdateManager, idpManager, mgmtSingleAccModeDomain,
				dnsDomain, eventStore, geo, userDeleteFromIDPEnabled, integratedPeerValidator, appMetrics, proxyController, settingsManager, permissionsManager, config.DisableDefaultPolicy)
			if err != nil {
				return fmt.Errorf("failed to build default manager: %v", err)
			}

			// Wire the cluster coordinator so the account manager can
			// publish posture-schedule-change invalidations and so the
			// posture scheduler below can subscribe + acquire locks.
			accountManager.SetCoordinator(clusterCoord)

			// Posture-schedule scheduler: one goroutine per account
			// with at least one ScheduleCheck, electing a leader via
			// the cluster lock so HA deployments only fire one
			// network-map recomputation per boundary cross. Single-
			// replica installs still benefit — boundary crosses become
			// near-real-time instead of waiting for the next peer
			// Sync poll. Errors at boot don't block the management
			// startup; we log and continue with the natural-Sync
			// fall-back path.
			postureScheduler := posture.NewScheduler(
				clusterCoord,
				accountManager,
				server.NewScheduleLoader(store),
			)
			go func() {
				if err := postureScheduler.Run(ctx); err != nil && ctx.Err() == nil {
					log.WithContext(ctx).Errorf("posture scheduler exited unexpectedly: %v", err)
				}
			}()

			secretsManager := server.NewTimeBasedAuthSecretsManager(peersUpdateManager, config.TURNConfig, config.Relay, settingsManager)

			trustedPeers := config.ReverseProxy.TrustedPeers
			defaultTrustedPeers := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
			if len(trustedPeers) == 0 || slices.Equal[[]netip.Prefix](trustedPeers, defaultTrustedPeers) {
				log.WithContext(ctx).Warn("TrustedPeers are configured to default value '0.0.0.0/0', '::/0'. This allows connection IP spoofing.")
				trustedPeers = defaultTrustedPeers
			}
			trustedHTTPProxies := config.ReverseProxy.TrustedHTTPProxies
			trustedProxiesCount := config.ReverseProxy.TrustedHTTPProxiesCount
			if len(trustedHTTPProxies) > 0 && trustedProxiesCount > 0 {
				log.WithContext(ctx).Warn("TrustedHTTPProxies and TrustedHTTPProxiesCount both are configured. " +
					"This is not recommended way to extract X-Forwarded-For. Consider using one of these options.")
			}
			realipOpts := []realip.Option{
				realip.WithTrustedPeers(trustedPeers),
				realip.WithTrustedProxies(trustedHTTPProxies),
				realip.WithTrustedProxiesCount(trustedProxiesCount),
				realip.WithHeaders([]string{realip.XForwardedFor, realip.XRealIp}),
			}
			gRPCOpts := []grpc.ServerOption{
				grpc.KeepaliveEnforcementPolicy(kaep),
				grpc.KeepaliveParams(kasp),
				grpc.ChainUnaryInterceptor(realip.UnaryServerInterceptorOpts(realipOpts...), unaryInterceptor),
				grpc.ChainStreamInterceptor(realip.StreamServerInterceptorOpts(realipOpts...), streamInterceptor),
			}

			var certManager *autocert.Manager
			var tlsConfig *tls.Config
			tlsEnabled := false
			if config.HttpConfig.LetsEncryptDomain != "" {
				certManager, err = encryption.CreateCertManager(config.Datadir, config.HttpConfig.LetsEncryptDomain)
				if err != nil {
					return fmt.Errorf("failed creating LetsEncrypt cert manager: %v", err)
				}
				transportCredentials := credentials.NewTLS(certManager.TLSConfig())
				gRPCOpts = append(gRPCOpts, grpc.Creds(transportCredentials))
				tlsEnabled = true
			} else if config.HttpConfig.CertFile != "" && config.HttpConfig.CertKey != "" {
				tlsConfig, err = loadTLSConfig(config.HttpConfig.CertFile, config.HttpConfig.CertKey)
				if err != nil {
					log.WithContext(ctx).Errorf("cannot load TLS credentials: %v", err)
					return err
				}
				transportCredentials := credentials.NewTLS(tlsConfig)
				gRPCOpts = append(gRPCOpts, grpc.Creds(transportCredentials))
				tlsEnabled = true
			}

			// auth/providers store + manager are constructed BEFORE
			authManager := auth.NewManager(store,
				config.HttpConfig.AuthIssuer,
				config.HttpConfig.AuthAudience,
				config.HttpConfig.AuthKeysLocation,
				config.HttpConfig.AuthUserIDClaim,
				config.GetAuthAudiences(),
				config.HttpConfig.IdpSignKeyRefreshEnabled)

			groupsManager := groups.NewManager(store, permissionsManager, accountManager)
			resourcesManager := resources.NewManager(store, permissionsManager, groupsManager, accountManager)
			routersManager := routers.NewManager(store, permissionsManager, accountManager)
			networksManager := networks.NewManager(store, permissionsManager, resourcesManager, routersManager, accountManager)

			// Flow events store: built early so the HTTP /network-traffic-events
			// handler and the gRPC FlowService both see the same instance.
			// Returns nil when OPENZRO_FLOW_STORE_ENGINE is unset/none.
			flowBuilt, err := flowFactory.NewFromEnv(ctx)
			if err != nil {
				return fmt.Errorf("flow store: %w", err)
			}
			var flowStore flowstore.Store
			if flowBuilt != nil {
				flowStore = flowBuilt.Store
				defer func() { _ = flowBuilt.Close() }()

				// Cold-archive read path (ADR-0012). When the operator has
				// configured a Parquet archive (S3 or GCS) AND this binary
				// was built with `-tags=archive_duckdb`, wrap the hot store
				// in a federated layer that routes queries by date window.
				// Otherwise — no archive configured, NDJSON-only archive,
				// or non-DuckDB build — federated falls through to hot-only
				// behaviour.
				archiveStore, archErr := flowArchive.NewFromEnv()
				switch {
				case errors.Is(archErr, flowArchive.ErrUnavailable):
					log.WithContext(ctx).Warn(
						"flow archive: configured but binary built without archive_duckdb — " +
							"rebuild with `-tags=archive_duckdb` to enable federated reads")
				case archErr != nil:
					return fmt.Errorf("flow archive store: %w", archErr)
				}
				if archiveStore != nil {
					fed, err := flowFederated.New(flowStore, archiveStore, flowBuilt.Retention)
					if err != nil {
						return fmt.Errorf("flow federated store: %w", err)
					}
					flowStore = fed
					log.WithContext(ctx).Infof(
						"flow archive: federated read enabled (hot retention=%s)", flowBuilt.Retention)
				}
			}

			// flow_exports: runtime-configurable destinations. The store
			// shares the management's primary DB so admin-created rows
			// land alongside accounts/peers/etc. Reads & writes go
			// through GORM with credentials encrypted at rest.
			var flowExportsStore *flowExports.Store
			var mdmStore *mdm.Store
			var mdmManager *mdm.Manager
			var activityExportersStore *activityExporters.Store
			var activityExportersManager *activityExporters.Manager
			var admissionBypassStore *admission.Store
			if sqlStore, ok := store.(*mgmtStore.SqlStore); ok {
				flowExportsStore, err = flowExports.NewStore(sqlStore.GetGormDB(), config.DataStoreEncryptionKey)
				if err != nil {
					return fmt.Errorf("flow_exports store: %w", err)
				}
				mdmStore, err = mdm.NewStore(sqlStore.GetGormDB(), config.DataStoreEncryptionKey)
				if err != nil {
					return fmt.Errorf("mdm store: %w", err)
				}
				mdmManager, err = mdm.NewManager(ctx, mdmStore, 0)
				if err != nil {
					return fmt.Errorf("mdm manager: %w", err)
				}
				activityExportersStore, err = activityExporters.NewStore(sqlStore.GetGormDB(), config.DataStoreEncryptionKey)
				if err != nil {
					return fmt.Errorf("activity_exporters store: %w", err)
				}
				activityExportersManager, err = activityExporters.NewManager(ctx, activityExportersStore)
				if err != nil {
					return fmt.Errorf("activity_exporters manager: %w", err)
				}
				defer activityExportersManager.Stop()
				// Per-peer admission bypass store (ADR-0004 — break-
				// glass overrides for the Device Admission gate, with
				// mandatory audit trail). The expiry worker fires
				// peer.admission.bypass.expired events as rows time
				// out so the auditor sees the full lifecycle.
				admissionBypassStore, err = admission.NewStore(sqlStore.GetGormDB())
				if err != nil {
					return fmt.Errorf("admission bypass store: %w", err)
				}
				// auth/providers store + manager are constructed
				// earlier (just before auth.NewManager) so the
				// multi-issuer JWT validator can route incoming
				// tokens. Nothing more to wire here.
				// Wire the posture check into the live manager. Set
				// the package-level resolver once so every Account's
				// posture eval can call the manager without threading
				// the dependency through every method.
				posture.SetDefaultMDMResolver(func(ctx context.Context, providerID uint64, deviceID string) (bool, string, error) {
					st, err := mdmManager.Lookup(ctx, providerID, deviceID)
					if err != nil {
						return false, "", err
					}
					return st.Compliant, st.Reason, nil
				})
				defer func() { _ = mdmManager.Close() }()
			}

			// Build FlowService + flow_exports.Manager BEFORE
			// NewAPIHandler so the admin endpoints can be registered
			// with the live manager (no nil manager + no late
			// binding).
			peerResolver := func(ctx context.Context, pubKeyBytes []byte) (string, string, error) {
				key := base64.StdEncoding.EncodeToString(pubKeyBytes)
				peer, err := store.GetPeerByPeerPubKey(ctx, mgmtStore.LockingStrengthShare, key)
				if err != nil {
					return "", "", err
				}
				return peer.ID, peer.AccountID, nil
			}

			extraSinks, err := flowSinks.NewFromEnv(ctx)
			if err != nil {
				return fmt.Errorf("flow sinks: %w", err)
			}
			defer func() {
				for _, s := range extraSinks {
					_ = s.Close()
				}
			}()

			initial := []flowstore.Sink{}
			if flowStore != nil {
				initial = append(initial, flowStore)
			}
			initial = append(initial, extraSinks...)

			// ADR-0018: the server-side resolver is the fallback path
			// for flow events the agent could not stamp at firewall
			// time (Linux kernel outbound-initiator flows). Wired into
			// both FlowService (Resolve at ingest) and AccountManager
			// (Rebuild on every account-graph change). When the flow
			// store is disabled (engine=none), the resolver still
			// fires but its output goes nowhere — kept on so SIEM
			// streams and cold-archive sinks observe enriched events.
			flowPolicyResolver := flowPolicyResolverPkg.New()
			flowSvc := server.NewFlowService(initial, peerResolver,
				server.WithPolicyResolver(flowPolicyResolver),
			)
			accountManager.SetFlowPolicyIndex(flowPolicyResolver)

			var flowExportsManager *flowExports.Manager
			if flowExportsStore != nil {
				flowExportsManager, err = flowExports.NewManager(ctx, flowExportsStore, flowSvc, flowStore, extraSinks)
				if err != nil {
					return fmt.Errorf("flow_exports manager: %w", err)
				}
			}

			// Wire the per-account activity streamer into StoreEvent
			// fan-out before the HTTP API comes online, so the very
			// first events the API can produce already flow through.
			if activityExportersManager != nil {
				accountManager.SetActivityExporters(activityExportersManager)
			}
			if admissionBypassStore != nil {
				accountManager.SetAdmissionBypasses(admissionBypassStore)
				// The worker emits peer.admission.bypass.expired
				// events through the same StoreEvent path the rest
				// of the audit log uses. Adapt to the package's
				// import-cycle-free EventEmitter signature:
				// activity.Activity is a typed int alias, so we
				// reconstruct it from the uint32 the worker passes.
				go admission.RunExpiryWorker(ctx, admissionBypassStore,
					func(ctx context.Context, initiatorID, targetID, accountID string, code uint32, meta map[string]any) {
						accountManager.StoreEvent(ctx, initiatorID, targetID, accountID, activity.Activity(code), meta)
					})
			}

			// Bypass-event emitter passed by closure: the handler
			// emits granted / revoked through the same StoreEvent
			// path as the rest of the audit log. Cast the activity
			// type alias since admission_bypass uses the activity
			// package directly (no import-cycle workaround needed
			// at the handler edge).
			bypassEmitter := func(ctx context.Context, initiatorID, targetID, accountID string, code activity.Activity, meta map[string]any) {
				accountManager.StoreEvent(ctx, initiatorID, targetID, accountID, code, meta)
			}

			// Dex gRPC client (ADR-0006). Built from env vars
			// the operator's setup.env / configure.sh emit. nil
			// when the env is empty — the dashboard's auth-
			// providers admin endpoints respond with 503 in that
			// case so the UI shows an "IdP not configured" empty
			// state rather than crashing.
			var dexClient *dex_proxy.Client
			if dexCfg, dcErr := dex_proxy.FromEnv(); dcErr != nil {
				return fmt.Errorf("dex_proxy config: %w", dcErr)
			} else if dexCfg != nil {
				dexClient, err = dex_proxy.New(*dexCfg)
				if err != nil {
					return fmt.Errorf("dex_proxy client: %w", err)
				}
				defer func() { _ = dexClient.Close() }()
				if hcErr := dexClient.HealthCheck(ctx); hcErr != nil {
					log.WithContext(ctx).Warnf("dex_proxy HealthCheck failed at boot: %v (admin auth-providers API may return 503 until Dex becomes reachable)", hcErr)
				} else {
					log.WithContext(ctx).Infof("dex_proxy connected to %s", dexCfg.Addr)
				}
			}

			httpAPIHandler, err := nbhttp.NewAPIHandler(ctx, accountManager, networksManager, resourcesManager, routersManager, groupsManager, geo, authManager, appMetrics, integratedPeerValidator, proxyController, permissionsManager, peersManager, settingsManager, flowStore, flowExportsStore, flowExportsManager, mdmStore, mdmManager, activityExportersStore, activityExportersManager, admissionBypassStore, bypassEmitter, dexClient)

			if err != nil {
				return fmt.Errorf("failed creating HTTP API handler: %v", err)
			}

			ephemeralManager := server.NewEphemeralManager(store, accountManager)
			ephemeralManager.LoadInitialPeers(ctx)

			gRPCAPIHandler := grpc.NewServer(gRPCOpts...)
			srv, err := server.NewServer(ctx, config, accountManager, settingsManager, peersUpdateManager, secretsManager, appMetrics, ephemeralManager, authManager, integratedPeerValidator)
			if err != nil {
				return fmt.Errorf("failed creating gRPC API handler: %v", err)
			}
			mgmtProto.RegisterManagementServiceServer(gRPCAPIHandler, srv)
			flowProto.RegisterFlowServiceServer(gRPCAPIHandler, flowSvc)

			installationID, err := getInstallationID(ctx, store)
			if err != nil {
				log.WithContext(ctx).Errorf("cannot load TLS credentials: %v", err)
				return err
			}

			if !disableMetrics {
				idpManager := "disabled"
				if config.IdpManagerConfig != nil && config.IdpManagerConfig.ManagerType != "" {
					idpManager = config.IdpManagerConfig.ManagerType
				}
				metricsWorker := metrics.NewWorker(ctx, installationID, store, peersUpdateManager, idpManager)
				go metricsWorker.Run(ctx)
			}

			var compatListener net.Listener
			if mgmtPort != ManagementLegacyPort {
				// The Management gRPC server was running on port 33073 previously. Old agents that are already connected to it
				// are using port 33073. For compatibility purposes we keep running a 2nd gRPC server on port 33073.
				compatListener, err = serveGRPC(ctx, gRPCAPIHandler, ManagementLegacyPort)
				if err != nil {
					return err
				}
				log.WithContext(ctx).Infof("running gRPC backward compatibility server: %s", compatListener.Addr().String())
			}

			rootHandler := handlerFunc(gRPCAPIHandler, httpAPIHandler)
			var listener net.Listener
			if certManager != nil {
				// a call to certManager.Listener() always creates a new listener so we do it once
				cml := certManager.Listener()
				if mgmtPort == 443 {
					// CertManager, HTTP and gRPC API all on the same port
					rootHandler = certManager.HTTPHandler(rootHandler)
					listener = cml
				} else {
					listener, err = tls.Listen("tcp", fmt.Sprintf(":%d", mgmtPort), certManager.TLSConfig())
					if err != nil {
						return fmt.Errorf("failed creating TLS listener on port %d: %v", mgmtPort, err)
					}
					log.WithContext(ctx).Infof("running HTTP server (LetsEncrypt challenge handler): %s", cml.Addr().String())
					serveHTTP(ctx, cml, certManager.HTTPHandler(nil))
				}
			} else if tlsConfig != nil {
				listener, err = tls.Listen("tcp", fmt.Sprintf(":%d", mgmtPort), tlsConfig)
				if err != nil {
					return fmt.Errorf("failed creating TLS listener on port %d: %v", mgmtPort, err)
				}
			} else {
				listener, err = net.Listen("tcp", fmt.Sprintf(":%d", mgmtPort))
				if err != nil {
					return fmt.Errorf("failed creating TCP listener on port %d: %v", mgmtPort, err)
				}
			}

			log.WithContext(ctx).Infof("management server version %s", version.OpenzroVersion())
			log.WithContext(ctx).Infof("running HTTP server and gRPC server on the same port: %s", listener.Addr().String())
			serveGRPCWithHTTP(ctx, listener, rootHandler, tlsEnabled)

			update := version.NewUpdate("nb/management")
			update.SetDaemonVersion(version.OpenzroVersion())
			update.SetOnUpdateListener(func() {
				log.WithContext(ctx).Infof("your management version, \"%s\", is outdated, a new management version is available. Learn more here: https://github.com/openzro/openzro/releases", version.OpenzroVersion())
			})
			defer update.StopWatch()

			SetupCloseHandler()

			<-stopCh
			integratedPeerValidator.Stop(ctx)
			if geo != nil {
				_ = geo.Stop()
			}
			ephemeralManager.Stop()
			_ = appMetrics.Close()
			_ = listener.Close()
			if certManager != nil {
				_ = certManager.Listener().Close()
			}
			gRPCAPIHandler.Stop()
			_ = store.Close(ctx)
			_ = eventStore.Close(ctx)
			log.WithContext(ctx).Infof("stopped Management Service")

			return nil
		},
	}
)

func unaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	reqID := uuid.New().String()
	//nolint
	ctx = context.WithValue(ctx, hook.ExecutionContextKey, hook.GRPCSource)
	//nolint
	ctx = context.WithValue(ctx, nbContext.RequestIDKey, reqID)
	return handler(ctx, req)
}

func streamInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	reqID := uuid.New().String()
	wrapped := grpcMiddleware.WrapServerStream(ss)
	//nolint
	ctx := context.WithValue(ss.Context(), hook.ExecutionContextKey, hook.GRPCSource)
	//nolint
	wrapped.WrappedContext = context.WithValue(ctx, nbContext.RequestIDKey, reqID)
	return handler(srv, wrapped)
}

func notifyStop(ctx context.Context, msg string) {
	select {
	case stopCh <- 1:
		log.WithContext(ctx).Error(msg)
	default:
		// stop has been already called, nothing to report
	}
}

func getInstallationID(ctx context.Context, store mgmtStore.Store) (string, error) {
	installationID := store.GetInstallationID()
	if installationID != "" {
		return installationID, nil
	}

	installationID = strings.ToUpper(uuid.New().String())
	err := store.SaveInstallationID(ctx, installationID)
	if err != nil {
		return "", err
	}
	return installationID, nil
}

func serveGRPC(ctx context.Context, grpcServer *grpc.Server, port int) (net.Listener, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	go func() {
		err := grpcServer.Serve(listener)
		if err != nil {
			notifyStop(ctx, fmt.Sprintf("failed running gRPC server on port %d: %v", port, err))
		}
	}()
	return listener, nil
}

func serveHTTP(ctx context.Context, httpListener net.Listener, handler http.Handler) {
	go func() {
		err := http.Serve(httpListener, handler)
		if err != nil {
			notifyStop(ctx, fmt.Sprintf("failed running HTTP server: %v", err))
		}
	}()
}

func serveGRPCWithHTTP(ctx context.Context, listener net.Listener, handler http.Handler, tlsEnabled bool) {
	go func() {
		var err error
		if tlsEnabled {
			err = http.Serve(listener, handler)
		} else {
			// the following magic is needed to support HTTP2 without TLS
			// and still share a single port between gRPC and HTTP APIs
			h1s := &http.Server{
				Handler: h2c.NewHandler(handler, &http2.Server{}),
			}
			err = h1s.Serve(listener)
		}

		if err != nil {
			select {
			case stopCh <- 1:
				log.WithContext(ctx).Errorf("failed to serve HTTP and gRPC server: %v", err)
			default:
				// stop has been already called, nothing to report
			}
		}
	}()
}

func handlerFunc(gRPCHandler *grpc.Server, httpHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		grpcHeader := strings.HasPrefix(request.Header.Get("Content-Type"), "application/grpc") ||
			strings.HasPrefix(request.Header.Get("Content-Type"), "application/grpc+proto")
		if request.ProtoMajor == 2 && grpcHeader {
			gRPCHandler.ServeHTTP(writer, request)
		} else {
			httpHandler.ServeHTTP(writer, request)
		}
	})
}

func loadMgmtConfig(ctx context.Context, mgmtConfigPath string) (*types.Config, error) {
	loadedConfig := &types.Config{}
	_, err := util.ReadJsonWithEnvSub(mgmtConfigPath, loadedConfig)
	if err != nil {
		return nil, err
	}
	if mgmtLetsencryptDomain != "" {
		loadedConfig.HttpConfig.LetsEncryptDomain = mgmtLetsencryptDomain
	}
	if mgmtDataDir != "" {
		loadedConfig.Datadir = mgmtDataDir
	}

	if certKey != "" && certFile != "" {
		loadedConfig.HttpConfig.CertFile = certFile
		loadedConfig.HttpConfig.CertKey = certKey
	}

	oidcEndpoint := loadedConfig.HttpConfig.OIDCConfigEndpoint
	if oidcEndpoint != "" {
		// if OIDCConfigEndpoint is specified, we can load DeviceAuthEndpoint and TokenEndpoint automatically
		log.WithContext(ctx).Infof("loading OIDC configuration from the provided IDP configuration endpoint %s", oidcEndpoint)
		oidcConfig, err := fetchOIDCConfig(ctx, oidcEndpoint)
		if err != nil {
			return nil, err
		}
		log.WithContext(ctx).Infof("loaded OIDC configuration from the provided IDP configuration endpoint: %s", oidcEndpoint)

		log.WithContext(ctx).Infof("overriding HttpConfig.AuthIssuer with a new value %s, previously configured value: %s",
			oidcConfig.Issuer, loadedConfig.HttpConfig.AuthIssuer)
		loadedConfig.HttpConfig.AuthIssuer = oidcConfig.Issuer

		log.WithContext(ctx).Infof("overriding HttpConfig.AuthKeysLocation (JWT certs) with a new value %s, previously configured value: %s",
			oidcConfig.JwksURI, loadedConfig.HttpConfig.AuthKeysLocation)
		loadedConfig.HttpConfig.AuthKeysLocation = oidcConfig.JwksURI

		if !(loadedConfig.DeviceAuthorizationFlow == nil || strings.ToLower(loadedConfig.DeviceAuthorizationFlow.Provider) == string(types.NONE)) {
			log.WithContext(ctx).Infof("overriding DeviceAuthorizationFlow.TokenEndpoint with a new value: %s, previously configured value: %s",
				oidcConfig.TokenEndpoint, loadedConfig.DeviceAuthorizationFlow.ProviderConfig.TokenEndpoint)
			loadedConfig.DeviceAuthorizationFlow.ProviderConfig.TokenEndpoint = oidcConfig.TokenEndpoint
			log.WithContext(ctx).Infof("overriding DeviceAuthorizationFlow.DeviceAuthEndpoint with a new value: %s, previously configured value: %s",
				oidcConfig.DeviceAuthEndpoint, loadedConfig.DeviceAuthorizationFlow.ProviderConfig.DeviceAuthEndpoint)
			loadedConfig.DeviceAuthorizationFlow.ProviderConfig.DeviceAuthEndpoint = oidcConfig.DeviceAuthEndpoint

			u, err := url.Parse(oidcEndpoint)
			if err != nil {
				return nil, err
			}
			log.WithContext(ctx).Infof("overriding DeviceAuthorizationFlow.ProviderConfig.Domain with a new value: %s, previously configured value: %s",
				u.Host, loadedConfig.DeviceAuthorizationFlow.ProviderConfig.Domain)
			loadedConfig.DeviceAuthorizationFlow.ProviderConfig.Domain = u.Host

			if loadedConfig.DeviceAuthorizationFlow.ProviderConfig.Scope == "" {
				loadedConfig.DeviceAuthorizationFlow.ProviderConfig.Scope = types.DefaultDeviceAuthFlowScope
			}
		}

		if loadedConfig.PKCEAuthorizationFlow != nil {
			log.WithContext(ctx).Infof("overriding PKCEAuthorizationFlow.TokenEndpoint with a new value: %s, previously configured value: %s",
				oidcConfig.TokenEndpoint, loadedConfig.PKCEAuthorizationFlow.ProviderConfig.TokenEndpoint)
			loadedConfig.PKCEAuthorizationFlow.ProviderConfig.TokenEndpoint = oidcConfig.TokenEndpoint
			log.WithContext(ctx).Infof("overriding PKCEAuthorizationFlow.AuthorizationEndpoint with a new value: %s, previously configured value: %s",
				oidcConfig.AuthorizationEndpoint, loadedConfig.PKCEAuthorizationFlow.ProviderConfig.AuthorizationEndpoint)
			loadedConfig.PKCEAuthorizationFlow.ProviderConfig.AuthorizationEndpoint = oidcConfig.AuthorizationEndpoint
		}
	}

	if loadedConfig.Relay != nil {
		log.Infof("Relay addresses: %v", loadedConfig.Relay.Addresses)
	}

	return loadedConfig, err
}

func updateMgmtConfig(ctx context.Context, path string, config *types.Config) error {
	return util.DirectWriteJson(ctx, path, config)
}

// OIDCConfigResponse used for parsing OIDC config response
type OIDCConfigResponse struct {
	Issuer                string `json:"issuer"`
	TokenEndpoint         string `json:"token_endpoint"`
	DeviceAuthEndpoint    string `json:"device_authorization_endpoint"`
	JwksURI               string `json:"jwks_uri"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
}

// fetchOIDCConfig fetches OIDC configuration from the IDP
func fetchOIDCConfig(ctx context.Context, oidcEndpoint string) (OIDCConfigResponse, error) {
	res, err := http.Get(oidcEndpoint)
	if err != nil {
		return OIDCConfigResponse{}, fmt.Errorf("failed fetching OIDC configuration from endpoint %s %v", oidcEndpoint, err)
	}

	defer func() {
		err := res.Body.Close()
		if err != nil {
			log.WithContext(ctx).Debugf("failed closing response body %v", err)
		}
	}()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return OIDCConfigResponse{}, fmt.Errorf("failed reading OIDC configuration response body: %v", err)
	}

	if res.StatusCode != 200 {
		return OIDCConfigResponse{}, fmt.Errorf("OIDC configuration request returned status %d with response: %s",
			res.StatusCode, string(body))
	}

	config := OIDCConfigResponse{}
	err = json.Unmarshal(body, &config)
	if err != nil {
		return OIDCConfigResponse{}, fmt.Errorf("failed unmarshaling OIDC configuration response: %v", err)
	}

	return config, nil
}

func loadTLSConfig(certFile string, certKey string) (*tls.Config, error) {
	// Load server's certificate and private key
	serverCert, err := tls.LoadX509KeyPair(certFile, certKey)
	if err != nil {
		return nil, err
	}

	// NewDefaultAppMetrics the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.NoClientCert,
		NextProtos: []string{
			"h2", "http/1.1", // enable HTTP/2
		},
	}

	return config, nil
}

func handleRebrand(cmd *cobra.Command) error {
	var err error
	if logFile == defaultLogFile {
		if migrateToOpenzro(oldDefaultLogFile, defaultLogFile) {
			cmd.Printf("will copy Log dir %s and its content to %s\n", oldDefaultLogDir, defaultLogDir)
			err = cpDir(oldDefaultLogDir, defaultLogDir)
			if err != nil {
				return err
			}
		}
	}
	if types.MgmtConfigPath == defaultMgmtConfig {
		if migrateToOpenzro(oldDefaultMgmtConfig, defaultMgmtConfig) {
			cmd.Printf("will copy Config dir %s and its content to %s\n", oldDefaultMgmtConfigDir, defaultMgmtConfigDir)
			err = cpDir(oldDefaultMgmtConfigDir, defaultMgmtConfigDir)
			if err != nil {
				return err
			}
		}
	}
	if mgmtDataDir == defaultMgmtDataDir {
		if migrateToOpenzro(oldDefaultMgmtDataDir, defaultMgmtDataDir) {
			cmd.Printf("will copy Config dir %s and its content to %s\n", oldDefaultMgmtDataDir, defaultMgmtDataDir)
			err = cpDir(oldDefaultMgmtDataDir, defaultMgmtDataDir)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func cpFile(src, dst string) error {
	var err error
	var srcfd *os.File
	var dstfd *os.File
	var srcinfo os.FileInfo

	if srcfd, err = os.Open(src); err != nil {
		return err
	}
	defer srcfd.Close()

	if dstfd, err = os.Create(dst); err != nil {
		return err
	}
	defer dstfd.Close()

	if _, err = io.Copy(dstfd, srcfd); err != nil {
		return err
	}
	if srcinfo, err = os.Stat(src); err != nil {
		return err
	}
	return os.Chmod(dst, srcinfo.Mode())
}

func copySymLink(source, dest string) error {
	link, err := os.Readlink(source)
	if err != nil {
		return err
	}
	return os.Symlink(link, dest)
}

func cpDir(src string, dst string) error {
	var err error
	var fds []os.DirEntry
	var srcinfo os.FileInfo

	if srcinfo, err = os.Stat(src); err != nil {
		return err
	}

	if err = os.MkdirAll(dst, srcinfo.Mode()); err != nil {
		return err
	}

	if fds, err = os.ReadDir(src); err != nil {
		return err
	}
	for _, fd := range fds {
		srcfp := path.Join(src, fd.Name())
		dstfp := path.Join(dst, fd.Name())

		fileInfo, err := os.Stat(srcfp)
		if err != nil {
			log.Fatalf("Couldn't get fileInfo; %v", err)
		}

		switch fileInfo.Mode() & os.ModeType {
		case os.ModeSymlink:
			if err = copySymLink(srcfp, dstfp); err != nil {
				log.Fatalf("Failed to copy from %s to %s; %v", srcfp, dstfp, err)
			}
		case os.ModeDir:
			if err = cpDir(srcfp, dstfp); err != nil {
				log.Fatalf("Failed to copy from %s to %s; %v", srcfp, dstfp, err)
			}
		default:
			if err = cpFile(srcfp, dstfp); err != nil {
				log.Fatalf("Failed to copy from %s to %s; %v", srcfp, dstfp, err)
			}
		}
	}
	return nil
}

func migrateToOpenzro(oldPath, newPath string) bool {
	_, errOld := os.Stat(oldPath)
	_, errNew := os.Stat(newPath)

	if errors.Is(errOld, fs.ErrNotExist) || errNew == nil {
		return false
	}

	return true
}

