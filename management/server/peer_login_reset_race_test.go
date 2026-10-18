package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// txUserPausingStore pauses the first in-transaction GetUserByUserID of one
// user right after it returned, so the row lock that read took is held,
// until released.
type txUserPausingStore struct {
	store.Store
	userID  string
	once    sync.Once
	locked  chan struct{}
	proceed chan struct{}
}

func (p *txUserPausingStore) ExecuteInTransaction(ctx context.Context, op func(store.Store) error) error {
	return p.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		return op(&txUserPause{Store: tx, p: p})
	})
}

type txUserPause struct {
	store.Store
	p *txUserPausingStore
}

func (t *txUserPause) GetUserByUserID(ctx context.Context, lockStrength store.LockingStrength,
	userID string) (*types.User, error) {
	user, err := t.Store.GetUserByUserID(ctx, lockStrength, userID)
	if userID == t.p.userID && lockStrength != store.LockingStrengthNone {
		t.p.once.Do(func() {
			close(t.p.locked)
			<-t.p.proceed
		})
	}
	return user, err
}

// ADR-0027 D5, #220 review: re-logging an expired peer locks the user row
// and then writes last_login to it. With only a share lock that write is an
// upgrade, which can deadlock against a reset already queued for the row;
// the login takes the update lock up front. The login is paused holding
// that lock while the reset queues behind it, then released: both must
// converge without error, and the peer ends expired.
func TestLoginPeer_ExpiredReloginAndResetConverge(t *testing.T) {
	m, accountID, _ := setupLocalUsers(t)
	if sql, ok := m.Store.(*store.SqlStore); ok && sql.GetStoreEngine() == types.SqliteStoreEngine {
		t.Skip("SQLite serializes on its single connection: there is no row-lock interleaving to stage")
	}
	ctx := context.Background()
	u := createLocal(t, m, accountID, "ana@example.com")
	peerID, _ := connectPeer(t, m, u)
	peer, err := m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	expired := peer.Status.Copy()
	expired.LoginExpired = true
	require.NoError(t, m.Store.SavePeerStatus(ctx, store.LockingStrengthUpdate, accountID, peerID, *expired))

	plain := m.Store
	paused := &txUserPausingStore{Store: plain, userID: u.Id,
		locked: make(chan struct{}), proceed: make(chan struct{})}
	m.Store = paused
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(paused.proceed) }) }
	t.Cleanup(release)

	loginDone := make(chan error, 1)
	go func() {
		_, _, _, err := m.LoginPeer(ctx, types.PeerLogin{
			WireGuardPubKey: peer.Key, Meta: peer.Meta, UserID: u.Id, JWTIssuedAt: time.Now(),
		})
		loginDone <- err
	}()
	select {
	case <-paused.locked:
	case err := <-loginDone:
		t.Fatalf("the login finished before locking the user: %v", err)
	case <-time.After(barrierTimeout):
		t.Fatal("the login never read the user")
	}

	resetDone := make(chan error, 1)
	go func() {
		_, err := m.SCIMResetLocalPassword(ctx, accountID, userID, u.Id, newPassword, nil)
		resetDone <- err
	}()
	// The reset needs the user row the paused login holds (its first read
	// already waits on it with the update lock), so it must not finish
	// before the login releases it.
	select {
	case err := <-resetDone:
		t.Fatalf("the reset finished while the login held the user row: %v", err)
	case <-time.After(time.Second):
	}
	release()

	for name, done := range map[string]chan error{"login": loginDone, "reset": resetDone} {
		select {
		case err := <-done:
			require.NoError(t, err, "%s", name)
		case <-time.After(barrierTimeout):
			t.Fatalf("the %s did not finish", name)
		}
	}
	m.Store = plain

	peer, err = m.Store.GetPeerByID(ctx, store.LockingStrengthShare, accountID, peerID)
	require.NoError(t, err)
	assert.True(t, peer.Status.LoginExpired, "the reset committed after the login, so the peer ends expired")
	stored, err := m.Store.GetUserByUserID(ctx, store.LockingStrengthShare, u.Id)
	require.NoError(t, err)
	assert.NotNil(t, stored.CredentialsValidAfter)
}
