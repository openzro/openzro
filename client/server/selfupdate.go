package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"sync"

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

// startSelfUpdateWatcher is the background path: the existing 30-min
// version check fires the (single-flighted) cycle. It deliberately
// does NOT use selfupdate.NewListener — that binds a STATIC Config,
// but the daemon must read fresh config every cycle (runtime toggle).
// NewListener stays the documented seam for embedders without that
// requirement. Blocks until ctx is done; launch as a goroutine.
func (s *Server) startSelfUpdateWatcher(ctx context.Context) {
	// Platform gate WITHOUT reading s.config: this goroutine is
	// launched from inside Start(), which mutates s.config without
	// s.mutex (the existing code never had a concurrent reader). A
	// runtime.GOOS check needs no config, so the watcher never races
	// Start()'s config writes. Phase 1 is macOS-only anyway.
	if runtime.GOOS != "darwin" {
		return
	}

	up := version.NewUpdate("openzro-daemon")
	up.SetDaemonVersion(version.OpenzroVersion())

	var mu sync.Mutex
	running := false
	up.SetOnUpdateListener(func() {
		mu.Lock()
		if running {
			mu.Unlock()
			return
		}
		running = true
		mu.Unlock()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("selfupdate watcher: recovered from panic: %v", r)
			}
			mu.Lock()
			running = false
			mu.Unlock()
		}()
		res, err := s.runSelfUpdate(ctx)
		if err != nil {
			log.Errorf("selfupdate watcher: %v", err)
			return
		}
		log.Infof("selfupdate watcher: installed=%v skipped=%v version=%s reason=%q",
			res.Installed, res.Skipped, res.Version, res.Reason)
	})

	<-ctx.Done()
	up.StopWatch()
}
