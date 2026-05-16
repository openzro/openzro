package selfupdate

import (
	"fmt"
	"hash/crc32"

	goversion "github.com/hashicorp/go-version"
)

// GateInput is everything the rollout decision needs. It is pure data
// so Evaluate is a total, side-effect-free function — the whole
// fleet-safety story is unit-testable without a network or a disk.
type GateInput struct {
	// Current is the running client version.
	Current string
	// Manifest is the fetched static release descriptor.
	Manifest *Manifest
	// AutoInstallEnabled is the "Auto-install updates" setting. It is
	// default-OFF (#5): without an explicit opt-in the client only
	// surfaces the update, it does not apply it automatically.
	AutoInstallEnabled bool
	// PinnedVersion, when set, pins this client to exactly that
	// version — an explicit operator override.
	PinnedVersion string
	// ClientID is a stable per-client identifier (machine id / wg key
	// hash). It buckets the client for staged rollout so the same
	// client is consistently in or out of a partial release.
	ClientID string
}

// Decision is the gate result. Reason is always populated for logs.
type Decision struct {
	Eligible bool
	// Critical is true when Current is below Manifest.MinVersion — the
	// release we are on is no longer kept alive (e.g. a security cut).
	// Critical bypasses STAGED ROLLOUT only; it deliberately does NOT
	// override an explicit pin or an opt-out of auto-install (an
	// explicit operator/user choice winning is the less-surprising
	// default — flagged for confirmation in the #5 status report).
	Critical bool
	Reason   string
}

// bucketOf maps a client id to a stable 0..99 slot.
func bucketOf(clientID string) int {
	return int(crc32.ChecksumIEEE([]byte(clientID)) % 100)
}

// Evaluate decides whether this client should take the manifest's
// release now. Order matters: cheap/disqualifying checks first, the
// staged-rollout dice last.
func Evaluate(in GateInput) Decision {
	cur, err := goversion.NewVersion(in.Current)
	if err != nil {
		return Decision{Reason: fmt.Sprintf("current version %q unparseable — refusing to self-update", in.Current)}
	}
	next, err := goversion.NewVersion(in.Manifest.Version)
	if err != nil {
		return Decision{Reason: fmt.Sprintf("manifest version %q unparseable", in.Manifest.Version)}
	}

	critical := false
	if in.Manifest.MinVersion != "" {
		if floor, ferr := goversion.NewVersion(in.Manifest.MinVersion); ferr == nil {
			critical = cur.LessThan(floor)
		}
	}

	if !next.GreaterThan(cur) {
		return Decision{Critical: critical, Reason: fmt.Sprintf("already at or above %s", in.Manifest.Version)}
	}
	if in.PinnedVersion != "" && in.PinnedVersion != in.Manifest.Version {
		return Decision{Critical: critical, Reason: fmt.Sprintf("pinned to %s, manifest is %s", in.PinnedVersion, in.Manifest.Version)}
	}
	if !in.AutoInstallEnabled {
		return Decision{Critical: critical, Reason: "auto-install disabled — surfacing for manual update only"}
	}

	// Staged rollout is the last gate, and critical updates skip it: a
	// security floor breach should not be slow-rolled. Everything here
	// is fail-CLOSED (Codex-1): absent declaration, 0%, or no stable
	// client id all mean "do not update", never "update everyone".
	if !critical {
		if in.Manifest.StagedRollout == nil {
			return Decision{Reason: "manifest declares no staged_rollout — refusing"}
		}
		r := *in.Manifest.StagedRollout
		switch {
		case r <= 0:
			return Decision{Reason: "staged_rollout 0% — not released to any client yet"}
		case r >= 100:
			// everyone — no per-client gate
		default:
			if in.ClientID == "" {
				return Decision{Reason: fmt.Sprintf("no stable client id — excluded from %d%% staged rollout", r)}
			}
			if b := bucketOf(in.ClientID); b >= r {
				return Decision{Reason: fmt.Sprintf("not in staged rollout (bucket %d >= %d%%)", b, r)}
			}
		}
	}

	reason := fmt.Sprintf("eligible for %s", in.Manifest.Version)
	if critical {
		reason = fmt.Sprintf("CRITICAL: below min_version %s — updating to %s", in.Manifest.MinVersion, in.Manifest.Version)
	}
	return Decision{Eligible: true, Critical: critical, Reason: reason}
}
