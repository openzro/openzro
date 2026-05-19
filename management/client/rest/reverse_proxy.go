package rest

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/openzro/openzro/management/server/http/api"
)

// ReverseProxyServicesAPI is the REST surface for the reverse-proxy
// Services feature (post-fork upstream addition; clean-room
// re-introduced — see api.Service).
//
// Currently consumed by openzro/openzro-operator's HTTPRoute
// controller, which materializes Gateway API HTTPRoutes as
// openZro Services. Server-side implementation is tracked
// separately — see memory file project_enterprise_gaps.md
// "Reverse-proxy Services".
type ReverseProxyServicesAPI struct {
	c *Client
}

// List returns all reverse-proxy services in the authenticated
// account.
func (a *ReverseProxyServicesAPI) List(ctx context.Context) ([]api.Service, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/services", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[[]api.Service](resp)
}

// Get returns a single service by ID.
func (a *ReverseProxyServicesAPI) Get(ctx context.Context, serviceID string) (api.Service, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/services/"+serviceID, nil, nil)
	if err != nil {
		return api.Service{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.Service](resp)
}

// Create adds a new reverse-proxy service.
func (a *ReverseProxyServicesAPI) Create(ctx context.Context, req api.ServiceRequest) (api.Service, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return api.Service{}, err
	}
	resp, err := a.c.NewRequest(ctx, "POST", "/api/services", bytes.NewReader(body), nil)
	if err != nil {
		return api.Service{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.Service](resp)
}

// Update replaces a service's configuration.
func (a *ReverseProxyServicesAPI) Update(ctx context.Context, serviceID string, req api.ServiceRequest) (api.Service, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return api.Service{}, err
	}
	resp, err := a.c.NewRequest(ctx, "PUT", "/api/services/"+serviceID, bytes.NewReader(body), nil)
	if err != nil {
		return api.Service{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.Service](resp)
}

// Delete removes a service.
func (a *ReverseProxyServicesAPI) Delete(ctx context.Context, serviceID string) error {
	resp, err := a.c.NewRequest(ctx, "DELETE", "/api/services/"+serviceID, nil, nil)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return nil
}
