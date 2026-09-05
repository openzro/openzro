//go:build archive_duckdb

package compact

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// fsStore is an ObjectStore over a directory, so the compaction logic
// can be exercised end to end without a cloud account. DuckDB reads
// local paths and gcs:// paths through the same code path, so the only
// thing this substitutes is the SDK half.
type fsStore struct {
	root string

	mu             sync.Mutex
	putErr         error // injected: fail the next Write
	truncateWrites bool  // injected: store a short prefix of the bytes
	deleteErr      error // injected: fail the next Delete
	deleteErrAfter int   // injected: allow this many deletes before failing once
	deleteErrFired bool
	deleted        []string
	put            []string
}

func (f *fsStore) List(_ context.Context, prefix string) ([]string, error) {
	var out []string
	base := filepath.Join(f.root, filepath.FromSlash(prefix))
	err := filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(f.root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func (f *fsStore) Write(_ context.Context, key string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return f.putErr
	}
	p := filepath.Join(f.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f.put = append(f.put, key)
	if f.truncateWrites && len(body) > 8 {
		body = body[:len(body)/2]
	}
	return os.WriteFile(p, body, 0o644)
}

func (f *fsStore) Read(_ context.Context, key string) ([]byte, error) {
	return os.ReadFile(filepath.Join(f.root, filepath.FromSlash(key)))
}

func (f *fsStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil && !f.deleteErrFired && len(f.deleted) >= f.deleteErrAfter {
		f.deleteErrFired = true
		return f.deleteErr
	}
	f.deleted = append(f.deleted, key)
	err := os.Remove(filepath.Join(f.root, filepath.FromSlash(key)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (f *fsStore) count(prefix string) int {
	keys, _ := f.List(context.Background(), prefix)
	n := 0
	for _, k := range keys {
		if strings.HasSuffix(k, ".parquet") {
			n++
		}
	}
	return n
}
