package server

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// peerLoginFixture is an enrolled user with a stepped clock shared by the
// manager and its token signer.
type peerLoginFixture struct {
	am        *DefaultAccountManager
	clock     *mfaTestClock
	accountID string
	userID    string
	secret    string
}

func newPeerLoginFixture(t *testing.T, userID string) *peerLoginFixture {
	t.Helper()
	return newPeerLoginFixtureOn(t, newMFATestManager(t), userID)
}

// newPeerLoginFixtureOn builds the fixture on am, whose MFA subsystem is
// set up.
func newPeerLoginFixtureOn(t *testing.T, am *DefaultAccountManager, userID string) *peerLoginFixture {
	t.Helper()
	clock := newMFATestClock()
	am.mfaClock = clock.Now
	am.mfaSigner.SetClock(clock.Now)
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, userID, "example.com")
	require.NoError(t, err)
	setUserEmail(t, am, userID, "ana.lima@example.com")
	secret := enrollAt(t, am, clock, acc.Id, userID)
	// Leave the enrollment step behind, so the next code is unused.
	clock.Advance(30 * time.Second)
	return &peerLoginFixture{am: am, clock: clock, accountID: acc.Id, userID: userID, secret: secret}
}

func (f *peerLoginFixture) request(t *testing.T, key string) (*PeerLoginMFAChallenge, error) {
	t.Helper()
	return f.am.RequestPeerLoginMFAApproval(context.Background(), PeerLoginMFARequest{
		AccountID: f.accountID, UserID: f.userID, WGPubKey: key,
		Hostname: "ana-laptop", OS: "linux", IP: "203.0.113.7", GeoCountry: "BR", GeoCity: "São Paulo", NewPeer: true,
		JWTIssuedAt: f.clock.Now(),
	})
}

func (f *peerLoginFixture) code(t *testing.T) string {
	t.Helper()
	return totpCodeAt(t, f.secret, f.clock.Now())
}

func (f *peerLoginFixture) record(t *testing.T, id string) *types.PeerLoginMFAApproval {
	t.Helper()
	a, err := f.am.Store.GetPeerLoginMFAApprovalByID(context.Background(), store.LockingStrengthNone, id)
	require.NoError(t, err)
	return a
}

const wgKeyA = "Oxc1ylvTfBm7y8MbEZvyMMYw5LwMqLbqvPxbTN0cCVQ="

// A login for the same key reuses its pending record; the user holds at
// most five unexpired records, in any state; a denied key stays denied
// until its record expires.
func TestRequestPeerLoginMFAApproval(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	t0 := f.clock.Now()

	first, err := f.request(t, wgKeyA)
	require.NoError(t, err)
	assert.NotEmpty(t, first.Token)
	assert.True(t, t0.Add(types.PeerLoginMFAWindow).Equal(first.ExpiresAt))
	rec := f.record(t, first.ApprovalID)
	assert.Equal(t, "ana-laptop", rec.Hostname)
	assert.True(t, t0.Add(types.PeerLoginMFADeadline).Equal(rec.Deadline))
	requireEventually(t, f.am, f.accountID, activity.PeerLoginMFARequired)

	f.clock.Advance(time.Minute)
	again, err := f.request(t, wgKeyA)
	require.NoError(t, err)
	assert.Equal(t, first.ApprovalID, again.ApprovalID, "the pending record is reused")

	for i := 2; i <= types.PeerLoginMFAMaxPerUser; i++ {
		_, err := f.request(t, "key-"+string(rune('0'+i)))
		require.NoError(t, err)
	}
	f.clock.Advance(30 * time.Second)
	_, err = f.request(t, "key-6")
	var limited *PeerLoginMFARateLimitedError
	require.True(t, errors.As(err, &limited), "the sixth key is rate limited, got %v", err)
	// The oldest record (created at t0) frees a slot first.
	assert.Equal(t, t0.Add(types.PeerLoginMFAWindow).Sub(f.clock.Now()), limited.RetryAfter)

	_, err = f.am.DenyPeerLoginMFA(context.Background(), first.Token)
	require.NoError(t, err)
	_, err = f.request(t, wgKeyA)
	var denied *PeerLoginMFADeniedError
	require.True(t, errors.As(err, &denied), "a denied key stays denied, got %v", err)
	assert.Equal(t, t0.Add(types.PeerLoginMFAWindow).Sub(f.clock.Now()), denied.RetryAfter)

	// Once every record expires, the key may ask again.
	f.clock.Advance(types.PeerLoginMFAWindow)
	_, err = f.request(t, wgKeyA)
	require.NoError(t, err)
}

// The page shows the attempt with the email partly masked; a token for
// another user's record is refused.
func TestGetPeerLoginMFAApproval(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	ch, err := f.request(t, wgKeyA)
	require.NoError(t, err)

	view, err := f.am.GetPeerLoginMFAApproval(context.Background(), ch.Token)
	require.NoError(t, err)
	assert.Equal(t, account.PeerLoginMFAPending, view.State)
	assert.Equal(t, "a***@example.com", view.MaskedEmail)
	assert.Equal(t, "ana-laptop", view.Hostname)
	assert.Equal(t, "203.0.113.7", view.IP)
	assert.Equal(t, "Oxc1yl…CVQ=", view.KeyFingerprint)

	forged, err := f.am.mfaSigner.IssuePeerLogin("someone-else", ch.ApprovalID, f.clock.Now().Add(time.Minute))
	require.NoError(t, err)
	_, err = f.am.GetPeerLoginMFAApproval(context.Background(), forged)
	requireStatus(t, err, status.Unauthorized)
	_, err = f.am.GetPeerLoginMFAApproval(context.Background(), "garbage")
	requireStatus(t, err, status.Unauthorized)
}

// Approving takes the user's code once, extends the record within its
// deadline, and a retry of an approval whose response was lost succeeds
// without a code. Final states refuse with PreconditionFailed.
func TestApprovePeerLoginMFA(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	ctx := context.Background()
	ch, err := f.request(t, wgKeyA)
	require.NoError(t, err)
	created := f.clock.Now()

	out, err := f.am.ApprovePeerLoginMFA(ctx, ch.Token, "000000")
	require.NoError(t, err)
	assert.False(t, out.Approved, "a wrong code does not approve")
	assert.Equal(t, 1, failedAttempts(t, f.am, f.userID))
	requireEventually(t, f.am, f.accountID, activity.PeerLoginMFAFailed)

	// Approve just before the initial window ends.
	f.clock.Advance(types.PeerLoginMFAWindow - time.Second)
	code := f.code(t)
	out, err = f.am.ApprovePeerLoginMFA(ctx, ch.Token, code)
	require.NoError(t, err)
	require.True(t, out.Approved)
	assert.Equal(t, 0, failedAttempts(t, f.am, f.userID))
	rec := f.record(t, ch.ApprovalID)
	assert.True(t, f.clock.Now().Add(types.PeerLoginMFAApprovedGrace).Equal(rec.ExpiresAt),
		"the approval gets its full grace, got %s", rec.ExpiresAt)
	assert.False(t, rec.ExpiresAt.After(created.Add(types.PeerLoginMFADeadline)), "never past the deadline")
	requireEventually(t, f.am, f.accountID, activity.PeerLoginMFAApproved)

	// The response was lost: a retry near the end of the extension succeeds
	// with any code, since the approval already happened.
	f.clock.Advance(90 * time.Second)
	out, err = f.am.ApprovePeerLoginMFA(ctx, ch.Token, "")
	require.NoError(t, err)
	assert.True(t, out.Approved)

	// The login consumes it; approving again is refused.
	ok, err := f.am.Store.ConsumePeerLoginMFAApproval(ctx, f.accountID, f.userID, wgKeyA, f.clock.Now())
	require.NoError(t, err)
	require.True(t, ok)
	_, err = f.am.ApprovePeerLoginMFA(ctx, ch.Token, f.code(t))
	requireStatus(t, err, status.PreconditionFailed)
}

func TestApprovePeerLoginMFA_RefusedStates(t *testing.T) {
	ctx := context.Background()

	t.Run("reused code", func(t *testing.T) {
		f := newPeerLoginFixture(t, "ana")
		ch, err := f.request(t, wgKeyA)
		require.NoError(t, err)
		code := f.code(t)
		outcome, err := f.am.MFAChallenge(ctx, f.userID, code, "jti")
		require.NoError(t, err)
		require.True(t, outcome.OK)
		out, err := f.am.ApprovePeerLoginMFA(ctx, ch.Token, code)
		require.NoError(t, err)
		assert.False(t, out.Approved)
		assert.True(t, out.CodeReused, "a code used on the dashboard cannot approve a device")
	})
	t.Run("denied", func(t *testing.T) {
		f := newPeerLoginFixture(t, "ana")
		ch, err := f.request(t, wgKeyA)
		require.NoError(t, err)
		_, err = f.am.DenyPeerLoginMFA(ctx, ch.Token)
		require.NoError(t, err)
		_, err = f.am.ApprovePeerLoginMFA(ctx, ch.Token, f.code(t))
		requireStatus(t, err, status.PreconditionFailed)
	})
	t.Run("expired", func(t *testing.T) {
		f := newPeerLoginFixture(t, "ana")
		ch, err := f.request(t, wgKeyA)
		require.NoError(t, err)
		f.clock.Advance(types.PeerLoginMFAWindow)
		_, err = f.am.ApprovePeerLoginMFA(ctx, ch.Token, f.code(t))
		requireStatus(t, err, status.PreconditionFailed)
	})
	t.Run("past the deadline the token itself is refused", func(t *testing.T) {
		f := newPeerLoginFixture(t, "ana")
		ch, err := f.request(t, wgKeyA)
		require.NoError(t, err)
		f.clock.Advance(types.PeerLoginMFADeadline)
		_, err = f.am.ApprovePeerLoginMFA(ctx, ch.Token, f.code(t))
		requireStatus(t, err, status.Unauthorized)
	})
	t.Run("no 2FA", func(t *testing.T) {
		f := newPeerLoginFixture(t, "ana")
		ch, err := f.request(t, wgKeyA)
		require.NoError(t, err)
		_, err = f.am.Store.DeleteUserMFA(ctx, store.LockingStrengthUpdate, f.userID)
		require.NoError(t, err)
		_, err = f.am.ApprovePeerLoginMFA(ctx, ch.Token, f.code(t))
		requireStatus(t, err, status.PreconditionFailed)
	})
}

// The page shares the dashboard challenge's lockout: five wrong codes lock
// the user, and while locked no code is checked or counted.
func TestApprovePeerLoginMFA_Lockout(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	ctx := context.Background()
	ch, err := f.request(t, wgKeyA)
	require.NoError(t, err)

	for i := 1; i <= types.MFALockoutThreshold; i++ {
		out, err := f.am.ApprovePeerLoginMFA(ctx, ch.Token, "000000")
		require.NoError(t, err)
		assert.Equal(t, i == types.MFALockoutThreshold, out.Locked, "attempt %d", i)
	}
	out, err := f.am.ApprovePeerLoginMFA(ctx, ch.Token, f.code(t))
	require.NoError(t, err)
	assert.False(t, out.Approved, "a right code is not checked while locked")
	assert.True(t, out.Locked)
	require.NotNil(t, out.LockedUntil)
	assert.Equal(t, types.MFALockoutThreshold, failedAttempts(t, f.am, f.userID))
}

// "Not me" denies a pending attempt once; denying again is harmless; an
// approved attempt cannot be denied.
func TestDenyPeerLoginMFA(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	ctx := context.Background()
	ch, err := f.request(t, wgKeyA)
	require.NoError(t, err)

	view, err := f.am.DenyPeerLoginMFA(ctx, ch.Token)
	require.NoError(t, err)
	assert.Equal(t, account.PeerLoginMFADenied, view.State)
	requireEventually(t, f.am, f.accountID, activity.PeerLoginMFADenied)
	events, err := f.am.eventStore.Get(ctx, f.accountID, 0, 200, false)
	require.NoError(t, err)
	for _, e := range events {
		if e.Activity == activity.PeerLoginMFADenied {
			assert.Equal(t, activity.SystemInitiator, e.InitiatorID, "a denial by link is not attributed to the user")
			assert.Equal(t, f.userID, e.TargetID)
		}
	}
	view, err = f.am.DenyPeerLoginMFA(ctx, ch.Token)
	require.NoError(t, err)
	assert.Equal(t, account.PeerLoginMFADenied, view.State)

	other, err := f.request(t, "key-2")
	require.NoError(t, err)
	out, err := f.am.ApprovePeerLoginMFA(ctx, other.Token, f.code(t))
	require.NoError(t, err)
	require.True(t, out.Approved)
	_, err = f.am.DenyPeerLoginMFA(ctx, other.Token)
	requireStatus(t, err, status.PreconditionFailed)
}

func countEvents(t *testing.T, am *DefaultAccountManager, accountID string, code activity.Activity) int {
	t.Helper()
	events, err := am.eventStore.Get(context.Background(), accountID, 0, 500, false)
	require.NoError(t, err)
	n := 0
	for _, e := range events {
		if e.Activity == code {
			n++
		}
	}
	return n
}

// The sweep deletes expired records in batches and audits as expired only
// the attempts nobody approved or denied.
func TestSweepPeerLoginMFAApprovals(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	ctx := context.Background()
	read := f.peerLoginMFAMeter(t)
	pending, err := f.request(t, "key-pending")
	require.NoError(t, err)
	approved, err := f.request(t, "key-approved")
	require.NoError(t, err)
	out, err := f.am.ApprovePeerLoginMFA(ctx, approved.Token, f.code(t))
	require.NoError(t, err)
	require.True(t, out.Approved)
	denied, err := f.request(t, "key-denied")
	require.NoError(t, err)
	_, err = f.am.DenyPeerLoginMFA(ctx, denied.Token)
	require.NoError(t, err)

	f.clock.Advance(types.PeerLoginMFADeadline)
	fresh, err := f.request(t, "key-fresh")
	require.NoError(t, err)

	// A batch of one still works through the backlog.
	f.am.sweepPeerLoginMFAApprovals(ctx, f.clock.Now(), 1)

	for _, id := range []string{pending.ApprovalID, approved.ApprovalID, denied.ApprovalID} {
		_, err := f.am.Store.GetPeerLoginMFAApprovalByID(ctx, store.LockingStrengthNone, id)
		require.ErrorIs(t, err, store.ErrPeerLoginMFAApprovalNotFound)
	}
	f.record(t, fresh.ApprovalID)
	assert.Eventually(t, func() bool {
		return countEvents(t, f.am, f.accountID, activity.PeerLoginMFAExpired) == 1
	}, 2*time.Second, 20*time.Millisecond)
	assert.Never(t, func() bool {
		return countEvents(t, f.am, f.accountID, activity.PeerLoginMFAExpired) != 1
	}, 300*time.Millisecond, 20*time.Millisecond, "only the attempt nobody acted on expired")
	outcomes, _ := read("management.peer.login.mfa.outcome.counter")
	assert.Equal(t, int64(1), outcomes["expired"], "the expiry is counted once, by the replica that swept it")
}

// A 2FA reset removes the user's approval records in the same transaction
// as the factor, so an approved login can no longer be consumed.
func TestMFADisenroll_ClearsPeerLoginApprovals(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	ctx := context.Background()
	ch, err := f.request(t, wgKeyA)
	require.NoError(t, err)
	out, err := f.am.ApprovePeerLoginMFA(ctx, ch.Token, f.code(t))
	require.NoError(t, err)
	require.True(t, out.Approved)

	require.NoError(t, f.am.MFADisenroll(ctx, f.accountID, f.userID, f.userID))

	ok, err := f.am.Store.ConsumePeerLoginMFAApproval(ctx, f.accountID, f.userID, wgKeyA, f.clock.Now())
	require.NoError(t, err)
	assert.False(t, ok, "the approval did not survive the reset")
	_, err = f.am.Store.GetPeerLoginMFAApprovalByID(ctx, store.LockingStrengthNone, ch.ApprovalID)
	require.ErrorIs(t, err, store.ErrPeerLoginMFAApprovalNotFound)

	// Clearing approvals of a user with no 2FA is still done, and still
	// not audited as a reset.
	_, err = f.request(t, "key-2")
	require.NoError(t, err)
	require.NoError(t, f.am.MFADisenroll(ctx, f.accountID, f.userID, f.userID))
	live, err := f.am.Store.GetUserPeerLoginMFAApprovals(ctx, store.LockingStrengthNone, f.accountID, f.userID, f.clock.Now())
	require.NoError(t, err)
	assert.Empty(t, live)
	assert.Equal(t, 1, countEvents(t, f.am, f.accountID, activity.UserMFADisabled))
}

// A password reset cuts the user's access, including logins waiting for or
// holding a 2FA approval.
func TestSCIMResetLocalPassword_ClearsPeerLoginApprovals(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	now := time.Now().UTC()
	require.NoError(t, m.Store.CreatePeerLoginMFAApproval(ctx, &types.PeerLoginMFAApproval{
		ID: "approval-1", AccountID: accountID, UserID: u.Id, WGPubKey: wgKeyA, CreatedAt: now,
		ExpiresAt: now.Add(types.PeerLoginMFAWindow), Deadline: now.Add(types.PeerLoginMFADeadline),
	}))

	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)

	live, err := m.Store.GetUserPeerLoginMFAApprovals(ctx, store.LockingStrengthNone, accountID, u.Id, now)
	require.NoError(t, err)
	assert.Empty(t, live)
}

// The token never expires before its record: record times are stored at
// the token's one-second precision, so a record created mid-second is
// still approvable through its token just before the deadline.
func TestPeerLoginMFA_TokenOutlivesRecordAtSubSecondBoundary(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	ctx := context.Background()
	f.clock.Advance(900 * time.Millisecond)
	ch, err := f.request(t, wgKeyA)
	require.NoError(t, err)
	out, err := f.am.ApprovePeerLoginMFA(ctx, ch.Token, f.code(t))
	require.NoError(t, err)
	require.True(t, out.Approved)

	rec := f.record(t, ch.ApprovalID)
	assert.Zero(t, rec.Deadline.Nanosecond(), "the deadline is a whole second")
	f.clock.Advance(rec.ExpiresAt.Sub(f.clock.Now()) - 500*time.Millisecond)

	view, err := f.am.GetPeerLoginMFAApproval(ctx, ch.Token)
	require.NoError(t, err, "the token is valid while the record is")
	assert.Equal(t, account.PeerLoginMFAApproved, view.State)
	out, err = f.am.ApprovePeerLoginMFA(ctx, ch.Token, "")
	require.NoError(t, err)
	assert.True(t, out.Approved, "the idempotent retry still works")
}

// A login authenticated before a password reset cannot start an approval
// after it: the request rechecks the token's issue time against the
// credentials cutoff under the user lock.
func TestRequestPeerLoginMFAApproval_RefusesTokensFromBeforeAReset(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	require.NoError(t, m.SetMFA(base64.StdEncoding.EncodeToString(make([]byte, 32))))
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	issuedAt := time.Now().Add(-time.Minute)

	_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
	require.NoError(t, err)

	_, err = m.RequestPeerLoginMFAApproval(ctx, PeerLoginMFARequest{
		AccountID: accountID, UserID: u.Id, WGPubKey: wgKeyA, JWTIssuedAt: issuedAt})
	requireStatus(t, err, status.Unauthenticated)
	live, err := m.Store.GetUserPeerLoginMFAApprovals(ctx, store.LockingStrengthNone, accountID, u.Id, time.Now())
	require.NoError(t, err)
	assert.Empty(t, live, "no approval outlives the reset")

	_, err = m.RequestPeerLoginMFAApproval(ctx, PeerLoginMFARequest{
		AccountID: accountID, UserID: u.Id, WGPubKey: wgKeyA, JWTIssuedAt: time.Now().Add(2 * time.Second)})
	require.NoError(t, err, "a token from after the reset is accepted")
}
