package server

import (
	"context"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/activity"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
)

// errEmailDomainNotAllowed is the single message for every allowlist deny,
// so a caller cannot tell an unlisted domain from an unverified email.
const errEmailDomainNotAllowed = "email domain not allowed for this account"

// Deny reasons recorded in logs and in the UserJoinDeniedByDomain event.
const (
	emailDenyMissing    = "missing"
	emailDenyUnverified = "unverified"
	emailDenyDomain     = "domain"
)

// checkJoinEmailDomain enforces the account's AllowedEmailDomains on a JWT
// identity that is about to join accountID for the first time (ADR-0025).
// It returns nil when the allowlist is empty or the identity carries a
// verified email in a listed domain; otherwise it logs, records an audit
// event and returns a PermissionDenied status error.
func (am *DefaultAccountManager) checkJoinEmailDomain(ctx context.Context, accountID string, userAuth nbcontext.UserAuth) error {
	settings, err := am.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return fmt.Errorf("get account settings: %w", err)
	}
	if len(settings.AllowedEmailDomains) == 0 {
		return nil
	}

	var reason string
	switch {
	case userAuth.Email == "":
		reason = emailDenyMissing
	case !userAuth.EmailVerified:
		reason = emailDenyUnverified
	case !settings.EmailDomainAllowed(userAuth.Email):
		reason = emailDenyDomain
	default:
		return nil
	}

	domain := emailDomainOf(userAuth.Email)
	log.WithContext(ctx).Warnf("denied first login of user %s to account %s: email domain %q (%s)",
		userAuth.UserId, accountID, domain, reason)
	am.StoreEvent(ctx, userAuth.UserId, userAuth.UserId, accountID, activity.UserJoinDeniedByDomain,
		map[string]any{"domain": domain, "reason": reason})

	return status.Errorf(status.PermissionDenied, errEmailDomainNotAllowed)
}

// emailDomainOf returns the lowercased part of email after the last "@",
// or "" when there is none. Only the domain is ever logged, never the
// full address.
func emailDomainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

// checkProvisionedEmailDomain enforces AllowedEmailDomains on an email an
// admin provisions explicitly (SCIM userName, IdP-manager invite). Unlike
// the login gate this is a caller error, so it returns InvalidArgument and
// names the rejected domain.
func (am *DefaultAccountManager) checkProvisionedEmailDomain(ctx context.Context, accountID, email string) error {
	settings, err := am.Store.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return fmt.Errorf("get account settings: %w", err)
	}
	if settings.EmailDomainAllowed(email) {
		return nil
	}
	return status.Errorf(status.InvalidArgument, "email domain %q is not allowed for this account", emailDomainOf(email))
}
