package context

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type key int

const (
	UserAuthContextKey key = iota
)

type UserAuth struct {
	// The account id the user is accessing
	AccountId string
	// The account domain
	Domain string
	// The account domain category, TBC values
	DomainCategory string
	// Indicates whether this user was invited, TBC logic
	Invited bool
	// Indicates whether this is a child account
	IsChild bool

	// The user id
	UserId string
	// Email from the JWT `email` claim. Populated on every JWT
	// validation. Used to back-fill User.Email when the deployment
	// doesn't run a queryable IdpManager (e.g. Dex with passwordDB
	// or generic OIDC where there's no directory API to look up
	// user metadata after the fact).
	Email string
	// Name from the JWT `name` claim, falling back to
	// `preferred_username` when `name` is absent. Same back-fill
	// rationale as Email.
	Name string
	// Last login time for this user
	LastLogin time.Time
	// The Groups the user belongs to on this account
	Groups []string

	// Indicates whether this user has authenticated with a Personal Access Token
	IsPAT bool

	// ConnectorID is the identifier of the connector that
	// authenticated this JWT. Resolved by the JWT extractor with
	// `federated_claims.connector_id` first and Dex's `sub` protobuf
	// as fallback so Dex deployments populate this even though
	// federated_claims only ships on the id_token. Used by the MFA
	// gate (issue #31) to apply MFAEnforceLocal vs.
	// MFAEnforceFederated. Empty for PATs, service users, and
	// non-Dex IdPs (Auth0, Keycloak, …) whose subs aren't Dex
	// protobufs — resolveMFAEnforcement treats those as the
	// federated branch.
	ConnectorID string
}

func GetUserAuthFromRequest(r *http.Request) (UserAuth, error) {
	return GetUserAuthFromContext(r.Context())
}

func SetUserAuthInRequest(r *http.Request, userAuth UserAuth) *http.Request {
	return r.WithContext(SetUserAuthInContext(r.Context(), userAuth))
}

func GetUserAuthFromContext(ctx context.Context) (UserAuth, error) {
	if userAuth, ok := ctx.Value(UserAuthContextKey).(UserAuth); ok {
		return userAuth, nil
	}
	return UserAuth{}, fmt.Errorf("user auth not in context")
}

func SetUserAuthInContext(ctx context.Context, userAuth UserAuth) context.Context {
	//nolint
	ctx = context.WithValue(ctx, UserIDKey, userAuth.UserId)
	//nolint
	ctx = context.WithValue(ctx, AccountIDKey, userAuth.AccountId)
	return context.WithValue(ctx, UserAuthContextKey, userAuth)
}
