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
// SaveAccount carries clause.OnConflict{UpdateAll: true}. Postgres renders that
// with the primary key as the conflict target, so a violation of any other
// unique index still surfaces. MySQL renders ON DUPLICATE KEY UPDATE, which has
// no target and absorbs every unique key — so SaveAccount returns nil, writes
// nothing, and hands the caller an account id for a row that does not exist.
//
// That is worse than a duplicate and completely silent, which is why creation
// paths that can collide on idx_accounts_primary_private_domain use
// CreateAccount instead. This asserts both halves: the conflict is reported,
// and the account really was not created.
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
