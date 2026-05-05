package geolocation

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strconv"

	"github.com/glebarez/sqlite"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	geoLiteCityMMDB = "GeoLite2-City.mmdb"
	geoLiteCityCSV  = "GeoLite2-City-Locations-en.csv"

	// Mirror hosted by openZro (a fork-specific CDN that mirrors
	// MaxMind GeoLite2). Default download path: zero operator
	// configuration, but every install pings this host on cold
	// boot. Operators in air-gapped networks set --disable-geolite-
	// update=true and stage a local mmdb instead.
	geoLiteOpenzroMirror = "https://pkg.openzro.io/geolocation-dbs"

	// MaxMind's official direct-download endpoint. Requires a
	// (free) license key from https://www.maxmind.com/en/geolite2/signup.
	// Operators with stricter compliance / "no third-party mirrors"
	// requirements use this path; we never see the data.
	geoLiteMaxMindDirect = "https://download.maxmind.com/app/geoip_download"
)

// DownloadSource resolves where to fetch the GeoLite2 database
// from. A zero value (empty LicenseKey) defaults to the openZro
// mirror; operators who don't want third-party indirection set a
// MaxMind license key and the source flips to MaxMind's direct
// endpoint. Same checksum-and-extract pipeline either way — only
// the URLs differ.
type DownloadSource struct {
	LicenseKey string
}

// MMDB returns the URL for the GeoLite2-City .mmdb tarball.
func (s DownloadSource) MMDB() string { return s.archiveURL("GeoLite2-City", "tar.gz") }

// MMDBChecksum returns the URL for the GeoLite2-City tarball SHA-256.
func (s DownloadSource) MMDBChecksum() string {
	return s.archiveURL("GeoLite2-City", "tar.gz.sha256")
}

// CSV returns the URL for the GeoLite2-City CSV zip.
func (s DownloadSource) CSV() string { return s.archiveURL("GeoLite2-City-CSV", "zip") }

// CSVChecksum returns the URL for the GeoLite2-City CSV zip SHA-256.
func (s DownloadSource) CSVChecksum() string {
	return s.archiveURL("GeoLite2-City-CSV", "zip.sha256")
}

// archiveURL builds the per-edition / per-suffix download URL.
// Branch on LicenseKey: empty → openZro mirror (no key in URL);
// non-empty → MaxMind direct (license key is a query param).
func (s DownloadSource) archiveURL(editionID, suffix string) string {
	if s.LicenseKey != "" {
		v := url.Values{}
		v.Set("edition_id", editionID)
		v.Set("license_key", s.LicenseKey)
		v.Set("suffix", suffix)
		return geoLiteMaxMindDirect + "?" + v.Encode()
	}
	v := url.Values{}
	v.Set("suffix", suffix)
	return fmt.Sprintf("%s/%s/download?%s", geoLiteOpenzroMirror, editionID, v.Encode())
}

// String redacts the license key when the source is logged. Always
// use this instead of formatting the raw URL into a log line —
// otherwise the secret hits debug logs.
func (s DownloadSource) String() string {
	if s.LicenseKey == "" {
		return geoLiteOpenzroMirror
	}
	return geoLiteMaxMindDirect + " (with license key)"
}

// redactURL strips the license_key query parameter from a URL
// before it goes into logs. Used on every error path that mentions
// the download URL — operators who run management at debug level
// shouldn't see their MaxMind key in the journal.
func redactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	if q.Get("license_key") == "" {
		return rawURL
	}
	q.Set("license_key", "REDACTED")
	u.RawQuery = q.Encode()
	return u.String()
}

// loadGeolocationDatabases loads the MaxMind databases. src
// dictates whether downloads come from the openZro mirror (default)
// or MaxMind direct (when a license key was provided).
func loadGeolocationDatabases(ctx context.Context, src DownloadSource, dataDir string, mmdbFile string, geonamesdbFile string) error {
	for _, file := range []string{mmdbFile, geonamesdbFile} {
		exists, _ := fileExists(path.Join(dataDir, file))
		if exists {
			continue
		}

		log.WithContext(ctx).Infof("Geolocation database file %s not found, file will be downloaded from %s", file, src)

		switch file {
		case mmdbFile:
			extractFunc := func(src string, dst string) error {
				if err := decompressTarGzFile(src, dst); err != nil {
					return err
				}
				return copyFile(path.Join(dst, geoLiteCityMMDB), path.Join(dataDir, mmdbFile))
			}
			if err := loadDatabase(
				src.MMDBChecksum(),
				src.MMDB(),
				extractFunc,
			); err != nil {
				return err
			}

		case geonamesdbFile:
			extractFunc := func(srcPath string, dst string) error {
				if err := decompressZipFile(srcPath, dst); err != nil {
					return err
				}
				extractedCsvFile := path.Join(dst, geoLiteCityCSV)
				return importCsvToSqlite(dataDir, extractedCsvFile, geonamesdbFile)
			}

			if err := loadDatabase(
				src.CSVChecksum(),
				src.CSV(),
				extractFunc,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadDatabase downloads a file from the specified URL and verifies its checksum.
// It then calls the extract function to perform additional processing on the extracted files.
func loadDatabase(checksumURL string, fileURL string, extractFunc func(src string, dst string) error) error {
	temp, err := os.MkdirTemp(os.TempDir(), "geolite")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)

	checksumFilename, err := getFilenameFromURL(checksumURL)
	if err != nil {
		return err
	}
	checksumFile := path.Join(temp, checksumFilename)

	err = downloadFile(checksumURL, checksumFile)
	if err != nil {
		return err
	}

	sha256sum, err := loadChecksumFromFile(checksumFile)
	if err != nil {
		return err
	}

	dbFilename, err := getFilenameFromURL(fileURL)
	if err != nil {
		return err
	}
	dbFile := path.Join(temp, dbFilename)

	err = downloadFile(fileURL, dbFile)
	if err != nil {
		return err
	}

	if err := verifyChecksum(dbFile, sha256sum); err != nil {
		return err
	}

	return extractFunc(dbFile, temp)
}

// importCsvToSqlite imports a CSV file into a SQLite database.
func importCsvToSqlite(dataDir string, csvFile string, geonamesdbFile string) error {
	geonames, err := loadGeonamesCsv(csvFile)
	if err != nil {
		return err
	}

	db, err := gorm.Open(sqlite.Open(path.Join(dataDir, geonamesdbFile)), &gorm.Config{
		Logger:          logger.Default.LogMode(logger.Silent),
		CreateBatchSize: 1000,
	})
	if err != nil {
		return err
	}
	defer func() {
		sql, err := db.DB()
		if err != nil {
			return
		}
		sql.Close()
	}()

	if err := db.AutoMigrate(&GeoNames{}); err != nil {
		return err
	}

	return db.Create(geonames).Error
}

func loadGeonamesCsv(filepath string) ([]GeoNames, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var geoNames []GeoNames
	for index, record := range records {
		if index == 0 {
			continue
		}
		geoNameID, err := strconv.Atoi(record[0])
		if err != nil {
			return nil, err
		}

		geoName := GeoNames{
			GeoNameID:           geoNameID,
			LocaleCode:          record[1],
			ContinentCode:       record[2],
			ContinentName:       record[3],
			CountryIsoCode:      record[4],
			CountryName:         record[5],
			Subdivision1IsoCode: record[6],
			Subdivision1Name:    record[7],
			Subdivision2IsoCode: record[8],
			Subdivision2Name:    record[9],
			CityName:            record[10],
			MetroCode:           record[11],
			TimeZone:            record[12],
			IsInEuropeanUnion:   record[13],
		}
		geoNames = append(geoNames, geoName)
	}

	return geoNames, nil
}

// copyFile performs a file copy operation from the source file to the destination.
func copyFile(src string, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	return nil
}
