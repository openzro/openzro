package internal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/management/client/peerloginmfa"
)

var fastPoll = PeerLoginMFAPoll{Initial: time.Millisecond, Max: 5 * time.Millisecond}

// scripted returns the next error each call, then nil (approved).
func scripted(errs ...error) (func(context.Context) error, *int) {
	calls := 0
	return func(context.Context) error {
		calls++
		if calls <= len(errs) {
			return errs[calls-1]
		}
		return nil
	}, &calls
}

func TestWaitPeerLoginMFA_ApprovedAfterWaiting(t *testing.T) {
	required := peerloginmfa.Required("https://app.example.com", "tok", time.Minute)
	unavailable := status.Error(codes.Unavailable, "management restarting")
	login, calls := scripted(required, unavailable, required)
	err := WaitPeerLoginMFA(context.Background(), time.Now().Add(time.Minute), login, "", fastPoll)
	require.NoError(t, err)
	assert.Equal(t, 4, *calls, "keeps trying through waiting and transient errors")
}

// Denied, rate limited and enrollment-required end the wait at once.
func TestWaitPeerLoginMFA_TerminalRefusals(t *testing.T) {
	for _, refusal := range []error{
		peerloginmfa.Denied(time.Minute),
		peerloginmfa.RateLimited(time.Minute),
		peerloginmfa.EnrollmentRequired("https://app.example.com"),
	} {
		login, calls := scripted(refusal, nil)
		err := WaitPeerLoginMFA(context.Background(), time.Now().Add(time.Minute), login, "", fastPoll)
		assert.Equal(t, refusal, err, "the refusal is returned as the server sent it")
		_, ok := peerloginmfa.Parse(err, "")
		assert.True(t, ok)
		assert.Equal(t, 1, *calls)
	}
}

// Any other error ends the wait and is returned as is.
func TestWaitPeerLoginMFA_OtherErrors(t *testing.T) {
	denied := status.Error(codes.PermissionDenied, "user blocked")
	login, calls := scripted(denied)
	err := WaitPeerLoginMFA(context.Background(), time.Now().Add(time.Minute), login, "", fastPoll)
	assert.Equal(t, denied, err)
	assert.Equal(t, 1, *calls)
}

func TestWaitPeerLoginMFA_DeadlineAndCancel(t *testing.T) {
	alwaysRequired := func(context.Context) error {
		return peerloginmfa.Required("https://app.example.com", "tok", time.Minute)
	}
	err := WaitPeerLoginMFA(context.Background(), time.Now().Add(20*time.Millisecond), alwaysRequired, "", fastPoll)
	require.ErrorIs(t, err, ErrPeerLoginMFAExpired)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = WaitPeerLoginMFA(ctx, time.Now().Add(time.Minute), alwaysRequired, "", fastPoll)
	require.ErrorIs(t, err, context.Canceled)
}

// The wait backs off from Initial to Max with jitter.
func TestPeerLoginMFAPoll_Next(t *testing.T) {
	p := PeerLoginMFAPoll{Initial: 3 * time.Second, Max: 10 * time.Second}
	d := p.Initial
	for i := 0; i < 20; i++ {
		got := p.jitter(d)
		assert.GreaterOrEqual(t, got, d*8/10)
		assert.LessOrEqual(t, got, d*12/10)
		d = p.next(d)
		assert.LessOrEqual(t, d, p.Max)
	}
	assert.Equal(t, p.Max, d)
}
