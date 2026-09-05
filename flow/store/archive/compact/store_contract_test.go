package compact

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// Both real stores must refuse a key outside their prefix before the SDK
// is reached, on every operation and not only Delete: a Write to the
// wrong place is how a later Delete finds the wrong thing to remove.
//
// These construct the stores without contacting anything. If the guard
// were missing, the call would reach the SDK and fail differently -- so
// the assertion is on the type, not merely on there being an error.
func TestStoresRefuseKeysOutsideTheirPrefix(t *testing.T) {
	ctx := context.Background()

	gcs := &GCSStore{prefix: "flows"}
	s3s := &S3Store{prefix: "flows", bucket: "b"}

	for name, st := range map[string]ObjectStore{"gcs": gcs, "s3": s3s} {
		t.Run(name, func(t *testing.T) {
			for _, key := range []string{
				"other/x.parquet",
				"flowsomething/x.parquet",
				"flows/../other/x.parquet",
				"",
			} {
				t.Run("delete "+key, func(t *testing.T) {
					err := st.Delete(ctx, key)
					var outside *ErrOutsidePrefix
					require.True(t, errors.As(err, &outside),
						"delete %q must be refused before the SDK, got %v", key, err)
				})
				t.Run("write "+key, func(t *testing.T) {
					err := st.Write(ctx, key, []byte("x"))
					var outside *ErrOutsidePrefix
					require.True(t, errors.As(err, &outside),
						"write %q must be refused before the SDK, got %v", key, err)
				})
				t.Run("read "+key, func(t *testing.T) {
					_, err := st.Read(ctx, key)
					var outside *ErrOutsidePrefix
					require.True(t, errors.As(err, &outside))
				})
				t.Run("list "+key, func(t *testing.T) {
					_, err := st.List(ctx, key)
					var outside *ErrOutsidePrefix
					require.True(t, errors.As(err, &outside))
				})
			}
		})
	}
}

// The fsStore used by the compaction tests and the two real stores must
// satisfy the same interface, so a change to it cannot leave one behind.
var (
	_ ObjectStore = (*GCSStore)(nil)
	_ ObjectStore = (*S3Store)(nil)
)
