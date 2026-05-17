package selfupdate

import (
	"context"
	"sync"

	log "github.com/sirupsen/logrus"
)

// NewListener adapts the updater to version.Update.SetOnUpdateListener
// — no change to the version package is needed, the callback seam
// already exists. The returned func is what the client registers so a
// detected newer release drives a (rollout-gated, default-off) cycle.
//
// It single-flights. The version checker fires the listener on
// registration AND every 30-minute tick; a download can take minutes,
// so without single-flight an overlapping tick could stack a second
// install on top of an in-flight one. A trigger while a cycle is
// running is dropped (the next tick re-evaluates anyway). The cycle
// runs on its own goroutine with panic recovery so a bug in the
// updater can never take down the caller's version-check loop.
//
// Returns ErrUnsupportedPlatform on non-macOS so the caller simply
// does not register a listener there (Phase 1 is macOS only).
func NewListener(cfg Config) (func(), error) {
	u, err := New(cfg)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	running := false
	return func() {
		mu.Lock()
		if running {
			mu.Unlock()
			log.Debug("selfupdate: a cycle is already in progress; ignoring this trigger")
			return
		}
		running = true
		mu.Unlock()

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Errorf("selfupdate: recovered from panic in update cycle: %v", r)
				}
				mu.Lock()
				running = false
				mu.Unlock()
			}()
			res, err := u.RunOnce(context.Background())
			if err != nil {
				log.Errorf("selfupdate: cycle failed: %v", err)
				return
			}
			log.Infof("selfupdate: cycle done installed=%v skipped=%v version=%s reason=%q",
				res.Installed, res.Skipped, res.Version, res.Reason)
		}()
	}, nil
}
