package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	log "github.com/sirupsen/logrus"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openzro/openzro/client/proto"
	"github.com/openzro/openzro/version"
	"github.com/openzro/openzro/version/selfupdate"
)

// clientUpdateID derives a stable, opaque per-install id for staged-
// rollout bucketing from the WireGuard identity (D2: hash the PUBLIC
// key, never the raw key, so it cannot leak into a log line or a
// manifest bucket). Falls back to hashing the key string as-is if it
// does not parse — still stable per install, still opaque.
func clientUpdateID(wgPrivKey string) string {
	src := wgPrivKey
	if k, err := wgtypes.ParseKey(wgPrivKey); err == nil {
		src = k.PublicKey().String()
	}
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:])
}

// selfUpdateConfig snapshots the live client config into a
// selfupdate.Config. The server mutex is held only for the snapshot;
// the (network + install) cycle runs without it.
func (s *Server) selfUpdateConfig() selfupdate.Config {
	s.mutex.Lock()
	cfg := s.config
	s.mutex.Unlock()

	c := selfupdate.Config{
		CurrentVersion: version.OpenzroVersion(),
		ManifestURL:    selfupdate.ResolveManifestURL(),
		UserAgent:      "openzro-daemon/" + version.OpenzroVersion(),
		ExpectedTeamID: selfupdate.BuildTeamID(),
	}
	if cfg != nil {
		c.AutoInstallEnabled = cfg.AutoInstallUpdates
		c.PinnedVersion = cfg.UpdatePinnedVersion
		c.ClientID = clientUpdateID(cfg.PrivateKey)
	}
	return c
}

// runSelfUpdate executes one rollout-gated cycle. Shared by the
// Update RPC (manual "Install now") and the periodic watcher so both
// always read FRESH config — the user can toggle AutoInstallUpdates
// or the pin at runtime and it takes effect next cycle, no restart.
func (s *Server) runSelfUpdate(ctx context.Context) (*proto.UpdateResponse, error) {
	u, err := selfupdate.New(s.selfUpdateConfig())
	if err == selfupdate.ErrUnsupportedPlatform {
		return nil, status.Error(codes.Unimplemented, "self-update is macOS-only in phase 1")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "selfupdate init: %v", err)
	}
	res, err := u.RunOnce(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "selfupdate: %v", err)
	}
	return &proto.UpdateResponse{
		Installed: res.Installed,
		Skipped:   res.Skipped,
		Version:   res.Version,
		Reason:    res.Reason,
		Critical:  res.Critical,
	}, nil
}

// Update is DaemonService.Update: the unprivileged Fyne UI asks the
// privileged daemon to run the cycle (C1 — only the daemon can run
// `installer -target /` as root). "skipped" (not eligible) is a
// normal response; a genuine failure is a gRPC status error.
func (s *Server) Update(ctx context.Context, _ *proto.UpdateRequest) (*proto.UpdateResponse, error) {
	return s.runSelfUpdate(ctx)
}

// updateDirective is the latest management-conveyed client
// self-update decision (openZro #5). targetVersion == "" means the
// operator has no active directive — clients do nothing. seen
// distinguishes "never received a Sync directive" from "operator
// explicitly cleared the target", which R3b/R3c need to surface
// Available correctly.
type updateDirective struct {
	targetVersion string
	force         bool
	seen          bool
}

// onUpdateDirective is the daemon side of the management-driven
// self-update seam (openZro #5). The engine invokes it — deduped by
// (target,force), while holding syncMsgMux — whenever the operator's
// fleet decision changes on the Sync stream. It MUST stay cheap and
// non-blocking: R3a only records the directive and logs it. The
// rollout-gated secure pipeline (preflight in R3c, install in R3d)
// runs on the daemon's own goroutines and reads s.updateDirective —
// it is never driven from inside this callback.
func (s *Server) onUpdateDirective(targetVersion string, force bool) {
	s.updateDirectiveMu.Lock()
	s.updateDirective = updateDirective{
		targetVersion: targetVersion,
		force:         force,
		seen:          true,
	}
	s.updateDirectiveMu.Unlock()

	if targetVersion == "" {
		log.Infof("client self-update: management cleared the update directive")
		return
	}
	log.Infof("client self-update: management directive target=%s force=%t "+
		"(recorded; preflight/install land in R3c/R3d)", targetVersion, force)
}

// buildUpdateState renders the latest recorded directive into the
// proto surface the UI reads (openZro #5). Returns nil when no
// directive has been seen this daemon lifetime — after a daemon
// restart the state is empty until the next Sync re-delivers it
// (state is keyed off the live Sync stream, never persisted; this is
// the Codex post-restart-staleness fix). available is a deliberately
// minimal target!=running check for R3b; R3c replaces it with the
// full rollout-gated preflight (manifest resolve + staged bucket).
func (s *Server) buildUpdateState() *proto.UpdateState {
	s.updateDirectiveMu.Lock()
	d := s.updateDirective
	s.updateDirectiveMu.Unlock()

	if !d.seen {
		return nil
	}

	running := version.OpenzroVersion()
	available := d.targetVersion != "" && d.targetVersion != running

	var decision string
	switch {
	case d.targetVersion == "":
		decision = "no directive (operator cleared the target)"
	case !available:
		decision = "up to date (running the directed version)"
	case d.force:
		decision = "update available — operator forced (silent install)"
	default:
		decision = "update available — operator offered (user opt-in)"
	}

	return &proto.UpdateState{
		TargetVersion: d.targetVersion,
		Force:         d.force,
		Available:     available,
		LastDecision:  decision,
	}
}
