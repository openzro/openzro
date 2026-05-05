//go:build !linux || android

package conntrack

import nftypes "github.com/openzro/openzro/client/internal/netflow/types"

// PolicyResolver mirrors the Linux-only interface so the call site in
// netflow/manager.go type-checks on every platform. The stub never
// invokes it — kernel conntrack only exists on Linux.
type PolicyResolver interface {
	LookupPolicyID(ruleIndex uint32) ([]byte, bool)
}

func New(_ nftypes.FlowLogger, _ nftypes.IFaceMapper, _ PolicyResolver) nftypes.ConnTracker {
	return nil
}
