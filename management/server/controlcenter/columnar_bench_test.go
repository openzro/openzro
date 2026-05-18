package controlcenter

import (
	"context"
	"fmt"
	"net"
	"testing"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/types"
)

// largeAcct is a synthetic tenant sized to exercise the R5 indices:
// without the source-group index buildUserFocus is
// O(userPeers × allPolicies × rules); with it, O(peerGroups ×
// matchingRules) and the per-policy state memo + gatherCache remove
// the per-rule / per-shared-group recompute.
func largeAcct(peers, groups, policies, userPeers int) *types.Account {
	a := &types.Account{
		Peers:  map[string]*nbpeer.Peer{},
		Users:  map[string]*types.User{"u1": {Id: "u1", Name: "Owner", Email: "o@x.io"}},
		Groups: map[string]*types.Group{},
		PostureChecks: []*posture.Checks{{
			ID: "pc1", Name: "min-version",
			Checks: posture.ChecksDefinition{
				NBVersionCheck: &posture.NBVersionCheck{MinVersion: "0.20.0"},
			},
		}},
	}
	for g := 0; g < groups; g++ {
		a.Groups[fmt.Sprintf("g%d", g)] = &types.Group{
			ID: fmt.Sprintf("g%d", g), Name: fmt.Sprintf("group-%d", g),
		}
	}
	for p := 0; p < peers; p++ {
		id := fmt.Sprintf("p%d", p)
		owner := ""
		if p < userPeers {
			owner = "u1"
		}
		a.Peers[id] = &nbpeer.Peer{
			ID: id, Name: id, UserID: owner,
			IP:   net.IPv4(100, 64, byte(p/256), byte(p%256)),
			Meta: nbpeer.PeerSystemMeta{GoOS: "linux", WtVersion: "0.30.0"},
		}
		// each peer in 3 groups (round-robin) — many peers share the
		// same group-set, which is exactly what gatherCache collapses.
		for k := 0; k < 3; k++ {
			gid := fmt.Sprintf("g%d", (p+k)%groups)
			g := a.Groups[gid]
			g.Peers = append(g.Peers, id)
		}
	}
	for i := 0; i < policies; i++ {
		src := fmt.Sprintf("g%d", i%groups)
		dst := fmt.Sprintf("g%d", (i+7)%groups)
		pol := &types.Policy{
			ID: fmt.Sprintf("pol%d", i), Name: fmt.Sprintf("policy-%d", i),
			Enabled: true,
			Rules: []*types.PolicyRule{
				{
					ID: fmt.Sprintf("pol%d-r0", i), Enabled: true,
					Sources: []string{src}, Destinations: []string{dst},
					Protocol: types.PolicyRuleProtocolTCP, Ports: []string{"443"},
				},
				{
					ID: fmt.Sprintf("pol%d-r1", i), Enabled: true,
					Sources: []string{src}, Destinations: []string{dst},
					Protocol: types.PolicyRuleProtocolTCP, Ports: []string{"22"},
				},
			},
		}
		if i%4 == 0 {
			pol.SourcePostureChecks = []string{"pc1"}
		}
		a.Policies = append(a.Policies, pol)
	}
	return a
}

func BenchmarkBuildUserFocus_Large(b *testing.B) {
	a := largeAcct(500, 80, 150, 40)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildGraph(ctx, a, Focus{Type: FocusUser, ID: "u1"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildPeerFocus_Large(b *testing.B) {
	a := largeAcct(500, 80, 150, 40)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildGraph(ctx, a, Focus{Type: FocusPeer, ID: "p1"}); err != nil {
			b.Fatal(err)
		}
	}
}
