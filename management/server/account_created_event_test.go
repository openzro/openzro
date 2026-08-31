package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/activity"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/types"
)

// account.create must describe an account that exists.
//
// newAccount used to emit it at construction time, which was close enough to
// true while the save that followed could not really fail. It can now: the
// loser of a contested private domain is refused by
// idx_accounts_primary_private_domain and joins the winner's account instead,
// so the event would name an account ID that never reached the database — a
// false record in the audit trail, and in whatever SIEM the activity store
// feeds.
//
// The two halves belong together. On its own, the absence check would pass
// just as well with events switched off entirely, which is exactly the kind of
// vacuous assertion this repository has been bitten by; the second half proves
// events do flow through this setup.
func TestAccountCreatedEventFollowsTheStore(t *testing.T) {
	ctx := context.Background()

	t.Run("building an account announces nothing", func(t *testing.T) {
		am, err := createManager(t)
		require.NoError(t, err)

		account, err := am.newAccount(ctx, "user-not-stored", "unstored.example")
		require.NoError(t, err)

		// StoreEvent is asynchronous, so absence has to hold over an interval
		// rather than at one instant.
		require.Never(t, func() bool {
			events, getErr := am.eventStore.Get(ctx, account.Id, 0, 100, false)
			return getErr == nil && len(events) > 0
		}, 500*time.Millisecond, 50*time.Millisecond,
			"an account that was never stored must not appear in the audit trail")
	})

	t.Run("storing one announces it", func(t *testing.T) {
		am, err := createManager(t)
		require.NoError(t, err)

		accountID, _, err := am.GetAccountIDFromUserAuth(ctx, nbcontext.UserAuth{
			UserId:         "user-stored",
			Domain:         "stored.example",
			DomainCategory: types.PrivateCategory,
		})
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			events, getErr := am.eventStore.Get(ctx, accountID, 0, 100, false)
			if getErr != nil {
				return false
			}
			for _, e := range events {
				if e.Activity == activity.AccountCreated && e.TargetID == accountID {
					return true
				}
			}
			return false
		}, 5*time.Second, 50*time.Millisecond,
			"a stored account must be announced")
	})
}
