package selfupdate

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingInstaller blocks in Install until released, counting calls —
// lets the single-flight property be asserted deterministically.
type blockingInstaller struct {
	calls   atomic.Int32
	release chan struct{}
}

func (b *blockingInstaller) Install(_ context.Context, _ string) error {
	b.calls.Add(1)
	<-b.release
	return nil
}

func TestNewListener_UnsupportedPlatform(t *testing.T) {
	if _, err := NewListener(Config{GOOS: "linux"}); err != ErrUnsupportedPlatform {
		t.Fatalf("non-macOS must not yield a listener, got %v", err)
	}
}

func TestNewListener_SingleFlight(t *testing.T) {
	srv := updaterServer(t, 0, "2.0.0")
	defer srv.Close()
	cfg := baseCfg(srv)
	bi := &blockingInstaller{release: make(chan struct{})}
	cfg.Verifier = &fakeVerifier{}
	cfg.Installer = bi

	listen, err := NewListener(cfg)
	if err != nil {
		t.Fatal(err)
	}

	listen() // starts a cycle; installer will block
	// Wait until the cycle is actually inside Install.
	deadline := time.Now().Add(2 * time.Second)
	for bi.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if bi.calls.Load() != 1 {
		t.Fatalf("first trigger should have reached Install once, got %d", bi.calls.Load())
	}

	// Triggers while the first cycle is mid-install must be dropped.
	listen()
	listen()
	time.Sleep(100 * time.Millisecond)
	if bi.calls.Load() != 1 {
		t.Fatalf("overlapping triggers must be single-flighted, got %d installs", bi.calls.Load())
	}

	// Closing release lets the in-flight cycle finish AND makes every
	// later Install return immediately — no field mutation, so no
	// data race with the still-running goroutine.
	close(bi.release)
	time.Sleep(150 * time.Millisecond) // first cycle drains, single-flight clears

	// A fresh trigger after completion runs again.
	listen()
	deadline = time.Now().Add(2 * time.Second)
	for bi.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if bi.calls.Load() < 2 {
		t.Fatalf("a trigger after the cycle finished should run again, got %d", bi.calls.Load())
	}
	time.Sleep(100 * time.Millisecond) // let the last cycle finish before srv.Close
}

// panicInstaller verifies a bug in the updater cannot take down the
// caller's version-check loop and that single-flight is released.
type panicInstaller struct{ n atomic.Int32 }

func (p *panicInstaller) Install(_ context.Context, _ string) error {
	p.n.Add(1)
	panic("simulated updater bug")
}

func TestNewListener_PanicRecovered(t *testing.T) {
	srv := updaterServer(t, 0, "2.0.0")
	defer srv.Close()
	cfg := baseCfg(srv)
	pi := &panicInstaller{}
	cfg.Verifier = &fakeVerifier{}
	cfg.Installer = pi

	listen, err := NewListener(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); listen() }()
	wg.Wait() // listen() itself must never panic to the caller

	// The panicking cycle must release single-flight so a later
	// trigger can run again.
	deadline := time.Now().Add(2 * time.Second)
	for pi.n.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	listen()
	deadline = time.Now().Add(2 * time.Second)
	for pi.n.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if pi.n.Load() < 2 {
		t.Fatalf("single-flight must be released after a panic, got %d", pi.n.Load())
	}
}
