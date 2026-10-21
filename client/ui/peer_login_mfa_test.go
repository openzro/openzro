//go:build !(linux && 386)

package main

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	contract "github.com/openzro/openzro/management/client/peerloginmfa"
)

func entryTexts(w fyne.Window) []string {
	var out []string
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.Entry:
			out = append(out, v.Text)
		case *fyne.Container:
			for _, c := range v.Objects {
				walk(c)
			}
		}
	}
	walk(w.Content())
	return out
}

func windowTexts(w fyne.Window) (labels, buttons []string) {
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.Label:
			labels = append(labels, v.Text)
		case *widget.Button:
			buttons = append(buttons, v.Text)
		case *fyne.Container:
			for _, c := range v.Objects {
				walk(c)
			}
		}
	}
	walk(w.Content())
	return labels, buttons
}

// A sign-in refused for its second factor is explained in a window; one
// that needs 2FA set up offers the dashboard.
func TestShowPeerLoginMFAError(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	s := &serviceClient{app: app, managementURL: "https://mgmt.example.com"}

	newest := func() fyne.Window {
		windows := app.Driver().AllWindows()
		require.NotEmpty(t, windows)
		return windows[len(windows)-1]
	}

	require.True(t, s.showPeerLoginMFAError(contract.Denied(2*time.Minute)))
	labels, buttons := windowTexts(newest())
	require.NotEmpty(t, labels)
	assert.Contains(t, labels[0], "denied")
	assert.Equal(t, []string{"Close"}, buttons)

	require.True(t, s.showPeerLoginMFAError(contract.EnrollmentRequired("")))
	labels, buttons = windowTexts(newest())
	assert.Contains(t, labels[0], "two-factor")
	assert.Equal(t, []string{"Open the openZro dashboard", "Close"}, buttons)

	// A browser that did not open leaves the user the link, in a window.
	count := len(app.Driver().AllWindows())
	s.showPeerLoginMFAApproval("https://app.example.com/mfa/peer-login#t=tok", true)
	assert.Len(t, app.Driver().AllWindows(), count, "no window when the browser opened")
	s.showPeerLoginMFAApproval("https://app.example.com/mfa/peer-login#t=tok", false)
	_, buttons = windowTexts(newest())
	assert.Contains(t, buttons, "Copy link")
	assert.Contains(t, buttons, "Open in browser")
	assert.Contains(t, entryTexts(newest()), "https://app.example.com/mfa/peer-login#t=tok")

	before := len(app.Driver().AllWindows())
	assert.False(t, s.showPeerLoginMFAError(status.Error(codes.PermissionDenied, "blocked")))
	assert.Len(t, app.Driver().AllWindows(), before, "no window for other errors")
}
