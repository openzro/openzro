package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootstrapToken_MintIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := NewBootstrapTokenStore(dir)
	require.NoError(t, err)

	a, err := s.EnsureMinted()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(a, BootstrapTokenPrefix), "missing prefix in %q", a)

	b, err := s.EnsureMinted()
	require.NoError(t, err)
	assert.Equal(t, a, b, "EnsureMinted must be idempotent within a single process")
}

func TestBootstrapToken_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	first, err := NewBootstrapTokenStore(dir)
	require.NoError(t, err)
	tok, err := first.EnsureMinted()
	require.NoError(t, err)

	// Second store reading the same datadir must surface the
	// previously minted value — restart-safety.
	second, err := NewBootstrapTokenStore(dir)
	require.NoError(t, err)
	assert.Equal(t, tok, second.Active())

	// And EnsureMinted on the second store stays idempotent.
	again, err := second.EnsureMinted()
	require.NoError(t, err)
	assert.Equal(t, tok, again)
}

func TestBootstrapToken_VerifyDisabledWhenAbsent(t *testing.T) {
	s, err := NewBootstrapTokenStore(t.TempDir())
	require.NoError(t, err)
	assert.ErrorIs(t, s.Verify("anything"), ErrBootstrapDisabled)
}

func TestBootstrapToken_VerifyMatchAndMismatch(t *testing.T) {
	s, err := NewBootstrapTokenStore(t.TempDir())
	require.NoError(t, err)
	tok, err := s.EnsureMinted()
	require.NoError(t, err)

	assert.NoError(t, s.Verify(tok))
	assert.ErrorIs(t, s.Verify(tok+"x"), ErrBootstrapTokenMismatch)
	assert.ErrorIs(t, s.Verify(""), ErrBootstrapTokenMismatch)
}

func TestBootstrapToken_InvalidateRemovesFile(t *testing.T) {
	dir := t.TempDir()
	s, err := NewBootstrapTokenStore(dir)
	require.NoError(t, err)
	_, err = s.EnsureMinted()
	require.NoError(t, err)

	path := filepath.Join(dir, BootstrapTokenFile)
	_, err = os.Stat(path)
	require.NoError(t, err)

	require.NoError(t, s.Invalidate())
	_, err = os.Stat(path)
	assert.True(t, errors.Is(err, os.ErrNotExist), "file should be removed; got %v", err)
	assert.ErrorIs(t, s.Verify("any"), ErrBootstrapDisabled)

	// Idempotent re-invalidate must not error.
	assert.NoError(t, s.Invalidate())
}

func TestBootstrapToken_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	s, err := NewBootstrapTokenStore(dir)
	require.NoError(t, err)
	_, err = s.EnsureMinted()
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, BootstrapTokenFile))
	require.NoError(t, err)
	mode := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0o600), mode, "token file must be 0600")
}

func TestBootstrapToken_RejectsEmptyDatadir(t *testing.T) {
	_, err := NewBootstrapTokenStore("")
	assert.Error(t, err)
}

func TestSetupURL(t *testing.T) {
	s, err := NewBootstrapTokenStore(t.TempDir())
	require.NoError(t, err)
	tok, err := s.EnsureMinted()
	require.NoError(t, err)

	u := SetupURL("https://openzro.example.com", s)
	assert.Contains(t, u, "https://openzro.example.com/setup?token=")
	assert.Contains(t, u, tok)
}

func TestSetupURL_NilOrEmpty(t *testing.T) {
	assert.Empty(t, SetupURL("https://x", nil))

	s, err := NewBootstrapTokenStore(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, SetupURL("https://x", s),
		"no minted token should produce empty URL")
}
