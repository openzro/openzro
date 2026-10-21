package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/client/internal"
	contract "github.com/openzro/openzro/management/client/peerloginmfa"
)

func testCmd() (*cobra.Command, *bytes.Buffer) {
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	return cmd, out
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
