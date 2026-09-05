//go:build archive_duckdb

// DuckDB-backed implementation of store.Store. Each Query opens a
// fresh DuckDB connection, registers an httpfs SECRET pointed at the
// configured bucket, and runs SQL against the Parquet objects via
// `read_parquet(... hive_partitioning=true)`. The connection is
// short-lived: DuckDB is in-process so opening costs are sub-ms, and
// the alternative (long-lived connection across queries) would mean
// caching httpfs state that we'd rather rebuild on each call to keep
// the read path stateless.
//
// Why CGo: go-duckdb embeds the DuckDB native shared library to
// share the engine's memory model and avoid an IPC hop per query.
// The whole archive package is gated behind `archive_duckdb` so a
// non-CGo build of management still compiles — see archive.go.

package archive

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	_ "github.com/marcboeker/go-duckdb"

	"github.com/openzro/openzro/flow/store"
)

const (
	defaultQueryTimeout         = 60 * time.Second
	defaultMemoryLimit          = "256MB"
	defaultThreads              = 2
	defaultMaxConcurrentQueries = 4
	// readParquetURL is the read_parquet glob template DuckDB
	// resolves at query time. The Hive-style partition layout
	// matches what flow/sinks/{s3,gcs}.go writes today.
	readParquetURL = "%s://%s/%syear=*/month=*/day=*/account=%s/*.parquet"
)

// duckdbStore implements store.Store on top of object-store Parquet
// archives. Save / Purge are no-ops because the archive is owned by
// the sink fan-out (write-only on that path) and retention is the
// bucket lifecycle policy's responsibility (S3 lifecycle rules / GCS
// object lifecycle). Query is the real work.
type duckdbStore struct {
	cfg Config
	// sem caps concurrent archive queries so a burst of dashboard
	// loads cannot multiply DuckDB's per-query memory footprint into
	// the pod's resources.limits.memory ceiling. See ADR-0012's
	// "Memory bounds" subsection of the consequences table.
	sem chan struct{}
}

// New returns a DuckDB-backed Store ready to serve archive queries.
// Performs a one-time validation of the configuration; the actual
// DuckDB connection opens lazily on each Query so a transient bucket
// outage does not bring management down on boot.
func New(cfg Config) (store.Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("flow archive store: Bucket is required")
	}
	if cfg.Provider == "" {
		return nil, fmt.Errorf("flow archive store: Provider is required (s3 | gcs)")
	}
	switch cfg.Provider {
	case "s3", "gcs":
	default:
		return nil, fmt.Errorf("flow archive store: unsupported provider %q (want s3 | gcs)", cfg.Provider)
	}
	if cfg.Provider == "gcs" && (cfg.AccessKeyID == "" || cfg.SecretAccessKey == "") {
		return nil, newGCSHMACCredentialsError()
	}
	if cfg.QueryTimeout <= 0 {
		cfg.QueryTimeout = defaultQueryTimeout
	}
	if cfg.MemoryLimit == "" {
		cfg.MemoryLimit = defaultMemoryLimit
	}
	if cfg.Threads <= 0 {
		cfg.Threads = defaultThreads
	}
	if cfg.MaxConcurrentQueries <= 0 {
		cfg.MaxConcurrentQueries = defaultMaxConcurrentQueries
	}
	return &duckdbStore{
		cfg: cfg,
		sem: make(chan struct{}, cfg.MaxConcurrentQueries),
	}, nil
}

// Save is a no-op: the archive is populated by the FlowService fan-out
// to the matching write-side Sink (flow/sinks/{s3,gcs}.go). Implementing
// it on the read store would conflate the responsibility and let a
// future bug in federated.Save() write hot events to cold by accident.
func (d *duckdbStore) Save(_ context.Context, _ []*store.Event) error {
	return nil
}

// Purge is a no-op: bucket retention is the operator's lifecycle
// policy, not management's. Implementing DELETE-from-object-store
// here would couple us to one provider's semantics and compete with
// the operator's own configured policies.
func (d *duckdbStore) Purge(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// Close releases any cached DuckDB resources. Today there are none —
// each Query opens its own connection — so this is a no-op kept for
// interface symmetry.
func (d *duckdbStore) Close() error { return nil }

// Query runs the filter against the bucket's Parquet objects via
// DuckDB. Returns an empty slice with no error when the prefix has
// no matching objects yet (e.g. a freshly enabled archive on a peer
// cluster that has not rotated its first batch).
func (d *duckdbStore) Query(ctx context.Context, f store.Filter) ([]*store.Event, error) {
	if f.AccountID == "" {
		return nil, fmt.Errorf("flow archive store: AccountID is required on Filter")
	}

	ctx, cancel := context.WithTimeout(ctx, d.cfg.QueryTimeout)
	defer cancel()

	// Gate concurrent archive queries so a Network Traffic
	// page-load burst cannot multiply DuckDB's per-query footprint
	// into the pod's memory ceiling. The select honors the
	// caller's context so a canceled request doesn't block forever
	// behind a slow archive query.
	select {
	case d.sem <- struct{}{}:
		defer func() { <-d.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("flow archive store: open duckdb: %w", err)
	}
	defer conn.Close()

	if err := d.bootstrapConn(ctx, conn); err != nil {
		return nil, err
	}

	url := d.parquetURL(f.AccountID)
	q, args := buildQuery(url, f, d.marginDays())
	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("flow archive store: query: %w", err)
	}
	defer rows.Close()

	out := make([]*store.Event, 0, 256)
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("flow archive store: scan: %w", err)
		}
		out = append(out, ev)
		if len(out) >= MaxRowsPerQuery {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flow archive store: rows: %w", err)
	}
	return out, nil
}

// bootstrapConn loads the httpfs extension, pins the resource
// budget, and registers the credentials secret so subsequent
// SELECTs can open the bucket. We run it on every Query because
// the connection is fresh per call. Cost: ~1ms on warm DuckDB
// shared library, dwarfed by the network round-trip to the object
// store on the SELECT itself.
//
// The memory_limit + threads SETs are critical: DuckDB defaults
// auto-detect from the host's /proc/meminfo and CPU count, which in
// a Kubernetes pod overcounts both. Explicit caps prevent the OOM
// path documented in ADR-0012's consequences table.
func (d *duckdbStore) bootstrapConn(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx,
		fmt.Sprintf("SET memory_limit = %s", quoteString(d.cfg.MemoryLimit)),
	); err != nil {
		return fmt.Errorf("flow archive store: set memory_limit: %w", err)
	}
	if _, err := conn.ExecContext(ctx,
		fmt.Sprintf("SET threads = %d", d.cfg.Threads),
	); err != nil {
		return fmt.Errorf("flow archive store: set threads: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "INSTALL httpfs"); err != nil {
		return fmt.Errorf("flow archive store: install httpfs: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "LOAD httpfs"); err != nil {
		return fmt.Errorf("flow archive store: load httpfs: %w", err)
	}
	if err := d.applyAuth(ctx, conn); err != nil {
		return err
	}
	return nil
}

// applyAuth wires DuckDB SECRET objects with the operator's credentials
// for either S3-compatible or GCS native auth. We prefer SECRET over
// the legacy `SET s3_*_key_id` pattern because it scopes per
// connection and clears automatically when the connection closes.
func (d *duckdbStore) applyAuth(ctx context.Context, conn *sql.DB) error {
	switch d.cfg.Provider {
	case "s3":
		return d.applyAuthS3(ctx, conn)
	case "gcs":
		return d.applyAuthGCS(ctx, conn)
	}
	return nil
}

func (d *duckdbStore) applyAuthS3(ctx context.Context, conn *sql.DB) error {
	parts := []string{"TYPE S3"}
	if d.cfg.AccessKeyID != "" {
		parts = append(parts,
			fmt.Sprintf("KEY_ID %s", quoteString(d.cfg.AccessKeyID)),
			fmt.Sprintf("SECRET %s", quoteString(d.cfg.SecretAccessKey)),
		)
	}
	if d.cfg.SessionToken != "" {
		parts = append(parts, fmt.Sprintf("SESSION_TOKEN %s", quoteString(d.cfg.SessionToken)))
	}
	if d.cfg.Region != "" {
		parts = append(parts, fmt.Sprintf("REGION %s", quoteString(d.cfg.Region)))
	}
	if d.cfg.Endpoint != "" {
		parts = append(parts,
			fmt.Sprintf("ENDPOINT %s", quoteString(stripScheme(d.cfg.Endpoint))),
			"USE_SSL "+useSSLLiteral(d.cfg.Endpoint),
			"URL_STYLE 'path'",
		)
	}
	stmt := fmt.Sprintf("CREATE OR REPLACE SECRET archive_secret (%s)", strings.Join(parts, ", "))
	if _, err := conn.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("flow archive store: configure s3 secret: %w", err)
	}
	return nil
}

func (d *duckdbStore) applyAuthGCS(ctx context.Context, conn *sql.DB) error {
	// No INSTALL gcs here, and this is not an omission. DuckDB has no
	// gcs extension — the request 404s against extensions.duckdb.org
	// for every version — and GCS is served by httpfs, which
	// bootstrapConn already loaded.
	//
	// The secret takes an HMAC key pair and nothing else. Service
	// account JSON is rejected outright:
	//
	//   Binder Error: Unknown parameter 'credential_chain' for secret
	//   type 'gcs' with default provider 'config'
	//
	// So the read path needs interoperability keys even where the sink
	// writes with a service account. Refuse during store construction so
	// management can disable the optional archive reader with one warning
	// instead of building a secret that cannot authenticate and failing
	// once per dashboard query.
	if d.cfg.AccessKeyID == "" || d.cfg.SecretAccessKey == "" {
		return newGCSHMACCredentialsError()
	}

	parts := []string{
		"TYPE GCS",
		fmt.Sprintf("KEY_ID %s", quoteString(d.cfg.AccessKeyID)),
		fmt.Sprintf("SECRET %s", quoteString(d.cfg.SecretAccessKey)),
	}
	stmt := fmt.Sprintf("CREATE OR REPLACE SECRET archive_secret (%s)", strings.Join(parts, ", "))
	if _, err := conn.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("flow archive store: configure gcs secret: %w", err)
	}
	return nil
}

func newGCSHMACCredentialsError() error {
	return fmt.Errorf(
		"%w: GCS archive reads need HMAC credentials — set %s and %s "+
			"(Cloud Storage → Interoperability). Service account JSON writes the archive "+
			"but DuckDB cannot read with it",
		ErrMissingCredentials, envGCSHMACKeyID, envGCSHMACSecret)
}

// parquetURL builds the read_parquet glob for an account. The
// account_id partition lives at a fixed depth so we substitute it
// into the URL rather than relying on a WHERE clause; pruning at the
// path level avoids opening every account's row groups looking for a
// match.
func (d *duckdbStore) parquetURL(accountID string) string {
	prefix := d.cfg.Prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return fmt.Sprintf(readParquetURL, d.cfg.Provider, d.cfg.Bucket, prefix, accountID)
}

// marginDays is the pruning margin this store reads with. It is a
// method rather than an inline call so the wiring from configuration to
// query has somewhere to be asserted: a reader that computes the right
// margin and then queries with the default is the same outage as one
// that computes it wrong.
func (d *duckdbStore) marginDays() int {
	return marginDaysFor(d.cfg.MaxBatchSpan)
}

// marginDaysFor turns the sink's worst-case batch span into the number
// of days the pruning predicate must reach beyond the requested window.
//
// One day is the floor, not the answer: an object written by a sink that
// buffers for 48h can carry events two days newer than the date in its
// own name, and a predicate that stopped at the requested day would skip
// it without saying so. Rounding up rather than down, because the cost
// of an extra directory is a directory and the cost of one too few is a
// missing row.
func marginDaysFor(span time.Duration) int {
	const day = 24 * time.Hour
	days := 1
	if span > 0 {
		days += int((span + day - 1) / day)
	}
	return days
}

// writePartitionBounds narrows the file list before any file is opened.
//
// Without it the reader filters only on received_at, a column that lives
// *inside* the parquet files, so DuckDB opens every object under the
// account to answer any question. The objects are laid out
// year=/month=/day= precisely so most of them can be skipped, and
// nothing was using it: a two-month window over a four-month archive
// read all four, ran past the ingress timeout, and returned 504.
//
// The bounds are one day wider on each side, deliberately. The sink
// names a file after the ReceivedAt of the *first* event in the batch
// (flow/sinks/gcs.go), so a batch that crosses midnight files its later
// events under the earlier day. These predicates only prune — the
// received_at clauses below still decide the answer — so a widened
// bound costs one extra directory while a tight one drops real rows and
// says nothing.
//
// The literals are quoted because the partition columns do not share a
// type: DuckDB infers year=2026 as BIGINT but leaves month=05 VARCHAR,
// the leading zero defeating the cast, and comparing that VARCHAR
// against an integer is a binder error. A string literal is accepted by
// both, and the sink zero-pads every component, so lexicographic order
// is numeric order. Written as nested comparisons on the bare columns
// rather than arithmetic over them, because that is the shape DuckDB
// prunes on.
func writePartitionBounds(sb *strings.Builder, since, until time.Time, marginDays int) {
	if marginDays < 1 {
		marginDays = 1
	}
	if !since.IsZero() {
		lo := since.UTC().AddDate(0, 0, -marginDays)
		fmt.Fprintf(sb,
			" AND (year > '%04d' OR (year = '%04d' AND (month > '%02d' OR (month = '%02d' AND day >= '%02d'))))",
			lo.Year(), lo.Year(), int(lo.Month()), int(lo.Month()), lo.Day())
	}
	if !until.IsZero() {
		hi := until.UTC().AddDate(0, 0, marginDays)
		fmt.Fprintf(sb,
			" AND (year < '%04d' OR (year = '%04d' AND (month < '%02d' OR (month = '%02d' AND day <= '%02d'))))",
			hi.Year(), hi.Year(), int(hi.Month()), int(hi.Month()), hi.Day())
	}
}

// buildQuery produces the parameterised SELECT. Filters that the
// caller did not set degenerate to TRUE so the query plan stays
// uniform regardless of how many fields are constrained.
func buildQuery(url string, f store.Filter, marginDays int) (string, []any) {
	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(selectColumns, ", "))
	// The glob is interpolated, not bound, and that is forced rather than
	// chosen. DuckDB resolves the file list while binding the statement,
	// so a parameter in that position leaves the binder with nothing to
	// expand — and it fails the whole prepare with "could not bind
	// parameter" as soon as any *other* parameter is present. A query
	// with only the path bound happens to work, which is why this
	// survived until a dashboard request arrived carrying a time window.
	//
	// url is built from the operator's bucket and prefix, never from
	// request input, and quoteString doubles any single quote.
	sb.WriteString(" FROM read_parquet(")
	sb.WriteString(quoteString(url))
	sb.WriteString(", hive_partitioning=true) WHERE 1=1")

	// The account is already in the glob, and this predicate does not
	// prune -- account_id lives inside the files, so every object the
	// path selects is still opened. It is here because the path is a
	// claim about a file's contents that the writer, until #186, did not
	// keep: a batch mixing accounts was filed whole under the first
	// event's account, and those objects are on disk now. Restricting on
	// the path alone hands one account another's events.
	//
	// Query() rejects an empty AccountID before reaching here, so this
	// is unconditional on purpose: a filter that somehow arrived without
	// one must produce no rows, not every row.
	sb.WriteString(" AND account_id = ?")
	args = append(args, f.AccountID)

	if f.PeerID != "" {
		sb.WriteString(" AND peer_id = ?")
		args = append(args, f.PeerID)
	}
	if f.SourceIP != "" {
		sb.WriteString(" AND source_ip = ?")
		args = append(args, f.SourceIP)
	}
	if f.DestIP != "" {
		sb.WriteString(" AND dest_ip = ?")
		args = append(args, f.DestIP)
	}
	if f.SourcePort != nil {
		sb.WriteString(" AND source_port = ?")
		args = append(args, *f.SourcePort)
	}
	if f.DestPort != nil {
		sb.WriteString(" AND dest_port = ?")
		args = append(args, *f.DestPort)
	}
	if f.Protocol != nil {
		sb.WriteString(" AND protocol = ?")
		args = append(args, *f.Protocol)
	}
	// The hot store keeps type and direction as integers and compares
	// them as integers; the parquet writer encodes them as strings
	// (flow/sinks/parquet.go). Same filter, two representations, so the
	// comparison has to be encoded rather than passed through -- and the
	// two encoders have to agree, which is what typeString/dirString
	// below exist to state.
	if f.Type != nil {
		sb.WriteString(" AND type = ?")
		args = append(args, typeString(*f.Type))
	}
	if f.Direction != nil {
		sb.WriteString(" AND direction = ?")
		args = append(args, dirString(*f.Direction))
	}
	writePartitionBounds(&sb, f.Since, f.Until, marginDays)

	if !f.Since.IsZero() {
		sb.WriteString(" AND received_at >= ?")
		args = append(args, f.Since.UTC())
	}
	if !f.Until.IsZero() {
		sb.WriteString(" AND received_at <= ?")
		args = append(args, f.Until.UTC())
	}
	if len(f.RuleID) > 0 {
		sb.WriteString(" AND rule_id = ?")
		args = append(args, hex.EncodeToString(f.RuleID))
	}

	sb.WriteString(" ORDER BY received_at DESC")

	limit := f.Limit
	if limit <= 0 || limit > MaxRowsPerQuery {
		limit = MaxRowsPerQuery
	}
	sb.WriteString(" LIMIT ?")
	args = append(args, limit)
	if f.Offset > 0 {
		sb.WriteString(" OFFSET ?")
		args = append(args, f.Offset)
	}
	return sb.String(), args
}

// selectColumns pins the column list (and its order) so scanEvent
// can rely on positional Scan rather than column-name lookups, which
// trip on Parquet schema additions.
var selectColumns = []string{
	"received_at",
	"occurred_at",
	"account_id",
	"peer_id",
	"event_id",
	"flow_id",
	"type",
	"direction",
	"protocol",
	"source_ip",
	"dest_ip",
	"source_port",
	"dest_port",
	"icmp_type",
	"icmp_code",
	"is_initiator",
	"rx_packets",
	"tx_packets",
	"rx_bytes",
	"tx_bytes",
	"rule_id",
	"source_resource_id",
	"dest_resource_id",
}

// scanEvent maps a DuckDB row onto store.Event. The hex-encoded byte
// IDs are decoded back to []byte — the dashboard re-encodes on the
// way out, but downstream consumers (federated, the API handler)
// expect the same shape the hot store emits.
func scanEvent(rows *sql.Rows) (*store.Event, error) {
	var (
		ev               store.Event
		typ, dir         string
		eventIDHex       string
		flowIDHex        string
		ruleIDHex        string
		sourceResHex     string
		destResHex       string
		protocol         uint32
		srcPort, dstPort uint32
		icmpT, icmpC     uint32
	)
	if err := rows.Scan(
		&ev.ReceivedAt,
		&ev.OccurredAt,
		&ev.AccountID,
		&ev.PeerID,
		&eventIDHex,
		&flowIDHex,
		&typ,
		&dir,
		&protocol,
		&ev.SourceIP,
		&ev.DestIP,
		&srcPort,
		&dstPort,
		&icmpT,
		&icmpC,
		&ev.IsInitiator,
		&ev.RxPackets,
		&ev.TxPackets,
		&ev.RxBytes,
		&ev.TxBytes,
		&ruleIDHex,
		&sourceResHex,
		&destResHex,
	); err != nil {
		return nil, err
	}
	ev.Type = parseEventType(typ)
	ev.Direction = parseDirection(dir)
	ev.Protocol = uint16(protocol)
	ev.SourcePort = srcPort
	ev.DestPort = dstPort
	ev.ICMPType = uint16(icmpT)
	ev.ICMPCode = uint16(icmpC)
	if b, err := hex.DecodeString(eventIDHex); err == nil {
		ev.EventID = b
	}
	if b, err := hex.DecodeString(flowIDHex); err == nil {
		ev.FlowID = b
	}
	if b, err := hex.DecodeString(ruleIDHex); err == nil {
		ev.RuleID = b
	}
	if b, err := hex.DecodeString(sourceResHex); err == nil {
		ev.SourceResource = b
	}
	if b, err := hex.DecodeString(destResHex); err == nil {
		ev.DestResource = b
	}
	return &ev, nil
}

// typeString and dirString are the inverse of parseEventType and
// parseDirection below, and must stay identical to the encoders in
// flow/sinks/parquet.go: they are what the archived strings were written
// with. A disagreement here does not fail, it silently matches nothing,
// which is why the round-trip is asserted rather than assumed.
func typeString(t store.EventType) string {
	switch t {
	case store.EventTypeStart:
		return "start"
	case store.EventTypeEnd:
		return "end"
	case store.EventTypeDrop:
		return "drop"
	}
	return "unknown"
}

func dirString(d store.Direction) string {
	switch d {
	case store.DirectionIngress:
		return "ingress"
	case store.DirectionEgress:
		return "egress"
	}
	return "unknown"
}

func parseEventType(s string) store.EventType {
	switch s {
	case "start":
		return store.EventTypeStart
	case "end":
		return store.EventTypeEnd
	case "drop":
		return store.EventTypeDrop
	}
	return store.EventTypeUnknown
}

func parseDirection(s string) store.Direction {
	switch s {
	case "ingress":
		return store.DirectionIngress
	case "egress":
		return store.DirectionEgress
	}
	return store.DirectionUnknown
}

// quoteString single-quotes a value for use inside a DuckDB DDL
// statement. database/sql does not parameterise DDL, so DDL-only
// values (CREATE SECRET clauses) flow through here. We never feed
// user-supplied data into DDL; values come exclusively from process
// env vars set by the operator at boot.
func quoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// stripScheme normalises endpoints DuckDB expects without a scheme
// (it manages http vs https via USE_SSL). Operators set
// `https://...` for production and `http://localhost:9000` for
// MinIO; we feed DuckDB the host portion either way.
func stripScheme(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		return s[i+3:]
	}
	return s
}

// useSSLLiteral picks the DuckDB boolean literal that matches the
// operator's endpoint protocol. A bare hostname defaults to https.
func useSSLLiteral(endpoint string) string {
	if strings.HasPrefix(endpoint, "http://") {
		return "false"
	}
	return "true"
}
