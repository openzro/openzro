package flow_exports

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// FieldEncrypt is an AES-256-GCM helper for protecting credential
// material at rest. It is functionally identical to the FieldEncrypt
// in management/server/activity/store/crypt.go but lives in this
// package to avoid a cross-package import that would force a refactor
// of the activity store. When that refactor lands, both can share a
// single implementation.
type FieldEncrypt struct {
	gcm cipher.AEAD
}

// NewFieldEncrypt builds a FieldEncrypt from the base64-encoded
// 32-byte key the management already holds in
// HttpConfig.DataStoreEncryptionKey.
func NewFieldEncrypt(key string) (*FieldEncrypt, error) {
	if key == "" {
		return nil, errors.New("flow_exports: encryption key is required")
	}
	binKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("flow_exports: decode key: %w", err)
	}
	if len(binKey) != 32 {
		return nil, fmt.Errorf("flow_exports: encryption key must be 32 bytes after base64 decode (got %d)", len(binKey))
	}

	block, err := aes.NewCipher(binKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &FieldEncrypt{gcm: gcm}, nil
}

// Encrypt seals the plaintext into nonce|ciphertext|tag, base64-
// encoded. Each call uses a fresh random nonce.
func (f *FieldEncrypt) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, f.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := f.gcm.Seal(nonce, nonce, plaintext, nil)
	out := make([]byte, base64.StdEncoding.EncodedLen(len(sealed)))
	base64.StdEncoding.Encode(out, sealed)
	return out, nil
}

// Decrypt reverses Encrypt. Returns an error if the ciphertext is
// truncated, the nonce was reused with a different key, or the
// authentication tag fails.
func (f *FieldEncrypt) Decrypt(envelope []byte) ([]byte, error) {
	sealed := make([]byte, base64.StdEncoding.DecodedLen(len(envelope)))
	n, err := base64.StdEncoding.Decode(sealed, envelope)
	if err != nil {
		return nil, fmt.Errorf("flow_exports: decode envelope: %w", err)
	}
	sealed = sealed[:n]

	nonceSize := f.gcm.NonceSize()
	if len(sealed) < nonceSize {
		return nil, errors.New("flow_exports: ciphertext too short")
	}
	nonce, ciphertext := sealed[:nonceSize], sealed[nonceSize:]

	plaintext, err := f.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("flow_exports: decrypt: %w", err)
	}
	return plaintext, nil
}
