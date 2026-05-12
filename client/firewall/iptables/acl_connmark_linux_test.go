package iptables

import (
	"reflect"
	"testing"

	"github.com/openzro/openzro/client/firewall/manager"
	nbnet "github.com/openzro/openzro/util/net"
)

// TestConnmarkSpecsFor_BuildsScopedMaskedRule asserts the iptables
// CONNMARK rule the iptables backend installs alongside every
// ACCEPT-action peer rule, so the netflow conntrack collector can
// resolve the originating PolicyID. The /mask form is what scopes
// the write to bits 17-31 and preserves the legacy mark space on
// bits 0-16 (DataPlaneMarkIn / DataPlaneMarkOut). Regression for
// the gap where ADR-0013 only landed in the nftables backend.
func TestConnmarkSpecsFor_BuildsScopedMaskedRule(t *testing.T) {
	matchSpec := []string{"-s", "10.0.0.1", "-p", "tcp", "--dport", "443"}
	const ruleIndex uint32 = 42

	got := connmarkSpecsFor(matchSpec, ruleIndex, manager.ActionAccept)

	wantSuffix := []string{"-j", "CONNMARK", "--set-mark", "0x540000/0xfffe0000"}
	if got == nil {
		t.Fatalf("got nil — expected a CONNMARK spec for ruleIndex=%d", ruleIndex)
	}
	if !reflect.DeepEqual(got[:len(matchSpec)], matchSpec) {
		t.Errorf("match selector not preserved.\n got: %v\nwant: %v", got[:len(matchSpec)], matchSpec)
	}
	if !reflect.DeepEqual(got[len(matchSpec):], wantSuffix) {
		t.Errorf("CONNMARK suffix wrong.\n got: %v\nwant: %v", got[len(matchSpec):], wantSuffix)
	}

	// Sanity: the markBits computed from ruleIndex must equal what
	// nbnet exports. Catches a future shift-constant change.
	if expected := ruleIndex << nbnet.RuleIndexShift; expected != 0x540000 {
		t.Errorf("markBits mismatch: got %#x, want %#x — RuleIndexShift drifted", expected, 0x540000)
	}
}

// TestConnmarkSpecsFor_SkipsZeroIndex covers the "no PolicyID
// registered" path. The collector sees `ct mark` with bits 17-31
// clear, returns an empty RuleId, and the dashboard renders the
// flow without a policy column entry — matching the pre-ADR-0013
// behaviour.
func TestConnmarkSpecsFor_SkipsZeroIndex(t *testing.T) {
	matchSpec := []string{"-s", "10.0.0.1"}
	if got := connmarkSpecsFor(matchSpec, 0, manager.ActionAccept); got != nil {
		t.Errorf("expected nil for ruleIndex=0, got %v", got)
	}
}

// TestConnmarkSpecsFor_SkipsDrop guards against installing a
// pointless CONNMARK before a DROP verdict. Drop packets do not
// open a conntrack entry, so the stamp would never be read.
func TestConnmarkSpecsFor_SkipsDrop(t *testing.T) {
	matchSpec := []string{"-s", "10.0.0.1"}
	if got := connmarkSpecsFor(matchSpec, 7, manager.ActionDrop); got != nil {
		t.Errorf("expected nil for ActionDrop, got %v", got)
	}
}
