package selfupdate

import (
	"context"
	"fmt"
)

// Verifier asserts the authenticity of a staged installer. The
// download step already proved integrity (SHA-256 vs manifest); this
// proves the artifact is the genuine, vendor-signed one. Fail-closed:
// any doubt is a refusal — installing an unverified package as root
// is the exact malware vector this whole feature must not become.
type Verifier interface {
	Verify(ctx context.Context, pkgPath string) error
}

func pkgutilArgs(pkg string) []string {
	return []string{"--check-signature", pkg}
}

// spctl assesses the package against Gatekeeper as an installer —
// this is what actually exercises the Apple *notarization* ticket,
// which is openZro's macOS authenticity anchor (the signed+notarized
// PKG validated this cycle, per #5).
func spctlArgs(pkg string) []string {
	return []string{"--assess", "--type", "install", "--verbose", pkg}
}

// verifyMacPkg runs pkgutil --check-signature then a Gatekeeper
// assessment. Either failing aborts: the signature could be absent or
// broken (pkgutil) or valid-but-not-notarized / revoked (spctl).
func verifyMacPkg(ctx context.Context, run CommandRunner, pkg string) error {
	if out, err := run(ctx, "pkgutil", pkgutilArgs(pkg)...); err != nil {
		return fmt.Errorf("selfupdate: pkgutil signature check failed: %w (%s)", err, trim(out))
	}
	if out, err := run(ctx, "spctl", spctlArgs(pkg)...); err != nil {
		return fmt.Errorf("selfupdate: Gatekeeper/notarization assessment failed: %w (%s)", err, trim(out))
	}
	return nil
}

type macVerifier struct{ run CommandRunner }

func (v macVerifier) Verify(ctx context.Context, pkgPath string) error {
	return verifyMacPkg(ctx, v.run, pkgPath)
}

func trim(b []byte) string {
	const max = 200
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
