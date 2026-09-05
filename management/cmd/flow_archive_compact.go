//go:build archive_duckdb

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	flowArchive "github.com/openzro/openzro/flow/store/archive"
	archiveCompact "github.com/openzro/openzro/flow/store/archive/compact"
)

type flowArchiveCompactOptions struct {
	from            string
	to              string
	manifest        string
	deleteOriginals bool
	concurrency     int

	provider           string
	bucket             string
	prefix             string
	endpoint           string
	region             string
	gcsCredentialsFile string
}

func init() {
	rootCmd.AddCommand(newFlowArchiveCommand())
}

func newFlowArchiveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "flow-archive",
		Short:        "Operate on archived flow traffic",
		SilenceUsage: true,
	}
	cmd.AddCommand(newFlowArchiveCompactCommand())
	return cmd
}

func newFlowArchiveCompactCommand() *cobra.Command {
	opts := &flowArchiveCompactOptions{concurrency: 1}
	cmd := &cobra.Command{
		Use:   "compact --from YYYY-MM-DD --to YYYY-MM-DD --manifest FILE [--delete-originals]",
		Short: "Compact and repartition archived flow Parquet objects",
		Long: "Compact and repartition archived flow Parquet objects.\n\n" +
			"Archive bucket settings default to OPENZRO_FLOW_ARCHIVE_* environment variables. " +
			"Credentials stay in that env/file contract rather than command-line flags, so they do not land in shell history.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFlowArchiveCompact(cmd, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.from, "from", "", "first UTC day to process, inclusive (YYYY-MM-DD)")
	flags.StringVar(&opts.to, "to", "", "last UTC day to process, inclusive (YYYY-MM-DD)")
	flags.StringVar(&opts.manifest, "manifest", "", "path for the JSONL manifest; must not already exist")
	flags.BoolVar(&opts.deleteOriginals, "delete-originals", false, "delete originals after replacements are verified; omitted means dry-run")
	flags.IntVar(&opts.concurrency, "concurrency", 1, "number of days to process at once")

	flags.StringVar(&opts.provider, "provider", "", "archive provider override (s3 or gcs); defaults to OPENZRO_FLOW_ARCHIVE_*")
	flags.StringVar(&opts.bucket, "bucket", "", "archive bucket override")
	flags.StringVar(&opts.prefix, "prefix", "", "archive key prefix override")
	flags.StringVar(&opts.endpoint, "endpoint", "", "object store endpoint override")
	flags.StringVar(&opts.region, "region", "", "S3 region override")
	flags.StringVar(&opts.gcsCredentialsFile, "gcs-credentials-file", "", "GCS service-account credentials file for writes/deletes")

	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	_ = cmd.MarkFlagRequired("manifest")
	return cmd
}

func runFlowArchiveCompact(cmd *cobra.Command, opts *flowArchiveCompactOptions) error {
	from, to, err := parseCompactRange(opts.from, opts.to)
	if err != nil {
		return err
	}
	if opts.concurrency <= 0 {
		return fmt.Errorf("flow archive compact: --concurrency must be greater than zero")
	}

	cfg, err := flowArchiveCompactConfig(cmd, opts)
	if err != nil {
		return err
	}
	if _, err := os.Stat(opts.manifest); err == nil {
		return fmt.Errorf("flow archive compact: manifest %s already exists", opts.manifest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("flow archive compact: check manifest %s: %w", opts.manifest, err)
	}

	// Fail configuration/bootstrap errors before creating the manifest.
	// Workers open their own handles below; this one proves the shared
	// auth and DuckDB setup are viable so an operator can retry with the
	// same manifest path after fixing configuration.
	db, err := flowArchive.OpenParquetDB(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	_ = db.Close()

	objStore, closeStore, err := flowArchiveCompactStore(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer func() { _ = closeStore() }()

	manifest, err := os.OpenFile(opts.manifest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("flow archive compact: create manifest %s: %w", opts.manifest, err)
	}
	defer func() { _ = manifest.Close() }()
	enc := json.NewEncoder(manifest)

	dryRun := !opts.deleteOriginals
	days, skipped := compactDays(from, to, time.Now().UTC(), dryRun)
	for _, entry := range skipped {
		if err := enc.Encode(entry); err != nil {
			return fmt.Errorf("flow archive compact: write manifest: %w", err)
		}
		cmd.Printf("%s skipped: %s\n", entry.Day, entry.SkippedBecause)
	}
	if len(days) == 0 {
		return nil
	}

	if dryRun {
		cmd.PrintErrln("flow archive compact: dry-run; no replacements will be written and no originals will be deleted")
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	jobs := make(chan time.Time)
	results := make(chan flowArchiveCompactManifestEntry)
	var wg sync.WaitGroup
	workers := opts.concurrency
	if workers > len(days) {
		workers = len(days)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			flowArchiveCompactWorker(ctx, cfg, objStore, dryRun, jobs, results)
		}()
	}
	go func() {
		defer close(jobs)
		for _, day := range days {
			select {
			case jobs <- day:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	return writeFlowArchiveCompactResults(cmd, enc, results, dryRun, cancel)
}

func writeFlowArchiveCompactResults(
	cmd *cobra.Command,
	enc *json.Encoder,
	results <-chan flowArchiveCompactManifestEntry,
	dryRun bool,
	cancel context.CancelFunc,
) error {
	var firstErr error
	for entry := range results {
		if err := enc.Encode(entry); err != nil {
			cancel()
			return fmt.Errorf("flow archive compact: write manifest: %w", err)
		}
		if entry.Error != "" && firstErr == nil {
			firstErr = errors.New(entry.Error)
			if !dryRun {
				cancel()
			}
		}
		if entry.Error != "" {
			cmd.Printf("%s failed: %s\n", entry.Day, entry.Error)
			continue
		}
		if entry.Skipped {
			cmd.Printf("%s skipped: %s\n", entry.Day, entry.SkippedBecause)
			continue
		}
		cmd.Printf("%s objects %d -> %d rows=%d dry_run=%t\n",
			entry.Day, entry.ObjectsBefore, entry.ObjectsAfter, entry.Rows, entry.DryRun)
	}
	return firstErr
}

func flowArchiveCompactWorker(
	ctx context.Context,
	cfg flowArchive.Config,
	objStore archiveCompact.ObjectStore,
	dryRun bool,
	jobs <-chan time.Time,
	results chan<- flowArchiveCompactManifestEntry,
) {
	db, err := flowArchive.OpenParquetDB(ctx, cfg)
	if err != nil {
		for day := range jobs {
			results <- flowArchiveCompactManifestEntry{Day: formatCompactDay(day), DryRun: dryRun, Error: err.Error()}
		}
		return
	}
	defer func() { _ = db.Close() }()

	compactor := &archiveCompact.Compactor{
		DB:        db,
		Store:     objStore,
		ReadRoot:  archiveRootURL(cfg),
		KeyPrefix: strings.Trim(cfg.Prefix, "/"),
		DryRun:    dryRun,
	}
	for day := range jobs {
		res, err := compactor.CompactDay(ctx, day)
		entry := flowArchiveCompactEntry(res)
		if err != nil {
			entry.Error = err.Error()
		}
		results <- entry
		if err != nil && !dryRun {
			return
		}
	}
}

type flowArchiveCompactManifestEntry struct {
	Day            string                     `json:"day"`
	DryRun         bool                       `json:"dry_run"`
	Skipped        bool                       `json:"skipped"`
	SkippedBecause string                     `json:"skipped_because,omitempty"`
	ObjectsBefore  int                        `json:"objects_before"`
	ObjectsAfter   int                        `json:"objects_after"`
	Rows           int64                      `json:"rows"`
	BytesWritten   int64                      `json:"bytes_written"`
	BytesPlanned   int64                      `json:"bytes_planned,omitempty"`
	Accounts       []string                   `json:"accounts,omitempty"`
	Orphans        []string                   `json:"orphans,omitempty"`
	Fingerprint    archiveCompact.Fingerprint `json:"fingerprint"`
	Error          string                     `json:"error,omitempty"`
}

func flowArchiveCompactEntry(res archiveCompact.Result) flowArchiveCompactManifestEntry {
	bytesWritten := res.BytesWritten
	var bytesPlanned int64
	if res.DryRun {
		bytesPlanned = res.BytesWritten
		bytesWritten = 0
	}
	return flowArchiveCompactManifestEntry{
		Day:            formatCompactDay(res.Day),
		DryRun:         res.DryRun,
		Skipped:        res.Skipped,
		SkippedBecause: res.SkippedBecause,
		ObjectsBefore:  res.ObjectsBefore,
		ObjectsAfter:   res.ObjectsAfter,
		Rows:           res.Rows,
		BytesWritten:   bytesWritten,
		BytesPlanned:   bytesPlanned,
		Accounts:       res.Accounts,
		Orphans:        res.Orphans,
		Fingerprint:    res.Fingerprint,
	}
}

func compactDays(from, to, now time.Time, dryRun bool) ([]time.Time, []flowArchiveCompactManifestEntry) {
	today := utcDay(now)
	yesterday := today.AddDate(0, 0, -1)
	var days []time.Time
	var skipped []flowArchiveCompactManifestEntry
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if !d.Before(yesterday) {
			skipped = append(skipped, flowArchiveCompactManifestEntry{
				Day:            formatCompactDay(d),
				DryRun:         dryRun,
				Skipped:        true,
				SkippedBecause: "today and yesterday may still be receiving archive writes",
			})
			continue
		}
		days = append(days, d)
	}
	return days, skipped
}

func parseCompactRange(from, to string) (time.Time, time.Time, error) {
	start, err := parseCompactDay(from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("flow archive compact: --from: %w", err)
	}
	end, err := parseCompactDay(to)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("flow archive compact: --to: %w", err)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("flow archive compact: --to must be on or after --from")
	}
	return start, end, nil
}

func parseCompactDay(s string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected YYYY-MM-DD")
	}
	return utcDay(d), nil
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func formatCompactDay(t time.Time) string {
	return utcDay(t).Format("2006-01-02")
}

func flowArchiveCompactConfig(cmd *cobra.Command, opts *flowArchiveCompactOptions) (flowArchive.Config, error) {
	cfg, _ := flowArchive.ConfigFromEnv()
	flags := cmd.Flags()
	if flags.Changed("provider") {
		cfg.Provider = opts.provider
	}
	if flags.Changed("bucket") {
		cfg.Bucket = opts.bucket
	}
	if flags.Changed("prefix") {
		cfg.Prefix = opts.prefix
	}
	if flags.Changed("endpoint") {
		cfg.Endpoint = opts.endpoint
	}
	if flags.Changed("region") {
		cfg.Region = opts.region
	}
	if flags.Changed("gcs-credentials-file") {
		cfg.CredentialsFile = opts.gcsCredentialsFile
	}
	if cfg.Provider == "" || cfg.Bucket == "" {
		return flowArchive.Config{}, fmt.Errorf(
			"flow archive compact: archive provider and bucket are required; set OPENZRO_FLOW_ARCHIVE_* or pass --provider and --bucket")
	}
	return cfg, nil
}

func flowArchiveCompactStore(ctx context.Context, cfg flowArchive.Config) (archiveCompact.ObjectStore, func() error, error) {
	switch cfg.Provider {
	case "gcs":
		st, err := archiveCompact.NewGCS(ctx, archiveCompact.GCSConfig{
			Bucket:          cfg.Bucket,
			Prefix:          cfg.Prefix,
			Endpoint:        cfg.Endpoint,
			CredentialsJSON: cfg.CredentialsJSON,
			CredentialsFile: cfg.CredentialsFile,
		})
		if err != nil {
			return nil, nil, err
		}
		return st, st.Close, nil
	case "s3":
		st, err := archiveCompact.NewS3(ctx, archiveCompact.S3Config{
			Bucket:    cfg.Bucket,
			Prefix:    cfg.Prefix,
			Region:    cfg.Region,
			Endpoint:  cfg.Endpoint,
			AccessKey: cfg.AccessKeyID,
			SecretKey: cfg.SecretAccessKey,
		})
		if err != nil {
			return nil, nil, err
		}
		return st, func() error { return nil }, nil
	default:
		return nil, nil, fmt.Errorf("flow archive compact: unsupported provider %q (want s3 | gcs)", cfg.Provider)
	}
}

func archiveRootURL(cfg flowArchive.Config) string {
	root := fmt.Sprintf("%s://%s", cfg.Provider, cfg.Bucket)
	if prefix := strings.Trim(cfg.Prefix, "/"); prefix != "" {
		root += "/" + prefix
	}
	return root
}
