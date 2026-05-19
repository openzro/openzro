// Package archive serves the read side of cold flow event storage:
// querying the Parquet files that flow/sinks/{s3,gcs}.go emit when
// `OPENZRO_FLOW_ARCHIVE_FORMAT=parquet`. See ADR-0012 for the
// federated read design that combines this Store with the hot
// store.
//
// Two build modes:
//
//   - With `-tags=archive_duckdb` (default for linux/darwin in CI):
//     archive.New() returns a DuckDB-backed Store that runs SQL
//     against the bucket via DuckDB's httpfs extension. CGo is
//     required because go-duckdb ships a native shared library.
//   - Without the build tag (windows release until cross-compile is
//     validated, plus operators who explicitly want a CGo-free
//     binary): archive.New() returns ErrUnavailable. The federated
//     wrapper detects the absent store at boot and silently falls
//     back to hot-only — same behavior as the v0.53.x line had
//     before this ADR.
//
// Operators do not interact with this package directly; the
// management binary wires it through flow/store/federated.
package archive

import (
	"errors"
	"time"
)

// ErrUnavailable is returned by New when the binary was built
// without the `archive_duckdb` tag. Callers that wrap archive.Store
// alongside a hot store should treat this as "no archive
// configured" and fall through to hot-only behavior.
var ErrUnavailable = errors.New(
	"flow archive store: built without archive_duckdb tag — " +
		"rebuild with `go build -tags=archive_duckdb` to enable")

// Config configures the DuckDB-backed archive Store. The fields
// mirror the env-var contract used by flow/sinks so the operator
// configures the bucket once and the read + write paths share it.
type Config struct {
	// Provider is the object-store family DuckDB will read from.
	// "s3" covers AWS S3, MinIO, Cloudflare R2, Backblaze B2 — any
	// S3-compatible service. "gcs" covers Google Cloud Storage native
	// auth (HMAC interop falls back to "s3").
	Provider string

	// Bucket is the destination bucket name. Required.
	Bucket string

	// Prefix is the same path prefix the sink writes under. The Store
	// constructs `<bucket>/<prefix>/year=*/month=*/day=*/account=<id>/*.parquet`
	// at query time. Empty is fine — matches an unprefixed sink.
	Prefix string

	// Endpoint overrides the default object-store endpoint. Used for
	// MinIO / R2 / fake-gcs-server in tests. Empty means "the real
	// AWS S3 / GCS endpoint".
	Endpoint string

	// Region for S3-style auth. Empty / "auto" works for R2 and
	// most non-AWS S3-compatible services.
	Region string

	// Credentials follow the same env-var contract as the sinks. The
	// archive Store reads them via DuckDB SECRET objects per ADR-0012
	// §"S3 vs GCS auth".
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string

	// CredentialsJSON / CredentialsFile / ProjectID are the GCS
	// equivalents. Set whichever the operator already has wired into
	// the matching sink.
	CredentialsJSON []byte
	CredentialsFile string
	ProjectID       string

	// QueryTimeout bounds a single archive query — DuckDB will cancel
	// the underlying httpfs reads when the context fires. Default
	// 60s; bump it for large-window forensics if 60s is not enough.
	QueryTimeout time.Duration

	// MemoryLimit caps the DuckDB engine's per-connection memory
	// footprint. DuckDB's auto-detect reads /proc/meminfo, which in a
	// Kubernetes pod reports the host's RAM rather than the cgroup
	// limit — so without an explicit cap a query against a long
	// archive window can blow the pod's resources.limits.memory and
	// trigger an OOMKill. Default "256MB", expressed as a DuckDB
	// memory string ("128MB", "1GB", etc).
	MemoryLimit string

	// Threads bounds the DuckDB worker pool per query. DuckDB
	// defaults to N=CPUs which amplifies memory per query under
	// concurrency. Default 2 keeps a single archive query
	// well-bounded; bump for single-tenant clusters that prioritize
	// query latency over predictable footprint.
	Threads int

	// MaxConcurrentQueries gates how many DuckDB queries can run at
	// once on this Store. Beyond this, callers block on a semaphore.
	// Default 4: at MemoryLimit=256MB that's a 1GB worst-case
	// archive footprint, sized for a 1Gi pod with headroom for the
	// rest of the management process.
	MaxConcurrentQueries int
}

// MaxRowsPerQuery caps how many archive rows a single Query() may
// return. The federated wrapper enforces a smaller cap on its own
// merged result, but we set this as a safety net so a malformed
// Filter cannot scan and ship a billion rows over the dashboard
// API. 100k matches the in-memory queue capacity flow/sinks defaults
// to, which is the natural upper bound for "one human paging through
// archive results".
const MaxRowsPerQuery = 100_000
