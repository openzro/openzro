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
	// credentialsRecheckDelay is when a reset's sweep runs (ADR-0027 D6).
	// Dex v2.45-v2.46 keeps an authorization code valid for 30 minutes
	// (hard-coded) and stamps its tokens with the time of redemption, so a
	// login that passed the old password just before the reset can mint
	// tokens newer than the reset's cutoff until then. One minute of margin
	// covers clock skew and the redemption itself.
	credentialsRecheckDelay = 31 * time.Minute

	// credentialsFenceDelay is how long after a sweep its fence runs. A
	// sweep, like the reset, can race a token Dex is signing at that moment
	// for a request that passed its checks before the revocation. No request
	// authenticated with the old password can start any more (every old code
	// has expired), and a client cannot delay Dex past its checks, but
	// Dex's own processing is not bounded: the fence is a best-effort
	// mitigation, not a guarantee (ADR-0027 D6). auth_time closes it (#221).
	credentialsFenceDelay = time.Minute

	// credentialsRecheckPage bounds how many due passes one query loads.
	credentialsRecheckPage = 50

	// resetRecheckIntervalEnv overrides how often each replica looks for
	// due passes.
	resetRecheckIntervalEnv = "OPENZRO_RESET_RECHECK_INTERVAL_SECONDS"
	// defaultResetRecheckInterval adds at most this much to the window a
	// pre-reset login can hold a session. An idle tick costs one indexed
	// query.
	defaultResetRecheckInterval = time.Minute
	// minResetRecheckInterval floors pathological values so a typo cannot
	// turn the loop into a tight DB loop; 0 and negative values fall back to
	// the default, since the pass bounds a security window.
	minResetRecheckInterval = 5 * time.Second
)

func resetRecheckInterval() time.Duration {
	secs, err := strconv.Atoi(os.Getenv(resetRecheckIntervalEnv))
	if err != nil || secs <= 0 {
		return defaultResetRecheckInterval
	}
	return max(time.Duration(secs)*time.Second, minResetRecheckInterval)
}

// runCredentialsRechecker runs the due passes of local password resets on
// every replica. A pass runs under the user's identity lock and only while
// its row exists, so replicas that load the same pass run it once.
func (am *DefaultAccountManager) runCredentialsRechecker(ctx context.Context) {
	interval := resetRecheckInterval()
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

// runDueCredentialsRechecks runs every pass due at now.
func (am *DefaultAccountManager) runDueCredentialsRechecks(ctx context.Context, now time.Time) {
	am.runDueCredentialsRechecksInPages(ctx, now, credentialsRecheckPage)
}

// runDueCredentialsRechecksInPages works through the due passes in pages of
// at most page rows, by cursor, until none is due, so a backlog does not
// wait for later ticks and no query loads the whole table. A failed pass
// keeps its row and is retried on the next tick; the cursor moves past it,
// so passes that keep failing never starve the ones behind them, and the
// round always ends.
func (am *DefaultAccountManager) runDueCredentialsRechecksInPages(ctx context.Context, now time.Time, page int) {
	if am.localCreds == nil {
		return
	}
	var cursor *store.CredentialsRecheck
	for ctx.Err() == nil {
		due, err := am.Store.ListDueCredentialsRechecks(ctx, now, cursor, page)
		if err != nil {
			log.WithContext(ctx).Errorf("failed to list due credentials rechecks: %v", err)
			return
		}
		for _, recheck := range due {
			if err := am.runCredentialsRecheck(ctx, recheck, now); err != nil {
				log.WithContext(ctx).Errorf("credentials recheck %s of user %s failed, retrying next tick: %v",
					recheck.ID, recheck.UserID, err)
			}
		}
		if len(due) < page {
			return
		}
		cursor = &due[len(due)-1]
	}
}

// runCredentialsRecheck runs one pass (ADR-0027 D6). For a user still
// active it cuts access exactly as the reset did; a sweep then enqueues its
// fence. A user deactivated or deleted meanwhile was cut by that
// transition, so its pass is only discarded. The pass is re-read after the
// identity lock: one that another replica already ran is gone, and running
// it again would cut the sessions of the user's new sign-ins once more.
func (am *DefaultAccountManager) runCredentialsRecheck(ctx context.Context, recheck store.CredentialsRecheck,
	now time.Time) error {
	user, err := am.Store.GetUserByUserID(ctx, store.LockingStrengthNone, recheck.UserID)
	if err != nil {
		if sErr, ok := status.FromError(err); ok && sErr.Type() == status.NotFound {
			return am.Store.DeleteCredentialsRecheck(ctx, recheck.ID)
		}
		return err
	}
	unlock, err := am.lockLocalIdentity(ctx, user.SCIMUserName)
	if err != nil {
		return err
	}
	defer unlock()

	pending, err := am.Store.CredentialsRecheckExists(ctx, recheck.ID)
	if err != nil || !pending {
		return err
	}
	user, err = am.Store.GetUserByUserID(ctx, store.LockingStrengthShare, recheck.UserID)
	if err != nil {
		return err
	}
	if user.LocalCredentialState != types.LocalCredentialActive {
		return am.Store.DeleteCredentialsRecheck(ctx, recheck.ID)
	}
	if err := am.cutLocalUserAccess(ctx, recheck.AccountID, user.Id, now); err != nil {
		return err
	}
	fenceAt := time.Now()
	if now.After(fenceAt) {
		fenceAt = now
	}
	// Completing a sweep and enqueuing its fence is one transaction: a crash
	// in between leaves the sweep pending, and its rerun enqueues the fence.
	if err := am.Store.FinishCredentialsRecheck(ctx, recheck, fenceAt.Add(credentialsFenceDelay)); err != nil {
		return err
	}
	log.WithContext(ctx).Infof("credentials recheck %s cut the sessions of local user %s", recheck.ID, user.Id)
	return nil
}
