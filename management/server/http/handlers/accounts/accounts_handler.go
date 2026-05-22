package accounts

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/settings"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// handler is a handler that handles the server.Account HTTP endpoints
type handler struct {
	accountManager  account.Manager
	settingsManager settings.Manager
}

func AddEndpoints(accountManager account.Manager, settingsManager settings.Manager, router *mux.Router) {
	accountsHandler := newHandler(accountManager, settingsManager)
	router.HandleFunc("/accounts/{accountId}", accountsHandler.updateAccount).Methods("PUT", "OPTIONS")
	router.HandleFunc("/accounts/{accountId}", accountsHandler.deleteAccount).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/accounts", accountsHandler.getAllAccounts).Methods("GET", "OPTIONS")
}

// newHandler creates a new handler HTTP handler
func newHandler(accountManager account.Manager, settingsManager settings.Manager) *handler {
	return &handler{
		accountManager:  accountManager,
		settingsManager: settingsManager,
	}
}

// getAllAccounts is HTTP GET handler that returns a list of accounts. Effectively returns just a single account.
func (h *handler) getAllAccounts(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	meta, err := h.accountManager.GetAccountMeta(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	settings, err := h.settingsManager.GetSettings(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	onboarding, err := h.accountManager.GetAccountOnboarding(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	resp := toAccountResponse(accountID, settings, meta, onboarding)
	util.WriteJSONObject(r.Context(), w, []*api.Account{resp})
}

// updateAccount is HTTP PUT handler that updates the provided account. Updates only account settings (server.Settings)
func (h *handler) updateAccount(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	_, userID := userAuth.AccountId, userAuth.UserId

	vars := mux.Vars(r)
	accountID := vars["accountId"]
	if len(accountID) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid accountID ID"), w)
		return
	}

	var req api.PutApiAccountsAccountIdJSONRequestBody
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	settings := &types.Settings{
		PeerLoginExpirationEnabled: req.Settings.PeerLoginExpirationEnabled,
		PeerLoginExpiration:        time.Duration(float64(time.Second.Nanoseconds()) * float64(req.Settings.PeerLoginExpiration)),
		RegularUsersViewBlocked:    req.Settings.RegularUsersViewBlocked,

		PeerInactivityExpirationEnabled: req.Settings.PeerInactivityExpirationEnabled,
		PeerInactivityExpiration:        time.Duration(float64(time.Second.Nanoseconds()) * float64(req.Settings.PeerInactivityExpiration)),
	}

	// MFA enforcement toggles (openZro #31). Both default OFF — when
	// the dashboard omits them (legacy clients) we preserve the
	// existing value rather than silently turning enforcement off,
	// which would let a user without MFA quietly bypass policy.
	if req.Settings.MfaEnforceLocal != nil {
		settings.MFAEnforceLocal = *req.Settings.MfaEnforceLocal
	} else if existing, err := h.settingsManager.GetSettings(r.Context(), accountID, userID); err == nil && existing != nil {
		settings.MFAEnforceLocal = existing.MFAEnforceLocal
	}
	if req.Settings.MfaEnforceFederated != nil {
		settings.MFAEnforceFederated = *req.Settings.MfaEnforceFederated
	} else if existing, err := h.settingsManager.GetSettings(r.Context(), accountID, userID); err == nil && existing != nil {
		settings.MFAEnforceFederated = existing.MFAEnforceFederated
	}

	if req.Settings.Extra != nil {
		var groups []string
		if req.Settings.Extra.NetworkTrafficLogsGroups != nil {
			groups = append(groups, *req.Settings.Extra.NetworkTrafficLogsGroups...)
		}
		// Seed the new Extra from the current account settings so that
		// fields the OpenAPI spec does not surface (FlowDnsCollectionEnabled,
		// FlowENCollectionEnabled) survive the round-trip. Without the
		// seed, the dashboard's NetworkSettingsTab — which only sends
		// the wire-visible toggles — wipes those flags on every Save.
		var seed types.ExtraSettings
		if existing, err := h.settingsManager.GetSettings(r.Context(), accountID, userID); err == nil && existing != nil && existing.Extra != nil {
			seed = *existing.Extra
		}
		extra := seed
		extra.PeerApprovalEnabled = req.Settings.Extra.PeerApprovalEnabled
		extra.FlowEnabled = req.Settings.Extra.NetworkTrafficLogsEnabled
		extra.FlowPacketCounterEnabled = req.Settings.Extra.NetworkTrafficPacketCounterEnabled
		extra.FlowEventsGroups = groups
		if req.Settings.Extra.NetworkTrafficDisableDefaultPortFilter != nil {
			extra.FlowDisableDefaultPortFilter = *req.Settings.Extra.NetworkTrafficDisableDefaultPortFilter
		}
		if req.Settings.Extra.NetworkTrafficExcludedPorts != nil {
			ports := make([]types.FlowPortFilter, 0, len(*req.Settings.Extra.NetworkTrafficExcludedPorts))
			for _, p := range *req.Settings.Extra.NetworkTrafficExcludedPorts {
				ports = append(ports, types.FlowPortFilter{
					Port:     uint32(p.Port),
					Protocol: string(p.Protocol),
				})
			}
			extra.FlowExcludedPorts = ports
		}
		if req.Settings.Extra.NetworkTrafficDefaultRange != nil {
			// API field is typed enum (oapi-codegen surfaces enum
			// constraint as a named string type). Internal type is
			// plain string; cast at the boundary.
			extra.FlowTrafficDefaultRange = string(*req.Settings.Extra.NetworkTrafficDefaultRange)
		}
		settings.Extra = &extra
	}

	if req.Settings.JwtGroupsEnabled != nil {
		settings.JWTGroupsEnabled = *req.Settings.JwtGroupsEnabled
	}
	if req.Settings.GroupsPropagationEnabled != nil {
		settings.GroupsPropagationEnabled = *req.Settings.GroupsPropagationEnabled
	}
	if req.Settings.JwtGroupsClaimName != nil {
		settings.JWTGroupsClaimName = *req.Settings.JwtGroupsClaimName
	}
	if req.Settings.JwtAllowGroups != nil {
		settings.JWTAllowGroups = *req.Settings.JwtAllowGroups
	}
	if req.Settings.RoutingPeerDnsResolutionEnabled != nil {
		settings.RoutingPeerDNSResolutionEnabled = *req.Settings.RoutingPeerDnsResolutionEnabled
	}
	if req.Settings.DnsDomain != nil {
		settings.DNSDomain = *req.Settings.DnsDomain
	}
	if req.Settings.LazyConnectionEnabled != nil {
		settings.LazyConnectionEnabled = *req.Settings.LazyConnectionEnabled
	}
	if req.Settings.AdmissionEnforcementEnabled != nil {
		settings.AdmissionEnforcementEnabled = *req.Settings.AdmissionEnforcementEnabled
	}
	if req.Settings.AdmissionPostureChecks != nil {
		settings.AdmissionPostureChecks = *req.Settings.AdmissionPostureChecks
	}
	if req.Settings.AdmissionExemptGroups != nil {
		settings.AdmissionExemptGroups = *req.Settings.AdmissionExemptGroups
	}

	// openZro #5 Q2: the client self-update directive + its server-side
	// subset targeting. Group/peer IDs are not existence-checked here
	// (consistent with admission_exempt_groups / admission_posture_checks
	// above — a dangling ID is a no-op; the dangling-EXCLUDE risk is a
	// documented operator concern, not enforced one-off). The rollout
	// percent IS hard-validated: OpenAPI min/max is documentation only
	// (no runtime validation middleware), and an out-of-range ring
	// would otherwise reach the fail-closed gate as silently "nobody".
	if req.Settings.ClientUpdateTargetVersion != nil {
		// Normalize the conventional Git-tag leading "v" so an
		// operator who copies a release tag verbatim ("v0.53.1-
		// alpha.76") doesn't end up with a target the resolver
		// templates into `releases/download/vv0.53.1-alpha.76/
		// update-manifest.json` — a non-existent path, fetched as
		// 404, surfaced in the macOS tray as the cryptic
		// "manifest fetch failed: ... returned HTTP 404". Forgiving
		// on input; the directive path stores the stripped form so
		// the per-version manifest URL resolves.
		settings.ClientUpdateTargetVersion = strings.TrimPrefix(
			*req.Settings.ClientUpdateTargetVersion, "v")
	}
	if req.Settings.ClientUpdateForce != nil {
		settings.ClientUpdateForce = *req.Settings.ClientUpdateForce
	}
	if req.Settings.ClientUpdateTargetGroups != nil {
		settings.ClientUpdateTargetGroups = *req.Settings.ClientUpdateTargetGroups
	}
	if req.Settings.ClientUpdateTargetPeers != nil {
		settings.ClientUpdateTargetPeers = *req.Settings.ClientUpdateTargetPeers
	}
	if req.Settings.ClientUpdateExcludeGroups != nil {
		settings.ClientUpdateExcludeGroups = *req.Settings.ClientUpdateExcludeGroups
	}
	if req.Settings.ClientUpdateRolloutPercent != nil {
		p := *req.Settings.ClientUpdateRolloutPercent
		if p < 0 || p > 100 {
			util.WriteError(r.Context(), status.Errorf(status.InvalidArgument,
				"client_update_rollout_percent must be between 0 and 100"), w)
			return
		}
		settings.ClientUpdateRolloutPercent = &p
	}

	var onboarding *types.AccountOnboarding
	if req.Onboarding != nil {
		onboarding = &types.AccountOnboarding{
			OnboardingFlowPending: req.Onboarding.OnboardingFlowPending,
			SignupFormPending:     req.Onboarding.SignupFormPending,
		}
	}

	updatedOnboarding, err := h.accountManager.UpdateAccountOnboarding(r.Context(), accountID, userID, onboarding)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	updatedSettings, err := h.accountManager.UpdateAccountSettings(r.Context(), accountID, userID, settings)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	meta, err := h.accountManager.GetAccountMeta(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	resp := toAccountResponse(accountID, updatedSettings, meta, updatedOnboarding)

	util.WriteJSONObject(r.Context(), w, &resp)
}

// deleteAccount is a HTTP DELETE handler to delete an account
func (h *handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	vars := mux.Vars(r)
	targetAccountID := vars["accountId"]
	if len(targetAccountID) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid account ID"), w)
		return
	}

	err = h.accountManager.DeleteAccount(r.Context(), targetAccountID, userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	util.WriteJSONObject(r.Context(), w, util.EmptyObject{})
}

func toAccountResponse(accountID string, settings *types.Settings, meta *types.AccountMeta, onboarding *types.AccountOnboarding) *api.Account {
	jwtAllowGroups := settings.JWTAllowGroups
	if jwtAllowGroups == nil {
		jwtAllowGroups = []string{}
	}

	admissionPostureChecks := settings.AdmissionPostureChecks
	if admissionPostureChecks == nil {
		admissionPostureChecks = []string{}
	}
	admissionExemptGroups := settings.AdmissionExemptGroups
	if admissionExemptGroups == nil {
		admissionExemptGroups = []string{}
	}

	// Normalise the Q2 targeting slices to [] (not null) so the
	// dashboard always gets arrays, mirroring admissionExemptGroups.
	clientUpdateTargetGroups := settings.ClientUpdateTargetGroups
	if clientUpdateTargetGroups == nil {
		clientUpdateTargetGroups = []string{}
	}
	clientUpdateTargetPeers := settings.ClientUpdateTargetPeers
	if clientUpdateTargetPeers == nil {
		clientUpdateTargetPeers = []string{}
	}
	clientUpdateExcludeGroups := settings.ClientUpdateExcludeGroups
	if clientUpdateExcludeGroups == nil {
		clientUpdateExcludeGroups = []string{}
	}

	apiSettings := api.AccountSettings{
		PeerLoginExpiration:             int(settings.PeerLoginExpiration.Seconds()),
		PeerLoginExpirationEnabled:      settings.PeerLoginExpirationEnabled,
		PeerInactivityExpiration:        int(settings.PeerInactivityExpiration.Seconds()),
		PeerInactivityExpirationEnabled: settings.PeerInactivityExpirationEnabled,
		GroupsPropagationEnabled:        &settings.GroupsPropagationEnabled,
		JwtGroupsEnabled:                &settings.JWTGroupsEnabled,
		JwtGroupsClaimName:              &settings.JWTGroupsClaimName,
		JwtAllowGroups:                  &jwtAllowGroups,
		RegularUsersViewBlocked:         settings.RegularUsersViewBlocked,
		RoutingPeerDnsResolutionEnabled: &settings.RoutingPeerDNSResolutionEnabled,
		LazyConnectionEnabled:           &settings.LazyConnectionEnabled,
		DnsDomain:                       &settings.DNSDomain,
		AdmissionEnforcementEnabled:     &settings.AdmissionEnforcementEnabled,
		AdmissionPostureChecks:          &admissionPostureChecks,
		AdmissionExemptGroups:           &admissionExemptGroups,
		ClientUpdateTargetVersion:       &settings.ClientUpdateTargetVersion,
		ClientUpdateForce:               &settings.ClientUpdateForce,
		ClientUpdateTargetGroups:        &clientUpdateTargetGroups,
		ClientUpdateTargetPeers:         &clientUpdateTargetPeers,
		ClientUpdateExcludeGroups:       &clientUpdateExcludeGroups,
		ClientUpdateRolloutPercent:      settings.ClientUpdateRolloutPercent,
		MfaEnforceLocal:                 &settings.MFAEnforceLocal,
		MfaEnforceFederated:             &settings.MFAEnforceFederated,
	}

	apiOnboarding := api.AccountOnboarding{
		OnboardingFlowPending: onboarding.OnboardingFlowPending,
		SignupFormPending:     onboarding.SignupFormPending,
	}

	if settings.Extra != nil {
		var groups *[]string
		if len(settings.Extra.FlowEventsGroups) > 0 {
			gs := append([]string(nil), settings.Extra.FlowEventsGroups...)
			groups = &gs
		}
		apiSettings.Extra = &api.AccountExtraSettings{
			PeerApprovalEnabled:                settings.Extra.PeerApprovalEnabled,
			NetworkTrafficLogsEnabled:          settings.Extra.FlowEnabled,
			NetworkTrafficPacketCounterEnabled: settings.Extra.FlowPacketCounterEnabled,
			NetworkTrafficLogsGroups:           groups,
		}
		disableDefault := settings.Extra.FlowDisableDefaultPortFilter
		apiSettings.Extra.NetworkTrafficDisableDefaultPortFilter = &disableDefault
		if len(settings.Extra.FlowExcludedPorts) > 0 {
			ports := make([]api.FlowPortFilter, 0, len(settings.Extra.FlowExcludedPorts))
			for _, p := range settings.Extra.FlowExcludedPorts {
				ports = append(ports, api.FlowPortFilter{
					Port:     int(p.Port),
					Protocol: api.FlowPortFilterProtocol(p.Protocol),
				})
			}
			apiSettings.Extra.NetworkTrafficExcludedPorts = &ports
		}
		if settings.Extra.FlowTrafficDefaultRange != "" {
			r := api.AccountExtraSettingsNetworkTrafficDefaultRange(settings.Extra.FlowTrafficDefaultRange)
			apiSettings.Extra.NetworkTrafficDefaultRange = &r
		}
	}

	return &api.Account{
		Id:             accountID,
		Settings:       apiSettings,
		CreatedAt:      meta.CreatedAt,
		CreatedBy:      meta.CreatedBy,
		Domain:         meta.Domain,
		DomainCategory: meta.DomainCategory,
		Onboarding:     apiOnboarding,
	}
}
