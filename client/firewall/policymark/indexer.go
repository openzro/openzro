// Package policymark owns the in-process mapping between a
// management-issued PolicyID and the small integer the firewall
// backend stamps on the conntrack mark (per ADR-0013), so the
// netlink-based flow collector can resolve a flow event back to the
// originating policy.
//
// The indexer is a process-level singleton. Firewall backends
// (nftables, iptables) ask Index() for the rule_index when they
// install a peer ACL rule; the conntrack collector asks Lookup()
// when it builds a flow event. Both sides hit the same instance via
// Default().
package policymark

import (
	"encoding/hex"
	"sync"
	"sync/atomic"

	log "github.com/sirupsen/logrus"

	nbnet "github.com/openzro/openzro/util/net"
)

// Indexer maps PolicyIDs to small in-process integers. Index 0 is a
// sentinel meaning "no rule associated" and is never handed out;
// indices count up from 1 to nbnet.MaxRuleIndex. After the counter
// crosses MaxRuleIndex the indexer logs a one-shot warning and
// returns 0 for further allocations — the firewall backend then
// installs the rule WITHOUT the rule_index stamp, and the resulting
// flow events arrive with empty RuleId. That degrades to the
// pre-ADR-0013 behavior rather than corrupting the lookup map.
type Indexer struct {
	mu       sync.RWMutex
	next     atomic.Uint32 // pre-incremented, so first hand-out is 1
	byPolicy map[string]uint32
	byIndex  map[uint32][]byte
	warnOnce sync.Once
}

// New returns a fresh indexer. Most callers should use Default().
func New() *Indexer {
	return &Indexer{
		byPolicy: make(map[string]uint32),
		byIndex:  make(map[uint32][]byte),
	}
}

// Index returns the rule_index assigned to policyID, allocating a
// fresh one on first call. Returns 0 when policyID is empty or when
// the counter has been exhausted (the firewall backend should then
// install the rule without a rule_index stamp).
func (i *Indexer) Index(policyID []byte) uint32 {
	if len(policyID) == 0 {
		return 0
	}

	key := string(policyID)

	i.mu.RLock()
	if idx, ok := i.byPolicy[key]; ok {
		i.mu.RUnlock()
		return idx
	}
	i.mu.RUnlock()

	i.mu.Lock()
	defer i.mu.Unlock()

	// Re-check under the write lock — another goroutine may have
	// allocated the same policy while we were upgrading the lock.
	if idx, ok := i.byPolicy[key]; ok {
		return idx
	}

	idx := i.next.Add(1)
	if idx > nbnet.MaxRuleIndex {
		i.warnOnce.Do(func() {
			log.Warnf(
				"policymark: rule_index space exhausted (>%d unique policies); flow events for newly installed rules will arrive without RuleId until agent restart",
				nbnet.MaxRuleIndex,
			)
		})
		return 0
	}

	i.byPolicy[key] = idx
	i.byIndex[idx] = append([]byte(nil), policyID...)
	return idx
}

// LookupPolicyID resolves a rule_index back to the PolicyID the
// firewall backend stamped onto the conntrack mark. Implements the
// PolicyResolver interface expected by the netflow conntrack
// collector. Returns ok=false when the index is 0 or unknown — in
// that case the collector emits an empty RuleId, matching pre-ADR
// behavior.
func (i *Indexer) LookupPolicyID(ruleIndex uint32) ([]byte, bool) {
	if ruleIndex == 0 {
		return nil, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	pid, ok := i.byIndex[ruleIndex]
	if !ok {
		return nil, false
	}
	// Defensive copy so the caller can't mutate the cached slice.
	out := make([]byte, len(pid))
	copy(out, pid)
	return out, true
}

// Snapshot returns a read-only view of the current index → PolicyID
// mapping. Intended for diagnostics / tests.
func (i *Indexer) Snapshot() map[uint32]string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make(map[uint32]string, len(i.byIndex))
	for idx, pid := range i.byIndex {
		out[idx] = hex.EncodeToString(pid)
	}
	return out
}

var defaultIndexer = New()

// Default returns the process-wide singleton. Both writers
// (firewall backends) and readers (conntrack collector) must talk to
// the same instance for lookups to resolve.
func Default() *Indexer {
	return defaultIndexer
}
