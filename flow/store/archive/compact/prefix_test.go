package compact

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// The guard exists for Delete, so its failures are tested as data loss
// rather than as string handling.
func TestCheckUnderPrefix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		key    string
		prefix string
		ok     bool
	}{
		{"exact prefix", "flows", "flows", true},
		{"under prefix", "flows/year=2026/x.parquet", "flows", true},
		{"prefix with trailing slash", "flows/x.parquet", "flows/", true},
		{"empty prefix allows any key", "anything/x.parquet", "", true},

		// The one that matters: a boundary that is not a path boundary.
		// Object stores have no directories, so "flows" would otherwise
		// match "flowsomething" and a delete would walk into a
		// neighboring dataset.
		{"sibling sharing a name prefix", "flowsomething/x.parquet", "flows", false},
		{"different tree", "other/x.parquet", "flows", false},
		{"parent of the prefix", "x.parquet", "flows", false},

		// Refused rather than resolved. These are legal object keys that
		// no traversal happens on, but they read as an escape and any
		// layer that ever normalizes them becomes one.
		{"dot dot segment", "flows/../other/x.parquet", "flows", false},
		{"dot segment", "flows/./x.parquet", "flows", false},
		{"double slash", "flows//x.parquet", "flows", false},
		{"leading slash", "/flows/x.parquet", "flows", false},
		{"empty key", "", "flows", false},
		{"empty key, empty prefix", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkUnderPrefix(tc.key, tc.prefix)
			if tc.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var outside *ErrOutsidePrefix
			require.True(t, errors.As(err, &outside),
				"the refusal must be typed so a caller can tell it from an outage")
			require.Equal(t, tc.key, outside.Key)
		})
	}
}
