package iptables

import (
	"fmt"

	"github.com/openzro/openzro/client/firewall/manager"
	nbnet "github.com/openzro/openzro/util/net"
)

// connmarkSpecsFor builds the iptables rule spec that stamps the
// agent-local rule_index onto bits 17-31 of the ct mark for every
// packet that matches the filter selector. The conntrack collector
// resolves the index back to the originating PolicyID and emits it
// on each FlowEvent (ADR-0013). The nftables backend does the same
// thing inline via `expr.Bitwise` in acl_linux.go.
//
// Returns nil — meaning "do not install a CONNMARK rule" — when:
//
//   - ruleIndex == 0 (no PolicyID was registered for this rule;
//     could be a rule the agent installs unprompted, or the indexer
//     hit its 32 767 ceiling); the flow events for this rule then
//     carry an empty RuleId, matching pre-ADR-0013 behaviour.
//   - action != ActionAccept; Drop packets never reach the conntrack
//     new-event path so the stamp would be wasted CPU, and we want
//     to keep the chain minimal.
//
// The /mask form of CONNMARK --set-mark (value/mask) restricts the
// write to bits 17-31, so the legacy mark space (DataPlaneMarkIn /
// DataPlaneMarkOut on bits 0-16) survives untouched. Requires
// iptables ≥ 1.4.13, available since 2012 — covers every distro we
// support.
func connmarkSpecsFor(matchSpec []string, ruleIndex uint32, action manager.Action) []string {
	if ruleIndex == 0 || action != manager.ActionAccept {
		return nil
	}
	markBits := ruleIndex << nbnet.RuleIndexShift
	value := fmt.Sprintf("%#x/%#x", markBits, nbnet.RuleIndexMask)
	spec := make([]string, 0, len(matchSpec)+4)
	spec = append(spec, matchSpec...)
	spec = append(spec, "-j", "CONNMARK", "--set-mark", value)
	return spec
}
