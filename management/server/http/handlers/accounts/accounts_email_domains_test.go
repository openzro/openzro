package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/settings"
	"github.com/openzro/openzro/management/server/types"
)

const emailDomainsAccountID = "test_account"

func putAccountSettings(t *testing.T, h *handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/accounts/"+emailDomainsAccountID, bytes.NewBufferString(body))
	req = nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
		UserId:    "test_user",
		AccountId: emailDomainsAccountID,
	})
	router := mux.NewRouter()
	router.HandleFunc("/api/accounts/{accountId}", h.updateAccount).Methods("PUT")
	router.ServeHTTP(recorder, req)
	return recorder
}

// ADR-0025: allowed_email_domains round-trips, and a client that omits the
// field keeps the stored list instead of silently disabling the control.
func TestAccounts_PutAllowedEmailDomains(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		existing []string
		want     []string
	}{
		{name: "omitted preserves existing", field: "", existing: []string{"example.com"}, want: []string{"example.com"}},
		{name: "explicit list replaces", field: `,"allowed_email_domains":["new.com"]`, existing: []string{"example.com"}, want: []string{"new.com"}},
		{name: "explicit empty clears", field: `,"allowed_email_domains":[]`, existing: []string{"example.com"}, want: []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adminUser := types.NewAdminUser("test_user")
			account := &types.Account{
				Id:      emailDomainsAccountID,
				Network: types.NewNetwork(),
				Users:   map[string]*types.User{adminUser.Id: adminUser},
				Settings: &types.Settings{
					PeerLoginExpiration: time.Hour,
					AllowedEmailDomains: tc.existing,
					Extra:               &types.ExtraSettings{},
				},
			}
			h := initAccountsTestData(t, account)

			var captured *types.Settings
			h.accountManager.(*mock_server.MockAccountManager).UpdateAccountSettingsFunc =
				func(_ context.Context, _, _ string, newSettings *types.Settings) (*types.Settings, error) {
					captured = newSettings
					return newSettings, nil
				}

			body := `{"settings":{"peer_login_expiration":3600,"peer_login_expiration_enabled":false` + tc.field + `}}`
			recorder := putAccountSettings(t, h, body)
			require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())

			require.NotNil(t, captured, "UpdateAccountSettings was not called")
			assert.ElementsMatch(t, tc.want, captured.AllowedEmailDomains)

			var got api.Account
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &got))
			require.NotNil(t, got.Settings.AllowedEmailDomains, "response must always carry the list")
			assert.ElementsMatch(t, tc.want, *got.Settings.AllowedEmailDomains)
		})
	}
}

// ADR-0025: if the stored list cannot be read, an update that omits the
// field must fail rather than store an empty (disabled) allowlist.
func TestAccounts_PutAllowedEmailDomains_LookupFailureAborts(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	settingsMock := settings.NewMockManager(ctrl)
	settingsMock.EXPECT().GetSettings(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("store unavailable")).AnyTimes()

	adminUser := types.NewAdminUser("test_user")
	h := initAccountsTestData(t, &types.Account{
		Id:       emailDomainsAccountID,
		Network:  types.NewNetwork(),
		Users:    map[string]*types.User{adminUser.Id: adminUser},
		Settings: &types.Settings{PeerLoginExpiration: time.Hour, Extra: &types.ExtraSettings{}},
	})
	h.settingsManager = settingsMock

	updated := false
	h.accountManager.(*mock_server.MockAccountManager).UpdateAccountSettingsFunc =
		func(_ context.Context, _, _ string, s *types.Settings) (*types.Settings, error) {
			updated = true
			return s, nil
		}

	recorder := putAccountSettings(t, h, `{"settings":{"peer_login_expiration":3600,"peer_login_expiration_enabled":false}}`)
	assert.NotEqual(t, http.StatusOK, recorder.Code)
	assert.False(t, updated, "settings must not be written when the allowlist could not be preserved")
}
