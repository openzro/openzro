package selfupdate

import (
	"context"
	"fmt"
	"regexp"
)

// Verifier asserts the authenticity of a staged installer. The
// download step already proved integrity (SHA-256 vs manifest); this
// proves the artifact is the genuine openZro-signed one. Fail-closed:
// any doubt is a refusal — installing an unverified package as root
// is the exact malware vector this whole feature must not become.
type Verifier interface {
	Verify(ctx context.Context, pkgPath string) error
}

func pkgutilArgs(pkg string) []string {
	return []string{"--check-signature", pkg}
}

// spctl assesses the package against Gatekeeper as an installer —
// this exercises the Apple *notarization* ticket (not revoked, scanned
// clean). Necessary but NOT sufficient: notarization is a malware
// scan, not an identity pin — Apple notarizes any developer's package,
// including an attacker's. Identity pinning (below) is what binds the
// install to openZro specifically.
func spctlArgs(pkg string) []string {
	return []string{"--assess", "--type", "install", "--verbose", pkg}
}

// teamIDRe extracts the Apple Team ID from the leaf "Developer ID
// Installer" certificate line of `pkgutil --check-signature` output,
// e.g. `1. Developer ID Installer: openZro (AB12CD34EF)`.
var teamIDRe = regexp.MustCompile(`Developer ID Installer:[^\n(]*\(([0-9A-Z]{10})\)`)

func parsePkgTeamID(out []byte) (string, bool) {
	m := teamIDRe.FindSubmatch(out)
	if m == nil {
		return "", false
	}
	return string(m[1]), true
}

// verifyMacPkg is the macOS authenticity gate. Order and rationale:
//
//  1. expectedTeamID MUST be configured. An empty pin means we cannot
//     prove the package is openZro's, so we refuse — fail-closed, not
//     fail-open. (The real Team ID is a release-infra input wired at
//     the binding layer, like the signing cert itself.)
//  2. pkgutil --check-signature must succeed AND its leaf certificate
//     must be openZro's Developer ID Installer (Team ID match). This
//     is the fix for the review finding that notarization alone only
//     proves "signed by *some* Apple developer".
//  3. spctl --assess must accept it as an installer — notarization is
//     valid and not revoked.
func verifyMacPkg(ctx context.Context, run CommandRunner, pkg, expectedTeamID string) error {
	if expectedTeamID == "" {
		return fmt.Errorf("selfupdate: no expected signing Team ID configured — refusing (fail-closed)")
	}

	out, err := run(ctx, "pkgutil", pkgutilArgs(pkg)...)
	if err != nil {
		return fmt.Errorf("selfupdate: pkgutil signature check failed: %w (%s)", err, trim(out))
	}
	gotTeamID, ok := parsePkgTeamID(out)
	if !ok {
		return fmt.Errorf("selfupdate: no Developer ID Installer identity in pkgutil output — refusing")
	}
	if gotTeamID != expectedTeamID {
		return fmt.Errorf("selfupdate: package signed by Team ID %q, expected %q — refusing", gotTeamID, expectedTeamID)
	}

	if out, err := run(ctx, "spctl", spctlArgs(pkg)...); err != nil {
		return fmt.Errorf("selfupdate: Gatekeeper/notarization assessment failed: %w (%s)", err, trim(out))
	}
	return nil
}

type macVerifier struct {
	run            CommandRunner
	expectedTeamID string
}

func (v macVerifier) Verify(ctx context.Context, pkgPath string) error {
	return verifyMacPkg(ctx, v.run, pkgPath, v.expectedTeamID)
}

func trim(b []byte) string {
	const maxLen = 200
	s := string(b)
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}
