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
