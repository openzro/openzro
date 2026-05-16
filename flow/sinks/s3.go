package sinks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/flow/store"
	"github.com/openzro/openzro/safedial"
)

// S3Config configures the cold-archive Sink. The endpoint and region
// fields make this work with AWS S3 (default), Cloudflare R2,
// Backblaze B2, MinIO, and any S3-compatible service. GCS works via
// its interoperability endpoint.
type S3Config struct {
	// Bucket is the destination bucket. Required.
	Bucket string
	// Region for the AWS SDK. Required for AWS S3; arbitrary for
	// non-AWS S3-compatible services (set to "auto" for R2).
	Region string
	// Endpoint overrides the default S3 endpoint. Empty for AWS S3;
	// set to https://<account>.r2.cloudflarestorage.com for R2,
	// https://s3.<region>.backblazeb2.com for B2, etc.
	Endpoint string
	// AccessKey + SecretKey are the static credentials. If both are
	// empty the SDK falls back to its default credentials chain
	// (env vars, profile, IAM role).
	AccessKey string
	SecretKey string

	// Prefix is prepended to every object key. Useful for sharing a
	// bucket between multiple environments. Trailing slash optional.
	Prefix string

	// FlushInterval bounds how stale buffered data can get before a
	// rotation is forced. Default 15m per ADR-0002.
	FlushInterval time.Duration

	// MaxEventsPerFile rotates after this many events even if the
	// interval has not elapsed. Default 100000 — at ~1k events/s this
	// is roughly the same trip as the 15m interval.
	MaxEventsPerFile int

	// BufferSize is the in-memory queue capacity. Default 50000.
	BufferSize int

	// Format selects the on-disk encoding. Empty defaults to "ndjson"
	// for back-compat with operators who set up the archive before
	// ADR-0012 landed. New deployments should set "parquet" so the
	// dashboard's federated read path (flow/store/archive) can query
	// historical events without leaving the UI.
	Format string

	// HTTPClient lets tests inject a stub. Empty means default.
	HTTPClient *http.Client
}

// S3 archives flow events to S3-compatible object storage. Format is
// gzipped NDJSON — one JSON event per line, gzip-compressed. DuckDB,
// Athena, BigQuery, and ClickHouse all read this format natively, so
// operators query their archive with the analytical tool of their
// choice. Parquet may land later as an opt-in alternative, but the
// JSON shape is the wire-format contract — never change field names
// without a release note.
//
// Object keys partition by date and account so common queries can
// prune at the path level:
//
//   <prefix>/year=2026/month=04/day=26/account=<id>/<unix-nano>-<rand>.ndjson.gz
type S3 struct {
	cfg    S3Config
	format archiveFormat
	client *s3.Client

	queue  chan *store.Event
	wg     sync.WaitGroup
	stopCh chan struct{}
	closed sync.Once
}

// NewS3 constructs and starts the S3 sink. Returns an error if the
// bucket is missing or the AWS SDK rejects the configuration.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("flow sink: S3 Bucket is required")
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

	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}
	if cfg.HTTPClient != nil {
		loadOpts = append(loadOpts, awsconfig.WithHTTPClient(cfg.HTTPClient))
	} else if cfg.Endpoint != "" {
		// A custom endpoint is the only SSRF surface here — the default
		// AWS regional endpoint is a fixed public domain. Guard the
		// dialer (loopback + cloud metadata) but pass timeout 0: the
		// AWS SDK manages its own per-operation deadlines and retries,
		// so only the dial-time block is wanted, not a client timeout.
		//
		// Sharp edge: WithHTTPClient applies to the whole aws.Config,
		// including the EC2 IMDS credential provider. So a custom
		// endpoint + reliance on instance-role creds (no static
		// AccessKey) breaks: the SDK's own IMDS fetch to 169.254.169.254
		// is now guard-blocked. This is acceptable because a custom
		// endpoint means a non-AWS target (MinIO/R2/B2) where auth is
		// static keys, not instance roles — set AccessKey/SecretKey.
		loadOpts = append(loadOpts, awsconfig.WithHTTPClient(safedial.Client(0)))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("flow sink S3: load aws config: %w", err)
	}

	clientOpts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // MinIO and most non-AWS S3 want path style
		})
	}

	s := &S3{
		cfg:    cfg,
		format: resolveFormat(cfg.Format),
		client: s3.NewFromConfig(awsCfg, clientOpts...),
		queue:  make(chan *store.Event, cfg.BufferSize),
		stopCh: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.loop()
	return s, nil
}

// Save enqueues a batch. Non-blocking; drops on full queue.
func (s *S3) Save(_ context.Context, events []*store.Event) error {
	for _, ev := range events {
		select {
		case s.queue <- ev:
		default:
			log.Errorf("flow sink S3: queue full (size=%d), dropping events", s.cfg.BufferSize)
			return nil
		}
	}
	return nil
}

// Close stops the loop and uploads whatever is buffered. Idempotent.
func (s *S3) Close() error {
	s.closed.Do(func() {
		close(s.stopCh)
		s.wg.Wait()
	})
	return nil
}

func (s *S3) loop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]*store.Event, 0, 1024)
	for {
		select {
		case <-s.stopCh:
			for {
				select {
				case ev := <-s.queue:
					batch = append(batch, ev)
				default:
					if len(batch) > 0 {
						s.upload(context.Background(), batch)
					}
					return
				}
			}
		case ev := <-s.queue:
			batch = append(batch, ev)
			if len(batch) >= s.cfg.MaxEventsPerFile {
				s.upload(context.Background(), batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				s.upload(context.Background(), batch)
				batch = batch[:0]
			}
		}
	}
}

// upload encodes the batch in the configured format and PUTs it.
// Errors are logged loud and dropped — the cold archive is
// best-effort, mirrored by the streaming exporter for durability
// needs.
func (s *S3) upload(ctx context.Context, batch []*store.Event) {
	body, contentType, contentEncoding, err := s.encodeForFormat(batch)
	if err != nil {
		log.Errorf("flow sink S3: encode batch (%d events): %v", len(batch), err)
		return
	}
	key := s.objectKey(batch[0])

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	}
	if contentEncoding != "" {
		input.ContentEncoding = aws.String(contentEncoding)
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		log.Errorf("flow sink S3: put %s/%s (%d events): %v",
			s.cfg.Bucket, key, len(batch), err)
		return
	}
}

// encodeForFormat produces the wire bytes plus the right HTTP
// content metadata for the configured archive format. NDJSON keeps
// the gzip Content-Encoding so the on-disk extension reflects the
// transport encoding; Parquet ships uncompressed at the HTTP layer
// (the file itself uses Snappy column compression).
func (s *S3) encodeForFormat(batch []*store.Event) (body []byte, contentType, contentEncoding string, err error) {
	switch s.format {
	case formatParquet:
		body, err = encodeBatchParquet(batch)
		return body, "application/vnd.apache.parquet", "", err
	default:
		body, err = encodeBatch(batch)
		return body, "application/x-ndjson", "gzip", err
	}
}

// objectKey builds the partitioned object path. Dates are taken from
// the first event's ReceivedAt — within a single batch they are
// nanoseconds apart, so partitioning by the head is fine and avoids
// fragmenting batches across day boundaries (which would produce many
// tiny files). Extension reflects the archive format so DuckDB / S3
// listing can filter by suffix without sniffing magic bytes.
func (s *S3) objectKey(first *store.Event) string {
	t := first.ReceivedAt.UTC()
	prefix := s.cfg.Prefix
	if prefix != "" && prefix[len(prefix)-1] != '/' {
		prefix += "/"
	}
	ext := "ndjson.gz"
	if s.format == formatParquet {
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

func encodeBatch(batch []*store.Event) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for _, ev := range batch {
		if err := enc.Encode(toJSONEvent(ev)); err != nil {
			return nil, err
		}
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// toJSONEvent flattens the event into a stable wire shape. Field
// names match the dashboard's eventDTO so an operator querying both
// hot (HTTP API) and cold (this archive) paths sees the same schema.
func toJSONEvent(e *store.Event) map[string]any {
	return map[string]any{
		"event_id":           hex.EncodeToString(e.EventID),
		"flow_id":            hex.EncodeToString(e.FlowID),
		"peer_id":            e.PeerID,
		"account_id":         e.AccountID,
		"is_initiator":       e.IsInitiator,
		"occurred_at":        e.OccurredAt.UTC().Format(time.RFC3339Nano),
		"received_at":        e.ReceivedAt.UTC().Format(time.RFC3339Nano),
		"type":               typeString(e.Type),
		"direction":          dirString(e.Direction),
		"protocol":           e.Protocol,
		"source_ip":          e.SourceIP,
		"dest_ip":            e.DestIP,
		"source_port":        e.SourcePort,
		"dest_port":          e.DestPort,
		"icmp_type":          e.ICMPType,
		"icmp_code":          e.ICMPCode,
		"rx_packets":         e.RxPackets,
		"tx_packets":         e.TxPackets,
		"rx_bytes":           e.RxBytes,
		"tx_bytes":           e.TxBytes,
		"rule_id":            hex.EncodeToString(e.RuleID),
		"source_resource_id": hex.EncodeToString(e.SourceResource),
		"dest_resource_id":   hex.EncodeToString(e.DestResource),
	}
}

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
