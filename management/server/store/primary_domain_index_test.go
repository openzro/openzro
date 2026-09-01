package store

import (
	"context"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/types"
)

// idx_accounts_primary_private_domain has a different shape on every engine: a
// partial index on Postgres and SQLite, a stored generated column plus a plain
// unique index on MySQL. The cross-replica tests that motivated it run only on
// Postgres and MySQL, because SQLite serializes on a single connection and
// cannot exhibit the race — which would leave the SQLite DDL written but never
// executed.
//
// So this exercises the index directly instead of through a race: does the
// database refuse the second account, and does it refuse only the rows the
// predicate covers.
//
// Through CreateAccount rather than SaveAccount, deliberately. SaveAccount's
// upsert absorbs the violation on MySQL and reports success — see
// TestCreateAccountReportsPrimaryDomainConflict below. Asserting the index
// through SaveAccount would measure the upsert, not the index.
func TestPrimaryPrivateDomainIndex(t *testing.T) {
	ctx := context.Background()

	newAccount := func(domain, category string, primary bool) *types.Account {
		return &types.Account{
			Id:                     xid.New().String(),
			Domain:                 domain,
			DomainCategory:         category,
			IsDomainPrimaryAccount: primary,
			Network:                types.NewNetwork(),
		}
	}

	t.Run("a second primary private account for the same domain is refused", func(t *testing.T) {
		store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
		t.Cleanup(cleanup)
		require.NoError(t, err)

		require.NoError(t, store.CreateAccount(ctx, newAccount("contested.example", types.PrivateCategory, true)))

		err = store.CreateAccount(ctx, newAccount("contested.example", types.PrivateCategory, true))
		require.Error(t, err, "the index must refuse a second primary account for the domain")
	})

	t.Run("the domain is compared case-insensitively", func(t *testing.T) {
		store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
		t.Cleanup(cleanup)
		require.NoError(t, err)

		require.NoError(t, store.CreateAccount(ctx, newAccount("mixedcase.example", types.PrivateCategory, true)))

		// The lookup lowercases what it searches for, but not every writer
		// lowercases what it stores. Keying the index on LOWER(domain) is what
		// stops "Foo.com" and "foo.com" from both being primary while the
		// lookup can only ever find one of them.
		err = store.CreateAccount(ctx, newAccount("MixedCase.Example", types.PrivateCategory, true))
		require.Error(t, err, "the index must treat the domain case-insensitively")
	})

	// The negative cases matter as much: an index that refuses too much would
	// break accounts that are legitimately allowed to share a domain.
	t.Run("rows outside the predicate may share the domain", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			category string
			primary  bool
		}{
			{"private but not primary", types.PrivateCategory, false},
			{"primary but not private", "public", true},
			{"neither", "public", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
				t.Cleanup(cleanup)
				require.NoError(t, err)

				require.NoError(t, store.CreateAccount(ctx, newAccount("shared.example", types.PrivateCategory, true)))
				require.NoError(t, store.CreateAccount(ctx, newAccount("shared.example", tc.category, tc.primary)),
					"the index must not constrain rows outside its predicate")
			})
		}
	})
}

// TestCreateAccountReportsPrimaryDomainConflict guards the trap the tests above
// walked past.
//
// It was written when SaveAccount still carried
// clause.OnConflict{UpdateAll: true}, which MySQL rendered without a conflict
// target and which therefore absorbed this index — the save reported success
// having written nothing, or landed the values on the winner's row. #164
// removed the clause and TestSaveAccountReportsPrimaryDomainConflict covers
// that side now.
//
// This one keeps its own value: the creation paths use CreateAccount, and an
// insert-only method has to report the conflict on its own terms. It asserts
// both halves — the conflict is reported, and the account really was not
// created.
func TestCreateAccountReportsPrimaryDomainConflict(t *testing.T) {
	ctx := context.Background()

	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	t.Cleanup(cleanup)
	require.NoError(t, err)

	winner := &types.Account{
		Id: xid.New().String(), Domain: "conflict.example",
		DomainCategory: types.PrivateCategory, IsDomainPrimaryAccount: true, Network: types.NewNetwork(),
	}
	require.NoError(t, store.CreateAccount(ctx, winner))

	loser := &types.Account{
		Id: xid.New().String(), Domain: "conflict.example",
		DomainCategory: types.PrivateCategory, IsDomainPrimaryAccount: true, Network: types.NewNetwork(),
	}
	err = store.CreateAccount(ctx, loser)
	require.Error(t, err, "creating a second primary account for the domain must be reported, never absorbed")

	// The account the caller was told about must not exist. A nil error with a
	// missing row is the failure mode this test exists for; so is an error with
	// the row written anyway.
	_, err = store.GetAccount(ctx, loser.Id)
	require.Error(t, err, "the refused account must not have been created")
}

// The index normalizes with LOWER(domain); the lookups have to agree.
//
// A row stored as "MixedCase.Example" occupies the lowercase slot in the
// index, so the database counts it as the primary account for
// "mixedcase.example". If GetAccountIDByPrivateDomain compares the raw column
// it will not find that row on Postgres or SQLite, where = is case sensitive,
// and the caller concludes the domain is free. It then writes, the index
// refuses it, and the retry looks for a winner that the lookup still cannot
// see — turning a domain that has an owner into a failed login.
//
// Mixed case reaches the column from data written before the domain was
// normalized on the way in, not from a live writer: signup lowercases, and
// updateAccountDomainAttributesIfNotUpToDate lowercases what it sets. It
// carries forward whatever the account already had, which is how an old row
// survives every later login.
func TestPrivateDomainLookupsMatchTheIndex(t *testing.T) {
	ctx := context.Background()

	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	t.Cleanup(cleanup)
	require.NoError(t, err)

	mixed := "MixedCase.Example"
	account := &types.Account{
		Id:                     xid.New().String(),
		Domain:                 mixed,
		DomainCategory:         types.PrivateCategory,
		IsDomainPrimaryAccount: true,
		Network:                types.NewNetwork(),
	}
	require.NoError(t, store.CreateAccount(ctx, account))

	// Both spellings, because two different normalizations have to agree: the
	// caller's argument is lowercased in Go, and the column is lowercased in
	// SQL. Searching only in lower case would exercise the second and leave
	// the first unproven.
	for _, searched := range []string{"mixedcase.example", "MIXEDCASE.EXAMPLE", "MixedCase.Example"} {
		t.Run("GetAccountIDByPrivateDomain finds it, searching "+searched, func(t *testing.T) {
			id, err := store.GetAccountIDByPrivateDomain(ctx, LockingStrengthNone, searched)
			require.NoError(t, err, "the index owns this domain but the lookup cannot see the owner")
			require.Equal(t, account.Id, id)
		})

		t.Run("CountAccountsByPrivateDomain counts it, searching "+searched, func(t *testing.T) {
			count, err := store.CountAccountsByPrivateDomain(ctx, searched)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}
