package posture

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	nbpeer "github.com/openzro/openzro/management/server/peer"
)

func TestEndpointSecurityCheck_FailOpen(t *testing.T) {
	peer := nbpeer.Peer{Name: "laptop", Meta: nbpeer.PeerSystemMeta{Hostname: "laptop"}}
	lookupErr := errors.New("vendor unreachable")

	tests := []struct {
		name      string
		failOpen  bool
		compliant bool
		reason    string
		err       error
		want      bool
	}{
		{name: "compliant device passes", compliant: true, want: true},
		{name: "non-compliant device fails", reason: "complianceState=noncompliant", want: false},
		{name: "lookup error fails closed", err: lookupErr, want: false},
		{name: "lookup error passes with fail-open", failOpen: true, err: lookupErr, want: true},
		{
			// A device the vendor does not know is a successful lookup
			// reporting non-compliance, not a lookup failure: fail-open
			// must not let unmanaged devices through.
			name:     "unknown device fails even with fail-open",
			failOpen: true,
			reason:   "device not found",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := func(context.Context, uint64, nbpeer.Peer) (bool, string, error) {
				return tt.compliant, tt.reason, tt.err
			}
			ctx := WithMDMResolver(context.Background(), resolver)
			check := &EndpointSecurityCheck{ProviderID: 1, FailOpen: tt.failOpen}

			got, _ := check.Check(ctx, peer)
			assert.Equal(t, tt.want, got)
		})
	}
}
