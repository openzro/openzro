package compact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/openzro/openzro/safedial"
)

// GCSConfig mirrors the fields flow/sinks.GCSConfig uses for auth, so an
// operator configures the bucket once and the repair tool reaches the
// same objects the sink wrote.
//
// Note what is absent: the HMAC pair the DuckDB reader needs. That is a
// read-path concern and belongs to whoever builds the DuckDB handle;
// this half writes and deletes with the service account, exactly as the
// sink does.
type GCSConfig struct {
	Bucket          string
	Prefix          string
	Endpoint        string
	CredentialsJSON []byte
	CredentialsFile string
	HTTPClient      *http.Client
}

// GCSStore is an ObjectStore over a GCS bucket.
type GCSStore struct {
	bucket *storage.BucketHandle
	prefix string
	client *storage.Client
}

// NewGCS builds a store for one bucket. The caller closes it.
func NewGCS(ctx context.Context, cfg GCSConfig) (*GCSStore, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("compact: GCS bucket is required")
	}
	var opts []option.ClientOption
	switch {
	case len(cfg.CredentialsJSON) > 0:
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, cfg.CredentialsJSON))
	case cfg.CredentialsFile != "":
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, cfg.CredentialsFile))
	}
	if cfg.Endpoint != "" {
		httpClient := cfg.HTTPClient
		if httpClient == nil {
			httpClient = safedial.Client(0)
		}
		opts = append(opts,
			option.WithEndpoint(cfg.Endpoint),
			option.WithoutAuthentication(),
			option.WithHTTPClient(httpClient),
		)
	}
	// Keep reads on the same JSON API as list/write/delete. The default
	// XML read path is a separate code path in the Go client and is not
	// what fake-gcs-server emulates; JSON reads are also the client
	// library's recommended direction.
	opts = append(opts, storage.WithJSONReads())
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("compact: new GCS client: %w", err)
	}
	return &GCSStore{
		bucket: client.Bucket(cfg.Bucket),
		prefix: strings.Trim(cfg.Prefix, "/"),
		client: client,
	}, nil
}

func (g *GCSStore) Close() error { return g.client.Close() }

func (g *GCSStore) List(ctx context.Context, prefix string) ([]string, error) {
	if err := checkUnderPrefix(prefix, g.prefix); err != nil {
		return nil, err
	}
	var keys []string
	it := g.bucket.Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return keys, nil
		}
		if err != nil {
			return nil, fmt.Errorf("compact: list %q: %w", prefix, err)
		}
		// The name is taken as the store reports it. Reconstructing it
		// from the query prefix plus a suffix is how a delete ends up
		// aimed at something the listing never saw.
		keys = append(keys, attrs.Name)
	}
}

func (g *GCSStore) Read(ctx context.Context, key string) ([]byte, error) {
	if err := checkUnderPrefix(key, g.prefix); err != nil {
		return nil, err
	}
	r, err := g.bucket.Object(key).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("compact: read %q: %w", key, err)
	}
	defer func() { _ = r.Close() }()
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("compact: read %q: %w", key, err)
	}
	return body, nil
}

func (g *GCSStore) Write(ctx context.Context, key string, body []byte) error {
	if err := checkUnderPrefix(key, g.prefix); err != nil {
		return err
	}
	w := g.bucket.Object(key).NewWriter(ctx)
	w.ContentType = "application/vnd.apache.parquet"
	if _, err := w.Write(body); err != nil {
		_ = w.Close()
		return fmt.Errorf("compact: write %q: %w", key, err)
	}
	// Close is where GCS reports the failure that matters: the bytes are
	// not durable until it returns, so a Write that only checked the
	// io.Writer would report success on an object that never landed.
	if err := w.Close(); err != nil {
		return fmt.Errorf("compact: finalize %q: %w", key, err)
	}
	return nil
}

func (g *GCSStore) Delete(ctx context.Context, key string) error {
	if err := checkUnderPrefix(key, g.prefix); err != nil {
		return err
	}
	err := g.bucket.Object(key).Delete(ctx)
	// An absent object is the state Delete is asked to produce, so a
	// retried compaction converges instead of failing on its own
	// progress.
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return fmt.Errorf("compact: delete %q: %w", key, err)
	}
	return nil
}
