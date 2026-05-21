// Package scim implements the SCIM 2.0 (RFC 7644) provisioning API
// surface for openZro. The first iteration is read-only:
//
//	GET /scim/v2/ServiceProviderConfig  describes capabilities
//	GET /scim/v2/Schemas                lists supported schemas
//	GET /scim/v2/ResourceTypes          lists provisioned resource types
//	GET /scim/v2/Users                  lists users in the caller's account
//	GET /scim/v2/Users/{id}             single-user lookup
//
// Mutating operations (POST/PUT/PATCH/DELETE on Users and Groups) are
// intentionally not yet exposed: they require a manager-level path
// that creates users *without* round-tripping the upstream IdP, which
// is a deliberate design choice and merits its own commit + ADR.
//
// SCIM clients (Okta, Entra, JumpCloud, …) authenticate with a
// pre-issued bearer token. We reuse the existing auth middleware that
// already accepts Personal Access Tokens (`nbp_*`); operators issue
// a PAT to a service user with admin role and configure that as the
// SCIM bearer token in their IdP. Account scoping flows from the PAT,
// so cross-account access is structurally impossible.
package scim

import (
	"encoding/json"
	"net/http"
	"time"
)

// SCIM URN constants from RFC 7643 §8.7. Stable; do not change.
const (
	SchemaUserURN         = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaGroupURN        = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaListResponseURN = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaErrorURN        = "urn:ietf:params:scim:api:messages:2.0:Error"
	SchemaServiceProvider = "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"
	SchemaResourceTypeURN = "urn:ietf:params:scim:schemas:core:2.0:ResourceType"
	SchemaEnterpriseUser  = "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
)

// ContentType is the SCIM-specific media type. Most IdPs fall back to
// application/json on Accept negotiation, but emitting the canonical
// type avoids surprises.
const ContentType = "application/scim+json"

// ResourceMeta is the common metadata stamp every SCIM resource carries.
// Per RFC 7643 §3.1.
type ResourceMeta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created,omitempty"`
	LastModified time.Time `json:"lastModified,omitempty"`
	Location     string    `json:"location,omitempty"`
	Version      string    `json:"version,omitempty"`
}

// ListResponse is the envelope for paginated reads. Per RFC 7644 §3.4.2.
type ListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex,omitempty"`
	ItemsPerPage int      `json:"itemsPerPage,omitempty"`
	Resources    []any    `json:"Resources"`
}

// ErrorResponse is the canonical SCIM error envelope. Per RFC 7644 §3.12.
// scimType is optional and only used for 4xx attribute-level errors.
type ErrorResponse struct {
	Schemas  []string `json:"schemas"`
	Detail   string   `json:"detail"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
}

// writeJSON serializes v as application/scim+json with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", ContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError emits the canonical SCIM error envelope.
func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, ErrorResponse{
		Schemas: []string{SchemaErrorURN},
		Detail:  detail,
		Status:  http.StatusText(status),
	})
}
