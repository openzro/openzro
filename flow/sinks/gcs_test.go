package sinks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/flow/store"
)

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, s)
	require.NoError(t, err)
	return parsed
}

// TestGCS_RequiresBucket locks the boot-time refusal — without a
// bucket the sink would silently accept events and lose them.
func TestGCS_RequiresBucket(t *testing.T) {
	_, err := NewGCS(context.Background(), GCSConfig{Endpoint: "http://localhost:1"})
	require.Error(t, err)
}

// TestGCS_CustomEndpoint_ConstructsWithGuardedClient is the SDK
// option-combo guard. Wiring the SSRF dialer means the custom-endpoint
// branch now passes option.WithHTTPClient alongside WithEndpoint +
// WithoutAuthentication. Some google.golang.org/api versions reject
// WithHTTPClient when combined with other transport options; this
// asserts our pinned SDK accepts the combo and storage.NewClient
// still constructs (it is lazy — no network at construction). Without
// a Bucket NewGCS errors before reaching storage.NewClient, so this
// path was previously untested.
func TestGCS_CustomEndpoint_ConstructsWithGuardedClient(t *testing.T) {
	g, err := NewGCS(context.Background(), GCSConfig{
		Bucket:   "b",
		Endpoint: "http://127.0.0.1:4443",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close() })
}

// TestGCS_ObjectKey locks the partitioned key shape so the operator
// can rely on it for downstream tools (BigQuery external tables,
// Athena, DuckDB) that key off these prefixes.
func TestGCS_ObjectKey(t *testing.T) {
	g := &GCS{cfg: GCSConfig{Prefix: "audit"}}
	ev := &store.Event{
		EventID:   []byte{0xab, 0xcd, 0xef, 0x01},
		AccountID: "acct-1",
	}
	ev.ReceivedAt = mustParseTime(t, "2026-04-26T13:14:15.000000016Z")

	got := g.objectKey(ev)
	// year=2026/month=04/day=26/account=acct-1/<unix-nano>-abcdef01.ndjson.gz
	assert.Contains(t, got, "audit/year=2026/month=04/day=26/account=acct-1/")
	assert.Contains(t, got, "abcdef01.ndjson.gz")
}

// TestGCS_ObjectKey_NoTrailingSlash exercises the prefix-normaliser:
// the operator may write the prefix with or without a trailing slash
// and the resulting object name is identical.
func TestGCS_ObjectKey_NoTrailingSlash(t *testing.T) {
	withSlash := &GCS{cfg: GCSConfig{Prefix: "audit/"}}
	withoutSlash := &GCS{cfg: GCSConfig{Prefix: "audit"}}
	ev := &store.Event{
		EventID:   []byte{0xab, 0xcd, 0xef, 0x01},
		AccountID: "acct-1",
	}
	ev.ReceivedAt = mustParseTime(t, "2026-04-26T13:14:15.000000016Z")
	assert.Equal(t, withSlash.objectKey(ev), withoutSlash.objectKey(ev))
}
