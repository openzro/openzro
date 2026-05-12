package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/settings"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

func initAccountsTestData(t *testing.T, account *types.Account) *handler {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	settingsMockManager := settings.NewMockManager(ctrl)
	settingsMockManager.EXPECT().
		GetSettings(gomock.Any(), account.Id, "test_user").
		Return(account.Settings, nil).
		AnyTimes()

	return &handler{
		accountManager: &mock_server.MockAccountManager{
			GetAccountSettingsFunc: func(ctx context.Context, accountID string, userID string) (*types.Settings, error) {
				return account.Settings, nil
			},
			UpdateAccountSettingsFunc: func(ctx context.Context, accountID, userID string, newSettings *types.Settings) (*types.Settings, error) {
				halfYearLimit := 180 * 24 * time.Hour
				if newSettings.PeerLoginExpiration > halfYearLimit {
					return nil, status.Errorf(status.InvalidArgument, "peer login expiration can't be larger than 180 days")
				}

				if newSettings.PeerLoginExpiration < time.Hour {
					return nil, status.Errorf(status.InvalidArgument, "peer login expiration can't be smaller than one hour")
				}

				return newSettings, nil
			},
			GetAccountByIDFunc: func(ctx context.Context, accountID string, userID string) (*types.Account, error) {
				return account.Copy(), nil
			},
			GetAccountMetaFunc: func(ctx context.Context, accountID string, userID string) (*types.AccountMeta, error) {
				return account.GetMeta(), nil
			},
			GetAccountOnboardingFunc: func(ctx context.Context, accountID string, userID string) (*types.AccountOnboarding, error) {
				return &types.AccountOnboarding{
					OnboardingFlowPending: true,
					SignupFormPending:     true,
				}, nil
			},
			UpdateAccountOnboardingFunc: func(ctx context.Context, accountID, userID string, onboarding *types.AccountOnboarding) (*types.AccountOnboarding, error) {
				return &types.AccountOnboarding{
					OnboardingFlowPending: true,
					SignupFormPending:     true,
				}, nil
			},
		},
		settingsManager: settingsMockManager,
	}
}

func TestAccounts_AccountsHandler(t *testing.T) {
	accountID := "test_account"
	adminUser := types.NewAdminUser("test_user")

	sr := func(v string) *string { return &v }
	br := func(v bool) *bool { return &v }

	handler := initAccountsTestData(t, &types.Account{
		Id:      accountID,
		Domain:  "hotmail.com",
		Network: types.NewNetwork(),
		Users: map[string]*types.User{
			adminUser.Id: adminUser,
		},
		Settings: &types.Settings{
			PeerLoginExpirationEnabled: false,
			PeerLoginExpiration:        time.Hour,
			RegularUsersViewBlocked:    true,
		},
	})

	tt := []struct {
		name             string
		expectedStatus   int
		expectedBody     bool
		expectedID       string
		expectedArray    bool
		expectedSettings api.AccountSettings
		requestType      string
		requestPath      string
		requestBody      io.Reader
	}{
		{
			name:           "getAllAccounts OK",
			expectedBody:   true,
			requestType:    http.MethodGet,
			requestPath:    "/api/accounts",
			expectedStatus: http.StatusOK,
			expectedSettings: api.AccountSettings{
				PeerLoginExpiration:             int(time.Hour.Seconds()),
				PeerLoginExpirationEnabled:      false,
				GroupsPropagationEnabled:        br(false),
				JwtGroupsClaimName:              sr(""),
				JwtGroupsEnabled:                br(false),
				JwtAllowGroups:                  &[]string{},
				RegularUsersViewBlocked:         true,
				RoutingPeerDnsResolutionEnabled: br(false),
				LazyConnectionEnabled:           br(false),
				DnsDomain:                       sr(""),
				AdmissionEnforcementEnabled:     br(false),
				AdmissionPostureChecks:          &[]string{},
				AdmissionExemptGroups:           &[]string{},
			},
			expectedArray: true,
			expectedID:    accountID,
		},
		{
			name:           "PutAccount OK",
			expectedBody:   true,
			requestType:    http.MethodPut,
			requestPath:    "/api/accounts/" + accountID,
			requestBody:    bytes.NewBufferString("{\"settings\": {\"peer_login_expiration\": 15552000,\"peer_login_expiration_enabled\": true},\"onboarding\": {\"onboarding_flow_pending\": true,\"signup_form_pending\": true}}"),
			expectedStatus: http.StatusOK,
			expectedSettings: api.AccountSettings{
				PeerLoginExpiration:             15552000,
				PeerLoginExpirationEnabled:      true,
				GroupsPropagationEnabled:        br(false),
				JwtGroupsClaimName:              sr(""),
				JwtGroupsEnabled:                br(false),
				JwtAllowGroups:                  &[]string{},
				RegularUsersViewBlocked:         false,
				RoutingPeerDnsResolutionEnabled: br(false),
				LazyConnectionEnabled:           br(false),
				DnsDomain:                       sr(""),
				AdmissionEnforcementEnabled:     br(false),
				AdmissionPostureChecks:          &[]string{},
				AdmissionExemptGroups:           &[]string{},
			},
			expectedArray: false,
			expectedID:    accountID,
		},
		{
			name:           "PutAccount OK with JWT",
			expectedBody:   true,
			requestType:    http.MethodPut,
			requestPath:    "/api/accounts/" + accountID,
			requestBody:    bytes.NewBufferString("{\"settings\": {\"peer_login_expiration\": 15552000,\"peer_login_expiration_enabled\": false,\"jwt_groups_enabled\":true,\"jwt_groups_claim_name\":\"roles\",\"jwt_allow_groups\":[\"test\"],\"regular_users_view_blocked\":true},\"onboarding\": {\"onboarding_flow_pending\": true,\"signup_form_pending\": true}}"),
			expectedStatus: http.StatusOK,
			expectedSettings: api.AccountSettings{
				PeerLoginExpiration:             15552000,
				PeerLoginExpirationEnabled:      false,
				GroupsPropagationEnabled:        br(false),
				JwtGroupsClaimName:              sr("roles"),
				JwtGroupsEnabled:                br(true),
				JwtAllowGroups:                  &[]string{"test"},
				RegularUsersViewBlocked:         true,
				RoutingPeerDnsResolutionEnabled: br(false),
				LazyConnectionEnabled:           br(false),
				DnsDomain:                       sr(""),
				AdmissionEnforcementEnabled:     br(false),
				AdmissionPostureChecks:          &[]string{},
				AdmissionExemptGroups:           &[]string{},
			},
			expectedArray: false,
			expectedID:    accountID,
		},
		{
			name:           "PutAccount OK with JWT Propagation",
			expectedBody:   true,
			requestType:    http.MethodPut,
			requestPath:    "/api/accounts/" + accountID,
			requestBody:    bytes.NewBufferString("{\"settings\": {\"peer_login_expiration\": 554400,\"peer_login_expiration_enabled\": true,\"jwt_groups_enabled\":true,\"jwt_groups_claim_name\":\"groups\",\"groups_propagation_enabled\":true,\"regular_users_view_blocked\":true},\"onboarding\": {\"onboarding_flow_pending\": true,\"signup_form_pending\": true}}"),
			expectedStatus: http.StatusOK,
			expectedSettings: api.AccountSettings{
				PeerLoginExpiration:             554400,
				PeerLoginExpirationEnabled:      true,
				GroupsPropagationEnabled:        br(true),
				JwtGroupsClaimName:              sr("groups"),
				JwtGroupsEnabled:                br(true),
				JwtAllowGroups:                  &[]string{},
				RegularUsersViewBlocked:         true,
				RoutingPeerDnsResolutionEnabled: br(false),
				LazyConnectionEnabled:           br(false),
				DnsDomain:                       sr(""),
				AdmissionEnforcementEnabled:     br(false),
				AdmissionPostureChecks:          &[]string{},
				AdmissionExemptGroups:           &[]string{},
			},
			expectedArray: false,
			expectedID:    accountID,
		},
		{
			name:           "PutAccount OK without onboarding",
			expectedBody:   true,
			requestType:    http.MethodPut,
			requestPath:    "/api/accounts/" + accountID,
			requestBody:    bytes.NewBufferString("{\"settings\": {\"peer_login_expiration\": 15552000,\"peer_login_expiration_enabled\": false,\"jwt_groups_enabled\":true,\"jwt_groups_claim_name\":\"roles\",\"jwt_allow_groups\":[\"test\"],\"regular_users_view_blocked\":true}}"),
			expectedStatus: http.StatusOK,
			expectedSettings: api.AccountSettings{
				PeerLoginExpiration:             15552000,
				PeerLoginExpirationEnabled:      false,
				GroupsPropagationEnabled:        br(false),
				JwtGroupsClaimName:              sr("roles"),
				JwtGroupsEnabled:                br(true),
				JwtAllowGroups:                  &[]string{"test"},
				RegularUsersViewBlocked:         true,
				RoutingPeerDnsResolutionEnabled: br(false),
				LazyConnectionEnabled:           br(false),
				DnsDomain:                       sr(""),
				AdmissionEnforcementEnabled:     br(false),
				AdmissionPostureChecks:          &[]string{},
				AdmissionExemptGroups:           &[]string{},
			},
			expectedArray: false,
			expectedID:    accountID,
		},
		{
			name:           "Update account failure with high peer_login_expiration more than 180 days",
			expectedBody:   true,
			requestType:    http.MethodPut,
			requestPath:    "/api/accounts/" + accountID,
			requestBody:    bytes.NewBufferString("{\"settings\": {\"peer_login_expiration\": 15552001,\"peer_login_expiration_enabled\": true},\"onboarding\": {\"onboarding_flow_pending\": true,\"signup_form_pending\": true}}"),
			expectedStatus: http.StatusUnprocessableEntity,
			expectedArray:  false,
		},
		{
			name:           "Update account failure with peer_login_expiration less than an hour",
			expectedBody:   true,
			requestType:    http.MethodPut,
			requestPath:    "/api/accounts/" + accountID,
			requestBody:    bytes.NewBufferString("{\"settings\": {\"peer_login_expiration\": 3599,\"peer_login_expiration_enabled\": true},\"onboarding\": {\"onboarding_flow_pending\": true,\"signup_form_pending\": true}}"),
			expectedStatus: http.StatusUnprocessableEntity,
			expectedArray:  false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(tc.requestType, tc.requestPath, tc.requestBody)
			req = nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
				UserId:    adminUser.Id,
				AccountId: accountID,
				Domain:    "hotmail.com",
			})

			router := mux.NewRouter()
			router.HandleFunc("/api/accounts", handler.getAllAccounts).Methods("GET")
			router.HandleFunc("/api/accounts/{accountId}", handler.updateAccount).Methods("PUT")
			router.ServeHTTP(recorder, req)

			res := recorder.Result()
			defer res.Body.Close()

			if status := recorder.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tc.expectedStatus)
				return
			}

			if tc.expectedStatus != http.StatusOK {
				return
			}

			if !tc.expectedBody {
				return
			}

			content, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("I don't know what I expected; %v", err)
			}

			var actual *api.Account
			if tc.expectedArray {
				var got []*api.Account
				if err = json.Unmarshal(content, &got); err != nil {
					t.Fatalf("Sent content is not in correct json format; %v", err)
				}

				assert.Len(t, got, 1)
				actual = got[0]
			} else {
				if err = json.Unmarshal(content, &actual); err != nil {
					t.Fatalf("Sent content is not in correct json format; %v", err)
				}
			}

			assert.Equal(t, tc.expectedID, actual.Id)
			assert.Equal(t, tc.expectedSettings, actual.Settings)
		})
	}
}

// TestAccounts_PutPreservesUnexposedFlowFlags is a regression test for
// the bug where the PUT /accounts/{id} handler rebuilt ExtraSettings
// from the wire shape and silently zeroed the Flow* flags that the
// OpenAPI spec does not surface (FlowDnsCollectionEnabled,
// FlowENCollectionEnabled). The dashboard's NetworkSettingsTab never
// sends those fields, so every "Save" call would reset them to false
// and disable per-flow DNS / exit-node collection until the operator
// hand-edited the DB.
//
// The fix: load existing settings, override only what the request
// declared. The non-pointer bools in the API contract (FlowEnabled,
// FlowPacketCounterEnabled, PeerApprovalEnabled) are still
// authoritative on the wire — operators relying on those flips must
// keep the dashboard's PUT body in sync.
func TestAccounts_PutPreservesUnexposedFlowFlags(t *testing.T) {
	accountID := "test_account"
	adminUser := types.NewAdminUser("test_user")

	existing := &types.Settings{
		PeerLoginExpirationEnabled: false,
		PeerLoginExpiration:        time.Hour,
		Extra: &types.ExtraSettings{
			FlowEnabled:              true,
			FlowPacketCounterEnabled: true,
			FlowDnsCollectionEnabled: true,
			FlowENCollectionEnabled:  true,
			FlowEventsGroups:         []string{"group-a"},
		},
	}

	account := &types.Account{
		Id:      accountID,
		Domain:  "hotmail.com",
		Network: types.NewNetwork(),
		Users: map[string]*types.User{
			adminUser.Id: adminUser,
		},
		Settings: existing,
	}

	handler := initAccountsTestData(t, account)

	// Intercept the settings that the handler hands off to
	// UpdateAccountSettings so we can assert on server-only Extra
	// fields that the OpenAPI response does not surface.
	var captured *types.Settings
	if m, ok := handler.accountManager.(*mock_server.MockAccountManager); ok {
		m.UpdateAccountSettingsFunc = func(ctx context.Context, accountID, userID string, newSettings *types.Settings) (*types.Settings, error) {
			captured = newSettings
			return newSettings, nil
		}
	}

	// Dashboard PUT shape — sends only the fields the v2 NetworkSettingsTab
	// surfaces (logs_enabled, logs_groups). Critically the body omits
	// network_traffic_packet_counter_enabled and the two server-only
	// flags. Before the fix, every one of these collapsed to false.
	body := `{"settings":{"peer_login_expiration":3600,"peer_login_expiration_enabled":false,"extra":{"network_traffic_logs_enabled":true,"network_traffic_logs_groups":["group-a"],"network_traffic_packet_counter_enabled":true}}}`

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/accounts/"+accountID, bytes.NewBufferString(body))
	req = nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
		UserId:    adminUser.Id,
		AccountId: accountID,
		Domain:    "hotmail.com",
	})

	router := mux.NewRouter()
	router.HandleFunc("/api/accounts/{accountId}", handler.updateAccount).Methods("PUT")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code, "handler returned wrong status: body=%s", recorder.Body.String())

	res := recorder.Result()
	defer res.Body.Close()
	content, err := io.ReadAll(res.Body)
	assert.NoError(t, err)

	var got api.Account
	assert.NoError(t, json.Unmarshal(content, &got))

	// Wire-visible fields: dashboard sent true, response echoes true.
	if assert.NotNil(t, got.Settings.Extra) {
		assert.True(t, got.Settings.Extra.NetworkTrafficLogsEnabled, "logs enabled should round-trip")
		assert.True(t, got.Settings.Extra.NetworkTrafficPacketCounterEnabled, "packet counter must persist when dashboard sends it")
	}

	// Server-only fields: dashboard didn't mention them, but the
	// handler must have preserved them from existing settings.
	if assert.NotNil(t, captured, "UpdateAccountSettings was not called") && assert.NotNil(t, captured.Extra) {
		assert.True(t, captured.Extra.FlowDnsCollectionEnabled, "FlowDnsCollectionEnabled must persist across a PUT that does not mention it")
		assert.True(t, captured.Extra.FlowENCollectionEnabled, "FlowENCollectionEnabled must persist across a PUT that does not mention it")
	}
}

// TestAccounts_PutRoundTripsFlowTrafficDefaultRange covers the new
// operator-controlled UX setting: pre-fill the date filter on the
// Flow Traffic dashboard page. Without round-trip the operator
// would set it in the UI, save, refresh, and see it disappear —
// same failure shape as the FlowDnsCollectionEnabled bug we fixed
// earlier.
func TestAccounts_PutRoundTripsFlowTrafficDefaultRange(t *testing.T) {
	accountID := "test_account"
	adminUser := types.NewAdminUser("test_user")

	account := &types.Account{
		Id:      accountID,
		Domain:  "example.org",
		Network: types.NewNetwork(),
		Users: map[string]*types.User{
			adminUser.Id: adminUser,
		},
		Settings: &types.Settings{
			PeerLoginExpirationEnabled: false,
			PeerLoginExpiration:        time.Hour,
			Extra:                      &types.ExtraSettings{},
		},
	}
	handler := initAccountsTestData(t, account)

	var captured *types.Settings
	if m, ok := handler.accountManager.(*mock_server.MockAccountManager); ok {
		m.UpdateAccountSettingsFunc = func(ctx context.Context, accountID, userID string, newSettings *types.Settings) (*types.Settings, error) {
			captured = newSettings
			return newSettings, nil
		}
	}

	body := `{"settings":{"peer_login_expiration":3600,"peer_login_expiration_enabled":false,"extra":{"network_traffic_logs_enabled":true,"network_traffic_packet_counter_enabled":true,"network_traffic_default_range":"24h"}}}`

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/accounts/"+accountID, bytes.NewBufferString(body))
	req = nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
		UserId:    adminUser.Id,
		AccountId: accountID,
		Domain:    "example.org",
	})

	router := mux.NewRouter()
	router.HandleFunc("/api/accounts/{accountId}", handler.updateAccount).Methods("PUT")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())

	res := recorder.Result()
	defer res.Body.Close()
	content, _ := io.ReadAll(res.Body)
	var got api.Account
	assert.NoError(t, json.Unmarshal(content, &got))

	if assert.NotNil(t, got.Settings.Extra) && assert.NotNil(t, got.Settings.Extra.NetworkTrafficDefaultRange) {
		assert.Equal(t, "24h", *got.Settings.Extra.NetworkTrafficDefaultRange)
	}
	if assert.NotNil(t, captured) && assert.NotNil(t, captured.Extra) {
		assert.Equal(t, "24h", captured.Extra.FlowTrafficDefaultRange)
	}
}
