package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ADR-0024 D9: the lock is global per email (Dex password uniqueness is
// deployment-wide), case-insensitive, domain-separated, and carries no
// plaintext email.
func TestLocalIdentityLockKey(t *testing.T) {
	key := localIdentityLockKey("Ana@Example.COM")
	assert.Equal(t, key, localIdentityLockKey("ana@example.com"))
	assert.NotEqual(t, key, localIdentityLockKey("bea@example.com"))
	assert.True(t, strings.HasPrefix(key, "scim-local:"))
	assert.NotContains(t, strings.ToLower(key), "example.com")

	plain := sha256.Sum256([]byte("ana@example.com"))
	assert.NotContains(t, key, hex.EncodeToString(plain[:]), "the hash must be domain-separated")
}

// recordingCoordinator records lock keys.
type recordingCoordinator struct {
	*fakeCoordinator
	mu   sync.Mutex
	keys []string
}

func (r *recordingCoordinator) Lock(ctx context.Context, name string) (func(), error) {
	r.mu.Lock()
	r.keys = append(r.keys, name)
	r.mu.Unlock()
	return r.fakeCoordinator.Lock(ctx, name)
}

func TestLockLocalIdentity(t *testing.T) {
	manager, err := createManager(t)
	require.NoError(t, err)
	ctx := context.Background()

	// Single replica: falls back to the process-local lock.
	unlock, err := manager.lockLocalIdentity(ctx, "ana@example.com")
	require.NoError(t, err)
	unlock()

	// HA: uses the cluster lock with the derived key.
	coord := &recordingCoordinator{fakeCoordinator: newFakeCoordinator()}
	manager.coordinator = coord
	unlock, err = manager.lockLocalIdentity(ctx, "Ana@Example.com")
	require.NoError(t, err)
	unlock()
	assert.Equal(t, []string{localIdentityLockKey("ana@example.com")}, coord.keys)
}
