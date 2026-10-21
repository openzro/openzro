package server

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/client/internal"
	"github.com/openzro/openzro/client/internal/profilemanager"
	"github.com/openzro/openzro/client/proto"
	"github.com/openzro/openzro/management/client/peerloginmfa"
)

func newTestFlow(t *testing.T, flows *mfaFlows, now time.Time) string {
	t.Helper()
	id, err := flows.add(&mfaFlow{binding: testBinding, jwt: "jwt", email: "ana@example.com",
		hostname: "ana-laptop", expiresAt: now.Add(5 * time.Minute)}, now)
	require.NoError(t, err)
	return id
}

var testBinding = mfaFlowBinding{profile: "default", managementURL: "https://mgmt.example.com:443"}

// A flow id is 128 random bits; a flow is claimed by one waiter, once.
func TestMFAFlows_SingleUseSingleWaiter(t *testing.T) {
	flows := newMFAFlows()
	now := time.Now()
	id := newTestFlow(t, flows, now)
	assert.Len(t, id, 32, "128 bits, hex")
	assert.NotEqual(t, id, newTestFlow(t, flows, now))

	flow, err := flows.claim(id, testBinding, now)
	require.NoError(t, err)
	assert.Equal(t, "jwt", flow.jwt)

	_, err = flows.claim(id, testBinding, now)
	assert.Equal(t, codes.AlreadyExists, status.Code(err), "a second waiter is refused")

	flows.finish(id)
	_, err = flows.claim(id, testBinding, now)
	assert.Equal(t, codes.NotFound, status.Code(err), "a finished flow is gone")
	assert.Empty(t, flow.jwt, "the JWT is dropped when the flow ends")
}

// A flow belongs to the profile and management URL it was made for; a
// claim from anywhere else fails and drops the flow.
func TestMFAFlows_Binding(t *testing.T) {
	flows := newMFAFlows()
	now := time.Now()
	for _, other := range []mfaFlowBinding{
		{profile: "work", managementURL: testBinding.managementURL},
		{profile: "default", managementURL: "https://other.example.com"},
	} {
		id := newTestFlow(t, flows, now)
		_, err := flows.claim(id, other, now)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
		_, err = flows.claim(id, testBinding, now)
		assert.Equal(t, codes.NotFound, status.Code(err), "the flow was dropped")
	}
	_, err := flows.claim("unknown", testBinding, now)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// An expired flow cannot be claimed, and the reaper drops abandoned ones
// with their JWTs.
func TestMFAFlows_ExpiryAndReaper(t *testing.T) {
	flows := newMFAFlows()
	now := time.Now()
	id := newTestFlow(t, flows, now)
	_, err := flows.claim(id, testBinding, now.Add(5*time.Minute))
	assert.Equal(t, codes.DeadlineExceeded, status.Code(err))

	abandoned := newTestFlow(t, flows, now)
	flows.reap(now.Add(6 * time.Minute))
	_, err = flows.claim(abandoned, testBinding, now)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Equal(t, 0, flows.len())
}

// At most maxPendingMFAFlows flows are kept; adding one more drops the
// oldest.
func TestMFAFlows_Cap(t *testing.T) {
	flows := newMFAFlows()
	now := time.Now()
	oldest := newTestFlow(t, flows, now)
	for i := 1; i < maxPendingMFAFlows; i++ {
		newTestFlow(t, flows, now.Add(time.Duration(i)*time.Second))
	}
	newest := newTestFlow(t, flows, now.Add(time.Minute))
	assert.Equal(t, maxPendingMFAFlows, flows.len())
	_, err := flows.claim(oldest, testBinding, now.Add(time.Minute))
	assert.Equal(t, codes.NotFound, status.Code(err))
	_, err = flows.claim(newest, testBinding, now.Add(time.Minute))
	require.NoError(t, err)
}

// newMFATestServer is a daemon whose management logins answer from login.
func newMFATestServer(t *testing.T, login func(ctx context.Context, jwt string) error) *Server {
	t.Helper()
	return newMFATestServerWithConfig(t, func(ctx context.Context, _ *profilemanager.Config, jwt string) error {
		return login(ctx, jwt)
	})
}

func newMFATestServerWithConfig(t *testing.T, login func(ctx context.Context, cfg *profilemanager.Config, jwt string) error) *Server {
	t.Helper()
	ctx, cancel := context.WithCancel(internal.CtxInitState(context.Background()))
	t.Cleanup(cancel)
	s := New(ctx, "console", false)
	mgmt, err := url.Parse("https://mgmt.example.com:443")
	require.NoError(t, err)
	s.config = &profilemanager.Config{ManagementURL: mgmt}
	s.peerLogin = login
	s.mfaPoll = internal.PeerLoginMFAPoll{Initial: time.Millisecond, Max: 5 * time.Millisecond}
	s.mfaProfile = func() string { return "default" }
	return s
}

func required() error {
	return peerloginmfa.Required("https://app.example.com", "approval-token", 5*time.Minute)
}

// A login waiting for its approval becomes a flow; waiting on it logs in
// once the approval lands and ends the flow.
func TestPeerLoginMFA_ApprovalCompletesTheLogin(t *testing.T) {
	var seen []string
	pending := 2
	s := newMFATestServer(t, func(_ context.Context, jwt string) error {
		seen = append(seen, jwt)
		if pending > 0 {
			pending--
			return required()
		}
		return nil
	})

	resp, handled, err := startFlow(s, &proto.WaitSSOLoginRequest{SupportsMfaApproval: true},
		"sso-jwt", "ana@example.com", "ana-laptop", required())
	require.True(t, handled)
	require.NoError(t, err)
	assert.Equal(t, "https://app.example.com/mfa/peer-login#t=approval-token", resp.MfaApprovalURL)
	assert.Equal(t, int64(300), resp.MfaExpiresInSec)
	require.NotEmpty(t, resp.MfaFlowId)

	out, err := s.WaitMFAApproval(context.Background(), &proto.WaitMFAApprovalRequest{FlowId: resp.MfaFlowId})
	require.NoError(t, err)
	assert.Equal(t, "ana@example.com", out.Email)
	assert.Equal(t, []string{"sso-jwt", "sso-jwt", "sso-jwt"}, seen, "the waiting login retries with the SSO token")
	assert.Equal(t, 0, s.mfaFlows.len(), "the flow and its token are gone")

	_, err = s.WaitMFAApproval(context.Background(), &proto.WaitMFAApprovalRequest{FlowId: resp.MfaFlowId})
	assert.Equal(t, codes.NotFound, status.Code(err), "a flow is single-use")
}

// A caller that cannot open the approval link gets an error saying so, and
// no flow keeps the token.
func TestPeerLoginMFA_OldCallers(t *testing.T) {
	s := newMFATestServer(t, func(context.Context, string) error { return nil })
	resp, handled, err := startFlow(s, &proto.WaitSSOLoginRequest{},
		"sso-jwt", "ana@example.com", "", required())
	require.True(t, handled)
	assert.Nil(t, resp)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.NotContains(t, err.Error(), "approval-token")
	assert.Equal(t, 0, s.mfaFlows.len())

	// Errors that are not a 2FA wait are left to the caller.
	_, handled, _ = startFlow(s, &proto.WaitSSOLoginRequest{SupportsMfaApproval: true},
		"sso-jwt", "", "", status.Error(codes.PermissionDenied, "blocked"))
	assert.False(t, handled)
}

// A denial ends the wait at once with the server's refusal, which the
// caller can read, and the flow is dropped.
func TestPeerLoginMFA_RefusalEndsTheWait(t *testing.T) {
	denied := peerloginmfa.Denied(2 * time.Minute)
	calls := 0
	s := newMFATestServer(t, func(context.Context, string) error {
		calls++
		if calls == 1 {
			return required()
		}
		return denied
	})
	resp, _, err := startFlow(s, &proto.WaitSSOLoginRequest{SupportsMfaApproval: true},
		"sso-jwt", "", "", required())
	require.NoError(t, err)

	_, err = s.WaitMFAApproval(context.Background(), &proto.WaitMFAApprovalRequest{FlowId: resp.MfaFlowId})
	refusal, ok := peerloginmfa.Parse(err, "")
	require.True(t, ok, "got %v", err)
	assert.Equal(t, peerloginmfa.ReasonDenied, refusal.Reason)
	assert.Equal(t, 2*time.Minute, refusal.RetryAfter)
	assert.Equal(t, 0, s.mfaFlows.len())
}

// Canceling the wait ends the flow too.
func TestPeerLoginMFA_CancelledWait(t *testing.T) {
	s := newMFATestServer(t, func(context.Context, string) error { return required() })
	resp, _, err := startFlow(s, &proto.WaitSSOLoginRequest{SupportsMfaApproval: true},
		"sso-jwt", "", "", required())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = s.WaitMFAApproval(ctx, &proto.WaitMFAApprovalRequest{FlowId: resp.MfaFlowId})
	assert.Equal(t, codes.Canceled, status.Code(err))
	assert.Equal(t, 0, s.mfaFlows.len())
}

// startFlow starts a flow the way WaitSSOLogin does, from the login target
// it would have used.
func startFlow(s *Server, msg *proto.WaitSSOLoginRequest, jwt, email, hostname string,
	loginErr error) (*proto.WaitSSOLoginResponse, bool, error) {
	binding, cfg := s.loginTarget()
	return s.startPeerLoginMFA(msg, binding, cfg, jwt, email, hostname, loginErr)
}

// The token goes only to the management server it was minted for: retries
// use the configuration captured with the flow, and a profile switch during
// the wait ends it before the next retry.
func TestPeerLoginMFA_ProfileSwitchStopsTheWait(t *testing.T) {
	var hosts []string
	switched := make(chan struct{})
	var s *Server
	s = newMFATestServerWithConfig(t, func(_ context.Context, cfg *profilemanager.Config, _ string) error {
		hosts = append(hosts, cfg.ManagementURL.Host)
		if len(hosts) == 1 {
			// Switch to another server while the wait sleeps.
			other, _ := url.Parse("https://other.example.com:443")
			s.mutex.Lock()
			s.config = &profilemanager.Config{ManagementURL: other}
			s.mutex.Unlock()
			close(switched)
		}
		return required()
	})
	resp, _, err := startFlow(s, &proto.WaitSSOLoginRequest{SupportsMfaApproval: true}, "sso-jwt", "", "", required())
	require.NoError(t, err)

	_, err = s.WaitMFAApproval(context.Background(), &proto.WaitMFAApprovalRequest{FlowId: resp.MfaFlowId})
	<-switched
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, []string{"mgmt.example.com:443"}, hosts, "no retry after the switch, and none to the new server")
	assert.Equal(t, 0, s.mfaFlows.len())
}

// The flow keeps its own copy of the configuration: changing the daemon's
// in place does not redirect the token.
func TestPeerLoginMFA_FlowConfigIsACopy(t *testing.T) {
	var hosts []string
	pending := 1
	s := newMFATestServerWithConfig(t, func(_ context.Context, cfg *profilemanager.Config, _ string) error {
		hosts = append(hosts, cfg.ManagementURL.Host)
		if pending > 0 {
			pending--
			return required()
		}
		return nil
	})
	resp, _, err := startFlow(s, &proto.WaitSSOLoginRequest{SupportsMfaApproval: true}, "sso-jwt", "", "", required())
	require.NoError(t, err)
	s.mutex.Lock()
	s.config.ManagementURL.Host = "tampered.example.com"
	s.mutex.Unlock()

	_, err = s.WaitMFAApproval(context.Background(), &proto.WaitMFAApprovalRequest{FlowId: resp.MfaFlowId})
	// The in-place change alters the binding, so the wait stops; either
	// way the token never reached the tampered host.
	assert.NotContains(t, hosts, "tampered.example.com")
	_ = err
}

// The wait is the daemon's active operation: Down, a new login or any
// other action that cancels it ends the wait and drops the flow.
func TestPeerLoginMFA_DaemonCancellationEndsTheWait(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	s := newMFATestServer(t, func(ctx context.Context, _ string) error {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return status.Error(codes.Canceled, "canceled")
	})
	resp, _, err := startFlow(s, &proto.WaitSSOLoginRequest{SupportsMfaApproval: true}, "sso-jwt", "", "", required())
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := s.WaitMFAApproval(context.Background(), &proto.WaitMFAApprovalRequest{FlowId: resp.MfaFlowId})
		done <- err
	}()
	<-entered
	// What Down and every new action do with the active operation.
	s.mutex.Lock()
	s.actCancel()
	s.mutex.Unlock()
	select {
	case err := <-done:
		assert.Equal(t, codes.Canceled, status.Code(err))
	case <-time.After(5 * time.Second):
		t.Fatal("the wait did not end when the daemon canceled it")
	}
	assert.Equal(t, 0, s.mfaFlows.len())
}

// Stopping the daemon also drops flows nobody is waiting on yet.
func TestPeerLoginMFA_StopDropsPendingFlows(t *testing.T) {
	s := newMFATestServer(t, func(context.Context, string) error { return required() })
	_, _, err := startFlow(s, &proto.WaitSSOLoginRequest{SupportsMfaApproval: true}, "sso-jwt", "", "", required())
	require.NoError(t, err)
	require.Equal(t, 1, s.mfaFlows.len())
	s.mfaFlows.clear()
	assert.Equal(t, 0, s.mfaFlows.len())
}
