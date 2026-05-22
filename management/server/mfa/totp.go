package mfa

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"image/png"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// Issuer is the label shown on the authenticator app's enrollment
// list. We hard-code "openZro" so a user looking at a screen full of
// "Issuer: …" entries can find the right one regardless of which
// deployment they enrolled against. Multi-deployment users
// disambiguate via the account label (passed in NewTOTPKey below).
const Issuer = "openZro"

// totpDigits, totpPeriod, totpAlgorithm match the de facto
// authenticator-app standard (Google Authenticator, Authy, 1Password,
// Bitwarden all accept these): 6-digit codes, 30s period, SHA-1. The
// SHA-1 choice tracks RFC 6238 §1.2 — apps in the wild still default
// to SHA-1, and switching to SHA-256/512 trips silent failures on
// older apps that don't read the `algorithm` query parameter.
const (
	totpDigits    = otp.DigitsSix
	totpPeriod    = uint(30)
	totpAlgorithm = otp.AlgorithmSHA1
	totpSkew      = uint(1) // ±30s clock-skew window (one period)
	totpSecretLen = 20      // RFC 4226 §5.1 recommendation: 160 bits
)

// NewTOTPKey provisions a fresh TOTP shared secret bound to the
// given (accountName, userEmail) pair. The returned key carries:
//   - Secret()  — base32-encoded shared secret, persist this encrypted
//   - URL()     — otpauth:// URL for the QR code (frontend renders the
//     QR client-side; we don't ship a QR PNG endpoint to keep the
//     server stateless)
//
// accountName is shown as the second label in the authenticator app
// (operators see something like "openZro (acme.openzro.io)") so a
// user with multiple openZro deployments can tell them apart. We
// pull the DNSDomain from settings at the call site.
func NewTOTPKey(accountName, userEmail string) (*otp.Key, error) {
	if userEmail == "" {
		return nil, errors.New("mfa: NewTOTPKey requires a non-empty user email for the account-name label")
	}
	secret := make([]byte, totpSecretLen)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("mfa: rand.Read: %w", err)
	}
	return totp.Generate(totp.GenerateOpts{
		Issuer:      Issuer,
		AccountName: fmt.Sprintf("%s (%s)", userEmail, accountName),
		SecretSize:  totpSecretLen,
		Secret:      secret,
		Period:      totpPeriod,
		Digits:      totpDigits,
		Algorithm:   totpAlgorithm,
	})
}

// ValidateTOTP verifies a 6-digit code against the given base32
// secret, with ±30s clock-skew tolerance. Pure function — no
// state. Returns (true, nil) on success, (false, nil) on a wrong
// code, and (false, err) on configuration failure (e.g. malformed
// secret). The wrong-code case is distinguished from the
// configuration-error case so the caller can increment failed-
// attempts only on the former.
func ValidateTOTP(code, secretBase32 string) (bool, error) {
	if secretBase32 == "" {
		return false, errors.New("mfa: empty TOTP secret")
	}
	// pquerna/otp accepts unpadded base32 (RFC 4648 §6) which is
	// what `totp.Generate` emits via Key.Secret(). No further
	// massaging needed.
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretBase32); err != nil {
		return false, fmt.Errorf("mfa: malformed base32 secret: %w", err)
	}
	ok, err := totp.ValidateCustom(code, secretBase32, time.Now().UTC(), totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      totpSkew,
		Digits:    totpDigits,
		Algorithm: totpAlgorithm,
	})
	if err != nil {
		// pquerna returns an error on malformed `code` (non-digit
		// characters etc.) — surface as a wrong-code result so the
		// auth middleware doesn't turn user typo into a 5xx. The
		// staticcheck nilerr false-positive flags this on purpose;
		// the contract (false, nil) for "wrong code" is what the
		// caller relies on to increment failed_attempts rather than
		// returning 500.
		return false, nil //nolint:nilerr // see comment above
	}
	return ok, nil
}

// QRPNG renders the otpauth:// URL embedded in `key` as a 256×256
// PNG. Optional helper for any future server-rendered QR endpoint —
// the current dashboard uses qrcode.react client-side and only needs
// the URL — but provided here so the management binary doesn't
// depend on a client-side library for ops scripts / CLI enrollment.
//
// boombuler/barcode is pulled in transitively by pquerna/otp so this
// adds no new dependency.
func QRPNG(key *otp.Key) ([]byte, error) {
	img, err := key.Image(256, 256)
	if err != nil {
		return nil, fmt.Errorf("mfa: QR image: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("mfa: PNG encode: %w", err)
	}
	return buf.Bytes(), nil
}
