//go:build !archive_duckdb

package archive

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNew_StubReturnsErrUnavailable locks in the stub-mode contract:
// a binary built without `archive_duckdb` returns ErrUnavailable so
// the federated wrapper can detect "no archive" and fall through to
// hot-only behavior without surfacing a panic to the operator.
func TestNew_StubReturnsErrUnavailable(t *testing.T) {
	_, err := New(Config{Provider: "s3", Bucket: "ignored"})
	assert.True(t, errors.Is(err, ErrUnavailable),
		"expected ErrUnavailable, got %v", err)
}
