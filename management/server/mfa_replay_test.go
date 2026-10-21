package server

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/store"
)

// mfaTestClock is a settable clock for stepping through TOTP windows.
type mfaTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newMFATestClock() *mfaTestClock {
	// One second into a 30-second step, so a step never ends mid-test.
	return &mfaTestClock{now: time.Date(2026, 10, 21, 12, 0, 1, 0, time.UTC)}
}

func (c *mfaTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mfaTestClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func totpCodeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, at)
	require.NoError(t, err)
	return code
}

// enrollAt enrolls the user with the code of the clock's current step and
// returns the TOTP secret.
func enrollAt(t *testing.T, am *DefaultAccountManager, clock *mfaTestClock, accountID, userID string) string {
	t.Helper()
	ctx := context.Background()
	start, err := am.MFAStartEnrollment(ctx, accountID, userID)
	require.NoError(t, err)
	_, err = am.MFAFinishEnrollment(ctx, accountID, userID, start.PendingToken,
		totpCodeAt(t, start.Secret, clock.Now()), "jti-enroll")
	require.NoError(t, err)
	return start.Secret
}

func failedAttempts(t *testing.T, am *DefaultAccountManager, userID string) int {
	t.Helper()
	row, err := am.Store.GetUserMFA(context.Background(), store.LockingStrengthNone, userID)
	require.NoError(t, err)
	return row.FailedAttempts
}

// A TOTP code works once (RFC 6238 §5.2). The code that finished enrollment
// is already used, a code is refused the second time, and so is a code from
// an earlier step. A reused code is refused without counting as a failed
// guess: it proves knowledge of the secret.
func TestMFAChallenge_RefusesReusedCodes(t *testing.T) {
	am := newMFATestManager(t)
	clock := newMFATestClock()
	am.mfaClock = clock.Now
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "dave", "example.com")
	require.NoError(t, err)
	setUserEmail(t, am, "dave", "dave@example.com")
	secret := enrollAt(t, am, clock, acc.Id, "dave")
	enrolledAt := clock.Now()

	outcome, err := am.MFAChallenge(ctx, "dave", totpCodeAt(t, secret, enrolledAt), "jti-1")
	require.NoError(t, err)
	assert.False(t, outcome.OK, "the enrollment code is already used")
	assert.True(t, outcome.CodeReused)
	assert.Equal(t, 0, failedAttempts(t, am, "dave"))

	clock.Advance(30 * time.Second)
	next := totpCodeAt(t, secret, clock.Now())
	outcome, err = am.MFAChallenge(ctx, "dave", next, "jti-2")
	require.NoError(t, err)
	require.True(t, outcome.OK, "a code from a later step is accepted")
	assert.NotEmpty(t, outcome.SessionToken)

	outcome, err = am.MFAChallenge(ctx, "dave", next, "jti-3")
	require.NoError(t, err)
	assert.False(t, outcome.OK, "the same code is refused the second time")
	assert.True(t, outcome.CodeReused)
	assert.Empty(t, outcome.SessionToken)

	outcome, err = am.MFAChallenge(ctx, "dave", totpCodeAt(t, secret, enrolledAt), "jti-4")
	require.NoError(t, err)
	assert.False(t, outcome.OK, "a code from an earlier step, still within the skew window, is refused")
	assert.True(t, outcome.CodeReused)
	assert.Equal(t, 0, failedAttempts(t, am, "dave"))
}

// A code from the next step, accepted early through the clock-skew window,
// uses that step: the same code is refused once its step arrives.
func TestMFAChallenge_FutureStepCountsAsUsed(t *testing.T) {
	am := newMFATestManager(t)
	clock := newMFATestClock()
	am.mfaClock = clock.Now
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "erin", "example.com")
	require.NoError(t, err)
	setUserEmail(t, am, "erin", "erin@example.com")
	secret := enrollAt(t, am, clock, acc.Id, "erin")

	ahead := totpCodeAt(t, secret, clock.Now().Add(30*time.Second))
	outcome, err := am.MFAChallenge(ctx, "erin", ahead, "jti-1")
	require.NoError(t, err)
	require.True(t, outcome.OK)

	clock.Advance(30 * time.Second)
	outcome, err = am.MFAChallenge(ctx, "erin", ahead, "jti-2")
	require.NoError(t, err)
	assert.False(t, outcome.OK)
	assert.True(t, outcome.CodeReused)
}

// The same code sent to two replicas at once is accepted by exactly one.
func TestMFAChallenge_ConcurrentReuseAcrossReplicas(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	encKey := base64.StdEncoding.EncodeToString(key)
	require.NoError(t, r.A.SetMFA(encKey))
	require.NoError(t, r.B.SetMFA(encKey))

	const accountID, userID = "mfa-replay-account", "mfa-replay-user"
	acc := newAccountWithId(ctx, accountID, userID, "mfa-replay.example", false)
	acc.Users[userID].Email = "replay@example.com"
	require.NoError(t, r.A.Store.SaveAccount(ctx, acc))

	start, err := r.A.MFAStartEnrollment(ctx, accountID, userID)
	require.NoError(t, err)
	_, err = r.A.MFAFinishEnrollment(ctx, accountID, userID, start.PendingToken,
		totpCodeAt(t, start.Secret, time.Now()), "jti-enroll")
	require.NoError(t, err)

	// The next step's code: unused, and within the skew window now.
	code := totpCodeAt(t, start.Secret, time.Now().Add(30*time.Second))
	align := newBarrier()
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	for i, am := range []*DefaultAccountManager{r.A, r.B} {
		go func(am *DefaultAccountManager, jti string) {
			align.wait(t)
			outcome, err := am.MFAChallenge(ctx, userID, code, jti)
			if err != nil {
				errs <- err
				return
			}
			results <- outcome.OK
		}(am, []string{"jti-a", "jti-b"}[i])
	}
	accepted := 0
	for range 2 {
		select {
		case err := <-errs:
			t.Fatalf("challenge failed: %v", err)
		case ok := <-results:
			if ok {
				accepted++
			}
		case <-time.After(30 * time.Second):
			t.Fatal("a challenge never returned")
		}
	}
	assert.Equal(t, 1, accepted, "one code, one acceptance")

	row, err := r.A.Store.GetUserMFA(ctx, store.LockingStrengthNone, userID)
	require.NoError(t, err)
	assert.Equal(t, 0, row.FailedAttempts)
}

// pauseAfterMFARead holds the replica's next read of user_mfas until
// release closes, or for at most two seconds when the read holds a lock
// that the other side is waiting on.
func pauseAfterMFARead(t *testing.T, s store.Store, read chan<- struct{}, release <-chan struct{}) {
	t.Helper()
	sqlStore, ok := s.(*store.SqlStore)
	require.True(t, ok)
	var once sync.Once
	hook := func(tx *gorm.DB) {
		if tx.Statement.Table != "user_mfas" {
			return
		}
		once.Do(func() {
			close(read)
			select {
			case <-release:
			case <-time.After(2 * time.Second):
			}
		})
	}
	// Not removed on cleanup; see pauseAfterFirstLock.
	require.NoError(t, sqlStore.GetDB().Callback().Query().After("gorm:query").Register("test:pause-mfa-read", hook))
}

// Regenerating backup codes must not write back a stale last_used_step. If
// it saved the row it read before another replica accepted a code, that
// code would be accepted again.
func TestMFARegenerateBackupCodes_KeepsConcurrentlyUsedStep(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	encKey := base64.StdEncoding.EncodeToString(key)
	require.NoError(t, r.A.SetMFA(encKey))
	require.NoError(t, r.B.SetMFA(encKey))

	const accountID, userID = "mfa-regen-account", "mfa-regen-user"
	acc := newAccountWithId(ctx, accountID, userID, "mfa-regen.example", false)
	acc.Users[userID].Email = "regen@example.com"
	require.NoError(t, r.A.Store.SaveAccount(ctx, acc))
	start, err := r.A.MFAStartEnrollment(ctx, accountID, userID)
	require.NoError(t, err)
	_, err = r.A.MFAFinishEnrollment(ctx, accountID, userID, start.PendingToken,
		totpCodeAt(t, start.Secret, time.Now()), "jti-enroll")
	require.NoError(t, err)
	code := totpCodeAt(t, start.Secret, time.Now().Add(30*time.Second))

	aRead, releaseA := make(chan struct{}), make(chan struct{})
	pauseAfterMFARead(t, r.A.Store, aRead, releaseA)

	regenErr := make(chan error, 1)
	go func() {
		_, err := r.A.MFARegenerateBackupCodes(ctx, userID)
		regenErr <- err
	}()
	select {
	case <-aRead:
	case <-time.After(10 * time.Second):
		t.Fatal("regeneration never read the row")
	}

	// B accepts the code while A holds its read. If A's read locks the row,
	// B waits for A instead, and A goes on after its timeout.
	challenged := make(chan bool, 1)
	go func() {
		outcome, err := r.B.MFAChallenge(ctx, userID, code, "jti-b")
		challenged <- err == nil && outcome.OK
	}()
	select {
	case ok := <-challenged:
		require.True(t, ok, "the first use of the code is accepted")
		close(releaseA)
	case <-time.After(3 * time.Second):
		close(releaseA)
		require.True(t, <-challenged, "the first use of the code is accepted")
	}
	require.NoError(t, <-regenErr)

	outcome, err := r.B.MFAChallenge(ctx, userID, code, "jti-c")
	require.NoError(t, err)
	assert.False(t, outcome.OK, "the code was already used")
	assert.True(t, outcome.CodeReused)
}
