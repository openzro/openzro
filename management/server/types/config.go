package types

import (
	"net/netip"

	"github.com/openzro/openzro/management/client/common"
	"github.com/openzro/openzro/management/server/idp"
	"github.com/openzro/openzro/util"
)

type (
	// Protocol type
	Protocol string

	// Provider authorization flow type
	Provider string
)

const (
	UDP   Protocol = "udp"
	DTLS  Protocol = "dtls"
	TCP   Protocol = "tcp"
	HTTP  Protocol = "http"
	HTTPS Protocol = "https"
	NONE  Provider = "none"
)

const (
	// DefaultDeviceAuthFlowScope defines the bare minimum scope to request in the device authorization flow
	DefaultDeviceAuthFlowScope string = "openid"
)

var MgmtConfigPath string

// Config of the Management service
type Config struct {
	Stuns      []*Host
	TURNConfig *TURNConfig
	Relay      *Relay
	Signal     *Host

	Datadir                string
	DataStoreEncryptionKey string

	HttpConfig *HttpServerConfig

	IdpManagerConfig *idp.Config

	DeviceAuthorizationFlow *DeviceAuthorizationFlow

	PKCEAuthorizationFlow *PKCEAuthorizationFlow

	StoreConfig StoreConfig

	ReverseProxy ReverseProxy

	// disable default all-to-all policy
	DisableDefaultPolicy bool

	// WgPrivateKey is the management daemon's WireGuard identity used to
	// decrypt the encrypted gRPC envelope every peer sends with its Login
	// and Sync requests (encryption.DecryptMessage in grpcserver.go). It
	// is base64-encoded `wgtypes.Key` material (32 random bytes).
	//
	// Single-instance deployments may leave this empty — NewServer
	// generates a fresh key at startup and the daemon persists it back
	// here on its first boot so subsequent restarts keep the same
	// identity (peers don't have to re-encrypt).
	//
	// HA deployments (>1 management replica) MUST share one key across
	// every pod. The K8s service round-robins requests across pods; a
	// peer that encrypted with pod A's public key would otherwise see
	// half its requests fail with `InvalidArgument: invalid request
	// message` when they land on pod B. The chart wires this via a
	// shared Secret + the `OPENZRO_MGMT_WG_PRIVATE_KEY` env var override
	// (see helms/charts/openzro/templates/management-identity-secret.yaml).
	//
	// Background: upstream NetBird hardcodes `wgtypes.GeneratePrivateKey()`
	// on every NewServer() call with no persistence and no config knob.
	// They run their managed cloud either single-replica or with an
	// internal patch that has never been upstreamed (issue #3547 stays
	// open). openZro's BSD-3 fix is to make the key explicit + injectable.
	WgPrivateKey string
}

// GetAuthAudiences returns the audience from the http config and device authorization flow config
func (c Config) GetAuthAudiences() []string {
	audiences := []string{c.HttpConfig.AuthAudience}

	if c.HttpConfig.ExtraAuthAudience != "" {
		audiences = append(audiences, c.HttpConfig.ExtraAuthAudience)
	}

	if c.DeviceAuthorizationFlow != nil && c.DeviceAuthorizationFlow.ProviderConfig.Audience != "" {
		audiences = append(audiences, c.DeviceAuthorizationFlow.ProviderConfig.Audience)
	}

	return audiences
}

// TURNConfig is a config of the TURNCredentialsManager
type TURNConfig struct {
	TimeBasedCredentials bool
	CredentialsTTL       util.Duration
	Secret               string
	Turns                []*Host
}

// Relay configuration type
type Relay struct {
	Addresses      []string
	CredentialsTTL util.Duration
	Secret         string
}

// HttpServerConfig is a config of the HTTP Management service server
type HttpServerConfig struct {
	LetsEncryptDomain string
	// CertFile is the location of the certificate
	CertFile string
	// CertKey is the location of the certificate private key
	CertKey string
	// AuthAudience identifies the recipients that the JWT is intended for (aud in JWT)
	AuthAudience string
	// AuthIssuer identifies principal that issued the JWT
	AuthIssuer string
	// AuthUserIDClaim is the name of the claim that used as user ID
	AuthUserIDClaim string
	// AuthKeysLocation is a location of JWT key set containing the public keys used to verify JWT
	AuthKeysLocation string
	// OIDCConfigEndpoint is the endpoint of an IDP manager to get OIDC configuration
	OIDCConfigEndpoint string
	// IdpSignKeyRefreshEnabled identifies the signing key is currently being rotated or not
	IdpSignKeyRefreshEnabled bool
	// Extra audience
	ExtraAuthAudience string
}

// Host represents a Openzro host (e.g. STUN, TURN, Signal)
type Host struct {
	Proto Protocol
	// URI e.g. turns://stun.openzro.io:4430 or signal.openzro.io:10000
	URI      string
	Username string
	Password string
}

// DeviceAuthorizationFlow represents Device Authorization Flow information
// that can be used by the client to login initiate a Oauth 2.0 device authorization grant flow
// see https://datatracker.ietf.org/doc/html/rfc8628
type DeviceAuthorizationFlow struct {
	Provider       string
	ProviderConfig ProviderConfig
}

// PKCEAuthorizationFlow represents Authorization Code Flow information
// that can be used by the client to login initiate a Oauth 2.0 authorization code grant flow
// with Proof Key for Code Exchange (PKCE). See https://datatracker.ietf.org/doc/html/rfc7636
type PKCEAuthorizationFlow struct {
	ProviderConfig ProviderConfig
}

// ProviderConfig has all attributes needed to initiate a device/pkce authorization flow
type ProviderConfig struct {
	// ClientID An IDP application client id
	ClientID string
	// ClientSecret An IDP application client secret
	ClientSecret string
	// Domain An IDP API domain
	// Deprecated. Use TokenEndpoint and DeviceAuthEndpoint
	Domain string
	// Audience An Audience for to authorization validation
	Audience string
	// TokenEndpoint is the endpoint of an IDP manager where clients can obtain access token
	TokenEndpoint string
	// DeviceAuthEndpoint is the endpoint of an IDP manager where clients can obtain device authorization code
	DeviceAuthEndpoint string
	// AuthorizationEndpoint is the endpoint of an IDP manager where clients can obtain authorization code
	AuthorizationEndpoint string
	// Scopes provides the scopes to be included in the token request
	Scope string
	// UseIDToken indicates if the id token should be used for authentication
	UseIDToken bool
	// RedirectURL handles authorization code from IDP manager
	RedirectURLs []string
	// DisablePromptLogin makes the PKCE flow to not prompt the user for login
	DisablePromptLogin bool
	// LoginFlag is used to configure the PKCE flow login behavior
	LoginFlag common.LoginFlag
}

// StoreConfig contains Store configuration
type StoreConfig struct {
	Engine Engine
}

// ReverseProxy contains reverse proxy configuration in front of management.
type ReverseProxy struct {
	// TrustedHTTPProxies represents a list of trusted HTTP proxies by their IP prefixes.
	// When extracting the real IP address from request headers, the middleware will verify
	// if the peer's address falls within one of these trusted IP prefixes.
	TrustedHTTPProxies []netip.Prefix

	// TrustedHTTPProxiesCount specifies the count of trusted HTTP proxies between the internet
	// and the server. When using the trusted proxy count method to extract the real IP address,
	// the middleware will search the X-Forwarded-For IP list from the rightmost by this count
	// minus one.
	TrustedHTTPProxiesCount uint

	// TrustedPeers represents a list of trusted peers by their IP prefixes.
	// These peers are considered trustworthy by the gRPC server operator,
	// and the middleware will attempt to extract the real IP address from
	// request headers if the peer's address falls within one of these
	// trusted IP prefixes.
	TrustedPeers []netip.Prefix
}
