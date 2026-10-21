package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// twoReplicaPeerLogin is an enrolled user on a two-replica pair with MFA
// on both sides.
func twoReplicaPeerLogin(t *testing.T) (*twoReplicas, string, string, string) {
	t.Helper()
	r := newTwoReplicas(t)
	ctx := context.Background()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	encKey := base64.StdEncoding.EncodeToString(key)
	require.NoError(t, r.A.SetMFA(encKey))
	require.NoError(t, r.B.SetMFA(encKey))

	const accountID, userID = "peer-login-account", "peer-login-user"
	acc := newAccountWithId(ctx, accountID, userID, "peer-login.example", false)
	acc.Users[userID].Email = "ana@example.com"
	require.NoError(t, r.A.Store.SaveAccount(ctx, acc))
	start, err := r.A.MFAStartEnrollment(ctx, accountID, userID)
	require.NoError(t, err)
	_, err = r.A.MFAFinishEnrollment(ctx, accountID, userID, start.PendingToken,
		totpCodeAt(t, start.Secret, time.Now()), "jti-enroll")
	require.NoError(t, err)
	return r, accountID, userID, start.Secret
}

// runOnBoth runs fn on the two replicas n times in total, alternating, all
// released together.
func runOnBoth[T any](t *testing.T, r *twoReplicas, n int, fn func(am *DefaultAccountManager, i int) T) []T {
	t.Helper()
	ready := make(chan struct{})
	results := make(chan T, n)
	for i := 0; i < n; i++ {
		am := r.A
		if i%2 == 1 {
			am = r.B
		}
		go func(am *DefaultAccountManager, i int) {
			<-ready
			results <- fn(am, i)
		}(am, i)
	}
	close(ready)
	out := make([]T, 0, n)
	for range n {
		select {
		case v := <-results:
			out = append(out, v)
		case <-time.After(30 * time.Second):
			t.Fatal("a call never returned")
		}
	}
	return out
}

// Logins for one key arriving on both replicas at once share one record.
func TestRequestPeerLoginMFAApproval_ConcurrentSameKey(t *testing.T) {
	r, accountID, userID, _ := twoReplicaPeerLogin(t)
	ctx := context.Background()
	ids := runOnBoth(t, r, 4, func(am *DefaultAccountManager, _ int) string {
		ch, err := am.RequestPeerLoginMFAApproval(ctx, PeerLoginMFARequest{
			AccountID: accountID, UserID: userID, WGPubKey: "same-key"})
		if err != nil {
			return "error: " + err.Error()
		}
		return ch.ApprovalID
	})
	for _, id := range ids {
		assert.Equal(t, ids[0], id)
	}
	live, err := r.A.Store.GetUserPeerLoginMFAApprovals(ctx, store.LockingStrengthNone, accountID, userID, time.Now())
	require.NoError(t, err)
	assert.Len(t, live, 1)
}

// Logins for many keys at once never leave more than the cap of records.
func TestRequestPeerLoginMFAApproval_ConcurrentCap(t *testing.T) {
	r, accountID, userID, _ := twoReplicaPeerLogin(t)
	ctx := context.Background()
	const n = types.PeerLoginMFAMaxPerUser + 3
	errs := runOnBoth(t, r, n, func(am *DefaultAccountManager, i int) error {
		_, err := am.RequestPeerLoginMFAApproval(ctx, PeerLoginMFARequest{
			AccountID: accountID, UserID: userID, WGPubKey: fmt.Sprintf("key-%d", i)})
		return err
	})
	limited := 0
	for _, err := range errs {
		var rl *PeerLoginMFARateLimitedError
		if errors.As(err, &rl) {
			limited++
			continue
		}
		require.NoError(t, err)
	}
	assert.Equal(t, n-types.PeerLoginMFAMaxPerUser, limited)
	live, err := r.A.Store.GetUserPeerLoginMFAApprovals(ctx, store.LockingStrengthNone, accountID, userID, time.Now())
	require.NoError(t, err)
	assert.Len(t, live, types.PeerLoginMFAMaxPerUser)
}

// Wrong codes sent to both replicas at once are counted exactly: the lock
// engages at the fifth and no later attempt is checked.
func TestApprovePeerLoginMFA_ConcurrentWrongCodes(t *testing.T) {
	r, accountID, userID, _ := twoReplicaPeerLogin(t)
	ctx := context.Background()
	ch, err := r.A.RequestPeerLoginMFAApproval(ctx, PeerLoginMFARequest{
		AccountID: accountID, UserID: userID, WGPubKey: "key"})
	require.NoError(t, err)

	const n = types.MFALockoutThreshold + 3
	locked := runOnBoth(t, r, n, func(am *DefaultAccountManager, _ int) bool {
		// Not require: this runs off the test goroutine.
		out, err := am.ApprovePeerLoginMFA(ctx, ch.Token, "000000")
		if !assert.NoError(t, err) {
			return false
		}
		assert.False(t, out.Approved)
		return out.Locked
	})
	lockedCount := 0
	for _, l := range locked {
		if l {
			lockedCount++
		}
	}
	// Attempts 1–4 are counted unlocked, the fifth sets the lock, the rest
	// find it set.
	assert.Equal(t, n-(types.MFALockoutThreshold-1), lockedCount)
	row, err := r.A.Store.GetUserMFA(ctx, store.LockingStrengthNone, userID)
	require.NoError(t, err)
	assert.Equal(t, types.MFALockoutThreshold, row.FailedAttempts)
}

// Both replicas sweeping the same expired records report each once.
func TestSweepPeerLoginMFAApprovals_ConcurrentReplicas(t *testing.T) {
	r, accountID, userID, _ := twoReplicaPeerLogin(t)
	ctx := context.Background()
	const n = types.PeerLoginMFAMaxPerUser
	for i := 0; i < n; i++ {
		_, err := r.A.RequestPeerLoginMFAApproval(ctx, PeerLoginMFARequest{
			AccountID: accountID, UserID: userID, WGPubKey: fmt.Sprintf("key-%d", i)})
		require.NoError(t, err)
	}
	later := time.Now().Add(types.PeerLoginMFADeadline)
	runOnBoth(t, r, 2, func(am *DefaultAccountManager, _ int) struct{} {
		am.sweepPeerLoginMFAApprovals(ctx, later, 2)
		return struct{}{}
	})

	expired := func() int {
		total := 0
		for _, am := range []*DefaultAccountManager{r.A, r.B} {
			total += countEvents(t, am, accountID, activity.PeerLoginMFAExpired)
		}
		return total
	}
	require.Eventually(t, func() bool { return expired() == n }, 3*time.Second, 20*time.Millisecond)
	assert.Never(t, func() bool { return expired() != n }, 500*time.Millisecond, 20*time.Millisecond)
}
