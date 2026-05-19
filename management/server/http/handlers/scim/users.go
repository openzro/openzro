package scim

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/types"
)

// User is the SCIM 2.0 User resource as we emit it. RFC 7643 §4.1.
// Optional ECS-style fields are omitted rather than emitted empty.
type User struct {
	Schemas     []string     `json:"schemas"`
	ID          string       `json:"id"`
	ExternalID  string       `json:"externalId,omitempty"`
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

// fromUser projects a types.User onto the SCIM User shape. Fields
// that came from the SCIM provisioning request live on the User
// struct directly (SCIMUserName, SCIMDisplayName, SCIMExternalID),
// so no IdP round-trip is needed to render the response.
func fromUser(u *types.User) User {
	un := u.SCIMUserName
	if un == "" {
		un = u.Id
	}
	out := User{
		Schemas:    []string{SchemaUserURN},
		ID:         u.Id,
		ExternalID: u.SCIMExternalID,
		UserName:   un,
		Active:     !u.Blocked,
		Meta: ResourceMeta{
			ResourceType: "User",
			Location:     "/scim/v2/Users/" + u.Id,
			Created:      u.CreatedAt,
		},
	}
	if u.SCIMDisplayName != "" {
		out.DisplayName = u.SCIMDisplayName
		out.Name = &userName{Formatted: u.SCIMDisplayName}
	}
	if u.SCIMUserName != "" && strings.Contains(u.SCIMUserName, "@") {
		out.Emails = []userEmail{{Value: u.SCIMUserName, Primary: true, Type: "work"}}
	}
	for _, gid := range u.AutoGroups {
		out.Groups = append(out.Groups, userGroup{Value: gid})
	}
	return out
}

// handleListUsers implements GET /Users with optional filter and
// pagination. RFC 7644 §3.4.2.
//
// Supported filter expressions (subset):
//
//	userName eq "alice@example.com"
//
// Anything else is ignored and a full unfiltered list is returned —
// well-behaved IdPs send only equality filters on userName.
func (h *Handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	filter := parseUserNameFilter(r.URL.Query().Get("filter"))
	startIndex, count := parsePaging(r.URL.Query())

	users, total, err := h.accountManager.SCIMListUsers(
		r.Context(), userAuth.AccountId, userAuth.UserId, filter, startIndex, count)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}

	resources := make([]any, 0, len(users))
	for _, u := range users {
		resources = append(resources, fromUser(u))
	}
	writeJSON(w, http.StatusOK, ListResponse{
		Schemas:      []string{SchemaListResponseURN},
		TotalResults: total,
		ItemsPerPage: len(resources),
		StartIndex:   startIndex,
		Resources:    resources,
	})
}

// handleGetUser implements GET /Users/{id}.
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
	user, err := h.accountManager.SCIMGetUser(r.Context(), userAuth.AccountId, userAuth.UserId, id)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fromUser(user))
}

// handleCreateUser implements POST /Users. Returns 201 with the
// created resource and a Location header.
func (h *Handler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	body, err := decodeUserBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input := body.toInput()
	created, err := h.accountManager.SCIMCreateUser(r.Context(), userAuth.AccountId, userAuth.UserId, input)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	w.Header().Set("Location", "/scim/v2/Users/"+created.Id)
	writeJSON(w, http.StatusCreated, fromUser(created))
}

// handleReplaceUser implements PUT /Users/{id} (full replacement).
func (h *Handler) handleReplaceUser(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	body, err := decodeUserBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.accountManager.SCIMReplaceUser(r.Context(), userAuth.AccountId, userAuth.UserId, id, body.toInput())
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fromUser(updated))
}

// patchOpRequest is the wire shape for PATCH per RFC 7644 §3.5.2.
type patchOpRequest struct {
	Schemas    []string  `json:"schemas"`
	Operations []patchOp `json:"Operations"`
}

type patchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

// handlePatchUser implements PATCH /Users/{id}. Supports replace on
// `active`, `userName`, and `displayName` — the operations IdPs
// actually send. Other paths are ignored (log only) so unknown
// extensions don't 400.
func (h *Handler) handlePatchUser(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])

	var body patchOpRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid PATCH body")
		return
	}

	patch := account.SCIMUserPatch{}
	for _, op := range body.Operations {
		// SCIM is case-insensitive on op and path per RFC.
		opName := strings.ToLower(op.Op)
		path := strings.ToLower(strings.TrimSpace(op.Path))

		// Most IdPs send `op=replace, path=active, value=true`.
		// Some send a single op without path and a value object.
		if opName != "replace" && opName != "add" {
			continue
		}

		if path == "" {
			// Whole-resource value: parse as an attribute bag.
			var bag map[string]json.RawMessage
			if err := json.Unmarshal(op.Value, &bag); err != nil {
				continue
			}
			applyPatchBag(&patch, bag)
			continue
		}

		switch path {
		case "active":
			var v bool
			if err := json.Unmarshal(op.Value, &v); err == nil {
				patch.Active = &v
			}
		case "username":
			var v string
			if err := json.Unmarshal(op.Value, &v); err == nil {
				patch.UserName = &v
			}
		case "displayname", "name.formatted":
			var v string
			if err := json.Unmarshal(op.Value, &v); err == nil {
				patch.DisplayName = &v
			}
		}
	}

	updated, err := h.accountManager.SCIMPatchUser(r.Context(), userAuth.AccountId, userAuth.UserId, id, patch)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fromUser(updated))
}

// applyPatchBag handles the "single replace with a JSON object" form
// that some IdPs (notably older Okta connectors) send.
func applyPatchBag(out *account.SCIMUserPatch, bag map[string]json.RawMessage) {
	for k, raw := range bag {
		switch strings.ToLower(k) {
		case "active":
			var v bool
			if err := json.Unmarshal(raw, &v); err == nil {
				out.Active = &v
			}
		case "username":
			var v string
			if err := json.Unmarshal(raw, &v); err == nil {
				out.UserName = &v
			}
		case "displayname":
			var v string
			if err := json.Unmarshal(raw, &v); err == nil {
				out.DisplayName = &v
			}
		}
	}
}

// handleDeleteUser implements DELETE /Users/{id} as a soft delete
// (active=false). Some IdPs expect 204 No Content; we return 204.
func (h *Handler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if err := h.accountManager.SCIMDeactivateUser(r.Context(), userAuth.AccountId, userAuth.UserId, id); err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// userBody is the wire shape for POST/PUT /Users. Only the fields
// SCIM-managed IdPs actually send are bound.
type userBody struct {
	Schemas     []string    `json:"schemas"`
	UserName    string      `json:"userName"`
	ExternalID  string      `json:"externalId"`
	Active      *bool       `json:"active"`
	DisplayName string      `json:"displayName"`
	Name        *userName   `json:"name"`
	Emails      []userEmail `json:"emails"`
}

func (b userBody) toInput() account.SCIMUserInput {
	in := account.SCIMUserInput{
		UserName:    b.UserName,
		DisplayName: b.DisplayName,
		ExternalID:  b.ExternalID,
		Active:      true,
	}
	if b.Active != nil {
		in.Active = *b.Active
	}
	// Prefer name.formatted if displayName is empty (Okta sends both).
	if in.DisplayName == "" && b.Name != nil {
		in.DisplayName = b.Name.Formatted
	}
	// If userName looks like an empty string but emails have a primary,
	// fall back to the primary email.
	if in.UserName == "" {
		for _, e := range b.Emails {
			if e.Primary {
				in.UserName = e.Value
				break
			}
		}
	}
	return in
}

func decodeUserBody(r *http.Request) (userBody, error) {
	var b userBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		return b, errors.New("invalid JSON body")
	}
	if b.UserName == "" && len(b.Emails) == 0 {
		return b, errors.New("userName or emails required")
	}
	return b, nil
}

func parseUserNameFilter(raw string) string {
	// Recognize: userName eq "value" — case-insensitive on the
	// attribute name and operator. Anything else: empty filter.
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Tokenize at whitespace, accept optional quotes around the value.
	parts := strings.Fields(s)
	if len(parts) < 3 {
		return ""
	}
	if !strings.EqualFold(parts[0], "userName") {
		return ""
	}
	if !strings.EqualFold(parts[1], "eq") {
		return ""
	}
	val := strings.Join(parts[2:], " ")
	val = strings.TrimSpace(val)
	val = strings.Trim(val, "\"'")
	return val
}

func parsePaging(q map[string][]string) (startIndex, count int) {
	startIndex = 1
	count = 100
	if vs, ok := q["startIndex"]; ok && len(vs) > 0 {
		if n, err := strconv.Atoi(vs[0]); err == nil && n > 0 {
			startIndex = n
		}
	}
	if vs, ok := q["count"]; ok && len(vs) > 0 {
		if n, err := strconv.Atoi(vs[0]); err == nil && n >= 0 {
			count = n
		}
	}
	return startIndex, count
}

// mapErrorStatus translates known SCIM errors to HTTP status codes.
// Anything else is 500. The SCIM-specific 409 for "already exists"
// matters because many IdP retry loops back off on 409 but treat
// 500 as a hard outage.
func mapErrorStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case strings.Contains(err.Error(), "already exists"):
		return http.StatusConflict
	case strings.Contains(err.Error(), "not found"):
		return http.StatusNotFound
	case strings.Contains(err.Error(), "unauthenticated"):
		return http.StatusUnauthorized
	case strings.Contains(err.Error(), "permission"):
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}
