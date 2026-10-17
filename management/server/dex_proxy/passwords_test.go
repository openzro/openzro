package dex_proxy

import (
	"context"
	"strings"
	"testing"

	apiv2 "github.com/dexidp/dex/api/v2"
	"github.com/stretchr/testify/require"
)

// Password-DB methods on the fake: keyed by lowercased email, like Dex.

func (f *fakeDex) CreatePassword(_ context.Context, req *apiv2.CreatePasswordReq) (*apiv2.CreatePasswordResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := req.GetPassword()
	for _, existing := range f.passwords {
		if strings.EqualFold(existing.GetEmail(), p.GetEmail()) {
			return &apiv2.CreatePasswordResp{AlreadyExists: true}, nil
		}
	}
	f.passwords = append(f.passwords, &apiv2.Password{
		Email: strings.ToLower(p.GetEmail()), Hash: p.GetHash(),
		Username: p.GetUsername(), UserId: p.GetUserId(),
	})
	return &apiv2.CreatePasswordResp{}, nil
}

func (f *fakeDex) UpdatePassword(_ context.Context, req *apiv2.UpdatePasswordReq) (*apiv2.UpdatePasswordResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.passwords {
		if strings.EqualFold(existing.GetEmail(), req.GetEmail()) {
			existing.Hash = req.GetNewHash()
			return &apiv2.UpdatePasswordResp{}, nil
		}
	}
	return &apiv2.UpdatePasswordResp{NotFound: true}, nil
}

func (f *fakeDex) ListPasswords(_ context.Context, _ *apiv2.ListPasswordReq) (*apiv2.ListPasswordResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*apiv2.Password, 0, len(f.passwords))
	for _, p := range f.passwords {
		// Dex never returns the hash from ListPasswords.
		out = append(out, &apiv2.Password{Email: p.GetEmail(), Username: p.GetUsername(), UserId: p.GetUserId()})
	}
	return &apiv2.ListPasswordResp{Passwords: out}, nil
}

func TestClient_CreateAndListPasswords(t *testing.T) {
	c, fake := newFakeClient(t)
	ctx := context.Background()

	require.NoError(t, c.CreatePassword(ctx, "Ana@Example.com", "ana", "uid-1", []byte("$2a$12$hash")))
	require.ErrorIs(t, c.CreatePassword(ctx, "ana@example.com", "ana", "uid-2", []byte("x")), ErrAlreadyExists)

	got, err := c.ListPasswords(ctx)
	require.NoError(t, err)
	require.Equal(t, []PasswordEntry{{Email: "ana@example.com", Username: "ana", UserID: "uid-1"}}, got)
	require.Equal(t, []byte("$2a$12$hash"), fake.passwords[0].GetHash())
}

func TestClient_UpdatePassword(t *testing.T) {
	c, fake := newFakeClient(t)
	ctx := context.Background()

	require.ErrorIs(t, c.UpdatePassword(ctx, "ana@example.com", []byte("x")), ErrNotFound)
	require.NoError(t, c.CreatePassword(ctx, "ana@example.com", "ana", "uid-1", []byte("old")))
	require.NoError(t, c.UpdatePassword(ctx, "ana@example.com", []byte("new")))
	require.Equal(t, []byte("new"), fake.passwords[0].GetHash())
}

func TestClient_PasswordArgsRequired(t *testing.T) {
	c, _ := newFakeClient(t)
	ctx := context.Background()
	require.Error(t, c.CreatePassword(ctx, "", "ana", "uid", []byte("h")))
	require.Error(t, c.CreatePassword(ctx, "a@b.c", "ana", "", []byte("h")))
	require.Error(t, c.CreatePassword(ctx, "a@b.c", "ana", "uid", nil))
	require.Error(t, c.UpdatePassword(ctx, "", []byte("h")))
}

func (f *fakeDex) DeletePassword(_ context.Context, req *apiv2.DeletePasswordReq) (*apiv2.DeletePasswordResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.passwords {
		if strings.EqualFold(p.GetEmail(), req.GetEmail()) {
			f.passwords = append(f.passwords[:i], f.passwords[i+1:]...)
			return &apiv2.DeletePasswordResp{}, nil
		}
	}
	return &apiv2.DeletePasswordResp{NotFound: true}, nil
}

func (f *fakeDex) ListRefresh(_ context.Context, req *apiv2.ListRefreshReq) (*apiv2.ListRefreshResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*apiv2.RefreshTokenRef
	for _, clientID := range f.refresh[req.GetUserId()] {
		out = append(out, &apiv2.RefreshTokenRef{Id: "r-" + clientID, ClientId: clientID})
	}
	return &apiv2.ListRefreshResp{RefreshTokens: out}, nil
}

func (f *fakeDex) RevokeRefresh(_ context.Context, req *apiv2.RevokeRefreshReq) (*apiv2.RevokeRefreshResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	clients := f.refresh[req.GetUserId()]
	for i, c := range clients {
		if c == req.GetClientId() {
			f.refresh[req.GetUserId()] = append(clients[:i], clients[i+1:]...)
			return &apiv2.RevokeRefreshResp{}, nil
		}
	}
	return &apiv2.RevokeRefreshResp{NotFound: true}, nil
}

func TestClient_DeletePassword(t *testing.T) {
	c, fake := newFakeClient(t)
	ctx := context.Background()
	require.ErrorIs(t, c.DeletePassword(ctx, "ana@example.com"), ErrNotFound)
	require.NoError(t, c.CreatePassword(ctx, "ana@example.com", "ana", "uid-1", []byte("h")))
	require.NoError(t, c.DeletePassword(ctx, "ana@example.com"))
	require.Empty(t, fake.passwords)
	require.Error(t, c.DeletePassword(ctx, ""))
}

// Revocation covers every client the user holds a refresh token for, and
// is idempotent: a token already gone is not an error.
func TestClient_RevokeAllRefreshTokens(t *testing.T) {
	c, fake := newFakeClient(t)
	ctx := context.Background()
	fake.refresh = map[string][]string{"sub-1": {"dashboard", "cli"}, "sub-2": {"dashboard"}}

	require.NoError(t, c.RevokeAllRefreshTokens(ctx, "sub-1"))
	require.Empty(t, fake.refresh["sub-1"])
	require.Equal(t, []string{"dashboard"}, fake.refresh["sub-2"], "other users are untouched")
	require.NoError(t, c.RevokeAllRefreshTokens(ctx, "sub-1"), "nothing left to revoke is fine")
	require.Error(t, c.RevokeAllRefreshTokens(ctx, ""))
}
