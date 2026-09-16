package conntrack

import (
	"os"
	"strconv"

	nblog "github.com/openzro/openzro/client/firewall/uspfilter/log"
)

// Environment overrides for the per-tracker entry caps. The defaults live in
// limits_desktop.go and limits_mobile.go. A value that does not parse, or is
// not positive, falls back to the default with a warning.
const (
	EnvTCPMaxEntries  = "OZ_CONNTRACK_TCP_MAX"
	EnvUDPMaxEntries  = "OZ_CONNTRACK_UDP_MAX"
	EnvICMPMaxEntries = "OZ_CONNTRACK_ICMP_MAX"
)

// evictSampleSize bounds how many entries one eviction inspects. Go map
// iteration starts at a random bucket, so a short scan is a random sample;
// choosing the stalest of eight is close enough to LRU for a table that only
// fills under abuse, and it keeps insertion O(1) while the flood lasts.
const evictSampleSize = 8

// envInt reads name as a positive integer, or returns def.
func envInt(logger *nblog.Logger, name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		logger.Warn("invalid %s=%q: must be a positive integer, using %d", name, v, def)
		return def
	}
	return n
}

// evictCandidate picks one entry to drop from a full table. It samples up to
// evictSampleSize entries and returns the first one dead() reports, or else
// the one with the smallest lastSeen(). The caller holds the table's lock.
func evictCandidate[K comparable, V any](m map[K]V, lastSeen func(V) int64, dead func(V) bool) (K, bool) {
	var (
		candKey  K
		candSeen int64
		found    bool
		sampled  int
	)
	for k, v := range m {
		if dead(v) {
			return k, true
		}
		if seen := lastSeen(v); !found || seen < candSeen {
			candKey, candSeen, found = k, seen, true
		}
		if sampled++; sampled >= evictSampleSize {
			break
		}
	}
	return candKey, found
}
