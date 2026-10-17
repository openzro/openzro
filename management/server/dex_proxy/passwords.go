package dex_proxy

import (
	"context"
	"errors"

	apiv2 "github.com/dexidp/dex/api/v2"
)

// PasswordEntry is one entry of Dex's password DB as ListPasswords reports
// it. Dex never returns the hash. Static (config-file) entries are listed
// too and are read-only in Dex.
type PasswordEntry struct {
	Email    string
	Username string
	UserID   string
}

// CreatePassword adds a password-DB entry (ADR-0024). hash must be a bcrypt
// hash: Dex does not accept plaintext. Returns ErrAlreadyExists when an
// entry for the email (compared case-insensitively by Dex) already exists.
func (c *Client) CreatePassword(ctx context.Context, email, username, userID string, hash []byte) error {
	if email == "" || userID == "" || len(hash) == 0 {
		return errors.New("dex_proxy: email, user id and hash are required")
	}
	resp, err := c.dex.CreatePassword(ctx, &apiv2.CreatePasswordReq{Password: &apiv2.Password{
		Email:    email,
		Hash:     hash,
		Username: username,
		UserId:   userID,
	}})
	if err != nil {
		return mapErr(err)
	}
	if resp.GetAlreadyExists() {
		return ErrAlreadyExists
	}
	return nil
}

// UpdatePassword replaces the hash of the entry for email. Dex keys the
// update by email only and never changes user_id, so callers must verify
// ownership through ListPasswords first (ADR-0024 D5). Returns ErrNotFound
// when no entry matches.
func (c *Client) UpdatePassword(ctx context.Context, email string, hash []byte) error {
	if email == "" || len(hash) == 0 {
		return errors.New("dex_proxy: email and hash are required")
	}
	resp, err := c.dex.UpdatePassword(ctx, &apiv2.UpdatePasswordReq{Email: email, NewHash: hash})
	if err != nil {
		return mapErr(err)
	}
	if resp.GetNotFound() {
		return ErrNotFound
	}
	return nil
}

// ListPasswords returns every password-DB entry, static ones included.
func (c *Client) ListPasswords(ctx context.Context) ([]PasswordEntry, error) {
	resp, err := c.dex.ListPasswords(ctx, &apiv2.ListPasswordReq{})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]PasswordEntry, 0, len(resp.GetPasswords()))
	for _, p := range resp.GetPasswords() {
		out = append(out, PasswordEntry{Email: p.GetEmail(), Username: p.GetUsername(), UserID: p.GetUserId()})
	}
	return out, nil
}

// DeletePassword removes the entry for email. Like UpdatePassword it is
// keyed by email only, so callers verify ownership first. Returns
// ErrNotFound when no entry matches.
func (c *Client) DeletePassword(ctx context.Context, email string) error {
	if email == "" {
		return errors.New("dex_proxy: email is required")
	}
	resp, err := c.dex.DeletePassword(ctx, &apiv2.DeletePasswordReq{Email: email})
	if err != nil {
		return mapErr(err)
	}
	if resp.GetNotFound() {
		return ErrNotFound
	}
	return nil
}

// RevokeAllRefreshTokens revokes every refresh token Dex holds for the
// user, across all clients, so no session can be renewed. sub is the token
// subject (the openZro user ID). Access tokens already issued stay valid
// until they expire. A token that disappears between the list and the
// revoke is not an error.
func (c *Client) RevokeAllRefreshTokens(ctx context.Context, sub string) error {
	if sub == "" {
		return errors.New("dex_proxy: subject is required")
	}
	resp, err := c.dex.ListRefresh(ctx, &apiv2.ListRefreshReq{UserId: sub})
	if err != nil {
		return mapErr(err)
	}
	for _, ref := range resp.GetRefreshTokens() {
		if _, err := c.dex.RevokeRefresh(ctx, &apiv2.RevokeRefreshReq{UserId: sub, ClientId: ref.GetClientId()}); err != nil {
			return mapErr(err)
		}
	}
	return nil
}
