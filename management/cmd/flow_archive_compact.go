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
	cmd.AddCommand(newFlowArchiveCompactCommand(&flowArchiveCompactOptions{concurrency: 1}))
	return cmd
}

// newFlowArchiveCompactCommand takes its options rather than making them,
// so a test can read back what the flags resolved to.
func newFlowArchiveCompactCommand(opts *flowArchiveCompactOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compact --from YYYY-MM-DD --to YYYY-MM-DD --manifest FILE [--delete-originals]",
		Short: "Compact and repartition archived flow Parquet objects",
		Long: "Compact and repartition archived flow Parquet objects.\n\n" +
			"Where the archive is, resolved in this order:\n" +
			"  1. OPENZRO_FLOW_ARCHIVE_* environment variables,\n" +
			"  2. --provider / --bucket / --prefix / --region / --endpoint, which\n" +
			"     override individual fields from the environment,\n" +
			"  3. if provider and bucket are both set by then, nothing else is read --\n" +
			"     no management config, no database,\n" +
			"  4. otherwise the archive configured in the dashboard under Integrations,\n" +
			"     with the flags layered back on top.\n\n" +
			"The environment is all-or-nothing: without a bucket it describes no archive\n" +
			"at all, so exporting only a prefix does not override the dashboard. Use a\n" +
			"flag to change one field.\n\n" +
			"Step 3 makes the flags an escape hatch rather than a preference. This\n" +
			"command deletes objects, so pointing it at a copy of the bucket, or working\n" +
			"around a broken deployment, must not depend on the management config being\n" +
			"readable or the database being up.\n\n" +
			"Step 4 is where a normal deployment lands: a cluster that configured its\n" +
			"bucket in the dashboard has no OPENZRO_FLOW_ARCHIVE_*_BUCKET anywhere and\n" +
			"its credential exists only as an encrypted row, so reading it is what avoids\n" +
			"a second copy of the service-account key.\n\n" +
			"GCS needs one thing no row can supply: the HMAC interoperability pair, in\n" +
			"OPENZRO_FLOW_ARCHIVE_GCS_HMAC_KEY_ID and _SECRET. Reads go through DuckDB,\n" +
			"whose GCS secret accepts those keys and rejects service-account JSON. Writes\n" +
			"and deletes use the service account. S3 needs no such split -- one key pair\n" +
			"signs both.\n\n" +
			"Credentials are never flags, so they do not land in shell history or a\n" +
			"process list.",
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

	cfg, err := flowArchiveCompactConfig(cmd.Context(), cmd, opts)
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

// flowArchiveCompactConfig resolves where the archive is.
//
// Explicit configuration first, the dashboard second:
//
//  1. OPENZRO_FLOW_ARCHIVE_* environment variables,
//  2. --provider / --bucket / --prefix / --region / --endpoint flags,
//     which override individual fields from the environment,
//  3. if provider and bucket are BOTH set by then, nothing else is read
//     -- no management config, no database,
//  4. otherwise the archive configured in the dashboard under
//     Integrations becomes the base, with the flags layered back on top.
//
// The environment is all-or-nothing, which surprises people: ConfigFromEnv
// reports "not configured" without a bucket and returns an empty Config,
// so exporting only a prefix does not override the dashboard. That is how
// the reader treats these variables everywhere, so it stays that way
// rather than growing a special case here; a flag overrides one field.
//
// Step 3 is what makes the flags an escape hatch rather than a
// preference. This command deletes objects, and an operator pointing it
// at a copy of the bucket -- or working around a broken deployment --
// must not be blocked because the management config is unreadable or the
// database is down. If the flags say where to go, the command goes.
//
// Step 4 is where a normal deployment lands. A cluster that configured
// its bucket in the dashboard has no OPENZRO_FLOW_ARCHIVE_*_BUCKET set
// anywhere and its credential exists only as an encrypted row, so
// reading it is what lets the CronJob run without a second copy of the
// service-account key.
//
// The GCS HMAC pair is not part of this. It comes from the environment
// in configWithRuntimeEnv, because DuckDB reaches GCS through httpfs,
// whose secret takes interoperability keys and rejects service-account
// JSON, and the row has no field for them.
func flowArchiveCompactConfig(ctx context.Context, cmd *cobra.Command, opts *flowArchiveCompactOptions) (flowArchive.Config, error) {
	envCfg, _ := flowArchive.ConfigFromEnv()

	cfg := envCfg
	applyArchiveFlags(cmd, opts, &cfg)
	if cfg.Provider != "" && cfg.Bucket != "" {
		return cfg, nil
	}

	fromRows, source, ok, err := archiveConfigFromIntegrations(ctx)
	if err != nil {
		return flowArchive.Config{}, err
	}
	if ok {
		cmd.PrintErrf("flow archive compact: using the archive configured in Integrations (%s)\n", source)
		cfg = fromRows
		mergeArchiveOverrides(envCfg, &cfg)
		applyArchiveFlags(cmd, opts, &cfg)
	}

	if cfg.Provider == "" || cfg.Bucket == "" {
		return flowArchive.Config{}, fmt.Errorf(
			"flow archive compact: no archive configured. Set one up under Integrations " +
				"in the dashboard, or set OPENZRO_FLOW_ARCHIVE_*, or pass --provider and --bucket")
	}
	return cfg, nil
}

// applyArchiveFlags overwrites the fields the operator named on the
// command line. Only flags that were actually given, so an unset flag
// does not blank a configured value.
func applyArchiveFlags(cmd *cobra.Command, opts *flowArchiveCompactOptions, cfg *flowArchive.Config) {
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
}

// mergeArchiveOverrides layers whatever the environment set on top of a
// base, leaving the base's value wherever the environment was silent.
// Used to keep env precedence over the dashboard row when the row had to
// be read to fill something else in.
func mergeArchiveOverrides(from flowArchive.Config, into *flowArchive.Config) {
	if from.Provider != "" {
		into.Provider = from.Provider
	}
	if from.Bucket != "" {
		into.Bucket = from.Bucket
	}
	if from.Prefix != "" {
		into.Prefix = from.Prefix
	}
	if from.Endpoint != "" {
		into.Endpoint = from.Endpoint
	}
	if from.Region != "" {
		into.Region = from.Region
	}
	if len(from.CredentialsJSON) > 0 {
		into.CredentialsJSON = from.CredentialsJSON
	}
	if from.CredentialsFile != "" {
		into.CredentialsFile = from.CredentialsFile
	}
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
