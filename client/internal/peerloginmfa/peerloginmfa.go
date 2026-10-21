// Package peerloginmfa is what the CLI and the desktop app share to finish
// an SSO login that waits for the user's 2FA approval (ADR-0028 D6): open
// the approval page, wait on the daemon's flow, and turn a refusal into a
// message a person can act on.
package peerloginmfa

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/client/internal"
	"github.com/openzro/openzro/client/proto"
	contract "github.com/openzro/openzro/management/client/peerloginmfa"
)

// WaitSSO finishes the SSO login userCode started, declaring that the
// caller can show the 2FA approval page. If the device login then waits for
// the approval, it calls announce (when set) with the approval URL and how
// long it stays open, opens the URL, and waits on the daemon until the
// approval lands. A browser that fails to open does not stop the wait:
// announce is where the caller shows the link. The URL carries a secret in
// its fragment; callers must not log it. It returns the signed-in email.
func WaitSSO(ctx context.Context, daemon proto.DaemonServiceClient, userCode, hostname string,
	open func(url string) error, announce func(url string, expiresIn time.Duration)) (string, error) {
	resp, err := daemon.WaitSSOLogin(ctx, &proto.WaitSSOLoginRequest{
		UserCode: userCode, Hostname: hostname, SupportsMfaApproval: true,
	})
	if err != nil {
		return "", err
	}
	if resp.GetMfaFlowId() == "" {
		return resp.GetEmail(), nil
	}

	if announce != nil {
		announce(resp.GetMfaApprovalURL(), time.Duration(resp.GetMfaExpiresInSec())*time.Second)
	}
	_ = open(resp.GetMfaApprovalURL())

	waited, err := daemon.WaitMFAApproval(ctx, &proto.WaitMFAApprovalRequest{FlowId: resp.GetMfaFlowId()})
	if err != nil {
		return "", err
	}
	if waited.GetEmail() != "" {
		return waited.GetEmail(), nil
	}
	return resp.GetEmail(), nil
}

// Describe turns a refused device login into a message for the user, and
// a link to open when there is something to do there (setting up 2FA). ok
// is false for errors this package does not know.
func Describe(err error, managementURL string) (msg, link string, ok bool) {
	if refusal, parsed := contract.Parse(err, managementURL); parsed {
		switch refusal.Reason {
		case contract.ReasonDenied:
			return fmt.Sprintf("This sign-in was denied on the approval page. If it was you, try again in %s.",
				humanize(refusal.RetryAfter)), "", true
		case contract.ReasonRateLimited:
			return fmt.Sprintf("Too many sign-ins are waiting for approval. Try again in %s.",
				humanize(refusal.RetryAfter)), "", true
		case contract.ReasonEnrollmentRequired:
			return "Set up two-factor authentication in the openZro dashboard, then sign in again.", refusal.URL, true
		}
	}
	if st, isStatus := status.FromError(err); isStatus && st.Code() == codes.DeadlineExceeded &&
		st.Message() == internal.ErrPeerLoginMFAExpired.Error() {
		return "The 2FA approval request expired before it was approved. Sign in again.", "", true
	}
	return "", "", false
}

func humanize(d time.Duration) string {
	if d < time.Minute {
		secs := int((d + time.Second - 1) / time.Second)
		if secs <= 1 {
			return "a moment"
		}
		return fmt.Sprintf("%d seconds", secs)
	}
	mins := int((d + time.Minute - 1) / time.Minute)
	if mins == 1 {
		return "a minute"
	}
	return fmt.Sprintf("%d minutes", mins)
}
