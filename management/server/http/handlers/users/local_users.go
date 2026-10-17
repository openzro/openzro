package users

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// Console management of local users (ADR-0026). Each handler translates the
// request and calls the same manager sagas SCIM uses (ADR-0024), so locks,
// ownership checks and error mapping are identical. Passwords are never
// written to a response or an error.

// createLocalUser handles POST /users/local.
func (h *handler) createLocalUser(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	var req api.LocalUserCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}
	if req.Password == "" {
		util.WriteError(r.Context(), status.Errorf(status.BadRequest, "password is required"), w)
		return
	}
	name := ""
	if req.Name != nil {
		name = *req.Name
	}
	autoGroups := req.AutoGroups
	if autoGroups == nil {
		autoGroups = []string{}
	}
	user, err := h.accountManager.SCIMCreateLocalUser(r.Context(), userAuth.AccountId, userAuth.UserId,
		account.SCIMUserInput{UserName: req.Email, DisplayName: name, Active: true, AutoGroups: autoGroups},
		req.Password)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	writeLocalUser(w, r, user, userAuth.UserId)
}

// resetLocalUserPassword handles POST /users/{userId}/password.
func (h *handler) resetLocalUserPassword(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	var req api.LocalUserPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}
	if req.Password == "" {
		util.WriteError(r.Context(), status.Errorf(status.BadRequest, "password is required"), w)
		return
	}
	user, err := h.accountManager.SCIMResetLocalPassword(r.Context(), userAuth.AccountId, userAuth.UserId,
		mux.Vars(r)["userId"], req.Password, nil)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	writeLocalUser(w, r, user, userAuth.UserId)
}

// deactivateLocalUser handles POST /users/{userId}/deactivate. It is
// permanent: a local user cannot be re-activated (ADR-0024 D7).
func (h *handler) deactivateLocalUser(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	user, err := h.accountManager.DeactivateLocalUser(r.Context(), userAuth.AccountId, userAuth.UserId, mux.Vars(r)["userId"])
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	writeLocalUser(w, r, user, userAuth.UserId)
}

// getLocalUserConfig handles GET /users/local/config.
func (h *handler) getLocalUserConfig(w http.ResponseWriter, r *http.Request) {
	if _, err := nbcontext.GetUserAuthFromContext(r.Context()); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, api.LocalUserConfig{CreationEnabled: h.accountManager.LocalUserCreationEnabled()})
}

func writeLocalUser(w http.ResponseWriter, r *http.Request, user *types.User, currentUserID string) {
	info, err := user.ToUserInfo(nil)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toUserResponse(info, currentUserID))
}
