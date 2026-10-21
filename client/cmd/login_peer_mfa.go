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
	contract "github.com/openzro/openzro/management/client/peerloginmfa"
)

// announcePeerLoginMFA prints the link to the page where the user approves
// this device's sign-in with their 2FA code (ADR-0028). It goes to the
// user's terminal only, never to a log: it carries a secret.
func announcePeerLoginMFA(cmd *cobra.Command, url string, expiresIn time.Duration, noBrowser bool) {
	how := "Approve it in your browser. If it didn't open, use this link:"
	if noBrowser {
		how = "Open this link to approve it:"
	}
	cmd.Println("This sign-in needs your 2FA approval. " + how + "\n\n" + url + "\n")
	if expiresIn > 0 {
		cmd.Printf("The request expires in %d minutes. Waiting for the approval...\n\n", int(expiresIn.Minutes()+0.5))
	}
}

// openPeerLoginMFA opens the approval page unless --no-browser was given.
func openPeerLoginMFA(noBrowser bool) func(string) error {
	return func(url string) error {
		if noBrowser {
			return nil
		}
		return open.Run(url)
	}
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
		announcePeerLoginMFA(cmd, refusal.URL, refusal.ExpiresIn, noBrowser)
		_ = openPeerLoginMFA(noBrowser)(refusal.URL)
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
