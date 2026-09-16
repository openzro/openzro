//go:build !ios && !android

package conntrack

// Default per-tracker entry caps on desktop and server platforms. In the
// range Linux ships for nf_conntrack_max on a workstation, with headroom: a
// TCPConnTrack is roughly 200 bytes plus map overhead, so the TCP table
// tops out around 20 MB before anything is evicted.
const (
	DefaultMaxTCPEntries  = 65536
	DefaultMaxUDPEntries  = 16384
	DefaultMaxICMPEntries = 2048
)
