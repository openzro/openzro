//go:build !(linux && 386)

package main

import (
	"context"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/openzro/openzro/client/internal/peerloginmfa"
	"github.com/openzro/openzro/client/proto"
)

// waitSSO finishes an SSO login through the daemon, showing the 2FA
// approval step when the device sign-in needs it. A window with the
// approval link (when the browser did not open) lives as long as the wait:
// it closes when the wait ends, so no stale link stays on screen, and
// closing it gives up the wait, since without it the user cannot approve.
func (s *serviceClient) waitSSO(ctx context.Context, conn proto.DaemonServiceClient, userCode string,
	open func(string) error) (string, error) {
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.closeApprovalWindow()
	return peerloginmfa.WaitSSO(waitCtx, conn, userCode, "", open,
		func(url string, _ time.Duration, opened bool) { s.showPeerLoginMFAApproval(url, opened, cancel) })
}

// showPeerLoginMFAApproval tells the user that this device's sign-in waits
// for their 2FA approval (ADR-0028). When the browser opened the page, a
// notification says so; the link carries a secret, so it is neither logged
// nor put in the notification. When the browser did not open, a window
// gives the user the link, to copy or to try opening again; onClose runs
// when that window closes.
func (s *serviceClient) showPeerLoginMFAApproval(url string, opened bool, onClose func()) {
	if opened {
		s.app.SendNotification(fyne.NewNotification("Approve this sign-in",
			"Enter your 2FA code on the page that opened in your browser. openZro connects once you approve."))
		return
	}
	w := s.app.NewWindow("Approve this sign-in")
	label := widget.NewLabel("Your browser did not open. Open this link in a browser and enter your 2FA code " +
		"to approve this sign-in. openZro connects once you approve. Closing this window cancels the sign-in.")
	label.Wrapping = fyne.TextWrapWord
	link := widget.NewEntry()
	link.SetText(url)
	link.Disable()
	content := container.NewVBox(label, link,
		widget.NewButton("Copy link", func() { w.Clipboard().SetContent(url) }),
		widget.NewButton("Open in browser", func() { _ = openURL(url) }),
		widget.NewButton("Cancel sign-in", w.Close),
	)
	w.SetContent(container.NewPadded(content))
	w.Resize(fyne.NewSize(460, 240))
	w.SetOnClosed(func() {
		s.approvalMu.Lock()
		if s.approvalWin == w {
			s.approvalWin = nil
		}
		s.approvalMu.Unlock()
		onClose()
	})

	s.approvalMu.Lock()
	previous := s.approvalWin
	s.approvalWin = w
	s.approvalMu.Unlock()
	if previous != nil {
		previous.Close()
	}
	w.Show()
}

// closeApprovalWindow closes the approval link window, if one is open.
func (s *serviceClient) closeApprovalWindow() {
	s.approvalMu.Lock()
	w := s.approvalWin
	s.approvalWin = nil
	s.approvalMu.Unlock()
	if w != nil {
		w.Close()
	}
}

func (s *serviceClient) approvalWindowOpen() bool {
	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()
	return s.approvalWin != nil
}

// showPeerLoginMFAError explains a sign-in refused for the user's second
// factor in a small window, with a button to the dashboard when there is
// something to do there. It returns false for other errors.
func (s *serviceClient) showPeerLoginMFAError(err error) bool {
	msg, link, ok := peerloginmfa.Describe(err, s.managementURL)
	if !ok {
		return false
	}
	w := s.app.NewWindow("openZro sign-in")
	label := widget.NewLabel(msg)
	label.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(label)
	if link != "" {
		content.Add(widget.NewButton("Open the openZro dashboard", func() {
			_ = openURL(link)
			w.Close()
		}))
	}
	content.Add(widget.NewButton("Close", w.Close))
	w.SetContent(container.NewPadded(content))
	w.Resize(fyne.NewSize(420, 160))
	w.Show()
	return true
}
