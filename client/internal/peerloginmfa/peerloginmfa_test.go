package peerloginmfa

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/client/proto"
	contract "github.com/openzro/openzro/management/client/peerloginmfa"
)

// fakeDaemon answers the two SSO calls; every other method is unused.
type fakeDaemon struct {
	proto.DaemonServiceClient
	ssoReq   *proto.WaitSSOLoginRequest
	ssoResp  *proto.WaitSSOLoginResponse
	ssoErr   error
	waitedOn string
	waitResp *proto.WaitMFAApprovalResponse
	waitErr  error
}

func (f *fakeDaemon) WaitSSOLogin(_ context.Context, in *proto.WaitSSOLoginRequest, _ ...grpc.CallOption) (*proto.WaitSSOLoginResponse, error) {
	f.ssoReq = in
	return f.ssoResp, f.ssoErr
}

func (f *fakeDaemon) WaitMFAApproval(_ context.Context, in *proto.WaitMFAApprovalRequest, _ ...grpc.CallOption) (*proto.WaitMFAApprovalResponse, error) {
	f.waitedOn = in.FlowId
	return f.waitResp, f.waitErr
}

// A plain SSO login is unchanged, and the caller declares it can show the
// approval page.
func TestWaitSSO_NoApprovalNeeded(t *testing.T) {
	d := &fakeDaemon{ssoResp: &proto.WaitSSOLoginResponse{Email: "ana@example.com"}}
	opened := ""
	email, err := WaitSSO(context.Background(), d, "code", "laptop", func(u string) error { opened = u; return nil }, nil)
	require.NoError(t, err)
	assert.Equal(t, "ana@example.com", email)
	assert.True(t, d.ssoReq.SupportsMfaApproval)
	assert.Equal(t, "laptop", d.ssoReq.Hostname)
	assert.Empty(t, opened)
	assert.Empty(t, d.waitedOn)
}

// A login that waits for its approval opens the page, tells the caller,
// and waits on the flow.
func TestWaitSSO_ApprovalFlow(t *testing.T) {
	d := &fakeDaemon{
		ssoResp: &proto.WaitSSOLoginResponse{Email: "ana@example.com", MfaFlowId: "flow-1",
			MfaApprovalURL: "https://app.example.com/mfa/peer-login#t=tok", MfaExpiresInSec: 300},
		waitResp: &proto.WaitMFAApprovalResponse{Email: "ana@example.com"},
	}
	var opened, announced string
	var expires time.Duration
	var browserOpened bool
	email, err := WaitSSO(context.Background(), d, "code", "", func(u string) error { opened = u; return nil },
		func(u string, in time.Duration, ok bool) { announced, expires, browserOpened = u, in, ok })
	require.NoError(t, err)
	assert.Equal(t, "ana@example.com", email)
	assert.Equal(t, "https://app.example.com/mfa/peer-login#t=tok", opened)
	assert.Equal(t, opened, announced)
	assert.Equal(t, 5*time.Minute, expires)
	assert.True(t, browserOpened)
	assert.Equal(t, "flow-1", d.waitedOn)
}

// The browser failing to open does not stop the wait, and the caller is
// told, so it can show the link instead.
func TestWaitSSO_BrowserFails(t *testing.T) {
	d := &fakeDaemon{
		ssoResp:  &proto.WaitSSOLoginResponse{MfaFlowId: "flow-1", MfaApprovalURL: "https://app.example.com/x#t=tok"},
		waitResp: &proto.WaitMFAApprovalResponse{},
	}
	browserOpened, shown := true, ""
	_, err := WaitSSO(context.Background(), d, "code", "", func(string) error { return assert.AnError },
		func(u string, _ time.Duration, ok bool) { shown, browserOpened = u, ok })
	require.NoError(t, err)
	assert.False(t, browserOpened)
	assert.Equal(t, "https://app.example.com/x#t=tok", shown)
	assert.Equal(t, "flow-1", d.waitedOn)
}

// How long a request stays open is never shown longer than it is, and
// never as zero.
func TestExpiresIn(t *testing.T) {
	tests := map[time.Duration]string{
		0:                              "",
		10 * time.Second:               "less than a minute",
		59 * time.Second:               "less than a minute",
		60 * time.Second:               "about a minute",
		89 * time.Second:               "about a minute",
		4*time.Minute + 59*time.Second: "4 minutes",
		5 * time.Minute:                "5 minutes",
	}
	for in, want := range tests {
		assert.Equal(t, want, ExpiresIn(in), in.String())
	}
}

func TestWaitSSO_Errors(t *testing.T) {
	d := &fakeDaemon{ssoErr: contract.Denied(time.Minute)}
	_, err := WaitSSO(context.Background(), d, "code", "", func(string) error { return nil }, nil)
	assert.Equal(t, d.ssoErr, err)

	d = &fakeDaemon{
		ssoResp: &proto.WaitSSOLoginResponse{MfaFlowId: "flow-1", MfaApprovalURL: "https://app.example.com/x#t=tok"},
		waitErr: contract.RateLimited(2 * time.Minute),
	}
	_, err = WaitSSO(context.Background(), d, "code", "", func(string) error { return nil }, nil)
	assert.Equal(t, d.waitErr, err)
}

// Refusals become messages a person can act on.
func TestDescribe(t *testing.T) {
	msg, link, ok := Describe(contract.Denied(90*time.Second), "")
	require.True(t, ok)
	assert.Contains(t, msg, "denied")
	assert.Contains(t, msg, "2 minutes")
	assert.Empty(t, link)

	msg, link, ok = Describe(contract.RateLimited(30*time.Second), "")
	require.True(t, ok)
	assert.Contains(t, msg, "30 seconds")
	assert.Empty(t, link)

	msg, link, ok = Describe(contract.EnrollmentRequired("https://app.example.com"), "")
	require.True(t, ok)
	assert.Contains(t, msg, "two-factor")
	assert.Equal(t, "https://app.example.com/", link)

	msg, _, ok = Describe(status.Error(codes.DeadlineExceeded, "the 2FA approval request expired; sign in again"), "")
	require.True(t, ok)
	assert.Contains(t, msg, "expired")

	_, _, ok = Describe(status.Error(codes.PermissionDenied, "blocked"), "")
	assert.False(t, ok)
}
