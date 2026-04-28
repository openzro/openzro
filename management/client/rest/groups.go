package rest

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/openzro/openzro/management/server/http/api"
)

// GroupsAPI APIs for Groups, do not use directly
type GroupsAPI struct {
	c *Client
}

// List list all groups
// See more: https://docs.openzro.io/api/resources/groups#list-all-groups
func (a *GroupsAPI) List(ctx context.Context) ([]api.Group, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/groups", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	ret, err := parseResponse[[]api.Group](resp)
	return ret, err
}

// GetByName looks up a group by its display name. Returns the
// first match (group names are not strictly unique upstream — the
// dashboard enforces uniqueness on create, but two accounts merged
// post-fact could have collisions). Used by the operator's
// reconcilers to resolve a CRD's `groupRef.name` to a server-side
// group ID.
//
// Errors: returns a *APIError with StatusCode 404 (testable via
// IsNotFound) when no group with that name exists in the account.
func (a *GroupsAPI) GetByName(ctx context.Context, name string) (api.Group, error) {
	groups, err := a.List(ctx)
	if err != nil {
		return api.Group{}, err
	}
	for _, g := range groups {
		if g.Name == name {
			return g, nil
		}
	}
	return api.Group{}, &APIError{
		StatusCode: 404,
		Message:    "group not found: " + name,
	}
}

// Get get group info
// See more: https://docs.openzro.io/api/resources/groups#retrieve-a-group
func (a *GroupsAPI) Get(ctx context.Context, groupID string) (*api.Group, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/groups/"+groupID, nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	ret, err := parseResponse[api.Group](resp)
	return &ret, err
}

// Create create new group
// See more: https://docs.openzro.io/api/resources/groups#create-a-group
func (a *GroupsAPI) Create(ctx context.Context, request api.PostApiGroupsJSONRequestBody) (*api.Group, error) {
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	resp, err := a.c.NewRequest(ctx, "POST", "/api/groups", bytes.NewReader(requestBytes), nil)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	ret, err := parseResponse[api.Group](resp)
	return &ret, err
}

// Update update group info
// See more: https://docs.openzro.io/api/resources/groups#update-a-group
func (a *GroupsAPI) Update(ctx context.Context, groupID string, request api.PutApiGroupsGroupIdJSONRequestBody) (*api.Group, error) {
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	resp, err := a.c.NewRequest(ctx, "PUT", "/api/groups/"+groupID, bytes.NewReader(requestBytes), nil)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	ret, err := parseResponse[api.Group](resp)
	return &ret, err
}

// Delete delete group
// See more: https://docs.openzro.io/api/resources/groups#delete-a-group
func (a *GroupsAPI) Delete(ctx context.Context, groupID string) error {
	resp, err := a.c.NewRequest(ctx, "DELETE", "/api/groups/"+groupID, nil, nil)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	return nil
}
