package cluster

import (
	"os"
	"runtime"
	"testing"
)

// skipOnDarwinCI bails out of any test that brings up a 2-pod
// cluster on loopback. The macOS GitHub-Actions runners are shared
// hosts whose loopback latency can spike past whatever HELLO budget
// we set: alpha.84 was tagged with helloTimeout at 10 s (3× the
// original 3 s — see issue #116 / PR #128), and the runners still
// drift past that under load, leaving `i/o timeout: read HELLO`
// across TestLocator_* and TestForwarder_*.
//
// Coverage is identical on the Linux + FreeBSD + Windows lanes, so
// skipping macOS-only here costs us nothing — the logic is OS-
// agnostic. Local macOS runs of `go test ./relay/server/cluster/...`
// still execute these tests; only CI is gated. Re-enable in a
// future commit if/when we move the relay cluster tests to a
// docker-backed runner with deterministic loopback latency.
func skipOnDarwinCI(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "darwin" && os.Getenv("CI") == "true" {
		t.Skip("non-hermetic on macOS CI: shared runner exceeds the loopback HELLO budget — covered on Linux / FreeBSD / Windows")
	}
}
