package scim

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/types"
)

// Group is the SCIM 2.0 Group resource. RFC 7643 §4.2.
type Group struct {
	Schemas     []string      `json:"schemas"`
	ID          string        `json:"id"`
	DisplayName string        `json:"displayName"`
	Members     []groupMember `json:"members,omitempty"`
	Meta        ResourceMeta  `json:"meta"`
}

type groupMember struct {
	Value   string `json:"value"`             // user ID
	Display string `json:"display,omitempty"` // optional display name
	Ref     string `json:"$ref,omitempty"`    // optional URL reference
}

func fromGroup(g *types.Group, memberUserIDs []string) Group {
	out := Group{
		Schemas:     []string{SchemaGroupURN},
		ID:          g.ID,
		DisplayName: g.Name,
		Meta: ResourceMeta{
			ResourceType: "Group",
			Location:     "/scim/v2/Groups/" + g.ID,
		},
	}
	for _, uid := range memberUserIDs {
		out.Members = append(out.Members, groupMember{
			Value: uid,
			Ref:   "/scim/v2/Users/" + uid,
		})
	}
	return out
}

// groupBody is the wire shape for POST/PUT.
type groupBody struct {
	Schemas     []string      `json:"schemas"`
	DisplayName string        `json:"displayName"`
	Members     []groupMember `json:"members"`
}

func (b groupBody) memberUserIDs() []string {
	out := make([]string, 0, len(b.Members))
	for _, m := range b.Members {
		if m.Value != "" {
			out = append(out, m.Value)
		}
	}
	return out
}

// handleListGroups: GET /Groups with displayName filter.
func (h *Handler) handleListGroups(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	filter := parseDisplayNameFilter(r.URL.Query().Get("filter"))
	startIndex, count := parsePaging(r.URL.Query())

	groups, total, err := h.accountManager.SCIMListGroups(r.Context(), userAuth.AccountId, userAuth.UserId, filter, startIndex, count)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	resources := make([]any, 0, len(groups))
	for _, g := range groups {
		// List view does not include members — RFC allows excluding
		// them for performance and Okta/Entra both call /Groups/{id}
		// when they need members.
		resources = append(resources, fromGroup(g, nil))
	}
	writeJSON(w, http.StatusOK, ListResponse{
		Schemas:      []string{SchemaListResponseURN},
		TotalResults: total,
		ItemsPerPage: len(resources),
		StartIndex:   startIndex,
		Resources:    resources,
	})
}

func (h *Handler) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	group, members, err := h.accountManager.SCIMGetGroup(r.Context(), userAuth.AccountId, userAuth.UserId, id)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fromGroup(group, members))
}

func (h *Handler) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var body groupBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	created, err := h.accountManager.SCIMCreateGroup(r.Context(), userAuth.AccountId, userAuth.UserId, body.DisplayName, body.memberUserIDs())
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	w.Header().Set("Location", "/scim/v2/Groups/"+created.ID)
	writeJSON(w, http.StatusCreated, fromGroup(created, body.memberUserIDs()))
}

func (h *Handler) handleReplaceGroup(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	var body groupBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	updated, err := h.accountManager.SCIMReplaceGroup(r.Context(), userAuth.AccountId, userAuth.UserId, id, body.DisplayName, body.memberUserIDs())
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fromGroup(updated, body.memberUserIDs()))
}

// groupPatchBody mirrors the SCIM PATCH PatchOp shape.
type groupPatchBody struct {
	Schemas    []string  `json:"schemas"`
	Operations []patchOp `json:"Operations"`
}

// memberPatchValue is the value shape for `add`/`remove` on members.
type memberPatchValue struct {
	Value string `json:"value"`
}

// handlePatchGroup: PATCH /Groups/{id}. Supports:
//
//	replace path=displayName value="..."
//	add path=members value=[{value:userId}, ...]
//	remove path=members[value eq "userId"]
//
// Other operations are ignored — the format above is what Okta and
// Entra send for the common create-and-add-members flow.
func (h *Handler) handlePatchGroup(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])

	var body groupPatchBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid PATCH body")
		return
	}

	for _, op := range body.Operations {
		opName := strings.ToLower(op.Op)
		path := strings.ToLower(strings.TrimSpace(op.Path))

		switch {
		case opName == "replace" && path == "displayname":
			var v string
			if err := json.Unmarshal(op.Value, &v); err == nil {
				if _, err := h.accountManager.SCIMRenameGroup(r.Context(), userAuth.AccountId, userAuth.UserId, id, v); err != nil {
					writeError(w, mapErrorStatus(err), err.Error())
					return
				}
			}

		case opName == "add" && path == "members":
			var members []memberPatchValue
			if err := json.Unmarshal(op.Value, &members); err == nil {
				for _, m := range members {
					_ = h.accountManager.SCIMAddGroupMember(r.Context(), userAuth.AccountId, userAuth.UserId, id, m.Value)
				}
			}

		case opName == "remove" && strings.HasPrefix(path, "members"):
			// Two forms: explicit `path=members[value eq "id"]` and
			// `op=remove path=members value=[{value:id}, ...]`. We
			// accept both.
			if uid := extractRemoveUserID(path); uid != "" {
				_ = h.accountManager.SCIMRemoveGroupMember(r.Context(), userAuth.AccountId, userAuth.UserId, id, uid)
			} else {
				var members []memberPatchValue
				if err := json.Unmarshal(op.Value, &members); err == nil {
					for _, m := range members {
						_ = h.accountManager.SCIMRemoveGroupMember(r.Context(), userAuth.AccountId, userAuth.UserId, id, m.Value)
					}
				}
			}
		}
	}

	// Return the resulting group so callers can verify the patch.
	group, members, err := h.accountManager.SCIMGetGroup(r.Context(), userAuth.AccountId, userAuth.UserId, id)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fromGroup(group, members))
}

func (h *Handler) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if err := h.accountManager.SCIMDeleteGroup(r.Context(), userAuth.AccountId, userAuth.UserId, id); err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseDisplayNameFilter(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	if len(parts) < 3 {
		return ""
	}
	if !strings.EqualFold(parts[0], "displayName") {
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

// extractRemoveUserID parses `members[value eq "user-id"]` and
// returns the quoted user ID. Empty string if the path doesn't match.
func extractRemoveUserID(path string) string {
	// Looking for: members[value eq "..."]
	open := strings.Index(path, "[")
	closeIdx := strings.LastIndex(path, "]")
	if open < 0 || closeIdx < 0 || closeIdx < open {
		return ""
	}
	inner := path[open+1 : closeIdx]
	parts := strings.Fields(inner)
	if len(parts) < 3 || !strings.EqualFold(parts[0], "value") || !strings.EqualFold(parts[1], "eq") {
		return ""
	}
	return strings.Trim(strings.Join(parts[2:], " "), "\"'")
}
