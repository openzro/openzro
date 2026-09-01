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

// SaveAccount used to end in a Create carrying
// clause.OnConflict{UpdateAll: true}, which the engines rendered
// differently. Postgres named the primary key as the conflict target, so a
// violation of any other unique index still escaped. MySQL rendered ON
// DUPLICATE KEY UPDATE, which has no target: it absorbed every unique key and
// applied the incoming values to whichever row held the conflicting one
// (#161). Harmless while accounts carried no unique index but the primary
// key; idx_accounts_primary_private_domain (#162) was the first, and #164
// removed the clause.
//
// These tests are what that removal rests on. The refusal has to be reported,
// the report has to be true, and it must not be paid for by overwriting the
// account that won.
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

// The same account object saved twice, which is not a contrived case:
// GetOrCreateAccountByUser saves the account it just built and then saves the
// same pointer again when the owner's domain has to be written (user.go:819
// and user.go:836).
//
// generateAccountSQLTypes projects the maps into the *G slices with append and
// no reset, so the second call leaves every child in there twice — measured,
// 1 then 2 then 3 across three saves of one object (#165).
//
// The worry was that removing the parent's upsert turns each duplicate into a
// plain insert of a key that already exists. It does not, and this test
// catches nothing today: the database ends correct on all three engines
// because GORM upserts the associations itself under FullSaveAssociations,
// independently of whatever clause the parent Create carries.
//
// It is here because that correctness rests on GORM behavior nobody chose to
// depend on. If association handling changes, this fails rather than
// GetOrCreateAccountByUser does.
func TestSaveAccountTwiceWithTheSameObject(t *testing.T) {
	runTestForAllEngines(t, "", func(t *testing.T, store Store) {
		ctx := context.Background()

		account := newAccountWithId(ctx, "resave-acc", "owner-user", "")
		key, _ := types.GenerateDefaultSetupKey()
		account.SetupKeys[key.Key] = key
		account.Peers["peer-a"] = &nbpeer.Peer{
			Key:    "resave-peer-key",
			IP:     net.IP{127, 0, 0, 3},
			Meta:   nbpeer.PeerSystemMeta{},
			Name:   "peer a",
			Status: &nbpeer.PeerStatus{Connected: true, LastSeen: time.Now().UTC()},
		}

		require.NoError(t, store.SaveAccount(ctx, account))

		// Same pointer, no reload. This is the shape user.go uses.
		account.Domain = "resaved.example"
		require.NoError(t, store.SaveAccount(ctx, account),
			"saving the same account object twice must not fail")

		stored, err := store.GetAccount(ctx, account.Id)
		require.NoError(t, err)
		require.Equal(t, "resaved.example", stored.Domain)
		require.Len(t, stored.Peers, 1, "the peer was written twice")
		require.Len(t, stored.Users, 1, "the user was written twice")
		require.Len(t, stored.SetupKeys, 1, "the setup key was written twice")
		require.Len(t, stored.Groups, len(account.Groups), "the groups were written twice")
	})
}
