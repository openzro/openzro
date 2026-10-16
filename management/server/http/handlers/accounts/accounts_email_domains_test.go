package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/mock_server"
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
					captured = newSettings.Copy()
					// Mirror the real manager's transactional preserve-on-omit
					// behavior so the handler response contains the stored value.
					if newSettings.AllowedEmailDomains == nil {
						newSettings.AllowedEmailDomains = append([]string(nil), tc.existing...)
					}
					return newSettings, nil
				}

			body := `{"settings":{"peer_login_expiration":3600,"peer_login_expiration_enabled":false` + tc.field + `}}`
			recorder := putAccountSettings(t, h, body)
			require.Equal(t, http.StatusOK, recorder.Code, "body=%s", recorder.Body.String())

			require.NotNil(t, captured, "UpdateAccountSettings was not called")
			if tc.field == "" {
				assert.Nil(t, captured.AllowedEmailDomains,
					"omission must reach the manager as nil for transactional preservation")
			} else {
				assert.ElementsMatch(t, tc.want, captured.AllowedEmailDomains)
			}

			var got api.Account
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &got))
			require.NotNil(t, got.Settings.AllowedEmailDomains, "response must always carry the list")
			assert.ElementsMatch(t, tc.want, *got.Settings.AllowedEmailDomains)
		})
	}
}
