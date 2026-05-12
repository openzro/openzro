package accounts

import (
	"encoding/json"
	"net/http"
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
