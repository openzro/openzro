package api

// Reverse-proxy services — clean-room re-introduction of the
// upstream NetBird "Services" feature (post-v0.53.0 BSD addition).
// A Service binds a virtual hostname (Domain) to one or more
// Targets that traffic is forwarded to. Used primarily by the
// Kubernetes operator's HTTPRoute controller to materialize
// Gateway API HTTPRoutes as openZro reverse-proxy services.
//
// As with DNS Zones, server-side handlers + storage are deferred
// (tracked in project_enterprise_gaps.md). The types here exist so
// the operator builds; runtime calls against an openZro
// management server will fail with 404 until the server side ships.

// Service is a configured reverse-proxy entry.
type Service struct {
	// Id is the server-assigned identifier.
	Id string `json:"id"`

	// Name is the operator-friendly label.
	Name string `json:"name"`

	// Domain is the virtual hostname clients address (`Host:` header
	// for HTTP, SNI for TLS-terminated, etc.).
	Domain string `json:"domain"`

	// Enabled toggles traffic forwarding without deleting the
	// configuration.
	Enabled bool `json:"enabled"`

	// Mode selects the protocol the proxy front-ends. Pointer so
	// JSON payloads from older servers (no Mode field) decode
	// cleanly.
	Mode *ServiceRequestMode `json:"mode,omitempty"`

	// PassHostHeader controls whether the original Host header is
	// preserved when forwarding (HTTP mode only).
	PassHostHeader *bool `json:"pass_host_header,omitempty"`

	// RewriteRedirects rewrites Location headers in upstream responses
	// to point back at the service's Domain (HTTP mode only).
	RewriteRedirects *bool `json:"rewrite_redirects,omitempty"`

	// Targets is the list of upstream peer endpoints.
	Targets *[]ServiceTarget `json:"targets,omitempty"`
}

// ServiceRequest is the payload for creating or updating a service.
type ServiceRequest struct {
	Name             string              `json:"name"`
	Domain           string              `json:"domain"`
	Enabled          bool                `json:"enabled"`
	Mode             *ServiceRequestMode `json:"mode,omitempty"`
	PassHostHeader   *bool               `json:"pass_host_header,omitempty"`
	RewriteRedirects *bool               `json:"rewrite_redirects,omitempty"`
	Targets          *[]ServiceTarget    `json:"targets,omitempty"`
}

// ServiceRequestMode enumerates the supported proxy modes.
type ServiceRequestMode string

const (
	ServiceRequestModeHttp  ServiceRequestMode = "http"
	ServiceRequestModeHttps ServiceRequestMode = "https"
	ServiceRequestModeTcp   ServiceRequestMode = "tcp"
)

// ServiceTarget is one upstream endpoint a Service forwards to.
type ServiceTarget struct {
	// TargetId references the openZro entity that hosts the upstream
	// (typically a NetworkResource ID or a Peer ID, distinguished by
	// TargetType).
	TargetId string `json:"target_id"`

	// TargetType is the kind of entity TargetId refers to.
	TargetType ServiceTargetTargetType `json:"target_type"`

	// Protocol selects the L7 protocol used between the proxy and
	// this target.
	Protocol ServiceTargetProtocol `json:"protocol"`

	// Path is an optional URL path prefix to mount this target at
	// (HTTP mode only). Pointer so omitting → "no path filter".
	Path *string `json:"path,omitempty"`

	// Enabled toggles this single target without removing it.
	Enabled bool `json:"enabled"`
}

// ServiceTargetProtocol enumerates the L7 protocols a target can speak.
type ServiceTargetProtocol string

const (
	ServiceTargetProtocolHttp  ServiceTargetProtocol = "http"
	ServiceTargetProtocolHttps ServiceTargetProtocol = "https"
	ServiceTargetProtocolGrpc  ServiceTargetProtocol = "grpc"
)

// ServiceTargetTargetType enumerates the kinds of entities a target
// can reference.
type ServiceTargetTargetType string

const (
	// ServiceTargetTargetTypeHost is a network resource (FQDN/IP).
	ServiceTargetTargetTypeHost ServiceTargetTargetType = "host"

	// ServiceTargetTargetTypePeer is a single peer in the mesh.
	ServiceTargetTargetTypePeer ServiceTargetTargetType = "peer"
)
