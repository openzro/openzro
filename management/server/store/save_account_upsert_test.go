package store

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nbdns "github.com/openzro/openzro/dns"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
	nbroute "github.com/openzro/openzro/route"
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
// The same-pointer path used to leave duplicate rows in the in-memory *G
// projections (#165). The database still ended correct because GORM upserts
// associations under FullSaveAssociations, but the object no longer described
// what was being saved and every subsequent save sent more duplicate work.
func TestSaveAccountTwiceWithTheSameObject(t *testing.T) {
	runTestForAllEngines(t, "", func(t *testing.T, store Store) {
		ctx := context.Background()

		account := newAccountWithId(ctx, "resave-acc", "owner-user", "")
		account.Users["owner-user"].PATs = map[string]*types.PersonalAccessToken{
			"pat-a": {
				ID:          "pat-a",
				UserID:      "owner-user",
				Name:        "pat a",
				HashedToken: "hash-a",
				CreatedBy:   "owner-user",
				CreatedAt:   time.Now().UTC(),
			},
		}
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

		require.Len(t, account.PeersG, len(account.Peers), "generated peers must be rebuilt, not accumulated")
		require.Len(t, account.UsersG, len(account.Users), "generated users must be rebuilt, not accumulated")
		require.Len(t, account.SetupKeysG, len(account.SetupKeys), "generated setup keys must be rebuilt, not accumulated")
		require.Len(t, account.GroupsG, len(account.Groups), "generated groups must be rebuilt, not accumulated")
		require.Len(t, account.Users["owner-user"].PATsG, 1, "nested PAT projections must be rebuilt too")

		stored, err := store.GetAccount(ctx, account.Id)
		require.NoError(t, err)
		require.Equal(t, "resaved.example", stored.Domain)
		require.Len(t, stored.Peers, 1, "the peer must remain single in the database")
		require.Len(t, stored.Users, 1, "the user must remain single in the database")
		require.Len(t, stored.SetupKeys, 1, "the setup key must remain single in the database")
		require.Len(t, stored.Groups, len(account.Groups), "the groups must remain single in the database")
		require.Len(t, stored.Users["owner-user"].PATs, 1, "the PAT must remain single in the database")
	})
}

func TestGenerateAccountSQLTypesRebuildsEveryProjection(t *testing.T) {
	ctx := context.Background()
	account := newAccountWithId(ctx, "projection-acc", "projection-user", "")
	account.DNSZones = map[string]*types.DNSZone{
		"zone-a": {AccountID: account.Id, Name: "zone a", Domain: "zone.example"},
	}
	account.NameServerGroups["ns-a"] = &nbdns.NameServerGroup{AccountID: account.Id, Name: "ns a"}
	account.Routes["route-a"] = &nbroute.Route{
		AccountID:   account.Id,
		Network:     netip.MustParsePrefix("10.10.0.0/24"),
		NetworkType: nbroute.IPv4Network,
	}
	account.Users["projection-user"].PATs = map[string]*types.PersonalAccessToken{
		"pat-a": {UserID: "projection-user", Name: "pat a", HashedToken: "hash-a"},
	}
	key, _ := types.GenerateDefaultSetupKey()
	account.SetupKeys[key.Key] = key
	account.Peers["peer-a"] = &nbpeer.Peer{
		Key:    "projection-peer-key",
		IP:     net.IP{127, 0, 0, 4},
		Meta:   nbpeer.PeerSystemMeta{},
		Name:   "peer a",
		Status: &nbpeer.PeerStatus{Connected: true, LastSeen: time.Now().UTC()},
	}

	for range 3 {
		generateAccountSQLTypes(account)
	}

	require.Len(t, account.SetupKeysG, len(account.SetupKeys))
	require.Len(t, account.PeersG, len(account.Peers))
	require.Len(t, account.UsersG, len(account.Users))
	require.Len(t, account.GroupsG, len(account.Groups))
	require.Len(t, account.RoutesG, len(account.Routes))
	require.Len(t, account.NameServerGroupsG, len(account.NameServerGroups))
	require.Len(t, account.DNSZonesG, len(account.DNSZones))
	require.Len(t, account.Users["projection-user"].PATsG, len(account.Users["projection-user"].PATs))
}
