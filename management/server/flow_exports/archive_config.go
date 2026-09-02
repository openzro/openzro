package flow_exports

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	flowArchive "github.com/openzro/openzro/flow/store/archive"
)

// ArchiveConfigFromRows returns the first enabled S3/GCS export whose
// effective format is Parquet. List() sorts by ID, so the choice is
// deterministic when an operator has configured more than one archive.
//
// Decrypt/type errors are logged and skipped, matching Manager.ApplyAll:
// one malformed export row must not prevent management from starting.
func ArchiveConfigFromRows(ctx context.Context, cfgStore *Store) (flowArchive.Config, string, bool, error) {
	if cfgStore == nil {
		return flowArchive.Config{}, "", false, nil
	}
	rows, err := cfgStore.List(ctx)
	if err != nil {
		return flowArchive.Config{}, "", false, err
	}
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		switch row.Type {
		case TypeS3, TypeGCS:
		default:
			continue
		}

		plain, err := cfgStore.Decrypt(&row)
		if err != nil {
			log.WithContext(ctx).Errorf(
				"flow_exports: skipping archive reader candidate #%d (%s/%s): %v",
				row.ID, row.Type, row.Name, err)
			continue
		}

		switch row.Type {
		case TypeS3:
			c, ok := plain.(*S3DestConfig)
			if !ok || c == nil {
				log.WithContext(ctx).Errorf(
					"flow_exports: skipping archive reader candidate #%d (%s/%s): decrypted config has unexpected type",
					row.ID, row.Type, row.Name)
				continue
			}
			if archiveFormatFor(c.Format) != "parquet" {
				continue
			}
			return flowArchive.Config{
				Provider:        "s3",
				Bucket:          c.Bucket,
				Prefix:          c.Prefix,
				Endpoint:        c.Endpoint,
				Region:          c.Region,
				AccessKeyID:     c.AccessKey,
				SecretAccessKey: c.SecretKey,
			}, archiveReaderSource(row), true, nil
		case TypeGCS:
			c, ok := plain.(*GCSDestConfig)
			if !ok || c == nil {
				log.WithContext(ctx).Errorf(
					"flow_exports: skipping archive reader candidate #%d (%s/%s): decrypted config has unexpected type",
					row.ID, row.Type, row.Name)
				continue
			}
			if archiveFormatFor(c.Format) != "parquet" {
				continue
			}
			return flowArchive.Config{
				Provider:        "gcs",
				Bucket:          c.Bucket,
				Prefix:          c.Prefix,
				Endpoint:        c.Endpoint,
				ProjectID:       c.ProjectID,
				CredentialsFile: c.CredentialsFile,
				CredentialsJSON: []byte(c.CredentialsJSON),
			}, archiveReaderSource(row), true, nil
		}
	}
	return flowArchive.Config{}, "", false, nil
}

func archiveReaderSource(row FlowExport) string {
	return fmt.Sprintf("flow_exports#%d %s/%s", row.ID, row.Type, row.Name)
}
