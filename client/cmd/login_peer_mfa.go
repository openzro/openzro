package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"

	"github.com/openzro/openzro/client/internal"
	"github.com/openzro/openzro/client/internal/peerloginmfa"
	"github.com/openzro/openzro/client/proto"
	contract "github.com/openzro/openzro/management/client/peerloginmfa"
)

// announcePeerLoginMFA prints the link to the page where the user approves
// this device's sign-in with their 2FA code (ADR-0028). It goes to the
// user's terminal only, never to a log: it carries a secret.
func announcePeerLoginMFA(cmd *cobra.Command, url string, expiresIn time.Duration, opened bool) {
	how := "Open this link to approve it:"
	if opened {
		how = "Approve it in your browser. If it didn't open, use this link:"
	}
	cmd.Println("This sign-in needs your 2FA approval. " + how + "\n\n" + url + "\n")
	if when := peerloginmfa.ExpiresIn(expiresIn); when != "" {
		cmd.Printf("The request expires in %s. Waiting for the approval...\n\n", when)
	}
}

// errNoBrowser marks a page deliberately not opened (--no-browser).
var errNoBrowser = errors.New("--no-browser")

// openPeerLoginMFA opens the approval page unless --no-browser was given.
func openPeerLoginMFA(noBrowser bool) func(string) error {
	return func(url string) error {
		if noBrowser {
			return errNoBrowser
		}
		return open.Run(url)
	}
}

// daemonPeerLoginMFAError turns a sign-in through the daemon refused for
// its second factor into an error worth showing, or returns nil for other
// errors. A link to the dashboard falls back to the management server of
// the daemon's profile when the server sent no dashboard URL.
func daemonPeerLoginMFAError(ctx context.Context, client proto.DaemonServiceClient, profileName, username string,
	err error) error {
	managementURL := ""
	if contract.Is(err) {
		req := &proto.GetConfigRequest{ProfileName: profileName, Username: username}
		if cfg, cfgErr := client.GetConfig(ctx, req); cfgErr == nil {
			managementURL = cfg.GetManagementUrl()
		}
	}
	if described, ok := peerLoginMFAError(err, managementURL); ok {
		return described
	}
	return nil
}

// peerLoginMFAError turns a refused device login into an error worth
// showing; ok is false for other errors.
func peerLoginMFAError(err error, managementURL string) (error, bool) {
	msg, link, ok := peerloginmfa.Describe(err, managementURL)
	if !ok {
		return nil, false
	}
	if link != "" {
		msg += "\n\n" + link
	}
	return errors.New(msg), true
}

// finishForegroundPeerLoginMFA handles a foreground login refused for its
// second factor: it shows the approval link and retries login until the
// user approves, or reports why it cannot go on.
func finishForegroundPeerLoginMFA(ctx context.Context, cmd *cobra.Command, loginErr error, managementURL string,
	noBrowser bool, login func(context.Context) error, poll internal.PeerLoginMFAPoll) error {
	refusal, ok := contract.Parse(loginErr, managementURL)
	if !ok {
		return loginErr
	}
	if refusal.Reason == contract.ReasonRequired {
		opened := openPeerLoginMFA(noBrowser)(refusal.URL) == nil
		announcePeerLoginMFA(cmd, refusal.URL, refusal.ExpiresIn, opened)
		expiresIn := refusal.ExpiresIn
		if expiresIn <= 0 {
			expiresIn = 5 * time.Minute
		}
		loginErr = internal.WaitPeerLoginMFA(ctx, time.Now().Add(expiresIn), login, managementURL, poll)
		if loginErr == nil {
			return nil
		}
		if errors.Is(loginErr, internal.ErrPeerLoginMFAExpired) {
			return loginErr
		}
	}
	if described, ok := peerLoginMFAError(loginErr, managementURL); ok {
		return described
	}
	return fmt.Errorf("login failed: %w", loginErr)
}
