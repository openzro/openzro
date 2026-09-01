package store

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// SaveAccount ends in a Create carrying clause.OnConflict{UpdateAll: true},
// and the two engines render that differently. Postgres names the primary key
// as the conflict target, so a violation of any other unique index still
// escapes. MySQL renders ON DUPLICATE KEY UPDATE, which has no target and
// absorbs every unique key — reporting success, writing nothing the caller
// asked for, and updating whichever row held the conflicting key (#161).
//
// That was harmless while accounts carried no unique index but the primary
// key. idx_accounts_primary_private_domain (#162) is the first, so these tests
// exist to keep the clause honest — or to prove it is not needed.
func TestSaveAccountReportsPrimaryDomainConflict(t *testing.T) {
	runTestForAllEngines(t, "", func(t *testing.T, store Store) {
		ctx := context.Background()

		winner := newAccountWithId(ctx, "winner-acc", "winner-user", "contested.example")
		winner.DomainCategory = types.PrivateCategory
		winner.IsDomainPrimaryAccount = true
		require.NoError(t, store.SaveAccount(ctx, winner))

		loser := newAccountWithId(ctx, "loser-acc", "loser-user", "contested.example")
		loser.DomainCategory = types.PrivateCategory
		loser.IsDomainPrimaryAccount = true

		err := store.SaveAccount(ctx, loser)

		// The poisonous mode is not "wrong error", it is "no error and no
		// write". Assert the report first, then that the report was true.
		require.Error(t, err, "a second primary account for the same private domain must be refused")
		s, ok := status.FromError(err)
		require.True(t, ok && s.Type() == status.AlreadyExists,
			"the refusal must be typed so callers can tell it from an outage, got %v", err)

		_, err = store.GetAccount(ctx, loser.Id)
		require.Error(t, err, "the refused account must not exist; it was reported as refused")

		// And the refusal must not have been paid for by somebody else's row:
		// with UpdateAll the update lands on whichever row held the
		// conflicting key, which is the account nobody asked to touch.
		survivor, err := store.GetAccount(ctx, winner.Id)
		require.NoError(t, err)
		require.Equal(t, "contested.example", survivor.Domain)
		require.True(t, survivor.IsDomainPrimaryAccount)
		require.Contains(t, survivor.Users, "winner-user",
			"the winning account was overwritten with the loser's payload")
	})
}

// The clause is being removed on the argument that SaveAccount already deletes
// the account and its associations inside the same transaction, so the upsert
// is a crutch rather than semantics. That argument is only worth as much as
// this test: create still creates, and replace still replaces.
func TestSaveAccountCreatesAndReplaces(t *testing.T) {
	runTestForAllEngines(t, "", func(t *testing.T, store Store) {
		ctx := context.Background()

		account := newAccountWithId(ctx, "contract-acc", "owner-user", "")
		key, _ := types.GenerateDefaultSetupKey()
		account.SetupKeys[key.Key] = key
		account.Peers["peer-a"] = &nbpeer.Peer{
			Key:    "peer-a-key",
			IP:     net.IP{127, 0, 0, 1},
			Meta:   nbpeer.PeerSystemMeta{},
			Name:   "peer a",
			Status: &nbpeer.PeerStatus{Connected: true, LastSeen: time.Now().UTC()},
		}

		require.NoError(t, store.SaveAccount(ctx, account), "a new account must be created")

		stored, err := store.GetAccount(ctx, account.Id)
		require.NoError(t, err)
		require.Len(t, stored.Peers, 1)
		require.Len(t, stored.SetupKeys, 1)
		require.NotEmpty(t, stored.Policies, "the default policy must have come with it")

		// Save it again, changed, with a different association set. This is
		// the path the removed clause was assumed to serve.
		stored.Peers["peer-b"] = &nbpeer.Peer{
			Key:    "peer-b-key",
			IP:     net.IP{127, 0, 0, 2},
			Meta:   nbpeer.PeerSystemMeta{},
			Name:   "peer b",
			Status: &nbpeer.PeerStatus{Connected: false, LastSeen: time.Now().UTC()},
		}
		delete(stored.Peers, "peer-a")
		stored.Domain = "replaced.example"

		require.NoError(t, store.SaveAccount(ctx, stored), "an existing account must be replaced, not rejected")

		replaced, err := store.GetAccount(ctx, account.Id)
		require.NoError(t, err)
		require.Equal(t, "replaced.example", replaced.Domain)
		require.Len(t, replaced.Peers, 1, "the replacement's peer set must be what was saved")
		require.Contains(t, replaced.Peers, "peer-b")
		require.NotContains(t, replaced.Peers, "peer-a", "a peer dropped from the payload must be gone")
		require.Len(t, store.GetAllAccounts(ctx), 1, "replacing must not leave a second account behind")
	})
}
