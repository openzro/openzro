package scim

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/types"
)

// User is the SCIM 2.0 User resource as we emit it. RFC 7643 §4.1.
// Only the attributes openZro actually has data for are populated;
// optional ones (phoneNumbers, addresses, x509Certificates, …) are
// omitted rather than emitted empty.
type User struct {
	Schemas     []string     `json:"schemas"`
	ID          string       `json:"id"`
	UserName    string       `json:"userName"`
	Active      bool         `json:"active"`
	DisplayName string       `json:"displayName,omitempty"`
	Name        *userName    `json:"name,omitempty"`
	Emails      []userEmail  `json:"emails,omitempty"`
	Groups      []userGroup  `json:"groups,omitempty"`
	Meta        ResourceMeta `json:"meta"`
}

type userName struct {
	Formatted string `json:"formatted,omitempty"`
}

type userEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

type userGroup struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

// toSCIMUser projects an openZro UserInfo onto the SCIM User shape.
// userName is the email when present (Okta/Entra both expect that),
// falling back to the openZro user ID for service users without an
// email.
func toSCIMUser(u *types.UserInfo) User {
	un := u.Email
	if un == "" {
		un = u.ID
	}
	out := User{
		Schemas:  []string{SchemaUserURN},
		ID:       u.ID,
		UserName: un,
		Active:   !u.IsBlocked,
		Meta: ResourceMeta{
			ResourceType: "User",
			Location:     "/scim/v2/Users/" + u.ID,
		},
	}
	if u.Name != "" {
		out.DisplayName = u.Name
		out.Name = &userName{Formatted: u.Name}
	}
	if u.Email != "" {
		out.Emails = []userEmail{{Value: u.Email, Primary: true, Type: "work"}}
	}
	for _, gid := range u.AutoGroups {
		out.Groups = append(out.Groups, userGroup{Value: gid})
	}
	return out
}

// handleListUsers returns every user in the caller's account in the
// SCIM ListResponse envelope. Filtering and pagination are not yet
// supported — ServiceProviderConfig advertises that, so well-behaved
// IdPs do a single full fetch. Callers that send `?filter=...` get
// the unfiltered list, which is a strict superset of the requested one
// and so still correct, just inefficient.
func (h *Handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	users, err := h.accountManager.GetUsersFromAccount(r.Context(), userAuth.AccountId, userAuth.UserId)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	resources := make([]any, 0, len(users))
	for _, u := range users {
		if u.NonDeletable {
			continue
		}
		resources = append(resources, toSCIMUser(u))
	}
	writeJSON(w, http.StatusOK, ListResponse{
		Schemas:      []string{SchemaListResponseURN},
		TotalResults: len(resources),
		ItemsPerPage: len(resources),
		StartIndex:   1,
		Resources:    resources,
	})
}

// handleGetUser returns a single user by ID. Returns 404 if the user
// is not in the caller's account — never reveals existence in another
// account.
func (h *Handler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing user id")
		return
	}

	users, err := h.accountManager.GetUsersFromAccount(r.Context(), userAuth.AccountId, userAuth.UserId)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	u, ok := users[id]
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, toSCIMUser(u))
}
