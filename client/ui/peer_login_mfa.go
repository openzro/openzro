//go:build !(linux && 386)

package main

import (
	"context"
	"sync/atomic"
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
	// Each wait closes its own window only: a newer wait may have replaced
	// it, and must keep its window and its wait.
	var mine fyne.Window
	defer func() { s.closeApprovalWindow(mine) }()
	return peerloginmfa.WaitSSO(waitCtx, conn, userCode, "", open,
		func(url string, _ time.Duration, opened bool) { mine = s.showPeerLoginMFAApproval(url, opened, cancel) })
}

// showPeerLoginMFAApproval tells the user that this device's sign-in waits
// for their 2FA approval (ADR-0028). When the browser opened the page, a
// notification says so; the link carries a secret, so it is neither logged
// nor put in the notification. When the browser did not open, a window
// gives the user the link, to copy or to try opening again, and is
// returned; onClose runs when it closes. A newer window replaces, and
// closes, an older one.
func (s *serviceClient) showPeerLoginMFAApproval(url string, opened bool, onClose func()) fyne.Window {
	if opened {
		s.app.SendNotification(fyne.NewNotification("Approve this sign-in",
			"Enter your 2FA code on the page that opened in your browser. openZro connects once you approve."))
		return nil
	}
	newWindow := s.app.NewWindow
	if s.newWindow != nil {
		newWindow = s.newWindow
	}
	w := newWindow("Approve this sign-in")
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
			s.approvalWin, s.approvalCancel = nil, nil
		}
		s.approvalMu.Unlock()
		onClose()
	})

	s.approvalMu.Lock()
	previous, previousCancel := s.approvalWin, s.approvalCancel
	s.approvalWin, s.approvalCancel = w, onClose
	s.approvalMu.Unlock()
	if previous != nil {
		retireApprovalWindow(previous, previousCancel)
	}
	w.Show()
	return w
}

// approvalRetryClose is how long a retired window is closed again: the
// desktop driver shows windows asynchronously and ignores Close until the
// native window exists.
const approvalRetryClose = 5 * time.Second

// retireApprovalWindow ends an approval window that may not have appeared
// yet. Its link is replaced first, so a window the driver shows late never
// shows it; its wait is canceled directly, since a close that the driver
// ignored fires no callback; and it is closed again until it can be.
func retireApprovalWindow(w fyne.Window, cancel func()) {
	ended := widget.NewLabel("This sign-in request has ended.")
	w.SetContent(container.NewPadded(container.NewVBox(ended, widget.NewButton("Close", w.Close))))
	if cancel != nil {
		cancel()
	}
	// onClosed fires only when a close took effect; until then, try again.
	var closed atomic.Bool
	w.SetOnClosed(func() { closed.Store(true) })
	w.Close()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(approvalRetryClose)
		for !closed.Load() {
			select {
			case <-ticker.C:
				if !closed.Load() {
					w.Close()
				}
			case <-deadline:
				return
			}
		}
	}()
}

// closeApprovalWindow closes w if it is still the open approval window. A
// window a newer one replaced was closed then, and is left alone.
func (s *serviceClient) closeApprovalWindow(w fyne.Window) {
	if w == nil {
		return
	}
	s.approvalMu.Lock()
	current := s.approvalWin == w
	var cancel func()
	if current {
		cancel = s.approvalCancel
		s.approvalWin, s.approvalCancel = nil, nil
	}
	s.approvalMu.Unlock()
	if current {
		retireApprovalWindow(w, cancel)
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

func (s *serviceClient) currentApprovalWindow() fyne.Window {
	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()
	return s.approvalWin
}
