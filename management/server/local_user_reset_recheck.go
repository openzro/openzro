package server

import (
	"context"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

const (
	// credentialsRecheckDelay is when the second pass of a local password
	// reset runs (ADR-0027 D6). Dex v2.45-v2.46 keeps an authorization code
	// valid for 30 minutes (hard-coded) and stamps its tokens with the time
	// of redemption, so a login that passed the old password just before
	// the reset can mint tokens newer than the first cutoff until then. One
	// minute of margin covers clock skew and the redemption itself.
	credentialsRecheckDelay = 31 * time.Minute

	// credentialsRecheckIntervalEnv overrides how often each replica looks
	// for due second passes.
	credentialsRecheckIntervalEnv = "OPENZRO_CREDENTIALS_RECHECK_INTERVAL_SECONDS"
	// defaultCredentialsRecheckInterval adds at most this much to the
	// window a pre-reset login can hold a session. The cost per pass is one
	// indexed query.
	defaultCredentialsRecheckInterval = time.Minute
	// minCredentialsRecheckInterval floors pathological values so a typo
	// cannot turn the loop into a tight DB loop; 0 and negative values fall
	// back to the default, since the pass bounds a security window.
	minCredentialsRecheckInterval = 5 * time.Second
)

func credentialsRecheckInterval() time.Duration {
	secs, err := strconv.Atoi(os.Getenv(credentialsRecheckIntervalEnv))
	if err != nil || secs <= 0 {
		return defaultCredentialsRecheckInterval
	}
	return max(time.Duration(secs)*time.Second, minCredentialsRecheckInterval)
}

// runCredentialsRechecker runs the due second passes of local password
// resets on every replica. Passes are idempotent and serialize on the local
// identity lock, so replicas running the same one concurrently converge.
func (am *DefaultAccountManager) runCredentialsRechecker(ctx context.Context) {
	interval := credentialsRecheckInterval()
	log.WithContext(ctx).Infof("credentials rechecker running every %s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			am.runDueCredentialsRechecks(ctx, time.Now())
		}
	}
}

// runDueCredentialsRechecks runs every second pass due at now. A failed pass
// keeps its schedule and is retried on the next tick.
func (am *DefaultAccountManager) runDueCredentialsRechecks(ctx context.Context, now time.Time) {
	if am.localCreds == nil {
		return
	}
	due, err := am.Store.ListUserCredentialsRechecksDue(ctx, now)
	if err != nil {
		log.WithContext(ctx).Errorf("failed to list due credentials rechecks: %v", err)
		return
	}
	for _, recheck := range due {
		if err := am.recheckLocalUserCredentials(ctx, recheck, now); err != nil {
			log.WithContext(ctx).Errorf("credentials recheck of user %s failed, retrying next tick: %v",
				recheck.UserID, err)
		}
	}
}

// recheckLocalUserCredentials is the second pass of a reset (ADR-0027 D6):
// for a user still active, it cuts access again exactly like the reset did,
// which revokes the refresh tokens and every JWT a login in flight during
// the reset could have minted since, and expires the user's peers. A user
// deactivated or deleted meanwhile was cut by that transition; its schedule
// is only cleared.
func (am *DefaultAccountManager) recheckLocalUserCredentials(ctx context.Context, recheck store.CredentialsRecheck,
	now time.Time) error {
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthNone, recheck.UserID)
	if err != nil {
		if sErr, ok := status.FromError(err); ok && sErr.Type() == status.NotFound {
			return nil
		}
		return err
	}
	unlock, err := am.lockLocalIdentity(ctx, user.SCIMUserName)
	if err != nil {
		return err
	}
	defer unlock()

	user, err = am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, recheck.UserID)
	if err != nil {
		return err
	}
	if user.LocalCredentialState == types.LocalCredentialActive {
		if err := am.cutLocalUserAccess(ctx, recheck.AccountID, user.Id, now); err != nil {
			return err
		}
		log.WithContext(ctx).Infof("credentials recheck cut the sessions of local user %s", user.Id)
	}
	return am.Store.ClearUserCredentialsRecheck(ctx, recheck.AccountID, user.Id, recheck.At)
}
