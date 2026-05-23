// Zone handler tests — Phase 1 review (medium): the OpenAPI ttl
// minimum:1 contract is enforced at the wire boundary by
// validateRecordTTLAtAPI. The manager layer keeps the historical
// default-to-300-when-zero behavior for direct internal callers.
// License posture per ADR-0022 D8: AGPL clean-room.
package dns

import (
	"errors"
	"testing"

	"github.com/openzro/openzro/management/server/status"
)

func TestValidateRecordTTLAtAPI(t *testing.T) {
	intp := func(v int) *int { return &v }
	cases := []struct {
		name    string
		ttl     *int
		wantErr bool
	}{
		{"omitted", nil, false},
		{"explicit zero", intp(0), true},
		{"explicit negative", intp(-1), true},
		{"minimum", intp(1), false},
		{"default", intp(300), false},
		{"large", intp(86400), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRecordTTLAtAPI(tc.ttl)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ttl=%v: expected InvalidArgument, got nil", tc.ttl)
				}
				var se *status.Error
				if !errors.As(err, &se) || se.Type() != status.InvalidArgument {
					t.Fatalf("ttl=%v: expected status.InvalidArgument, got %#v", tc.ttl, err)
				}
			} else if err != nil {
				t.Fatalf("ttl=%v: expected nil error, got %v", tc.ttl, err)
			}
		})
	}
}
