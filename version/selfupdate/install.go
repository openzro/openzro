package selfupdate

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
)

// Installer applies a verified installer. It must NOT be reached for
// an artifact that failed Verify.
type Installer interface {
	Install(ctx context.Context, pkgPath string) error
}

func installerArgs(pkg string) []string {
	return []string{"-pkg", pkg, "-target", "/"}
}

// launchctlKickstartArgs force-restarts a system launchd service.
// Only used when an explicit ServiceLabel is configured — see
// installMacPkg.
func launchctlKickstartArgs(label string) []string {
	return []string{"kickstart", "-k", "system/" + label}
}

// installMacPkg runs `installer -pkg <pkg> -target /` (which executes
// the package's own postinstall — that is what (re)registers the
// launchd daemon per ADR-0007). A failed install aborts BEFORE any
// restart so the running daemon is never bounced onto a half-applied
// state.
//
// serviceLabel is intentionally optional and unset by default: the
// concrete launchd label is defined in the PKG's postinstall, which
// lives in the release infra and is not in this tree — guessing it
// would be wrong. The PKG postinstall already handles the daemon;
// when an operator does supply a label we additionally kickstart it,
// best-effort (a kickstart failure is logged, not fatal, because the
// postinstall is the primary path).
func installMacPkg(ctx context.Context, run CommandRunner, pkg, serviceLabel string) error {
	if out, err := run(ctx, "installer", installerArgs(pkg)...); err != nil {
		return fmt.Errorf("selfupdate: installer failed (daemon NOT restarted): %w (%s)", err, trim(out))
	}
	if serviceLabel != "" {
		if out, err := run(ctx, "launchctl", launchctlKickstartArgs(serviceLabel)...); err != nil {
			log.Warnf("selfupdate: explicit launchctl kickstart of %q failed (PKG postinstall is the primary restart path): %v (%s)",
				serviceLabel, err, trim(out))
		}
	}
	return nil
}

type macInstaller struct {
	run          CommandRunner
	serviceLabel string
}

func (i macInstaller) Install(ctx context.Context, pkgPath string) error {
	return installMacPkg(ctx, i.run, pkgPath, i.serviceLabel)
}
