package selfupdate

import (
	"context"
	"errors"
	"os/exec"
)

// ErrUnsupportedPlatform is returned by the updater on any OS other
// than macOS. Phase 1 of #5 is macOS-only by design: an auto-installer
// with no verifiable artifact is a malware vector, so each platform is
// gated on having an authenticity anchor. macOS has one today
// (notarization); Windows is Phase 2 (blocked on signing, #1); Linux
// is out of scope (distro package manager).
var ErrUnsupportedPlatform = errors.New("selfupdate: unsupported platform (macOS only in phase 1)")

// CommandRunner runs an external command and returns its combined
// output. It is the single seam that keeps every platform code path
// unit-testable off a Mac: tests inject a scripted runner; production
// uses execRunner.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner is the production CommandRunner. It is OS-neutral Go —
// only the command *names* it is asked to run (pkgutil, spctl,
// installer, launchctl) are macOS-specific, and those are reached
// only when the platform gate has already passed.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
