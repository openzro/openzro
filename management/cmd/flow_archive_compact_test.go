//go:build archive_duckdb

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	flowArchive "github.com/openzro/openzro/flow/store/archive"
	archiveCompact "github.com/openzro/openzro/flow/store/archive/compact"
)

func TestParseCompactRange(t *testing.T) {
	from, to, err := parseCompactRange("2026-07-01", "2026-07-03")
	require.NoError(t, err)
	require.Equal(t, "2026-07-01", formatCompactDay(from))
	require.Equal(t, "2026-07-03", formatCompactDay(to))

	_, _, err = parseCompactRange("2026-07-03", "2026-07-01")
	require.ErrorContains(t, err, "--to must be on or after --from")

	_, _, err = parseCompactRange("07/01/2026", "2026-07-03")
	require.ErrorContains(t, err, "--from")
}

func TestCompactDaysSkipsTodayAndYesterday(t *testing.T) {
	from := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	days, skipped := compactDays(from, to, now, true)
	require.Equal(t, []string{"2026-07-27", "2026-07-28"}, compactDayStrings(days))
	require.Len(t, skipped, 2)
	require.Equal(t, "2026-07-29", skipped[0].Day)
	require.Equal(t, "2026-07-30", skipped[1].Day)
	for _, entry := range skipped {
		require.True(t, entry.Skipped)
		require.True(t, entry.DryRun)
		require.Contains(t, entry.SkippedBecause, "today and yesterday")
	}
}

func TestArchiveRootURL(t *testing.T) {
	require.Equal(t, "s3://bucket/flows", archiveRootURL(flowArchive.Config{
		Provider: "s3",
		Bucket:   "bucket",
		Prefix:   "/flows/",
	}))
	require.Equal(t, "gcs://bucket", archiveRootURL(flowArchive.Config{
		Provider: "gcs",
		Bucket:   "bucket",
	}))
}

func TestFlowArchiveCompactEntrySeparatesPlannedAndWrittenBytes(t *testing.T) {
	day := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)

	dryRun := flowArchiveCompactEntry(archiveCompact.Result{
		Day:          day,
		DryRun:       true,
		BytesWritten: 1234,
	})
	require.Zero(t, dryRun.BytesWritten, "dry-run writes nothing to the bucket")
	require.Equal(t, int64(1234), dryRun.BytesPlanned)

	apply := flowArchiveCompactEntry(archiveCompact.Result{
		Day:          day,
		DryRun:       false,
		BytesWritten: 1234,
	})
	require.Equal(t, int64(1234), apply.BytesWritten)
	require.Zero(t, apply.BytesPlanned)
}

func TestFlowArchiveCompactDryRunWritesEveryResultAfterError(t *testing.T) {
	results := make(chan flowArchiveCompactManifestEntry, 2)
	results <- flowArchiveCompactManifestEntry{Day: "2026-07-01", DryRun: true, Error: "bad parquet"}
	results <- flowArchiveCompactManifestEntry{Day: "2026-07-02", DryRun: true, Skipped: true, SkippedBecause: "no source objects"}
	close(results)

	var manifest bytes.Buffer
	canceled := false
	err := writeFlowArchiveCompactResults(&cobra.Command{}, json.NewEncoder(&manifest), results, true, func() {
		canceled = true
	})
	require.ErrorContains(t, err, "bad parquet")
	require.False(t, canceled, "dry-run is a survey; one bad day must not hide the rest of the range")

	var entries []flowArchiveCompactManifestEntry
	for _, line := range bytes.Split(bytes.TrimSpace(manifest.Bytes()), []byte("\n")) {
		var entry flowArchiveCompactManifestEntry
		require.NoError(t, json.Unmarshal(line, &entry))
		entries = append(entries, entry)
	}
	require.Len(t, entries, 2, "every day result must remain visible in the manifest")
	require.Equal(t, "2026-07-01", entries[0].Day)
	require.Equal(t, "bad parquet", entries[0].Error)
	require.Equal(t, "2026-07-02", entries[1].Day)
	require.True(t, entries[1].Skipped)
}

func TestFlowArchiveCompactDeleteRunCancelsOnFirstError(t *testing.T) {
	results := make(chan flowArchiveCompactManifestEntry, 1)
	results <- flowArchiveCompactManifestEntry{Day: "2026-07-01", Error: "delete failed"}
	close(results)

	canceled := false
	err := writeFlowArchiveCompactResults(&cobra.Command{}, json.NewEncoder(&bytes.Buffer{}), results, false, func() {
		canceled = true
	})
	require.ErrorContains(t, err, "delete failed")
	require.True(t, canceled, "a destructive run must stop on the first failed day")
}

func TestFlowArchiveCompactWorkerStopsDestructiveRunAfterDayError(t *testing.T) {
	jobs := make(chan time.Time, 2)
	jobs <- time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	jobs <- time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	close(jobs)
	results := make(chan flowArchiveCompactManifestEntry, 2)
	st := &listErrorObjectStore{err: errors.New("bucket refused list")}

	done := make(chan struct{})
	go func() {
		defer close(done)
		flowArchiveCompactWorker(context.Background(), testArchiveConfig(), st, false, jobs, results)
	}()

	requireWorkerDone(t, done)
	require.Len(t, st.prefixes, 1, "after a destructive day error the worker must not consume an already queued next day")
	require.Len(t, jobs, 1, "the next day must remain untouched for the canceled run to stop cleanly")

	got := drainCompactResults(results)
	require.Len(t, got, 1)
	require.Equal(t, "2026-07-01", got[0].Day)
	require.ErrorContains(t, errors.New(got[0].Error), "bucket refused list")
}

func TestFlowArchiveCompactWorkerContinuesDryRunAfterDayError(t *testing.T) {
	jobs := make(chan time.Time, 2)
	jobs <- time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	jobs <- time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	close(jobs)
	results := make(chan flowArchiveCompactManifestEntry, 2)
	st := &listErrorObjectStore{err: errors.New("bucket refused list")}

	done := make(chan struct{})
	go func() {
		defer close(done)
		flowArchiveCompactWorker(context.Background(), testArchiveConfig(), st, true, jobs, results)
	}()

	requireWorkerDone(t, done)
	require.Len(t, st.prefixes, 2, "dry-run is a survey and should report every queued day")
	require.Len(t, jobs, 0)

	got := drainCompactResults(results)
	require.Len(t, got, 2)
	require.Equal(t, "2026-07-01", got[0].Day)
	require.Equal(t, "2026-07-02", got[1].Day)
	require.True(t, got[0].DryRun)
	require.True(t, got[1].DryRun)
}

func TestFlowArchiveCompactDoesNotCreateManifestWhenStoreConfigFails(t *testing.T) {
	manifest := t.TempDir() + "/manifest.jsonl"
	cmd := newFlowArchiveCompactCommand()
	cmd.SetArgs([]string{
		"--from", "2026-07-01",
		"--to", "2026-07-01",
		"--manifest", manifest,
		"--provider", "unsupported",
		"--bucket", "bucket",
	})

	err := cmd.Execute()
	require.ErrorContains(t, err, "unsupported provider")

	_, statErr := os.Stat(manifest)
	require.True(t, errors.Is(statErr, os.ErrNotExist), "a config failure must not leave an empty manifest that blocks retry")
}

func TestFlowArchiveCompactDoesNotCreateManifestWhenDuckDBConfigFails(t *testing.T) {
	t.Setenv("OPENZRO_FLOW_ARCHIVE_GCS_HMAC_KEY_ID", "")
	t.Setenv("OPENZRO_FLOW_ARCHIVE_GCS_HMAC_SECRET", "")
	manifest := t.TempDir() + "/manifest.jsonl"
	cmd := newFlowArchiveCompactCommand()
	cmd.SetArgs([]string{
		"--from", "2026-07-01",
		"--to", "2026-07-01",
		"--manifest", manifest,
		"--provider", "gcs",
		"--bucket", "bucket",
	})

	err := cmd.Execute()
	require.ErrorContains(t, err, "GCS archive reads need HMAC credentials")

	_, statErr := os.Stat(manifest)
	require.True(t, errors.Is(statErr, os.ErrNotExist), "a DuckDB preflight failure must leave the manifest path reusable")
}

func compactDayStrings(days []time.Time) []string {
	out := make([]string, 0, len(days))
	for _, d := range days {
		out = append(out, formatCompactDay(d))
	}
	return out
}

func testArchiveConfig() flowArchive.Config {
	return flowArchive.Config{
		Provider:        "s3",
		Bucket:          "bucket",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
	}
}

type listErrorObjectStore struct {
	err      error
	prefixes []string
}

func (s *listErrorObjectStore) List(_ context.Context, prefix string) ([]string, error) {
	s.prefixes = append(s.prefixes, prefix)
	return nil, s.err
}

func (s *listErrorObjectStore) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("unexpected read")
}

func (s *listErrorObjectStore) Write(context.Context, string, []byte) error {
	return errors.New("unexpected write")
}

func (s *listErrorObjectStore) Delete(context.Context, string) error {
	return errors.New("unexpected delete")
}

func requireWorkerDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("flow archive compact worker did not return")
	}
}

func drainCompactResults(results <-chan flowArchiveCompactManifestEntry) []flowArchiveCompactManifestEntry {
	var out []flowArchiveCompactManifestEntry
	for {
		select {
		case entry := <-results:
			out = append(out, entry)
		default:
			return out
		}
	}
}
