// Package cluster implements coordinated multi-pod relay support
// for K8s deployments, per ADR-0014.
//
// Each relay pod owns the peers that registered directly with it
// (the "local store" — same as the single-pod build). When a peer
// asks for a session to a peer that isn't in the local store, this
// package finds which other pod owns it (broadcast WHO_HAS over a
// long-lived TCP fabric, cache the answer with a short TTL),
// establishes a per-(src, dst) channel multiplexed onto the same
// inter-pod TCP stream, and forwards bytes through.
//
// The package is K8s-only. Discovery uses Headless Service DNS to
// learn peer pod addresses; inter-pod traffic is plain TCP on a
// trusted backplane (NetworkPolicy isolated). Single-pod
// deployments do not import any of this — they continue to use the
// existing relay/server/store path unchanged.
//
// Bare-metal / VM operators run the per-region pattern from
// ADR-0009 instead. Cross-region routing is the client's
// responsibility (foreign-dial in relay/client) and is intentionally
// out of scope here — this package only coordinates pods inside one
// regional cluster.
package cluster
