package compact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Config mirrors the auth fields flow/sinks.S3Config uses, so the
// repair tool reaches the same objects the sink wrote. Covers any
// S3-compatible service: AWS, MinIO, R2, B2.
type S3Config struct {
	Bucket    string
	Prefix    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
}

// S3Store is an ObjectStore over an S3 bucket.
type S3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3 builds a store for one bucket.
func NewS3(ctx context.Context, cfg S3Config) (*S3Store, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("compact: S3 bucket is required")
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
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("compact: load aws config: %w", err)
	}
	var clientOpts []func(*s3.Options)
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // MinIO and most non-AWS S3 want path style
		})
	}
	return &S3Store{
		client: s3.NewFromConfig(awsCfg, clientOpts...),
		bucket: cfg.Bucket,
		prefix: strings.Trim(cfg.Prefix, "/"),
	}, nil
}

func (s *S3Store) List(ctx context.Context, prefix string) ([]string, error) {
	if err := checkUnderPrefix(prefix, s.prefix); err != nil {
		return nil, err
	}
	var keys []string
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("compact: list %q: %w", prefix, err)
		}
		for _, o := range page.Contents {
			if o.Key == nil {
				continue
			}
			// As reported by the store, never rebuilt from the query.
			keys = append(keys, *o.Key)
		}
	}
	return keys, nil
}

func (s *S3Store) Read(ctx context.Context, key string) ([]byte, error) {
	if err := checkUnderPrefix(key, s.prefix); err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("compact: read %q: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("compact: read %q: %w", key, err)
	}
	return body, nil
}

func (s *S3Store) Write(ctx context.Context, key string, body []byte) error {
	if err := checkUnderPrefix(key, s.prefix); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/vnd.apache.parquet"),
	})
	if err != nil {
		return fmt.Errorf("compact: write %q: %w", key, err)
	}
	return nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	if err := checkUnderPrefix(key, s.prefix); err != nil {
		return err
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	// Same reasoning as the GCS store: absent is the state being asked
	// for, so a retry converges rather than failing on its own progress.
	var missing *types.NoSuchKey
	if err != nil && !errors.As(err, &missing) {
		return fmt.Errorf("compact: delete %q: %w", key, err)
	}
	return nil
}
