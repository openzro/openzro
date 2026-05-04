package policymark

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	nbnet "github.com/openzro/openzro/util/net"
)

func TestIndexer_AllocatesAndLookups(t *testing.T) {
	idx := New()

	policyA := []byte("policy-A")
	policyB := []byte("policy-B")

	a1 := idx.Index(policyA)
	require.NotZero(t, a1, "first allocation must hand out non-zero index")
	require.LessOrEqual(t, a1, nbnet.MaxRuleIndex)

	a2 := idx.Index(policyA)
	require.Equal(t, a1, a2, "same policy must map to the same rule_index")

	b1 := idx.Index(policyB)
	require.NotEqual(t, a1, b1, "distinct policies must get distinct indices")

	pidA, ok := idx.LookupPolicyID(a1)
	require.True(t, ok)
	require.Equal(t, policyA, pidA, "lookup must return the original PolicyID")

	pidB, ok := idx.LookupPolicyID(b1)
	require.True(t, ok)
	require.Equal(t, policyB, pidB)
}

func TestIndexer_EmptyPolicyID(t *testing.T) {
	idx := New()
	require.Zero(t, idx.Index(nil), "empty PolicyID must return 0")
	require.Zero(t, idx.Index([]byte{}), "empty PolicyID must return 0")
}

func TestIndexer_LookupZeroIsAlwaysMiss(t *testing.T) {
	idx := New()
	idx.Index([]byte("policy"))
	_, ok := idx.LookupPolicyID(0)
	require.False(t, ok, "rule_index 0 is the sentinel for 'no rule' and must miss")
}

func TestIndexer_LookupReturnsCopy(t *testing.T) {
	idx := New()
	policy := []byte("policy-mutable")
	allocated := idx.Index(policy)
	pid, ok := idx.LookupPolicyID(allocated)
	require.True(t, ok)

	// Mutating the returned slice must not corrupt the cached entry.
	pid[0] = 'X'
	pid2, ok := idx.LookupPolicyID(allocated)
	require.True(t, ok)
	require.Equal(t, policy, pid2)
}

func TestIndexer_ConcurrentAllocationsAreStable(t *testing.T) {
	idx := New()
	const goroutines = 32
	const policiesEach = 8

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gi int) {
			defer wg.Done()
			for p := 0; p < policiesEach; p++ {
				policy := []byte{byte(gi), byte(p)}
				_ = idx.Index(policy)
				_ = idx.Index(policy) // re-allocation must yield the same index
			}
		}(g)
	}
	wg.Wait()

	// Each (gi, p) pair must end up with exactly one entry.
	snap := idx.Snapshot()
	require.Len(t, snap, goroutines*policiesEach)
}

func TestComposeMarkAndExtract(t *testing.T) {
	idx := New()
	policy := []byte("composition")
	ri := idx.Index(policy)
	require.NotZero(t, ri)

	composed := nbnet.ComposeRuleMark(nbnet.DataPlaneMarkIn, ri)
	require.Equal(t, uint32(nbnet.DataPlaneMarkIn), nbnet.MarkValue(composed),
		"low 17 bits must preserve DataPlaneMarkIn")
	require.Equal(t, ri, nbnet.MarkRuleIndex(composed),
		"high 15 bits must round-trip the rule_index")
	require.True(t, nbnet.IsDataPlaneMark(composed),
		"a stamped data-plane mark must still classify as data-plane")
}

func TestComposeMarkRejectsOutOfRange(t *testing.T) {
	require.Equal(t, uint32(nbnet.DataPlaneMarkIn),
		nbnet.ComposeRuleMark(nbnet.DataPlaneMarkIn, 0),
		"index 0 returns base unchanged")
	require.Equal(t, uint32(nbnet.DataPlaneMarkIn),
		nbnet.ComposeRuleMark(nbnet.DataPlaneMarkIn, nbnet.MaxRuleIndex+1),
		"index above MaxRuleIndex falls back to base unchanged")
}
