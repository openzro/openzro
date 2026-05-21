//go:build !(linux && 386)

package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/systray"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/client/proto"
)

type eventHandler struct {
	client *serviceClient
}

func newEventHandler(client *serviceClient) *eventHandler {
	return &eventHandler{
		client: client,
	}
}

func (h *eventHandler) listen(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.client.mUp.ClickedCh:
			h.handleConnectClick()
		case <-h.client.mDown.ClickedCh:
			h.handleDisconnectClick()
		case <-h.client.mAllowSSH.ClickedCh:
			h.handleAllowSSHClick()
		case <-h.client.mAutoConnect.ClickedCh:
			h.handleAutoConnectClick()
		case <-h.client.mEnableRosenpass.ClickedCh:
			h.handleRosenpassClick()
		case <-h.client.mLazyConnEnabled.ClickedCh:
			h.handleLazyConnectionClick()
		case <-h.client.mBlockInbound.ClickedCh:
			h.handleBlockInboundClick()
		case <-h.client.mAdvancedSettings.ClickedCh:
			h.handleAdvancedSettingsClick()
		case <-h.client.mCreateDebugBundle.ClickedCh:
			h.handleCreateDebugBundleClick()
		case <-h.client.mQuit.ClickedCh:
			h.handleQuitClick()
			return
		case <-h.client.mGitHub.ClickedCh:
			h.handleGitHubClick()
		case <-h.client.mInstallUpdate.ClickedCh:
			h.handleInstallUpdateClick()
		case <-h.client.mDownloadUpdate.ClickedCh:
			h.handleDownloadUpdateClick()
		case <-h.client.mNetworks.ClickedCh:
			h.handleNetworksClick()
		case <-h.client.mNotifications.ClickedCh:
			h.handleNotificationsClick()
		}
	}
}

func (h *eventHandler) handleConnectClick() {
	h.client.mUp.Disable()
	go func() {
		defer h.client.mUp.Enable()
		if err := h.client.menuUpClick(); err != nil {
			h.client.app.SendNotification(fyne.NewNotification("Error", "Failed to connect to Openzro service"))
		}
	}()
}

func (h *eventHandler) handleDisconnectClick() {
	h.client.mDown.Disable()
	go func() {
		defer h.client.mDown.Enable()
		if err := h.client.menuDownClick(); err != nil {
			h.client.app.SendNotification(fyne.NewNotification("Error", "Failed to connect to Openzro daemon"))
		}
	}()
}

func (h *eventHandler) handleAllowSSHClick() {
	h.toggleCheckbox(h.client.mAllowSSH)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mAllowSSH) // revert checkbox state on error
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("Error", "Failed to update SSH settings"))
	}

}

func (h *eventHandler) handleAutoConnectClick() {
	h.toggleCheckbox(h.client.mAutoConnect)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mAutoConnect) // revert checkbox state on error
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("Error", "Failed to update auto-connect settings"))
	}
}

func (h *eventHandler) handleRosenpassClick() {
	h.toggleCheckbox(h.client.mEnableRosenpass)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mEnableRosenpass) // revert checkbox state on error
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("Error", "Failed to update Rosenpass settings"))
	}
}

func (h *eventHandler) handleLazyConnectionClick() {
	h.toggleCheckbox(h.client.mLazyConnEnabled)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mLazyConnEnabled) // revert checkbox state on error
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("Error", "Failed to update lazy connection settings"))
	}
}

func (h *eventHandler) handleBlockInboundClick() {
	h.toggleCheckbox(h.client.mBlockInbound)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mBlockInbound) // revert checkbox state on error
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("Error", "Failed to update block inbound settings"))
	}
}

func (h *eventHandler) handleNotificationsClick() {
	h.toggleCheckbox(h.client.mNotifications)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mNotifications) // revert checkbox state on error
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("Error", "Failed to update notifications settings"))
	} else if h.client.eventManager != nil {
		h.client.eventManager.SetNotificationsEnabled(h.client.mNotifications.Checked())
	}

}

func (h *eventHandler) handleAdvancedSettingsClick() {
	h.client.mAdvancedSettings.Disable()
	go func() {
		defer h.client.mAdvancedSettings.Enable()
		defer h.client.getSrvConfig()
		h.runSelfCommand(h.client.ctx, "settings", "true")
	}()
}

func (h *eventHandler) handleCreateDebugBundleClick() {
	h.client.mCreateDebugBundle.Disable()
	go func() {
		defer h.client.mCreateDebugBundle.Enable()
		h.runSelfCommand(h.client.ctx, "debug", "true")
	}()
}

func (h *eventHandler) handleQuitClick() {
	systray.Quit()
}

func (h *eventHandler) handleGitHubClick() {
	if err := openURL("https://github.com/openzro/openzro"); err != nil {
		log.Errorf("failed to open GitHub URL: %v", err)
	}
}

// handleInstallUpdateClick asks the privileged daemon to run the
// rollout-gated self-update cycle (#5, C1). Long-running
// (download+verify+install): runs off the menu loop with the item
// disabled. No UI-side deadline — the daemon bounds the cycle
// (CycleTimeout); a short UI timeout would kill a legit download.
func (h *eventHandler) handleInstallUpdateClick() {
	h.client.mInstallUpdate.Disable()
	go func() {
		defer h.client.mInstallUpdate.Enable()

		conn, err := h.client.getSrvClient(defaultFailTimeout)
		if err != nil {
			log.Errorf("self-update: cannot reach daemon: %v", err)
			h.client.app.SendNotification(fyne.NewNotification("Update", "Cannot reach the openZro daemon"))
			return
		}

		// Snapshot pre-install state so a successful install (which
		// restarts the daemon and DROPS this RPC) is not misread as a
		// failure (#5 R5).
		var preVersion, target string
		if st, serr := conn.Status(h.client.ctx, &proto.StatusRequest{}); serr == nil {
			preVersion = st.GetDaemonVersion()
			target = st.GetUpdateState().GetTargetVersion()
		}

		h.client.app.SendNotification(fyne.NewNotification("Update", "Downloading and verifying the update…"))

		resp, err := conn.Update(h.client.ctx, &proto.UpdateRequest{})
		if err != nil {
			// The install restarts the daemon, killing THIS RPC — an
			// error here is ambiguous. Poll the daemon back up: if it
			// now runs the target (or simply a different version when
			// the target is unknown) the install actually SUCCEEDED.
			if h.installSucceededAfterRestart(preVersion, target) {
				h.client.app.SendNotification(fyne.NewNotification("Update installed",
					"openZro updated — the service restarted."))
				return
			}
			log.Errorf("self-update failed: %v", err)
			h.client.app.SendNotification(fyne.NewNotification("Update failed", err.Error()))
			return
		}
		switch {
		case resp.GetInstalled():
			h.client.app.SendNotification(fyne.NewNotification("Update installed",
				fmt.Sprintf("openZro %s installed — the service will restart.", resp.GetVersion())))
		case resp.GetSkipped():
			h.client.app.SendNotification(fyne.NewNotification("No update applied", resp.GetReason()))
		default:
			h.client.app.SendNotification(fyne.NewNotification("Update", resp.GetReason()))
		}
	}()
}

// handleDownloadUpdateClick opens the direct MSI asset URL for the
// currently directed target version in the default browser. Windows-
// only path: macOS uses handleInstallUpdateClick (daemon-driven
// auto-install), Linux uses the tray badge alone (no CTA). The
// browser handles the download, the user runs the MSI manually —
// info-only by design (the package-manager landscape on non-darwin
// makes a daemon-driven install unsafe).
//
// Target snapshot comes from a live Status fetch, mirroring the
// install handler (handleInstallUpdateClick at line 178) — the menu
// item is only shown when target != "", but the live read insulates
// us from a directive that moved between Show() and click.
func (h *eventHandler) handleDownloadUpdateClick() {
	go func() {
		conn, err := h.client.getSrvClient(defaultFailTimeout)
		if err != nil {
			log.Errorf("download update: cannot reach daemon: %v", err)
			return
		}
		st, err := conn.Status(h.client.ctx, &proto.StatusRequest{})
		if err != nil {
			log.Errorf("download update: status fetch failed: %v", err)
			return
		}
		us := st.GetUpdateState()
		target := us.GetTargetVersion()
		if target == "" {
			log.Warn("download update: clicked with empty target — directive cleared mid-click")
			return
		}
		// Recheck the rollout-gated verdict — the menu shows the CTA
		// only when applyUpdateStateLocked saw available=true, but the
		// daemon polls every ~2s. Between the menu Show() and the user
		// click, the directive could have moved or the manifest's
		// rollout could have retracted; bail rather than opening the
		// browser at a stale URL.
		if !us.GetAvailable() {
			log.Warnf("download update: clicked with available=false (decision %q) — directive moved mid-click",
				us.GetLastDecision())
			return
		}

		// Only windows_amd64 is shipped today. The check stays here
		// (not in the menu apply) so a future arm64 build can extend
		// the URL builder without changing apply call sites.
		if runtime.GOARCH != "amd64" {
			log.Warnf("download update: no MSI asset for windows/%s", runtime.GOARCH)
			return
		}

		// Mirror the path-escape pattern from
		// version/selfupdate/manifest.go::ResolveManifestTemplateURL —
		// the directive target is operator-supplied and could in
		// principle carry path-unsafe characters; PathEscape on each
		// segment keeps the URL well-formed.
		tag := url.PathEscape("v" + target)
		filename := "openzro_" + url.PathEscape(target) + "_windows_amd64.msi"
		assetURL := "https://github.com/openzro/openzro/releases/download/" + tag + "/" + filename

		if err := openURL(assetURL); err != nil {
			log.Errorf("download update: failed to open browser at %s: %v", assetURL, err)
		}
	}()
}

// installSucceededAfterRestart resolves the ambiguous post-install
// RPC drop: the install replaces+restarts the daemon, so the Update
// RPC connection dies even on success. Poll the daemon back up and
// report whether it is now running the update target (or, when the
// target is unknown, simply a different version than before). A
// genuine pre-install failure leaves the daemon untouched, so the
// version never changes and this correctly returns false (#5 R5).
func (h *eventHandler) installSucceededAfterRestart(preVersion, target string) bool {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		conn, err := h.client.getSrvClient(failFastTimeout)
		if err != nil {
			continue // daemon still restarting
		}
		st, err := conn.Status(h.client.ctx, &proto.StatusRequest{})
		if err != nil {
			continue
		}
		now := st.GetDaemonVersion()
		if now == "" {
			continue
		}
		// target (the directive at snapshot time) is the strongest
		// signal but NOT the only one: the manual RPC installs the
		// LIVE directive, which may have moved A->B between our
		// snapshot and the call. ANY post-restart version change vs
		// preVersion means the install succeeded (#5 R5 review). A
		// genuine pre-install failure never restarts the daemon, so
		// the version is unchanged and this stays false.
		if target != "" && now == target {
			return true
		}
		if preVersion != "" && now != preVersion {
			return true
		}
	}
	return false
}

func (h *eventHandler) handleNetworksClick() {
	h.client.mNetworks.Disable()
	go func() {
		defer h.client.mNetworks.Enable()
		h.runSelfCommand(h.client.ctx, "networks", "true")
	}()
}

func (h *eventHandler) toggleCheckbox(item *systray.MenuItem) {
	if item.Checked() {
		item.Uncheck()
	} else {
		item.Check()
	}
}

func (h *eventHandler) updateConfigWithErr() error {
	if err := h.client.updateConfig(); err != nil {
		return err
	}

	return nil
}

func (h *eventHandler) runSelfCommand(ctx context.Context, command, arg string) {
	proc, err := os.Executable()
	if err != nil {
		log.Errorf("error getting executable path: %v", err)
		return
	}

	cmd := exec.CommandContext(ctx, proc,
		fmt.Sprintf("--%s=%s", command, arg),
		fmt.Sprintf("--daemon-addr=%s", h.client.addr),
	)

	if out := h.client.attachOutput(cmd); out != nil {
		defer func() {
			if err := out.Close(); err != nil {
				log.Errorf("error closing log file %s: %v", h.client.logFile, err)
			}
		}()
	}

	log.Printf("running command: %s --%s=%s --daemon-addr=%s", proc, command, arg, h.client.addr)

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Printf("command '%s %s' failed with exit code %d", command, arg, exitErr.ExitCode())
		}
		return
	}

	log.Printf("command '%s %s' completed successfully", command, arg)
}
