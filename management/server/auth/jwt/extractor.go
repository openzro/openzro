package jwt

import (
	"encoding/base64"
	"errors"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protowire"

	nbcontext "github.com/openzro/openzro/management/server/context"
)

const (
	// AccountIDSuffix suffix for the account id claim
	AccountIDSuffix = "wt_account_id"
	// DomainIDSuffix suffix for the domain id claim
	DomainIDSuffix = "wt_account_domain"
	// DomainCategorySuffix suffix for the domain category claim
	DomainCategorySuffix = "wt_account_domain_category"
	// UserIDClaim claim for the user id
	UserIDClaim = "sub"
	// LastLoginSuffix claim for the last login
	LastLoginSuffix = "nb_last_login"
	// Invited claim indicates that an incoming JWT is from a user that just accepted an invitation
	Invited = "nb_invited"
)

var (
	errUserIDClaimEmpty = errors.New("user ID claim token value is empty")
)

// ClaimsExtractor struct that holds the extract function
type ClaimsExtractor struct {
	authAudience string
	userIDClaim  string
}

// ClaimsExtractorOption is a function that configures the ClaimsExtractor
type ClaimsExtractorOption func(*ClaimsExtractor)

// WithAudience sets the audience for the extractor
func WithAudience(audience string) ClaimsExtractorOption {
	return func(c *ClaimsExtractor) {
		c.authAudience = audience
	}
}

// WithUserIDClaim sets the user id claim for the extractor
func WithUserIDClaim(userIDClaim string) ClaimsExtractorOption {
	return func(c *ClaimsExtractor) {
		c.userIDClaim = userIDClaim
	}
}

// NewClaimsExtractor returns an extractor, and if provided with a function with ExtractClaims signature,
// then it will use that logic. Uses ExtractClaimsFromRequestContext by default
func NewClaimsExtractor(options ...ClaimsExtractorOption) *ClaimsExtractor {
	ce := &ClaimsExtractor{}
	for _, option := range options {
		option(ce)
	}

	if ce.userIDClaim == "" {
		ce.userIDClaim = UserIDClaim
	}
	return ce
}

func parseTime(timeString string) time.Time {
	if timeString == "" {
		return time.Time{}
	}
	parsedTime, err := time.Parse(time.RFC3339, timeString)
	if err != nil {
		return time.Time{}
	}
	return parsedTime
}

func (c ClaimsExtractor) audienceClaim(claimName string) string {
	url, err := url.JoinPath(c.authAudience, claimName)
	if err != nil {
		return c.authAudience + claimName // as it was previously
	}

	return url
}

func (c *ClaimsExtractor) ToUserAuth(token *jwt.Token) (nbcontext.UserAuth, error) {
	claims := token.Claims.(jwt.MapClaims)
	userAuth := nbcontext.UserAuth{}

	userID, ok := claims[c.userIDClaim].(string)
	if !ok {
		return userAuth, errUserIDClaimEmpty
	}
	userAuth.UserId = userID

	// Standard OIDC claims. Source-of-truth for self-hosted Dex / OIDC-
	// direct deployments where there's no IdpManager API to back-fill
	// user metadata after the fact (see types.User.{Email,Name}).
	if email, ok := claims["email"].(string); ok {
		userAuth.Email = email
	}
	if name, ok := claims["name"].(string); ok {
		userAuth.Name = name
	} else if pu, ok := claims["preferred_username"].(string); ok {
		userAuth.Name = pu
	}

	if accountIDClaim, ok := claims[c.audienceClaim(AccountIDSuffix)]; ok {
		userAuth.AccountId = accountIDClaim.(string)
	}

	if domainClaim, ok := claims[c.audienceClaim(DomainIDSuffix)]; ok {
		userAuth.Domain = domainClaim.(string)
	}

	if domainCategoryClaim, ok := claims[c.audienceClaim(DomainCategorySuffix)]; ok {
		userAuth.DomainCategory = domainCategoryClaim.(string)
	}

	if lastLoginClaimString, ok := claims[c.audienceClaim(LastLoginSuffix)]; ok {
		userAuth.LastLogin = parseTime(lastLoginClaimString.(string))
	}

	if invitedBool, ok := claims[c.audienceClaim(Invited)]; ok {
		if value, ok := invitedBool.(bool); ok {
			userAuth.Invited = value
		}
	}

	// ConnectorID is the identifier of the connector that authenticated
	// this JWT. Used by the MFA gate (issue #31) to distinguish "local"
	// (Dex staticPasswords, no IdP-side MFA, openZro TOTP is the
	// primary second factor) from federated providers (TOTP is an
	// optional redundancy layer on top of the IdP's own MFA).
	//
	// Resolution order:
	//   1. `federated_claims.connector_id` if present. Standard OIDC
	//      extension some IdPs emit; kept first so the gate stays
	//      forward-compatible if Dex (or a future IdP) ever starts
	//      shipping the claim on access tokens. Today Dex only
	//      populates federated_claims on id_token and the dashboard
	//      sends access_token as Bearer, so this branch is effectively
	//      unused in production.
	//   2. The `sub` claim parsed as a Dex internal-id protobuf —
	//      see parseDexSubConnector. Dex encodes the connector id
	//      directly inside `sub` on every token it issues, so this
	//      catches both staticPasswords (conn_id="local") and any
	//      federated connector (conn_id="google", "github", ...)
	//      without depending on federated_claims.
	//   3. Empty string. Non-Dex IdPs (Auth0, Keycloak, …) emit
	//      UUID-style subs that don't parse as a Dex protobuf;
	//      resolveMFAEnforcement then treats the absence as the
	//      federated branch — same legacy behavior, no regression
	//      for those deployments.
	if fc, ok := claims["federated_claims"].(map[string]any); ok {
		if cid, ok := fc["connector_id"].(string); ok {
			userAuth.ConnectorID = cid
		}
	}
	if userAuth.ConnectorID == "" {
		userAuth.ConnectorID = parseDexSubConnector(userID)
	}

	return userAuth, nil
}

// parseDexSubConnector decodes Dex's internal-id protobuf out of the
// JWT `sub` claim and returns the embedded connector id, or empty
// string if `sub` is not in that shape.
//
// Dex builds every issued token's sub by base64url-encoding a
// protobuf message with two LEN-typed string fields:
//
//	field 1 = user_id   (the connector-local subject)
//	field 2 = conn_id   (the configured connector identifier)
//
// See Dex `internal/server/handlers.go:formatSubject` and the
// `internalIDProto` schema. The format has been stable since Dex
// 2.x and is shared across staticPasswords + every federated
// connector (oauth, oidc, google, github, …).
//
// Non-Dex IdPs (Auth0, Keycloak, …) emit UUID-style subs that
// won't parse as a protobuf with field 2 = string; the consumer
// gets "" back and falls through to the legacy code path.
func parseDexSubConnector(sub string) string {
	if sub == "" {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(sub)
	if err != nil {
		return ""
	}
	for len(raw) > 0 {
		num, typ, n := protowire.ConsumeTag(raw)
		if n < 0 || typ != protowire.BytesType {
			return ""
		}
		raw = raw[n:]
		val, n := protowire.ConsumeBytes(raw)
		if n < 0 {
			return ""
		}
		raw = raw[n:]
		if num == 2 {
			return string(val)
		}
	}
	return ""
}

func (c *ClaimsExtractor) ToGroups(token *jwt.Token, claimName string) []string {
	claims := token.Claims.(jwt.MapClaims)
	userJWTGroups := make([]string, 0)

	if claim, ok := claims[claimName]; ok {
		if claimGroups, ok := claim.([]interface{}); ok {
			for _, g := range claimGroups {
				if group, ok := g.(string); ok {
					userJWTGroups = append(userJWTGroups, group)
				} else {
					log.Debugf("JWT claim %q contains a non-string group (type: %T): %v", claimName, g, g)
				}
			}
		}
	} else {
		log.Debugf("JWT claim %q is not a string array", claimName)
	}

	return userJWTGroups
}
