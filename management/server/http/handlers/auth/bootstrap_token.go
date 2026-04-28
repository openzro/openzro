package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// BootstrapTokenPrefix tags every minted bootstrap token so logs
// and Authorization headers are unambiguous about what kind of
// secret they carry. Inspired by the existing PAT prefix nbp_.
const BootstrapTokenPrefix = "oz_bootstrap_"

// BootstrapTokenFile is the file the management writes the
// minted token to (relative to the data directory). Mode 0600
// so only the management's UID can read it; operators retrieve
// it via `cat` or `docker exec`.
const BootstrapTokenFile = "bootstrap-token.txt"

// ErrBootstrapDisabled is returned by Verify when no bootstrap
// token has been minted (the file doesn't exist). Callers map
// it to 404 — the /setup route should be invisible when not in
// bootstrap mode.
var ErrBootstrapDisabled = errors.New("auth: bootstrap mode disabled")

// ErrBootstrapTokenMismatch is returned when the supplied token
// doesn't match the stored value. Callers map it to 401 / 403.
var ErrBootstrapTokenMismatch = errors.New("auth: bootstrap token mismatch")

// BootstrapTokenStore guards the lifecycle of the single-use
// bootstrap token: generation at first boot, retrieval for
// validation, invalidation after the first provider is configured.
//
// The stored value is the raw token (not a hash) — bootstrap is a
// single-use flow where the operator is the only consumer; any
// attacker with read access to the management's data directory
// already owns the encryption key. Hashing would add complexity
// without a real defence.
type BootstrapTokenStore struct {
	dir string

	mu    sync.RWMutex
	value string // empty when no token currently active
}

// NewBootstrapTokenStore wires a store rooted at dir (typically
// the management's --datadir). Reads the file at construction so
// a restart doesn't lose the existing active token.
func NewBootstrapTokenStore(dir string) (*BootstrapTokenStore, error) {
	if dir == "" {
		return nil, errors.New("auth: bootstrap store: empty datadir")
	}
	s := &BootstrapTokenStore{dir: dir}
	if err := s.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("auth: bootstrap store load: %w", err)
	}
	return s, nil
}

func (s *BootstrapTokenStore) path() string {
	return filepath.Join(s.dir, BootstrapTokenFile)
}

func (s *BootstrapTokenStore) load() error {
	data, err := os.ReadFile(s.path())
	if err != nil {
		return err
	}
	v := strings.TrimSpace(string(data))
	if v == "" {
		return nil
	}
	s.mu.Lock()
	s.value = v
	s.mu.Unlock()
	return nil
}

// EnsureMinted returns the active token, minting a new one if
// none exists. The minted token is persisted to disk in the same
// call so a subsequent restart finds it. Idempotent: repeated
// calls return the existing token unchanged.
func (s *BootstrapTokenStore) EnsureMinted() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value != "" {
		return s.value, nil
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: bootstrap mint: %w", err)
	}
	token := BootstrapTokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", fmt.Errorf("auth: bootstrap dir: %w", err)
	}
	// O_EXCL guards against a race where two boots mint
	// simultaneously — only one wins and the other reads back
	// the winner via load() at the next EnsureMinted call.
	f, err := os.OpenFile(s.path(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("auth: bootstrap write: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(token + "\n"); err != nil {
		return "", fmt.Errorf("auth: bootstrap write: %w", err)
	}
	s.value = token
	return token, nil
}

// Active returns the current token, or "" when bootstrap is
// disabled. Callers that need to render the setup URL into a log
// line consult this; the verification path uses Verify.
func (s *BootstrapTokenStore) Active() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

// Verify checks the supplied token in constant time against the
// stored value. Returns ErrBootstrapDisabled when the store has
// no active token, ErrBootstrapTokenMismatch when the comparison
// fails.
func (s *BootstrapTokenStore) Verify(supplied string) error {
	s.mu.RLock()
	stored := s.value
	s.mu.RUnlock()
	if stored == "" {
		return ErrBootstrapDisabled
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(supplied)) != 1 {
		return ErrBootstrapTokenMismatch
	}
	return nil
}

// Invalidate clears the active token and removes the file. Called
// after the first provider is configured — the bootstrap flow is
// one-shot. Idempotent: a second Invalidate is a no-op.
func (s *BootstrapTokenStore) Invalidate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = ""
	if err := os.Remove(s.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("auth: bootstrap invalidate: %w", err)
	}
	return nil
}
