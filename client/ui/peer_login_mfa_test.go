//go:build !(linux && 386)

package main

import (
	"context"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/client/proto"
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
	s.showPeerLoginMFAApproval("https://app.example.com/mfa/peer-login#t=tok", true, func() {})
	assert.Len(t, app.Driver().AllWindows(), count, "no window when the browser opened")
	s.showPeerLoginMFAApproval("https://app.example.com/mfa/peer-login#t=tok", false, func() {})
	_, buttons = windowTexts(newest())
	assert.Contains(t, buttons, "Copy link")
	assert.Contains(t, buttons, "Open in browser")
	assert.Contains(t, buttons, "Cancel sign-in")
	assert.Contains(t, entryTexts(newest()), "https://app.example.com/mfa/peer-login#t=tok")

	before := len(app.Driver().AllWindows())
	assert.False(t, s.showPeerLoginMFAError(status.Error(codes.PermissionDenied, "blocked")))
	assert.Len(t, app.Driver().AllWindows(), before, "no window for other errors")
}

// fakeDaemon answers the SSO calls; WaitMFAApproval waits for release or
// for its context.
type fakeDaemon struct {
	proto.DaemonServiceClient
	release chan struct{}
}

func (f *fakeDaemon) WaitSSOLogin(context.Context, *proto.WaitSSOLoginRequest, ...grpc.CallOption) (*proto.WaitSSOLoginResponse, error) {
	return &proto.WaitSSOLoginResponse{MfaFlowId: "flow", MfaApprovalURL: "https://app.example.com/mfa/peer-login#t=tok"}, nil
}

func (f *fakeDaemon) WaitMFAApproval(ctx context.Context, _ *proto.WaitMFAApprovalRequest, _ ...grpc.CallOption) (*proto.WaitMFAApprovalResponse, error) {
	select {
	case <-f.release:
		return &proto.WaitMFAApprovalResponse{Email: "ana@example.com"}, nil
	case <-ctx.Done():
		return nil, status.Error(codes.Canceled, "canceled")
	}
}

func noBrowser(string) error { return assert.AnError }

// The link window lives as long as the wait: it closes when the wait ends,
// so no stale link stays on screen.
func TestWaitSSO_LinkWindowEndsWithTheWait(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	s := &serviceClient{app: app, ctx: context.Background()}
	d := &fakeDaemon{release: make(chan struct{})}

	done := make(chan error, 1)
	go func() {
		_, err := s.waitSSO(context.Background(), d, "code", noBrowser)
		done <- err
	}()
	require.Eventually(t, func() bool { return s.approvalWindowOpen() }, 2*time.Second, 10*time.Millisecond)
	close(d.release)
	require.NoError(t, <-done)
	assert.False(t, s.approvalWindowOpen(), "the window closed when the approval landed")
}

// Closing the link window gives up the wait: without it the user cannot
// approve.
func TestWaitSSO_ClosingTheLinkWindowCancelsTheWait(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	s := &serviceClient{app: app, ctx: context.Background()}
	d := &fakeDaemon{release: make(chan struct{})}

	done := make(chan error, 1)
	go func() {
		_, err := s.waitSSO(context.Background(), d, "code", noBrowser)
		done <- err
	}()
	require.Eventually(t, func() bool { return s.approvalWindowOpen() }, 2*time.Second, 10*time.Millisecond)
	s.closeApprovalWindow(s.currentApprovalWindow())
	select {
	case err := <-done:
		assert.Equal(t, codes.Canceled, status.Code(err))
	case <-time.After(2 * time.Second):
		t.Fatal("closing the window did not end the wait")
	}
}

// A newer wait replaces an older one's window; the older wait ending late
// must not close the newer window or cancel the newer wait.
func TestWaitSSO_OlderWaitLeavesTheNewerWindowAlone(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	s := &serviceClient{app: app, ctx: context.Background()}
	a := &fakeDaemon{release: make(chan struct{})}
	b := &fakeDaemon{release: make(chan struct{})}

	doneA, doneB := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := s.waitSSO(context.Background(), a, "code-a", noBrowser)
		doneA <- err
	}()
	require.Eventually(t, s.approvalWindowOpen, 2*time.Second, 10*time.Millisecond)
	windowA := s.currentApprovalWindow()

	go func() {
		_, err := s.waitSSO(context.Background(), b, "code-b", noBrowser)
		doneB <- err
	}()
	require.Eventually(t, func() bool {
		w := s.currentApprovalWindow()
		return w != nil && w != windowA
	}, 2*time.Second, 10*time.Millisecond)
	windowB := s.currentApprovalWindow()

	// B's window replaced A's, which ended A's wait; A now finishes.
	select {
	case <-doneA:
	case <-time.After(2 * time.Second):
		t.Fatal("the replaced wait did not end")
	}
	assert.Equal(t, windowB, s.currentApprovalWindow(), "A's ending left B's window open")
	select {
	case err := <-doneB:
		t.Fatalf("B ended when A finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(b.release)
	require.NoError(t, <-doneB)
	assert.False(t, s.approvalWindowOpen())
}
