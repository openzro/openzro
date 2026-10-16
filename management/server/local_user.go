package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// localIdentityLockPrefix namespaces the cluster lock that serializes every
// local-user saga (create, password reset, deactivate, delete) for one
// email across all management replicas (ADR-0024 D9).
const localIdentityLockPrefix = "scim-local:"

// localIdentityLockDomain separates this hash from any other hashed key
// derived from an email.
const localIdentityLockDomain = "dex-password\x00"

// localIdentityLockKey derives the lock key for an email. It is global per
// email, not per account, because the Dex password DB is unique per email
// across the deployment. The email is lowercased (Dex keys on the
// lowercased form) and only its domain-separated SHA-256 appears in the
// key, so no plaintext email reaches the broker.
func localIdentityLockKey(email string) string {
	sum := sha256.Sum256([]byte(localIdentityLockDomain + strings.ToLower(email)))
	return localIdentityLockPrefix + hex.EncodeToString(sum[:])
}

// lockLocalIdentity takes the deployment-wide lock for one local identity.
// With a cluster coordinator it is a distributed lock; single-replica
// deployments fall back to the process-local lock, which is sufficient
// there. The caller must invoke the returned unlock.
func (am *DefaultAccountManager) lockLocalIdentity(ctx context.Context, email string) (func(), error) {
	key := localIdentityLockKey(email)
	if am.coordinator == nil {
		return am.Store.AcquireWriteLockByUID(ctx, key), nil
	}
	unlock, err := am.coordinator.Lock(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("lock local identity: %w", err)
	}
	return unlock, nil
}
