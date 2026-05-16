package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// maxArtifactBytes hard-caps the downloaded installer. The macOS PKG
// is tens of MB; 512 MB is generous headroom and stops a hostile or
// misconfigured endpoint from streaming forever onto the disk.
const maxArtifactBytes int64 = 512 * 1024 * 1024

// Download streams the artifact into a freshly-created temp file under
// dir, hashing as it goes, and verifies the SHA-256 against the
// manifest. This is the cross-platform INTEGRITY check; per-platform
// AUTHENTICITY (Apple notarization on macOS) is a separate step on
// the returned file. A size, status, hash or context failure removes
// the partial file and returns an error with an empty path — a
// download we cannot fully trust never reaches the verifier.
func Download(ctx context.Context, client *http.Client, a Artifact, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", fmt.Errorf("selfupdate: build download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("selfupdate: download: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("selfupdate: download endpoint returned HTTP %d", resp.StatusCode)
	}

	f, err := os.CreateTemp(dir, "openzro-update-*.pkg")
	if err != nil {
		return "", fmt.Errorf("selfupdate: create staging file: %w", err)
	}
	path := f.Name()

	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(path)
	}

	h := sha256.New()
	// Read one byte past the cap so an exactly-at-limit body is fine
	// but anything larger is detected and rejected.
	limited := io.LimitReader(resp.Body, maxArtifactBytes+1)
	n, err := io.Copy(io.MultiWriter(f, h), limited)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("selfupdate: stream artifact: %w", err)
	}
	if n > maxArtifactBytes {
		cleanup()
		return "", fmt.Errorf("selfupdate: artifact exceeds %d byte cap", maxArtifactBytes)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("selfupdate: flush staging file: %w", err)
	}

	sum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(sum, a.SHA256) {
		_ = os.Remove(path)
		return "", fmt.Errorf("selfupdate: integrity check failed: got %s want %s", sum, a.SHA256)
	}
	return path, nil
}
