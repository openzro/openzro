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

	// ConnectorID is the Dex connector that authenticated this JWT,
	// extracted from `federated_claims.connector_id`. Dex's bundled
	// staticPasswords connector emits the literal value "local";
	// federated providers (Okta, Google Workspace, Microsoft Entra,
	// Authentik, Keycloak, etc.) emit their configured connector id.
	// Used by the MFA gate (issue #31) to apply MFAEnforceLocal vs.
	// MFAEnforceFederated. Empty when the JWT didn't carry
	// federated_claims (PAT auth, legacy tokens, non-Dex IdPs that
	// don't emit the claim).
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
