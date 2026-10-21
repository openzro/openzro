package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	gstatus "google.golang.org/grpc/status"

	"github.com/openzro/openzro/client/internal"
	"github.com/openzro/openzro/client/proto"
	contract "github.com/openzro/openzro/management/client/peerloginmfa"
)

func testCmd() (*cobra.Command, *bytes.Buffer) {
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	return cmd, out
}

// fakeDaemonClient answers WaitSSOLogin and GetConfig.
type fakeDaemonClient struct {
	proto.DaemonServiceClient
	ssoErr  error
	mgmtURL string
	asked   *proto.GetConfigRequest
}

func (f *fakeDaemonClient) WaitSSOLogin(context.Context, *proto.WaitSSOLoginRequest, ...grpc.CallOption) (*proto.WaitSSOLoginResponse, error) {
	return nil, f.ssoErr
}

func (f *fakeDaemonClient) GetConfig(_ context.Context, in *proto.GetConfigRequest, _ ...grpc.CallOption) (*proto.GetConfigResponse, error) {
	f.asked = in
	return &proto.GetConfigResponse{ManagementUrl: f.mgmtURL}, nil
}

// Through the daemon, a sign-in that needs 2FA set up points at the
// dashboard on the management server's host when the server sent no
// dashboard URL.
func TestDaemonPeerLoginMFAError_FallsBackToManagementURL(t *testing.T) {
	d := &fakeDaemonClient{ssoErr: contract.EnrollmentRequired(""), mgmtURL: "https://mgmt.example.com:443"}
	err := daemonPeerLoginMFAError(context.Background(), d, "default", "ana", d.ssoErr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://mgmt.example.com/")
	require.NotNil(t, d.asked)
	assert.Equal(t, "default", d.asked.ProfileName)
	assert.Equal(t, "ana", d.asked.Username)

	assert.Nil(t, daemonPeerLoginMFAError(context.Background(), d, "default", "ana",
		gstatus.Error(codes.PermissionDenied, "blocked")), "other errors are left to the caller")
}

var fastPeerMFAPoll = internal.PeerLoginMFAPoll{Initial: time.Millisecond, Max: 2 * time.Millisecond}

// A foreground login that waits for its approval shows the link and logs
// in once it is approved.
func TestFinishForegroundPeerLoginMFA_Approved(t *testing.T) {
	cmd, out := testCmd()
	required := contract.Required("https://app.example.com", "tok", 5*time.Minute)
	calls := 0
	err := finishForegroundPeerLoginMFA(context.Background(), cmd, required, "https://mgmt.example.com", true,
		func(context.Context) error {
			calls++
			if calls < 3 {
				return required
			}
			return nil
		}, fastPeerMFAPoll)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "https://app.example.com/mfa/peer-login#t=tok")
	assert.Contains(t, out.String(), "2FA")
	assert.Equal(t, 3, calls)
}

// Refusals end the login with a message to act on, and no retries.
func TestFinishForegroundPeerLoginMFA_Refusals(t *testing.T) {
	cmd, _ := testCmd()
	calls := 0
	err := finishForegroundPeerLoginMFA(context.Background(), cmd, contract.EnrollmentRequired("https://app.example.com"),
		"", true, func(context.Context) error { calls++; return nil }, fastPeerMFAPoll)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "two-factor")
	assert.Contains(t, err.Error(), "https://app.example.com/")
	assert.Zero(t, calls)

	required := contract.Required("https://app.example.com", "tok", 5*time.Minute)
	err = finishForegroundPeerLoginMFA(context.Background(), cmd, required, "", true,
		func(context.Context) error { return contract.Denied(time.Minute) }, fastPeerMFAPoll)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied")
	assert.NotContains(t, err.Error(), "tok", "the approval token stays out of the error")
}
