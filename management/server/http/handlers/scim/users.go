package scim

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/status"
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
	var created *types.User
	if body.hasPassword() {
		password, ok := body.passwordString()
		if !ok {
			writeError(w, http.StatusBadRequest, "password must be a string")
			return
		}
		created, err = h.accountManager.SCIMCreateLocalUser(r.Context(), userAuth.AccountId, userAuth.UserId,
			input, password)
	} else {
		created, err = h.accountManager.SCIMCreateUser(r.Context(), userAuth.AccountId, userAuth.UserId, input)
	}
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
	if body.hasPassword() {
		// A PUT that carries a password is a password reset; the rest of the
		// body must match the current representation (checked by the manager).
		password, ok := body.passwordString()
		if !ok {
			writeError(w, http.StatusBadRequest, "password must be a string")
			return
		}
		input := body.toInput()
		updated, err := h.accountManager.SCIMResetLocalPassword(r.Context(), userAuth.AccountId, userAuth.UserId,
			id, password, &input)
		if err != nil {
			writeError(w, mapErrorStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, fromUser(updated))
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

	if password, handled, errMsg := passwordResetFromPatch(body.Operations); handled {
		if errMsg != "" {
			writeError(w, http.StatusBadRequest, errMsg)
			return
		}
		updated, err := h.accountManager.SCIMResetLocalPassword(r.Context(), userAuth.AccountId, userAuth.UserId,
			id, password, nil)
		if err != nil {
			writeError(w, mapErrorStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, fromUser(updated))
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
			if err := applyPatchBag(&patch, bag); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			continue
		}

		// A recognized attribute with a malformed value rejects the whole
		// request: dropping it would apply the rest (for a local user, an
		// irreversible deactivation) and report success.
		if err := applyPatchAttribute(&patch, path, op.Value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	updated, err := h.accountManager.SCIMPatchUser(r.Context(), userAuth.AccountId, userAuth.UserId, id, patch)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fromUser(updated))
}

// hasPassword reports whether the request carried the password attribute
// at all, whatever its value (null included).
func (b userBody) hasPassword() bool {
	return len(b.Password) > 0
}

// passwordString returns the password when it is a JSON string. null and
// other types are not passwords.
func (b userBody) passwordString() (string, bool) {
	if string(b.Password) == "null" {
		return "", false
	}
	var v string
	if err := json.Unmarshal(b.Password, &v); err != nil {
		return "", false
	}
	return v, true
}

// errPasswordNotIsolated is returned when a PATCH mentions the password
// alongside anything else, or with an op other than replace: a password
// reset must be an isolated operation (ADR-0024 D6), and a mentioned
// password is never silently dropped.
const errPasswordNotIsolated = "a password can only be set by a PATCH whose single operation replaces it"

// passwordResetFromPatch inspects PATCH operations for the password. handled
// is false when no operation mentions it. Otherwise either errMsg explains
// why the request is refused, or password is the value to set: the only
// accepted shape is a single replace, by path or by an attribute bag that
// holds nothing but the password.
func passwordResetFromPatch(ops []patchOp) (password string, handled bool, errMsg string) {
	mentions := false
	for _, op := range ops {
		if strings.EqualFold(strings.TrimSpace(op.Path), "password") || opBagHasPassword(op) {
			mentions = true
			break
		}
	}
	if !mentions {
		return "", false, ""
	}
	if len(ops) != 1 || !strings.EqualFold(ops[0].Op, "replace") {
		return "", true, errPasswordNotIsolated
	}
	raw := ops[0].Value
	if strings.TrimSpace(ops[0].Path) == "" {
		var bag map[string]json.RawMessage
		if err := json.Unmarshal(raw, &bag); err != nil || len(bag) != 1 {
			return "", true, errPasswordNotIsolated
		}
		for _, v := range bag {
			raw = v
		}
	}
	if string(raw) == "null" {
		return "", true, "password must be a string"
	}
	if err := json.Unmarshal(raw, &password); err != nil {
		return "", true, "password must be a string"
	}
	return password, true, ""
}

func opBagHasPassword(op patchOp) bool {
	if strings.TrimSpace(op.Path) != "" {
		return false
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(op.Value, &bag); err != nil {
		return false
	}
	for k := range bag {
		if strings.EqualFold(k, "password") {
			return true
		}
	}
	return false
}

// applyPatchBag handles the "single replace with a JSON object" form
// that some IdPs (notably older Okta connectors) send. Unknown keys are
// ignored; a recognized key with a malformed value is an error.
func applyPatchBag(out *account.SCIMUserPatch, bag map[string]json.RawMessage) error {
	for k, raw := range bag {
		if err := applyPatchAttribute(out, strings.ToLower(k), raw); err != nil {
			return err
		}
	}
	return nil
}

// applyPatchAttribute applies one recognized user attribute; other paths
// are ignored so unknown extensions don't 400.
func applyPatchAttribute(out *account.SCIMUserPatch, path string, raw json.RawMessage) error {
	switch path {
	case "active":
		v, err := parseSCIMBool(raw)
		if err != nil {
			return fmt.Errorf("active must be a boolean")
		}
		out.Active = &v
	case "username":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("userName must be a string")
		}
		out.UserName = &v
	case "displayname", "name.formatted":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("displayName must be a string")
		}
		out.DisplayName = &v
	}
	return nil
}

// parseSCIMBool accepts a JSON boolean or the strings "true"/"false" in any
// case: Microsoft Entra ID sends booleans as strings in PATCH operations.
func parseSCIMBool(raw json.RawMessage) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("not a boolean: %q", s)
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
	// Password is the RFC 7643 §4.1.1 writeOnly attribute. Only a create
	// accepts it (a local user, ADR-0024); it is never returned. Raw so an
	// omitted field can be told apart from one present as null or another
	// type: any presence must be handled, never silently ignored.
	Password json.RawMessage `json:"password"`
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
	if err == nil {
		return http.StatusOK
	}
	// Typed errors from the account manager carry their class; map it
	// before falling back to the message match below, which only exists
	// for the plain sentinels this package cannot import (cycle).
	if sErr, ok := status.FromError(err); ok {
		switch sErr.Type() {
		case status.InvalidArgument, status.BadRequest, status.PreconditionFailed:
			return http.StatusBadRequest
		case status.PermissionDenied:
			return http.StatusForbidden
		case status.NotFound:
			return http.StatusNotFound
		case status.AlreadyExists, status.UserAlreadyExists:
			return http.StatusConflict
		case status.Unauthenticated, status.Unauthorized:
			return http.StatusUnauthorized
		default:
			return http.StatusInternalServerError
		}
	}
	switch {
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
