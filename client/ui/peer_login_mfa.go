//go:build !(linux && 386)

package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/openzro/openzro/client/internal/peerloginmfa"
)

// announcePeerLoginMFA tells the user that this device's sign-in waits
// for their 2FA approval on the page just opened in their browser
// (ADR-0028). The link carries a secret, so it is neither logged nor put
// in the notification.
func (s *serviceClient) announcePeerLoginMFA() {
	s.app.SendNotification(fyne.NewNotification("Approve this sign-in",
		"Enter your 2FA code on the page that opened in your browser. openZro connects once you approve."))
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
