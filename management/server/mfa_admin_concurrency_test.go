package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// signalAfterMFARead reports the replica's first read of the user_mfa table
// and then waits for the other replica's, so a read-then-delete reset on
// both replicas has both reads done before either delete. A reset that
// does not read first never triggers it.
func signalAfterMFARead(t *testing.T, s store.Store, mine chan<- struct{}, theirs <-chan struct{}) {
	t.Helper()
	sqlStore, ok := s.(*store.SqlStore)
	require.True(t, ok)
	var once sync.Once
	hook := func(tx *gorm.DB) {
		if tx.Statement.Table != "user_mfas" {
			return
		}
		once.Do(func() {
			close(mine)
			select {
			case <-theirs:
			case <-time.After(2 * time.Second):
			}
		})
	}
	// Not removed on cleanup; see pauseAfterFirstLock.
	require.NoError(t, sqlStore.GetDB().Callback().Query().After("gorm:query").Register("test:mfa-read", hook))
}

// Two replicas resetting the same user's 2FA at once remove one row, so
// exactly one of them audits a reset.
func TestMFADisenroll_ConcurrentResetsAuditOnce(t *testing.T) {
	r := newTwoReplicas(t)
	ctx := context.Background()
	const (
		accountID = "mfa-reset-race-account"
		adminID   = "mfa-reset-race-admin"
		memberID  = "mfa-reset-race-member"
	)
	acc := newAccountWithId(ctx, accountID, adminID, "mfa-reset-race.example", false)
	acc.Users[memberID] = &types.User{Id: memberID, AccountID: accountID, Role: types.UserRoleUser, Issued: types.UserIssuedAPI}
	require.NoError(t, r.A.Store.SaveAccount(ctx, acc))
	require.NoError(t, r.A.Store.SaveUserMFA(ctx, store.LockingStrengthUpdate, &types.UserMFA{
		UserID: memberID, Method: types.MFAMethodTOTP, TOTPSecretEnc: "enc", EnrolledAt: time.Now().UTC(),
	}))

	aRead, bRead := make(chan struct{}), make(chan struct{})
	signalAfterMFARead(t, r.A.Store, aRead, bRead)
	signalAfterMFARead(t, r.B.Store, bRead, aRead)

	align := newBarrier()
	errs := make(chan error, 2)
	for _, am := range []*DefaultAccountManager{r.A, r.B} {
		go func(am *DefaultAccountManager) {
			align.wait(t)
			errs <- am.MFADisenroll(ctx, accountID, adminID, memberID)
		}(am)
	}
	for range 2 {
		select {
		case err := <-errs:
			require.NoError(t, err)
		case <-time.After(30 * time.Second):
			t.Fatal("a reset never returned")
		}
	}

	resets := func() int {
		n := 0
		for _, am := range []*DefaultAccountManager{r.A, r.B} {
			events, err := am.eventStore.Get(ctx, accountID, 0, 200, false)
			require.NoError(t, err)
			for _, e := range events {
				if e.Activity == activity.UserMFAReset {
					n++
				}
			}
		}
		return n
	}
	require.Eventually(t, func() bool { return resets() >= 1 }, 2*time.Second, 20*time.Millisecond)
	assert.Never(t, func() bool { return resets() != 1 }, 500*time.Millisecond, 20*time.Millisecond,
		"one removal must be audited once")

	_, err := r.A.Store.GetUserMFA(ctx, store.LockingStrengthNone, memberID)
	require.ErrorIs(t, err, store.ErrUserMFANotFound)
}
