package sinks

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/option"

	"github.com/openzro/openzro/flow/store"
	"github.com/openzro/openzro/safedial"
)

// GCSConfig configures the native Google Cloud Storage sink.
//
// GCS already works through the existing S3 sink in interoperability
// mode (HMAC keys + the `https://storage.googleapis.com` endpoint),
// but Interop has two real limitations: it does not accept Service
// Account JSON or Workload Identity (HMAC keys are an extra surface
// to manage and rotate), and a couple of GCS features (lifecycle on
// resumable uploads, Customer-Managed Encryption Keys via headers
// the S3 SDK does not pass) are off-limits. This sink uses Google's
// own SDK so all of that lights up.
//
// Auth precedence (highest to lowest):
//
//   1. CredentialsJSON inline (the JSON content of a service account)
//   2. CredentialsFile path
//   3. Application Default Credentials — picks up GOOGLE_APPLICATION_CREDENTIALS,
//      gcloud user creds, GCE/GKE Workload Identity, and Cloud Run/
//      Cloud Functions injected creds, in that order.
//
// The recommended posture for production is GKE Workload Identity:
// no credential files in the container, IAM bound to the workload
// identity pool. Self-host outside GCP uses CredentialsFile.
type GCSConfig struct {
	// Bucket is the destination bucket. Required.
	Bucket string

	// Prefix is prepended to every object name. Trailing slash optional.
	Prefix string

	// CredentialsJSON is a service account key as raw JSON. Use only
	// when you cannot mount a file (e.g. shipping creds via
	// environment variable in a constrained runner). Prefer the file
	// or Workload Identity options.
	CredentialsJSON []byte

	// CredentialsFile is the filesystem path to a service account
	// JSON. Empty falls back to Application Default Credentials.
	CredentialsFile string

	// ProjectID is optional. The bucket already pins the project on
	// Google's side, so this is informational; we set it as a header
	// for clearer billing attribution when the operator's IAM grants
	// access via project-level binding.
	ProjectID string

	// FlushInterval bounds how stale buffered data can get before a
	// rotation is forced. Default 15m — same as S3.
	FlushInterval time.Duration

	// MaxEventsPerFile rotates after this many events even if the
	// interval has not elapsed. Default 100000.
	MaxEventsPerFile int

	// BufferSize is the in-memory queue capacity. Default 50000.
	BufferSize int

	// Format selects the on-disk encoding. Empty defaults to "ndjson"
	// for back-compat with operators who set up the archive before
	// ADR-0012 landed. Set to "parquet" so the dashboard's federated
	// read path (flow/store/archive) can query historical events.
	Format string

	// Endpoint overrides the default GCS endpoint. Empty for the real
	// service; set to `http://localhost:4443` (or wherever) for
	// fake-gcs-server in CI / dev.
	Endpoint string

	// HTTPClient lets tests inject a stub. Empty means default. When a
	// custom Endpoint is set and this is nil, NewGCS installs the
	// SSRF-guarded client (loopback + cloud-metadata blocked) — the
	// custom endpoint is the only SSRF surface, since the default GCS
	// endpoint is a fixed public Google domain.
	HTTPClient *http.Client
}

// GCS archives flow events to a Google Cloud Storage bucket. Format
// is gzipped NDJSON with the same per-record shape as the S3 sink so
// the operator can move between them (or run both side-by-side)
// without changing downstream tooling.
//
// Object keys partition by date and account, identical to S3:
//
//   <prefix>/year=2026/month=04/day=26/account=<id>/<unix-nano>-<rand>.ndjson.gz
type GCS struct {
	cfg    GCSConfig
	format archiveFormat
	client *storage.Client
	bucket *storage.BucketHandle

	queue  chan *store.Event
	wg     sync.WaitGroup
	stopCh chan struct{}
	closed sync.Once
}

// NewGCS constructs and starts the GCS sink.
func NewGCS(ctx context.Context, cfg GCSConfig) (*GCS, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("flow sink GCS: Bucket is required")
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 15 * time.Minute
	}
	if cfg.MaxEventsPerFile <= 0 {
		cfg.MaxEventsPerFile = 100000
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 50000
	}

	opts := []option.ClientOption{}
	switch {
	case len(cfg.CredentialsJSON) > 0:
		// SA1019: option.WithCredentialsJSON is being phased out in favor
		// of the new cloud.google.com/go/auth.Credentials surface. Keeping
		// the legacy path here because the alternative changes how the
		// storage client minds token refresh, and that needs explicit
		// testing against the prod GCS sink before flipping. Tracked in
		// #82.
		opts = append(opts, option.WithCredentialsJSON(cfg.CredentialsJSON)) //nolint:staticcheck // SA1019 — migration to cloud.google.com/go/auth tracked in #82.
	case cfg.CredentialsFile != "":
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile)) //nolint:staticcheck // SA1019 — same as above, migration tracked.
	}
	if cfg.Endpoint != "" {
		httpClient := cfg.HTTPClient
		if httpClient == nil {
			// timeout 0: the storage SDK owns its own per-operation
			// deadlines and retries; only the dial-time guard (loopback
			// + cloud metadata) is wanted here. This branch already runs
			// without Google auth, so there is no OAuth2 transport for
			// WithHTTPClient to clobber.
			httpClient = safedial.Client(0)
		}
		opts = append(opts,
			option.WithEndpoint(cfg.Endpoint),
			// fake-gcs-server typically does not enforce auth.
			// Tests pass through this branch and need a no-op auth.
			option.WithoutAuthentication(),
			option.WithHTTPClient(httpClient),
		)
	}

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("flow sink GCS: new client: %w", err)
	}

	g := &GCS{
		cfg:    cfg,
		format: resolveFormat(cfg.Format),
		client: client,
		bucket: client.Bucket(cfg.Bucket),
		queue:  make(chan *store.Event, cfg.BufferSize),
		stopCh: make(chan struct{}),
	}
	g.wg.Add(1)
	go g.loop()
	return g, nil
}

// Save enqueues a batch. Non-blocking; drops on full queue.
func (g *GCS) Save(_ context.Context, events []*store.Event) error {
	for _, ev := range events {
		select {
		case g.queue <- ev:
		default:
			log.Errorf("flow sink GCS: queue full (size=%d), dropping events", g.cfg.BufferSize)
			return nil
		}
	}
	return nil
}

// Close stops the loop, uploads whatever is buffered, and closes the
// underlying client. Idempotent.
func (g *GCS) Close() error {
	g.closed.Do(func() {
		close(g.stopCh)
		g.wg.Wait()
		_ = g.client.Close()
	})
	return nil
}

func (g *GCS) loop() {
	defer g.wg.Done()
	ticker := time.NewTicker(g.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]*store.Event, 0, 1024)
	for {
		select {
		case <-g.stopCh:
			for {
				select {
				case ev := <-g.queue:
					batch = append(batch, ev)
				default:
					if len(batch) > 0 {
						g.upload(context.Background(), batch)
					}
					return
				}
			}
		case ev := <-g.queue:
			batch = append(batch, ev)
			if len(batch) >= g.cfg.MaxEventsPerFile {
				g.upload(context.Background(), batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				g.upload(context.Background(), batch)
				batch = batch[:0]
			}
		}
	}
}

// upload encodes the batch in the configured format and PUTs it via
// the GCS SDK. Errors are logged loud and dropped — same posture as
// the S3 sink: durability comes from the streaming pipeline
// (Datadog / Elastic), not from a single archive.
func (g *GCS) upload(ctx context.Context, batch []*store.Event) {
	body, contentType, contentEncoding, err := g.encodeForFormat(batch)
	if err != nil {
		log.Errorf("flow sink GCS: encode batch (%d events): %v", len(batch), err)
		return
	}
	key := g.objectKey(batch[0])

	w := g.bucket.Object(key).NewWriter(ctx)
	w.ContentType = contentType
	w.ContentEncoding = contentEncoding
	if _, err := w.Write(body); err != nil {
		_ = w.Close()
		log.Errorf("flow sink GCS: write %s/%s (%d events): %v",
			g.cfg.Bucket, key, len(batch), err)
		return
	}
	if err := w.Close(); err != nil {
		log.Errorf("flow sink GCS: finalize %s/%s (%d events): %v",
			g.cfg.Bucket, key, len(batch), err)
		return
	}
}

// encodeForFormat mirrors the S3 sink helper so the dispatch shape
// stays identical between backends.
func (g *GCS) encodeForFormat(batch []*store.Event) (body []byte, contentType, contentEncoding string, err error) {
	switch g.format {
	case formatParquet:
		body, err = encodeBatchParquet(batch)
		return body, "application/vnd.apache.parquet", "", err
	default:
		body, err = encodeBatch(batch)
		return body, "application/x-ndjson", "gzip", err
	}
}

// objectKey mirrors the S3 sink's path layout so an operator can run
// both archives in parallel and have a stable schema in either tool.
func (g *GCS) objectKey(first *store.Event) string {
	t := first.ReceivedAt.UTC()
	prefix := g.cfg.Prefix
	if prefix != "" && prefix[len(prefix)-1] != '/' {
		prefix += "/"
	}
	ext := "ndjson.gz"
	if g.format == formatParquet {
		ext = "parquet"
	}
	return fmt.Sprintf(
		"%syear=%04d/month=%02d/day=%02d/account=%s/%d-%s.%s",
		prefix,
		t.Year(), t.Month(), t.Day(),
		first.AccountID,
		t.UnixNano(),
		hex.EncodeToString(first.EventID[:min(4, len(first.EventID))]),
		ext,
	)
}

// Compile-time check that GCS satisfies store.Sink.
var _ store.Sink = (*GCS)(nil)

// Avoid an unused-import warning when the SDK changes how bytes is
// referenced; keep this footnote import safe.
var _ = bytes.NewReader
