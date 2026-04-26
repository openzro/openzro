package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
)

// TestEvaluateAdmission_NoChecksConfigured verifies the gate is a
// no-op when nothing is configured. This is the default state for
// every account that hasn't opted into admission enforcement, and
// the contract guarantees zero overhead for that case.
func TestEvaluateAdmission_NoChecksConfigured(t *testing.T) {
	peer := &nbpeer.Peer{ID: "p1", Meta: nbpeer.PeerSystemMeta{Hostname: "test"}}

	require.Nil(t, EvaluateAdmission(context.Background(), peer, nil, nil))
	require.Nil(t, EvaluateAdmission(context.Background(), peer, []string{}, nil))
	require.Nil(t, EvaluateAdmission(context.Background(), peer, []string{"id-1"}, map[string]*posture.Checks{}))
}

// TestEvaluateAdmission_PassWhenCompliant verifies the green-path:
// every listed check returns true → no denial.
func TestEvaluateAdmission_PassWhenCompliant(t *testing.T) {
	peer := &nbpeer.Peer{
		ID:   "p1",
		Meta: nbpeer.PeerSystemMeta{Hostname: "host", GoOS: "linux"},
	}
	check := &posture.Checks{
		ID:   "c1",
		Name: "min-os",
		Checks: posture.ChecksDefinition{
			OSVersionCheck: &posture.OSVersionCheck{
				Linux: &posture.MinKernelVersionCheck{MinKernelVersion: "0.0.1"},
			},
		},
	}
	peer.Meta.KernelVersion = "5.15.0"

	got := EvaluateAdmission(context.Background(), peer, []string{"c1"}, map[string]*posture.Checks{
		"c1": check,
	})
	require.Nil(t, got, "compliant peer should not produce a denial")
}

// TestEvaluateAdmission_DenyOnFailure verifies the red-path: a
// failing check produces a structured denial that names the failed
// check, the type, and the reason — exactly the fields the audit
// trail needs.
func TestEvaluateAdmission_DenyOnFailure(t *testing.T) {
	peer := &nbpeer.Peer{
		ID:   "p1",
		Meta: nbpeer.PeerSystemMeta{Hostname: "host", GoOS: "linux", KernelVersion: "1.0.0"},
	}
	check := &posture.Checks{
		ID:   "c1",
		Name: "kernel-floor",
		Checks: posture.ChecksDefinition{
			OSVersionCheck: &posture.OSVersionCheck{
				Linux: &posture.MinKernelVersionCheck{MinKernelVersion: "999.0.0"},
			},
		},
	}

	got := EvaluateAdmission(context.Background(), peer, []string{"c1"}, map[string]*posture.Checks{
		"c1": check,
	})
	require.NotNil(t, got)
	assert.Equal(t, "c1", got.PostureCheckID)
	assert.Equal(t, "kernel-floor", got.PostureCheckName)
	assert.Equal(t, posture.OSVersionCheckName, got.CheckType)
	assert.NotEmpty(t, got.Reason, "reason should describe why the check failed")
}

// TestEvaluateAdmission_FirstFailureWins verifies the gate short-
// circuits on the first failing check. Operators will compose
// multi-check admission policies (e.g. "OS version AND MDM
// compliance") and the audit event must point at the first thing
// that broke, not aggregate noise.
func TestEvaluateAdmission_FirstFailureWins(t *testing.T) {
	peer := &nbpeer.Peer{
		ID:   "p1",
		Meta: nbpeer.PeerSystemMeta{Hostname: "host", GoOS: "linux", KernelVersion: "1.0.0"},
	}
	failing := &posture.Checks{
		ID:   "c1",
		Name: "first-fail",
		Checks: posture.ChecksDefinition{
			OSVersionCheck: &posture.OSVersionCheck{
				Linux: &posture.MinKernelVersionCheck{MinKernelVersion: "999.0.0"},
			},
		},
	}
	alsoFailing := &posture.Checks{
		ID:   "c2",
		Name: "second-fail",
		Checks: posture.ChecksDefinition{
			OSVersionCheck: &posture.OSVersionCheck{
				Linux: &posture.MinKernelVersionCheck{MinKernelVersion: "888.0.0"},
			},
		},
	}

	got := EvaluateAdmission(context.Background(), peer, []string{"c1", "c2"}, map[string]*posture.Checks{
		"c1": failing,
		"c2": alsoFailing,
	})
	require.NotNil(t, got)
	assert.Equal(t, "first-fail", got.PostureCheckName, "first listed failure should win")
}

// TestEvaluateAdmission_UnknownIDsAreIgnored verifies an admission
// list referencing a deleted/missing check ID does not blow up the
// evaluator. This matters because the API admits the IDs without a
// foreign-key constraint — a posture check deletion shouldn't
// suddenly lock everyone out.
func TestEvaluateAdmission_UnknownIDsAreIgnored(t *testing.T) {
	peer := &nbpeer.Peer{ID: "p1", Meta: nbpeer.PeerSystemMeta{Hostname: "host"}}

	got := EvaluateAdmission(context.Background(), peer, []string{"missing"}, map[string]*posture.Checks{})
	require.Nil(t, got)
}
