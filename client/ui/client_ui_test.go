//go:build !(linux && 386)

package main

import (
	"testing"

	"github.com/openzro/openzro/client/proto"
)

// TestDecideUpdateMenu pins the per-platform tray surface of the
// management-driven update directive (#5): macOS gets a daemon-
// install CTA, Windows gets a direct-MSI download CTA, Linux gets
// nothing (badge-only — package manager is the install path).
func TestDecideUpdateMenu(t *testing.T) {
	eligible := &proto.UpdateState{
		TargetVersion: "0.53.1-alpha.79",
		Force:         false,
		Available:     true,
		LastDecision:  "eligible for 0.53.1-alpha.79 (management-directed)",
	}
	forced := &proto.UpdateState{
		TargetVersion: "0.53.1-alpha.79",
		Force:         true,
		Available:     true,
		LastDecision:  "eligible for 0.53.1-alpha.79 (management-directed)",
	}
	noUpdate := &proto.UpdateState{
		TargetVersion: "0.53.1-alpha.79",
		Available:     false,
		LastDecision:  "already at or above 0.53.1-alpha.79",
	}
	cleared := &proto.UpdateState{
		// no directive — empty target, not available
		LastDecision: "no directive (operator cleared the target)",
	}

	t.Run("darwin: eligible non-force shows status + install", func(t *testing.T) {
		v := decideUpdateMenu("darwin", eligible)
		if !v.statusShown {
			t.Error("status line must show whenever a target is set")
		}
		if v.statusTitle != "Update: 0.53.1-alpha.79" {
			t.Errorf("status title %q must carry the target", v.statusTitle)
		}
		if !v.installShown {
			t.Error("install CTA must show for available && !force on darwin")
		}
		if v.installTitle != "Install openZro 0.53.1-alpha.79" {
			t.Errorf("install title %q must include target", v.installTitle)
		}
		if v.downloadShown {
			t.Error("download CTA must stay hidden on darwin")
		}
	})

	t.Run("darwin: forced directive hides the install CTA", func(t *testing.T) {
		// Force-mode means the daemon installs silently; surfacing a
		// manual CTA would be misleading.
		v := decideUpdateMenu("darwin", forced)
		if !v.statusShown {
			t.Error("status line stays — operator wants to know what's coming")
		}
		if v.installShown {
			t.Error("install CTA must be hidden when force=true")
		}
	})

	t.Run("darwin: not-available hides install but shows status", func(t *testing.T) {
		v := decideUpdateMenu("darwin", noUpdate)
		if !v.statusShown {
			t.Error("status line must still show — operator sees the decided reason")
		}
		if v.installShown {
			t.Error("install CTA must be hidden when available=false")
		}
	})

	t.Run("windows: eligible shows status + download (no install)", func(t *testing.T) {
		v := decideUpdateMenu("windows", eligible)
		if !v.statusShown {
			t.Error("status line must show")
		}
		if v.installShown {
			t.Error("install CTA must stay hidden on windows (no auto-install)")
		}
		if !v.downloadShown {
			t.Error("download CTA must show for available on windows")
		}
		if v.downloadTitle != "Download openZro 0.53.1-alpha.79 (.msi)" {
			t.Errorf("download title %q must include target + .msi suffix", v.downloadTitle)
		}
	})

	t.Run("windows: forced directive still shows download (info-only)", func(t *testing.T) {
		// On Windows there is no silent install path, so a force
		// directive doesn't change the surface — the operator still
		// needs to download + run the MSI manually.
		v := decideUpdateMenu("windows", forced)
		if !v.downloadShown {
			t.Error("download CTA must show on windows regardless of force")
		}
		if v.installShown {
			t.Error("install CTA must never show on windows")
		}
	})

	t.Run("windows: not-available hides download but shows status", func(t *testing.T) {
		v := decideUpdateMenu("windows", noUpdate)
		if !v.statusShown {
			t.Error("status line stays — operator sees the decided reason")
		}
		if v.downloadShown {
			t.Error("download CTA must be hidden when available=false")
		}
	})

	t.Run("linux: badge-only, all menu items hidden", func(t *testing.T) {
		// Linux installs via package manager (apt/dnf/pacman); the
		// tray icon variant is the entire signal.
		v := decideUpdateMenu("linux", eligible)
		if v.statusShown || v.installShown || v.downloadShown {
			t.Errorf("linux must keep all items hidden, got %+v", v)
		}
	})

	t.Run("linux: same on every state", func(t *testing.T) {
		for _, us := range []*proto.UpdateState{eligible, forced, noUpdate, cleared} {
			v := decideUpdateMenu("linux", us)
			if v.statusShown || v.installShown || v.downloadShown {
				t.Errorf("linux badge-only invariant broken for %+v: verdict %+v", us, v)
			}
		}
	})

	t.Run("cleared directive: empty target hides status on every platform", func(t *testing.T) {
		for _, goos := range []string{"darwin", "windows", "linux"} {
			v := decideUpdateMenu(goos, cleared)
			if v.statusShown {
				t.Errorf("%s: cleared directive must hide status line, got title %q",
					goos, v.statusTitle)
			}
			if v.installShown || v.downloadShown {
				t.Errorf("%s: cleared directive must hide all CTAs", goos)
			}
		}
	})

	t.Run("nil state is the no-directive verdict", func(t *testing.T) {
		// The status struct may be nil between daemon restart and the
		// first preflight publish; getters are nil-safe in proto and
		// the verdict must collapse to all-hidden.
		v := decideUpdateMenu("darwin", nil)
		if v.statusShown || v.installShown || v.downloadShown {
			t.Errorf("nil state must yield all-hidden, got %+v", v)
		}
	})

	t.Run("unknown goos: no CTA, conservative default", func(t *testing.T) {
		// Anything that isn't darwin/windows falls through to the
		// Linux/default branch — badge-only. Pins the freebsd/openbsd
		// path even though those clients don't ship today.
		v := decideUpdateMenu("freebsd", eligible)
		if v.statusShown || v.installShown || v.downloadShown {
			t.Errorf("unknown goos must default to badge-only, got %+v", v)
		}
	})
}
