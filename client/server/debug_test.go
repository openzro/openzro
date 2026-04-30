package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/upload-server/server"
	"github.com/openzro/openzro/upload-server/types"
)

func TestUpload(t *testing.T) {
	if os.Getenv("DOCKER_CI") == "true" {
		t.Skip("Skipping upload test on docker ci")
	}
	testDir := t.TempDir()
	testURL := "http://localhost:8080"
	t.Setenv("SERVER_URL", testURL)
	t.Setenv("STORE_DIR", testDir)
	srv := server.NewServer()
	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Failed to start server: %v", err)
		}
	}()
	t.Cleanup(func() {
		if err := srv.Stop(); err != nil {
			t.Errorf("Failed to stop server: %v", err)
		}
	})

	// Wait for the server to bind :8080 before issuing the upload — the
	// goroutine above calls Start() asynchronously and the test was
	// hitting "connection refused" on slow CI runners.
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", "127.0.0.1:8080", 50*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 5*time.Second, 25*time.Millisecond, "upload server did not start listening on :8080")

	file := filepath.Join(t.TempDir(), "tmpfile")
	fileContent := []byte("test file content")
	err := os.WriteFile(file, fileContent, 0640)
	require.NoError(t, err)
	key, err := uploadDebugBundle(context.Background(), testURL+types.GetURLPath, testURL, file)
	require.NoError(t, err)
	id := getURLHash(testURL)
	require.Contains(t, key, id+"/")
	expectedFilePath := filepath.Join(testDir, key)
	createdFileContent, err := os.ReadFile(expectedFilePath)
	require.NoError(t, err)
	require.Equal(t, fileContent, createdFileContent)
}
