//go:build !archive_duckdb

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	flowArchive "github.com/openzro/openzro/flow/store/archive"
)

func init() {
	archiveCmd := &cobra.Command{
		Use:          "flow-archive",
		Short:        "Operate on archived flow traffic",
		SilenceUsage: true,
	}
	compactCmd := &cobra.Command{
		Use:   "compact --from YYYY-MM-DD --to YYYY-MM-DD --manifest FILE [--delete-originals]",
		Short: "Compact and repartition archived flow Parquet objects",
		Long: "Compact and repartition archived flow Parquet objects.\n\n" +
			"Archive bucket settings default to OPENZRO_FLOW_ARCHIVE_* environment variables. " +
			"Credentials stay in that env/file contract rather than command-line flags, so they do not land in shell history.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("%w: flow archive compaction requires a binary built with -tags=archive_duckdb", flowArchive.ErrUnavailable)
		},
	}
	var ignoredString string
	var ignoredBool bool
	var ignoredInt int
	flags := compactCmd.Flags()
	flags.StringVar(&ignoredString, "from", "", "first UTC day to process, inclusive (YYYY-MM-DD)")
	flags.StringVar(&ignoredString, "to", "", "last UTC day to process, inclusive (YYYY-MM-DD)")
	flags.StringVar(&ignoredString, "manifest", "", "path for the JSONL manifest; must not already exist")
	flags.BoolVar(&ignoredBool, "delete-originals", false, "delete originals after replacements are verified; omitted means dry-run")
	flags.IntVar(&ignoredInt, "concurrency", 1, "number of days to process at once")
	flags.StringVar(&ignoredString, "provider", "", "archive provider override (s3 or gcs); defaults to OPENZRO_FLOW_ARCHIVE_*")
	flags.StringVar(&ignoredString, "bucket", "", "archive bucket override")
	flags.StringVar(&ignoredString, "prefix", "", "archive key prefix override")
	flags.StringVar(&ignoredString, "endpoint", "", "object store endpoint override")
	flags.StringVar(&ignoredString, "region", "", "S3 region override")
	flags.StringVar(&ignoredString, "gcs-credentials-file", "", "GCS service-account credentials file for writes/deletes")
	archiveCmd.AddCommand(compactCmd)
	rootCmd.AddCommand(archiveCmd)
}
