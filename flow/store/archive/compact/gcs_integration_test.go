//go:build archive_duckdb

package compact_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/openzro/openzro/flow/store/archive/compact"
)

const (
	fakeGCSTestBucket = "flow-archive-gcs"
	fakeGCSTestPrefix = "flows"
)

func startFakeGCS(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test; needs a container runtime")
	}

	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			// Pinned for the same reason as the MinIO image: the fake
			// server is part of the contract this test is exercising.
			Image:        "fsouza/fake-gcs-server:1.52.3",
			ExposedPorts: []string{"4443/tcp"},
			Cmd:          []string{"-scheme", "http", "-port", "4443", "-backend", "memory"},
			WaitingFor: wait.ForHTTP("/storage/v1/b").
				WithPort("4443/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("failed to start fake-gcs-server container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	host, err := c.Host(ctx)
	require.NoError(t, err)
	port, err := c.MappedPort(ctx, "4443/tcp")
	require.NoError(t, err)
	endpoint := "http://" + host + ":" + port.Port()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint+"/storage/v1/b?project=openzro-test",
		bytes.NewBufferString(`{"name":"`+fakeGCSTestBucket+`"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create fake GCS bucket: status=%d body=%s", resp.StatusCode, body)
	}
	return endpoint + "/storage/v1/"
}

func newFakeGCSStore(t *testing.T) *compact.GCSStore {
	t.Helper()
	st, err := compact.NewGCS(context.Background(), compact.GCSConfig{
		Bucket:   fakeGCSTestBucket,
		Prefix:   fakeGCSTestPrefix,
		Endpoint: startFakeGCS(t),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestGCSStoreAgainstFakeServer(t *testing.T) {
	st := newFakeGCSStore(t)
	ctx := context.Background()
	key := fakeGCSTestPrefix + "/year=2026/month=07/day=29/account=acct-A/one.parquet"
	body := bytes.Repeat([]byte("parquet-ish"), 8)

	require.NoError(t, st.Write(ctx, key, body))
	got, err := st.Read(ctx, key)
	require.NoError(t, err)
	require.Equal(t, body, got, "Write must be durable by the time it returns")

	keys, err := st.List(ctx, fakeGCSTestPrefix+"/year=2026/month=07/day=29")
	require.NoError(t, err)
	require.Equal(t, []string{key}, keys)

	require.NoError(t, st.Delete(ctx, key))
	require.NoError(t, st.Delete(ctx, key), "deleting an absent object must converge")
	keys, err = st.List(ctx, fakeGCSTestPrefix+"/year=2026/month=07/day=29")
	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestGCSListPagesPastTheThousandKeyLimit(t *testing.T) {
	st := newFakeGCSStore(t)
	ctx := context.Background()
	const objects = 1005
	prefix := fakeGCSTestPrefix + "/year=2026/month=07/day=30/account=acct-A"
	body := []byte("x")
	for i := range objects {
		require.NoError(t, st.Write(ctx, fmt.Sprintf("%s/%04d.parquet", prefix, i), body))
	}

	keys, err := st.List(ctx, fakeGCSTestPrefix+"/year=2026/month=07/day=30")
	require.NoError(t, err)
	require.Len(t, keys, objects,
		"a List that stops at the first page leaves GCS originals undeleted while their rows are copied into the replacement")
}
