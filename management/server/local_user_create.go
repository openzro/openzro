package server

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/auth/jwt"
	"github.com/openzro/openzro/management/server/dex_proxy"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

const (
	// dexLocalConnector is the connector ID Dex assigns to its password DB.
	dexLocalConnector = "local"
	// localPasswordMinRunes and localPasswordMaxBytes are the ADR-0024 D7
	// policy. 72 bytes is bcrypt's input limit, which Dex also applies at
	// login, so longer passwords cannot be accepted without diverging.
	localPasswordMinRunes = 12
	localPasswordMaxBytes = 72
	// localPasswordBcryptCost is the cost of the hash stored in Dex.
	localPasswordBcryptCost = 12
)

// localCredentials is the slice of Dex's password DB the local-user sagas
// use. *dex_proxy.Client implements it; tests use a fake.
type localCredentials interface {
	CreatePassword(ctx context.Context, email, username, userID string, hash []byte) error
	UpdatePassword(ctx context.Context, email string, hash []byte) error
	ListPasswords(ctx context.Context) ([]dex_proxy.PasswordEntry, error)
	DeletePassword(ctx context.Context, email string) error
	RevokeAllRefreshTokens(ctx context.Context, sub string) error
}

// EnableLocalUsers switches on SCIM-provisioned local users (ADR-0024),
// backed by Dex's password DB. Call it only when the operator opted in and
// every management replica runs a version with the local-user store guards
// (#205); without it, creating a local user is refused.
func (am *DefaultAccountManager) EnableLocalUsers(creds localCredentials) {
	am.localCreds = creds
}

// validateLocalPassword applies the ADR-0024 D7 policy. The error never
// echoes the password.
func validateLocalPassword(password string) error {
	if !utf8.ValidString(password) ||
		utf8.RuneCountInString(password) < localPasswordMinRunes ||
		len(password) > localPasswordMaxBytes {
		return status.Errorf(status.BadRequest,
			"password must be %d to %d bytes of UTF-8 with at least %d characters",
			localPasswordMinRunes, localPasswordMaxBytes, localPasswordMinRunes)
	}
	return nil
}

// validateLocalUserName requires a plain email address: Dex keys local
// credentials by email.
func validateLocalUserName(userName string) error {
	addr, err := mail.ParseAddress(userName)
	if err != nil || addr.Address != userName || addr.Name != "" {
		return status.Errorf(status.BadRequest, "userName of a local user must be a plain email address")
	}
	return nil
}

// SCIMCreateLocalUser creates a password-backed local user (ADR-0024 D4):
// reserve the user record as pending and blocked, create the Dex
// credential, then finalize. Every step is fail-closed: an interruption
// leaves a blocked record (no JIT, no access), and retrying the same
// request resumes it with the stored identity.
func (am *DefaultAccountManager) SCIMCreateLocalUser(ctx context.Context, accountID, callerID string,
	input account.SCIMUserInput, password string) (*types.User, error) {
	if am.localCreds == nil {
		return nil, status.Errorf(status.BadRequest, "local users are not enabled on this deployment")
	}
	if err := am.requireSCIMPermission(ctx, accountID, callerID, operations.Create); err != nil {
		return nil, err
	}
	if err := validateSCIMUserInput(&input); err != nil {
		return nil, err
	}
	if !input.Active {
		return nil, status.Errorf(status.BadRequest, "a local user cannot be created inactive")
	}
	if err := validateLocalUserName(input.UserName); err != nil {
		return nil, err
	}
	if err := validateLocalPassword(password); err != nil {
		return nil, err
	}
	if err := am.checkProvisionedEmailDomain(ctx, accountID, input.UserName); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), localPasswordBcryptCost)
	if err != nil {
		return nil, status.Errorf(status.Internal, "hash local user password: %v", err)
	}

	unlock, err := am.lockLocalIdentity(ctx, input.UserName)
	if err != nil {
		return nil, status.Errorf(status.Internal, "%v", err)
	}
	defer unlock()
	// Held from the reservation to the finalize, after the identity lock:
	// a concurrent account delete either sees this user in its preflight or
	// runs to completion first (see lockAccountLifecycle).
	unlockLifecycle, err := am.lockAccountLifecycle(ctx, accountID)
	if err != nil {
		return nil, status.Errorf(status.Internal, "%v", err)
	}
	defer unlockLifecycle()

	user, dexUserID, reservedNow, err := am.reserveLocalUser(ctx, accountID, input)
	if err != nil {
		return nil, err
	}
	if err := am.ensureLocalCredential(ctx, user, dexUserID, reservedNow, hash); err != nil {
		if reservedNow && isStatus(err, status.AlreadyExists) {
			am.discardReservation(ctx, accountID, user.Id)
		}
		return nil, err
	}
	if err := am.Store.FinalizeLocalUser(ctx, accountID, user.Id); err != nil {
		return nil, status.Errorf(status.Internal, "finalize local user %s: %v", user.Id, err)
	}

	am.StoreEvent(ctx, callerID, user.Id, accountID, activity.UserInvited, map[string]any{
		"scim": true, "local": true, "username": input.UserName,
	})
	user.LocalCredentialState = types.LocalCredentialActive
	user.Blocked = false
	return user, nil
}

// reserveLocalUser finds this userName's pending reservation (a resumed
// create) or writes a new one with a fresh identity. Any other user with
// the same userName is a conflict.
func (am *DefaultAccountManager) reserveLocalUser(ctx context.Context, accountID string,
	input account.SCIMUserInput) (user *types.User, dexUserID string, reservedNow bool, err error) {
	existing, err := am.findSCIMUserByUserName(ctx, accountID, input.UserName)
	if err != nil {
		return nil, "", false, err
	}
	if existing != nil {
		if existing.LocalCredentialState != types.LocalCredentialPending {
			return nil, "", false, ErrSCIMUserAlreadyExists
		}
		uid, conn, ok := jwt.DecodeDexSubject(existing.Id)
		if !ok || conn != dexLocalConnector {
			return nil, "", false, status.Errorf(status.Internal, "pending local user %s has no Dex identity", existing.Id)
		}
		return existing, uid, false, nil
	}

	uid := uuid.NewString()
	user = types.NewRegularUser(jwt.EncodeDexSubject(uid, dexLocalConnector))
	user.AccountID = accountID
	user.AutoGroups = input.AutoGroups
	user.Issued = types.UserIssuedIntegration
	user.SCIMUserName = input.UserName
	user.SCIMDisplayName = input.DisplayName
	user.SCIMExternalID = input.ExternalID
	if err := am.Store.CreatePendingLocalUser(ctx, user); err != nil {
		return nil, "", false, err
	}
	return user, uid, true, nil
}

// ensureLocalCredential creates the Dex credential, or overwrites one that
// provably belongs to this resumed reservation (ADR-0024 D4 step 3, D5).
// Any other existing entry is an AlreadyExists conflict and is left
// untouched; that includes read-only static entries and an AlreadyExists
// that appears after an empty preflight, which is never adopted.
func (am *DefaultAccountManager) ensureLocalCredential(ctx context.Context, user *types.User, dexUserID string,
	reservedNow bool, hash []byte) error {
	email := user.SCIMUserName
	entries, err := am.localCreds.ListPasswords(ctx)
	if err != nil {
		return status.Errorf(status.Internal, "list Dex passwords: %v", err)
	}

	var found *dex_proxy.PasswordEntry
	for i := range entries {
		if strings.EqualFold(entries[i].Email, email) {
			found = &entries[i]
			break
		}
	}

	switch {
	case found == nil:
		err := am.localCreds.CreatePassword(ctx, email, email, dexUserID, hash)
		if errors.Is(err, dex_proxy.ErrAlreadyExists) {
			return status.Errorf(status.AlreadyExists, "a credential for this email already exists")
		}
		if err != nil {
			return status.Errorf(status.Internal, "create Dex password: %v", err)
		}
		return nil
	case found.UserID == dexUserID && !reservedNow:
		if err := am.localCreds.UpdatePassword(ctx, email, hash); err != nil {
			return status.Errorf(status.Internal, "update Dex password: %v", err)
		}
		return nil
	default:
		return status.Errorf(status.AlreadyExists, "a credential for this email already exists")
	}
}

// discardReservation removes a reservation this request created when the
// create cannot proceed. Failing to do so is harmless: the record stays
// pending, blocked and hidden from SCIM.
func (am *DefaultAccountManager) discardReservation(ctx context.Context, accountID, userID string) {
	if err := am.Store.DeletePendingLocalUser(ctx, accountID, userID); err != nil {
		log.WithContext(ctx).Errorf("failed to discard pending local user %s: %v", userID, err)
	}
}

func isStatus(err error, t status.Type) bool {
	sErr, ok := status.FromError(err)
	return ok && sErr != nil && sErr.Type() == t
}
