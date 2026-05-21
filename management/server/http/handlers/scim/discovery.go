package scim

import "net/http"

// ServiceProviderConfig is the response shape for
// GET /scim/v2/ServiceProviderConfig per RFC 7643 §5. Every IdP that
// speaks SCIM checks this endpoint at integration time to learn what
// the server supports — clients refuse to connect if it returns 404.
type ServiceProviderConfig struct {
	Schemas               []string      `json:"schemas"`
	DocumentationURI      string        `json:"documentationUri,omitempty"`
	Patch                 supportedFlag `json:"patch"`
	Bulk                  bulkConfig    `json:"bulk"`
	Filter                filterConfig  `json:"filter"`
	ChangePassword        supportedFlag `json:"changePassword"`
	Sort                  supportedFlag `json:"sort"`
	ETag                  supportedFlag `json:"etag"`
	AuthenticationSchemes []authnScheme `json:"authenticationSchemes"`
}

type supportedFlag struct {
	Supported bool `json:"supported"`
}
type bulkConfig struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations"`
	MaxPayloadSize int  `json:"maxPayloadSize"`
}
type filterConfig struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults"`
}
type authnScheme struct {
	Type             string `json:"type"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	DocumentationURI string `json:"documentationUri,omitempty"`
	Primary          bool   `json:"primary,omitempty"`
}

// handleServiceProviderConfig advertises capabilities. Patch and
// userName-equality filter are now supported; bulk operations and
// sort are not. Most IdPs (Okta, Entra, JumpCloud) are happy with
// this profile.
func (h *Handler) handleServiceProviderConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ServiceProviderConfig{
		Schemas:        []string{SchemaServiceProvider},
		Patch:          supportedFlag{true},
		Bulk:           bulkConfig{Supported: false, MaxOperations: 0, MaxPayloadSize: 0},
		Filter:         filterConfig{Supported: true, MaxResults: 1000},
		ChangePassword: supportedFlag{false},
		Sort:           supportedFlag{false},
		ETag:           supportedFlag{false},
		AuthenticationSchemes: []authnScheme{
			{
				Type:        "oauthbearertoken",
				Name:        "Bearer Token",
				Description: "Authentication via openZro Personal Access Token (nbp_*) issued to a service user with admin role.",
				Primary:     true,
			},
		},
	})
}

// resourceType is the response shape for /ResourceTypes. RFC 7643 §6.
type resourceType struct {
	Schemas     []string     `json:"schemas"`
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Endpoint    string       `json:"endpoint"`
	Description string       `json:"description"`
	Schema      string       `json:"schema"`
	Meta        ResourceMeta `json:"meta"`
}

// handleResourceTypes returns the resource catalog: Users + Groups.
func (h *Handler) handleResourceTypes(w http.ResponseWriter, _ *http.Request) {
	resources := []any{
		resourceType{
			Schemas:     []string{SchemaResourceTypeURN},
			ID:          "User",
			Name:        "User",
			Endpoint:    "/Users",
			Description: "User Account",
			Schema:      SchemaUserURN,
			Meta: ResourceMeta{
				ResourceType: "ResourceType",
				Location:     "/scim/v2/ResourceTypes/User",
			},
		},
		resourceType{
			Schemas:     []string{SchemaResourceTypeURN},
			ID:          "Group",
			Name:        "Group",
			Endpoint:    "/Groups",
			Description: "Group",
			Schema:      SchemaGroupURN,
			Meta: ResourceMeta{
				ResourceType: "ResourceType",
				Location:     "/scim/v2/ResourceTypes/Group",
			},
		},
	}
	writeJSON(w, http.StatusOK, ListResponse{
		Schemas:      []string{SchemaListResponseURN},
		TotalResults: len(resources),
		Resources:    resources,
	})
}

// handleSchemas returns the schema catalog. Minimal: just the URN
// listing — full attribute descriptions are not yet served. RFC 7643
// §7. Most IdPs fetch this to display friendly attribute pickers, but
// none refuse to provision when the response is the bare list.
func (h *Handler) handleSchemas(w http.ResponseWriter, _ *http.Request) {
	resources := []any{
		map[string]any{"id": SchemaUserURN, "name": "User"},
		map[string]any{"id": SchemaGroupURN, "name": "Group"},
	}
	writeJSON(w, http.StatusOK, ListResponse{
		Schemas:      []string{SchemaListResponseURN},
		TotalResults: len(resources),
		Resources:    resources,
	})
}
