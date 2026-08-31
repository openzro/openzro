package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// The first user of a corporate domain creates the account that every later
// user of that domain joins. Exactly one account may hold the domain as
// primary — GetAccountIDByPrivateDomain matches on
// `domain = ? AND is_domain_primary_account = true AND domain_category =
// 'private'`, and the rest of onboarding assumes it finds at most one.
//
// That is proven by a read and then written on: getPrivateDomainWithGlobalLock
// looks the domain up, takes AcquireGlobalLock when it finds nothing, looks
// again, and only then does addNewPrivateAccount create the account with
// IsDomainPrimaryAccount = true. The second lookup exists precisely because of
// "simultaneous requests" — but AcquireGlobalLock is s.globalAccountLock, a
// mutex inside one process. With a second replica it serializes nothing.
//
// This is the most reachable of the six invariants in #143 step 3: it does not
// need an administrator doing something unusual, only two people from the same
// company signing in for the first time at the same moment, on different
// replicas. What they get is two primary accounts for one domain, and from
// then on which one a user lands in depends on which row the lookup returns.
//
// The engines differ as in #159 and #160: Postgres persists both, MySQL's gap
// locks under REPEATABLE READ refuse the second insert. The assertion is what
// both must satisfy — one account, and both users inside it.
func TestAccountDomain_ConcurrentFirstLoginAcrossReplicas(t *testing.T) {
	const domain = "contested-company.example"

	r := newTwoReplicas(t)
	ctx := context.Background()

	type loginResult struct {
		accountID string
		err       error
	}
	login := func(am *DefaultAccountManager, userID string) <-chan loginResult {
		resCh := make(chan loginResult, 1)
		go func() {
			b := userAuthFor(userID, domain)
			b.UserId = userID
			accountID, _, err := am.GetAccountIDFromUserAuth(ctx, b)
			resCh <- loginResult{accountID: accountID, err: err}
		}()
		return resCh
	}

	// Warm each replica first: the barrier aligns the calls, not the moment
	// each reaches the database, and a cold pool offsets one past the other.
	// See the note in posture_check_concurrency_test.go.
	for i, am := range []*DefaultAccountManager{r.A, r.B} {
		_, _, err := am.GetAccountIDFromUserAuth(ctx, userAuthFor("warmup-user-"+string(rune('a'+i)), "warmup-"+string(rune('a'+i))+".example"))
		require.NoError(t, err)
	}

	errA, errB := login(r.A, "user-a"), login(r.B, "user-b")

	join := func(name string, ch <-chan loginResult) loginResult {
		t.Helper()
		select {
		case res := <-ch:
			return res
		case <-time.After(30 * time.Second):
			t.Fatalf("replica %s never returned from GetAccountIDFromUserAuth", name)
			return loginResult{}
		}
	}
	resultA, resultB := join("A", errA), join("B", errB)

	// Neither user may be turned away: losing the race is not a login failure.
	require.NoError(t, resultA.err, "first login on replica A must succeed")
	require.NoError(t, resultB.err, "first login on replica B must succeed")

	// Measured from committed state through the other replica's store.
	accounts := r.B.Store.GetAllAccounts(ctx)
	var primaryIDs []string
	for _, acc := range accounts {
		if strings.EqualFold(acc.Domain, domain) && acc.IsDomainPrimaryAccount && acc.DomainCategory == types.PrivateCategory {
			primaryIDs = append(primaryIDs, acc.Id)
		}
	}
	require.Len(t, primaryIDs, 1,
		"the domain %q is primary on %d accounts %v; exactly one may hold it", domain, len(primaryIDs), primaryIDs)

	// Counting primaries is not enough on its own. The failure mode measured
	// on MySQL (#161) is a save that reports success and writes nothing: the
	// loser is told it has an account, one primary row exists, and the count
	// above is satisfied while that user has in fact been dropped. What
	// separates the two is where each caller was sent.
	require.Equal(t, primaryIDs[0], resultA.accountID, "replica A was sent to an account that does not hold the domain")
	require.Equal(t, primaryIDs[0], resultB.accountID, "replica B was sent to an account that does not hold the domain")

	for _, userID := range []string{"user-a", "user-b"} {
		user, err := r.B.Store.GetUserByUserID(ctx, store.LockingStrengthNone, userID)
		require.NoError(t, err, "%s has no user row; the losing login returned an account it was never added to", userID)
		require.Equal(t, primaryIDs[0], user.AccountID, "%s landed outside the account that holds the domain", userID)
	}
}

// userAuthFor builds the claims a first login carries for a private domain.
func userAuthFor(userID, domain string) nbcontext.UserAuth {
	return nbcontext.UserAuth{
		UserId:         userID,
		Domain:         domain,
		DomainCategory: types.PrivateCategory,
	}
}

// TestAccountDomain_ConcurrentExistingLoginAcrossReplicas covers the second
// login path into the same invariant, and the one that is easiest to miss.
//
// handleExistingUserAccount decides
// `primaryDomain := domainAccountID == "" || userAccountID == domainAccountID`
// and hands that to UpdateAccountDomainAttributes, which writes domain,
// domain_category and is_domain_primary_account in a single statement. So a
// user who already has an account claims the domain as primary whenever the
// lookup found none — and that lookup is the same one protected by
// AcquireGlobalLock, a mutex inside one process.
//
// The situation this models is real: a domain that was previously
// unclassified, so several users signed up with accounts of their own, and is
// later classified as private. The next logins race to claim it.
//
// Losing that race must not fail the login. The user whose account does not
// win should simply end up non-primary, which is what the code already does
// when the lookup does find a primary.
func TestAccountDomain_ConcurrentExistingLoginAcrossReplicas(t *testing.T) {
	const domain = "reclassified-company.example"

	r := newTwoReplicas(t)
	ctx := context.Background()

	// Two users who already have accounts on that domain, neither primary —
	// the state a previously unclassified domain leaves behind.
	for _, seed := range []struct{ accountID, userID string }{
		{"existing-account-a", "existing-user-a"},
		{"existing-account-b", "existing-user-b"},
	} {
		account := newAccountWithId(ctx, seed.accountID, seed.userID, domain, false)
		// Previously unclassified: the category is what the login will update,
		// and updateAccountDomainAttributesIfNotUpToDate returns early when the
		// account already matches the claims.
		account.DomainCategory = ""
		account.IsDomainPrimaryAccount = false
		require.NoError(t, r.A.Store.SaveAccount(ctx, account))
	}

	login := func(am *DefaultAccountManager, userID string) <-chan error {
		errCh := make(chan error, 1)
		go func() {
			_, _, err := am.GetAccountIDFromUserAuth(ctx, userAuthFor(userID, domain))
			errCh <- err
		}()
		return errCh
	}

	for i, am := range []*DefaultAccountManager{r.A, r.B} {
		_, _, err := am.GetAccountIDFromUserAuth(ctx,
			userAuthFor("warm-existing-"+string(rune('a'+i)), "warm-existing-"+string(rune('a'+i))+".example"))
		require.NoError(t, err)
	}

	errA, errB := login(r.A, "existing-user-a"), login(r.B, "existing-user-b")

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("replica %s never returned from GetAccountIDFromUserAuth", name)
			return nil
		}
	}
	resultA, resultB := join("A", errA), join("B", errB)

	require.NoError(t, resultA, "an existing user's login must not fail because it lost the primary-domain race")
	require.NoError(t, resultB, "an existing user's login must not fail because it lost the primary-domain race")

	accounts := r.B.Store.GetAllAccounts(ctx)
	primary := 0
	for _, acc := range accounts {
		if acc.Domain == domain && acc.IsDomainPrimaryAccount && acc.DomainCategory == types.PrivateCategory {
			primary++
		}
	}
	require.Equal(t, 1, primary,
		"the domain %q is primary on %d accounts; exactly one may hold it", domain, primary)
}
