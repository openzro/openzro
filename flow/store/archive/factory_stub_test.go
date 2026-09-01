//go:build !archive_duckdb

package archive

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFromEnvSkipsNonParquetArchiveFormats(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format string
	}{
		{name: "empty format defaults to ndjson on the sink", format: ""},
		{name: "explicit ndjson", format: "ndjson"},
		{name: "unknown format also resolves to ndjson on the sink", format: "csv"},
		{name: "uppercase parquet still resolves to ndjson on the sink", format: "PARQUET"},
		{name: "spaced parquet still resolves to ndjson on the sink", format: " parquet "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearArchiveEnv(t)
			t.Setenv(envS3Bucket, "flow-archive")
			t.Setenv(envFormat, tc.format)

			st, err := NewFromEnv()
			require.NoError(t, err)
			assert.Nil(t, st)
		})
	}
}

func TestNewFromEnvParquetRequiresDuckDBBuild(t *testing.T) {
	clearArchiveEnv(t)
	t.Setenv(envS3Bucket, "flow-archive")
	t.Setenv(envFormat, "parquet")

	st, err := NewFromEnv()
	assert.Nil(t, st)
	assert.True(t, errors.Is(err, ErrUnavailable), "expected ErrUnavailable, got %v", err)
}

func clearArchiveEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		envS3Bucket,
		envS3Region,
		envS3Endpoint,
		envS3AccessKey,
		envS3SecretKey,
		envS3Prefix,
		envGCSBucket,
		envGCSPrefix,
		envGCSCredentialsFile,
		envGCSCredentialsJSON,
		envGCSProjectID,
		envGCSEndpoint,
		envFormat,
		envQueryTimeout,
		envMemoryLimit,
		envThreads,
		envMaxConcurrentQueries,
	} {
		t.Setenv(key, "")
	}
}
