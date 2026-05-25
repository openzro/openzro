package jwt

import (
	"errors"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
	log "github.com/sirupsen/logrus"

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

	// federated_claims.connector_id is the Dex-emitted identifier for
	// the connector that authenticated this JWT. Used by the MFA gate
	// (issue #31) to distinguish "local" (Dex staticPasswords, no
	// IdP-side MFA, openZro TOTP is the primary second factor) from
	// federated providers (TOTP is an optional redundancy layer on
	// top of the IdP's own MFA).
	//
	// Dex emits federated_claims for *external* connectors (google,
	// github, oidc, oauth, etc.) but NOT for the bundled staticPasswords
	// connector — and even when it does, the claim only ships on the
	// id_token, not the access_token the dashboard sends as Bearer.
	// Without a fallback, every staticPasswords login would arrive
	// here with ConnectorID="" and resolveMFAEnforcement would route
	// it to the federated branch, silently bypassing MFAEnforceLocal.
	// Default to "local" when the claim is absent so the gate honours
	// the operator's local-enforcement toggle. PATs are screened by
	// IsPAT upstream and never reach resolveMFAEnforcement, so this
	// default has no impact on them.
	userAuth.ConnectorID = "local"
	if fc, ok := claims["federated_claims"].(map[string]any); ok {
		if cid, ok := fc["connector_id"].(string); ok && cid != "" {
			userAuth.ConnectorID = cid
		}
	}

	return userAuth, nil
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
