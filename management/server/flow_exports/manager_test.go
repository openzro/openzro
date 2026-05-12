package flow_exports

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestArchiveFormatFor_RowOverride pins the contract: when a row
// explicitly sets Format, the row wins regardless of what the
// environment says. Operators who want a specific format on a
// specific export can override the operator-level default.
func TestArchiveFormatFor_RowOverride(t *testing.T) {
	t.Setenv(envArchiveFormat, "parquet")
	assert.Equal(t, "ndjson", archiveFormatFor("ndjson"),
		"row-level Format must override the env default")
}

// TestArchiveFormatFor_InheritsEnv is the regression test for the
// Cora deployment finding: a dashboard-created GCS export was
// emitting *.ndjson.gz despite OPENZRO_FLOW_ARCHIVE_FORMAT=parquet
// being set on the management pod. The flow_exports manager's
// buildSink path used to construct sinks.GCSConfig / S3Config
// without a Format field, so the sink dropped to its ndjson
// fallback. The fix: inherit the env default when the row leaves
// Format empty.
func TestArchiveFormatFor_InheritsEnv(t *testing.T) {
	t.Setenv(envArchiveFormat, "parquet")
	assert.Equal(t, "parquet", archiveFormatFor(""),
		"empty row Format must fall through to OPENZRO_FLOW_ARCHIVE_FORMAT")
}

// TestArchiveFormatFor_EmptyEnvReturnsEmpty covers the pre-Parquet
// back-compat default: when neither row nor env specify a format,
// the sink itself decides (historically ndjson).
func TestArchiveFormatFor_EmptyEnvReturnsEmpty(t *testing.T) {
	t.Setenv(envArchiveFormat, "")
	assert.Empty(t, archiveFormatFor(""),
		"no override anywhere → caller (sink constructor) decides")
}
