package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/openzro/openzro/upload-server/types"
)

func Test_S3HandlerGetUploadURL(t *testing.T) {
	if runtime.GOOS != "linux" && os.Getenv("CI") == "true" {
		t.Skip("Skipping test on non-Linux and CI environment due to docker dependency")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows due to potential docker dependency")
	}

	// We use MinIO instead of LocalStack here. The previous test ran
	// localstack/localstack:s3-latest, but LocalStack discontinued
	// the community s3-only image in v2026.03 (March 23, 2026) and
	// gates the equivalent behind their paid `localstack-pro` SKU.
	// MinIO is S3-compatible, pure-Go, and the official `minio/minio`
	// image is freely usable.
	awsRegion := "us-east-1"
	const minioAccessKey = "minioadmin"
	const minioSecretKey = "minioadmin"

	ctx := context.Background()
	containerRequest := testcontainers.ContainerRequest{
		// Pin to a specific release rather than `:latest` — MinIO ships
		// behavior changes (notably the 2025 console-removal flap) on
		// rolling tags, and we don't want a CI run to surprise us with
		// a newer image that needs different env vars or endpoints.
		Image:        "minio/minio:RELEASE.2025-04-22T22-12-26Z",
		ExposedPorts: []string{"9000/tcp"},
		Cmd:          []string{"server", "/data"},
		Env: map[string]string{
			"MINIO_ROOT_USER":     minioAccessKey,
			"MINIO_ROOT_PASSWORD": minioSecretKey,
		},
		// `/ready` waits until the server is actually serving S3 ops;
		// `/live` only confirms the process is up.
		WaitingFor: wait.ForHTTP("/minio/health/ready").WithPort("9000/tcp").WithStartupTimeout(60 * time.Second),
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: containerRequest,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start MinIO container: %v", err)
	}
	defer func(c testcontainers.Container, ctx context.Context) {
		if err := c.Terminate(ctx); err != nil {
			t.Log(err)
		}
	}(c, ctx)

	mappedPort, err := c.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatalf("failed to read MinIO mapped port: %v", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("failed to read MinIO container host: %v", err)
	}
	awsEndpoint := "http://" + host + ":" + mappedPort.Port()

	t.Setenv("AWS_REGION", awsRegion)
	t.Setenv("AWS_ENDPOINT_URL", awsEndpoint)
	t.Setenv("AWS_ACCESS_KEY_ID", minioAccessKey)
	t.Setenv("AWS_SECRET_ACCESS_KEY", minioSecretKey)

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(awsRegion), config.WithBaseEndpoint(awsEndpoint))
	if err != nil {
		t.Error(err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = cfg.BaseEndpoint
	})

	bucketName := "test"
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: &bucketName,
	}); err != nil {
		t.Error(err)
	}

	list, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		t.Error(err)
	}

	assert.Equal(t, len(list.Buckets), 1)
	assert.Equal(t, *list.Buckets[0].Name, bucketName)

	t.Setenv(bucketVar, bucketName)

	mux := http.NewServeMux()
	err = configureS3Handlers(mux)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, types.GetURLPath+"?id=test-file", nil)
	req.Header.Set(types.ClientHeader, types.ClientHeaderValue)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var response types.GetURLResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Contains(t, response.URL, "test-file/")
	require.NotEmpty(t, response.Key)
	require.Contains(t, response.Key, "test-file/")
}
