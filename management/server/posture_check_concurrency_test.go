package server

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// SavePostureChecks rejects a name already taken in the account. It proves that
// absence with a read (posture_checks.go:213) and then writes on the strength
// of it, which holds only as long as nothing else can insert the same name in
// between.
//
// AcquireWriteLockByUID makes that true inside one process and false across
// two, which is the deployment this project ships. Postgres has no gap locks —
// predicate locking only exists at SERIALIZABLE — so the shared-lock read locks
// the rows that exist, and there are none.
//
// Unlike the resource-name case (#159), the create path takes no exclusive lock
// at all: it validates and saves, with no IncrementNetworkSerial to serialize
// on. That absence is what makes the invariant vulnerable, and it is also what
// makes it awkward to demonstrate — there is no row a third transaction can
// hold to park both replicas inside the window, the way #159 used the network
// row.
//
// Worth knowing before copying this pattern to the next invariant: the barrier
// aligns the two *calls*, not the two transactions. Each side then runs
// permission validation — several round trips — before opening its
// transaction, and on a cold connection pool that offsets one replica past the
// other's entire transaction. The first version of this test passed for
// exactly that reason. The warm-up below removes the offset; without it, a
// green run means nothing.
//
// The engines do not fail this the same way, and running it on MySQL alone
// would look like proof the invariant is fine:
//
//	Postgres   two rows with the same name
//	MySQL      the second insert is refused by InnoDB's gap locks under
//	           REPEATABLE READ, and the loser sees a 1213 deadlock
//
// The assertion below is what both must satisfy: exactly one row survives.
func TestPostureChecks_ConcurrentCreateAcrossReplicas(t *testing.T) {
	const (
		accountID = "posture-race-account"
		userID    = "posture-race-user"
		checkName = "contested-posture-check"
	)

	r := newTwoReplicas(t)
	ctx := context.Background()

	account := newAccountWithId(ctx, accountID, userID, "", false)
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))

	// Warm each replica first. The barrier aligns the two calls, but each then
	// runs permission validation — several round trips — before opening its
	// transaction, and a cold connection pool on one side offsets it past the
	// other's whole transaction. Without this the two never overlap and the
	// test passes for the wrong reason.
	for i, am := range []*DefaultAccountManager{r.A, r.B} {
		_, err := am.SavePostureChecks(ctx, accountID, userID, &posture.Checks{
			Name:   "warmup-" + string(rune('a'+i)),
			Checks: posture.ChecksDefinition{NBVersionCheck: &posture.NBVersionCheck{MinVersion: "0.26.0"}},
		}, true)
		require.NoError(t, err)
	}

	b := newBarrier()
	create := func(am *DefaultAccountManager) <-chan error {
		errCh := make(chan error, 1)
		go func() {
			b.wait(t)
			_, err := am.SavePostureChecks(ctx, accountID, userID, &posture.Checks{
				Name: checkName,
				Checks: posture.ChecksDefinition{
					NBVersionCheck: &posture.NBVersionCheck{MinVersion: "0.26.0"},
				},
			}, true)
			errCh <- err
		}()
		return errCh
	}
	errA, errB := create(r.A), create(r.B)

	join := func(name string, ch <-chan error) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(30 * time.Second):
			t.Fatalf("replica %s never returned from SavePostureChecks", name)
			return nil
		}
	}
	// Both sides are joined before anything is asserted, so a failure on one
	// cannot leave the other running into the next assertion.
	resultA, resultB := join("A", errA), join("B", errB)

	accepted := 0
	for _, err := range []error{resultA, resultB} {
		if err == nil {
			accepted++
		}
	}

	// Measured from committed state through the other replica's store, not from
	// what either side believes it did.
	stored, err := r.B.Store.GetAccountPostureChecks(ctx, store.LockingStrengthNone, accountID)
	require.NoError(t, err)

	named := 0
	for _, check := range stored {
		if check.Name == checkName {
			named++
		}
	}

	require.Equal(t, 1, named,
		"the account holds %d posture checks named %q; SavePostureChecks accepted %d of 2 concurrent creates (A=%v, B=%v)",
		named, checkName, accepted, resultA, resultB)
	require.Equal(t, 1, accepted, "exactly one create must be accepted")

	// The loser's error is engine-specific; assert it where it is
	// deterministic. See the note at the top of this file.
	if types.Engine(strings.ToLower(os.Getenv("OPENZRO_STORE_ENGINE"))) == types.PostgresStoreEngine {
		loser := resultA
		if loser == nil {
			loser = resultB
		}
		require.ErrorContains(t, loser, "already exists",
			"the losing create must be reported as a taken name, not an internal error")
	}
}

// TestPostureChecks_UpdateToTakenNameIsRejected pins the behavior change that
// comes with idx_posture_checks_account_name.
//
// Before it, validatePostureChecks returned early for anything carrying an ID:
// it confirmed the row existed and skipped the duplicate-name check entirely,
// so renaming a posture check onto another one's name was accepted. That was a
// gap rather than a decision — the OpenAPI has described the field as "Posture
// check unique name identifier" since it was introduced, with no exception for
// update, and the early return arrived with the refactor to store methods.
//
// This is the assertion that makes the documented contract true, and it is a
// deliberate behavior change: a rename that used to be accepted now returns a
// validation error.
func TestPostureChecks_UpdateToTakenNameIsRejected(t *testing.T) {
	const (
		accountID = "posture-rename-account"
		userID    = "posture-rename-user"
		takenName = "already-taken"
	)

	r := newTwoReplicas(t)
	ctx := context.Background()

	account := newAccountWithId(ctx, accountID, userID, "", false)
	require.NoError(t, r.A.Store.SaveAccount(ctx, account))

	newCheck := func(name string) *posture.Checks {
		return &posture.Checks{
			Name:   name,
			Checks: posture.ChecksDefinition{NBVersionCheck: &posture.NBVersionCheck{MinVersion: "0.26.0"}},
		}
	}

	_, err := r.A.SavePostureChecks(ctx, accountID, userID, newCheck(takenName), true)
	require.NoError(t, err)

	other, err := r.A.SavePostureChecks(ctx, accountID, userID, newCheck("some-other-name"), true)
	require.NoError(t, err)

	other.Name = takenName
	_, err = r.A.SavePostureChecks(ctx, accountID, userID, other, false)
	require.ErrorContains(t, err, "already exists",
		"renaming a posture check onto a name already in use must be rejected")

	// And the rename left nothing behind: still one row under that name.
	stored, err := r.B.Store.GetAccountPostureChecks(ctx, store.LockingStrengthNone, accountID)
	require.NoError(t, err)
	named := 0
	for _, check := range stored {
		if check.Name == takenName {
			named++
		}
	}
	require.Equal(t, 1, named, "the rejected rename must not have written a second row named %q", takenName)
}
