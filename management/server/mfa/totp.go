package mfa

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
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
// secret, with ±30s clock-skew tolerance. Returns (true, nil) on success,
// (false, nil) on a wrong code, and (false, err) on configuration failure
// (e.g. malformed secret), so the caller increments failed attempts only
// on the former. It does not stop a code from being used twice; callers
// that persist state use MatchTOTPStep.
func ValidateTOTP(code, secretBase32 string) (bool, error) {
	_, ok, err := MatchTOTPStep(code, secretBase32, time.Now().UTC())
	return ok, err
}

// MatchTOTPStep checks a 6-digit code at `now` within the ±1 step clock-skew
// window and returns the time step (Unix seconds / 30) it belongs to. The
// caller refuses a step at or below the last one it accepted, so a code
// cannot be used twice (RFC 6238 §5.2). Every candidate step is compared in
// constant time; if a code matches more than one, the latest is returned.
// A malformed code is a wrong code: (0, false, nil). A malformed secret is
// a configuration error.
func MatchTOTPStep(code, secretBase32 string, now time.Time) (uint64, bool, error) {
	if secretBase32 == "" {
		return 0, false, errors.New("mfa: empty TOTP secret")
	}
	// pquerna/otp accepts unpadded base32 (RFC 4648 §6), which is what
	// totp.Generate emits via Key.Secret().
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretBase32); err != nil {
		return 0, false, fmt.Errorf("mfa: malformed base32 secret: %w", err)
	}
	if len(code) != int(totpDigits) {
		return 0, false, nil
	}

	current := uint64(now.Unix()) / uint64(totpPeriod)
	var matched uint64
	var ok bool
	for s := current - uint64(totpSkew); s <= current+uint64(totpSkew); s++ {
		want, err := totp.GenerateCodeCustom(secretBase32, time.Unix(int64(s*uint64(totpPeriod)), 0).UTC(),
			totp.ValidateOpts{Period: totpPeriod, Digits: totpDigits, Algorithm: totpAlgorithm})
		if err != nil {
			return 0, false, fmt.Errorf("mfa: generate code: %w", err)
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			matched, ok = s, true
		}
	}
	return matched, ok, nil
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
