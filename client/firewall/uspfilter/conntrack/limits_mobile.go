//go:build ios || android

package conntrack

// Default per-tracker entry caps on mobile. An iOS network extension is
// killed past ~50 MB and Android reclaims memory aggressively, so the whole
// conntrack footprint is kept under 5 MB at worst.
const (
	DefaultMaxTCPEntries  = 4096
	DefaultMaxUDPEntries  = 2048
	DefaultMaxICMPEntries = 512
)
