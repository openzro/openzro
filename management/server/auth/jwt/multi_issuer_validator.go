package jwt

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"github.com/openzro/openzro/management/server/auth/providers"
)

// ErrUnknownIssuer is returned when an incoming token's `iss`
// claim doesn't match any configured AuthenticationProvider AND
// no legacy single-issuer fallback is wired up.
var ErrUnknownIssuer = errors.New("jwt: unknown issuer")

// FallbackValidator is the contract MultiIssuerValidator expects
// from the legacy single-issuer path. *Validator satisfies it
// directly; tests substitute stubs to assert the fallback is
// reached without standing up a real keys-location HTTP server.
type FallbackValidator interface {
	ValidateAndParse(ctx context.Context, raw string) (*jwt.Token, error)
}

// MultiIssuerValidator routes incoming JWTs to the right OIDC
// verifier based on their `iss` claim. The providers.Manager is
// the source of truth for which issuers are trusted; this
// validator queries it on every call so admin mutations are live
// without a restart.
//
// A legacy fallback may be supplied: when the token's `iss`
// doesn't match any configured provider, the validator falls back
// to it. That keeps existing single-IdP deployments working
// unchanged during the transition to centralized login.
//
// See ADR-0005 for the design rationale.
type MultiIssuerValidator struct {
	manager *providers.Manager
	legacy  FallbackValidator
}

// NewMultiIssuerValidator wires the validator. legacy is
// optional; pass nil for deployments that have already migrated
// to the AuthenticationProvider table as the only source of
// trusted issuers.
func NewMultiIssuerValidator(manager *providers.Manager, legacy FallbackValidator) *MultiIssuerValidator {
	return &MultiIssuerValidator{manager: manager, legacy: legacy}
}

// ValidateAndParse extracts the `iss` claim from the unverified
// token payload, picks the right verifier from the manager, and
// verifies the token. Falls back to the legacy validator when
// the issuer is unknown.
//
// Returns a *jwt.Token compatible with the existing single-issuer
// Validator's contract so callers (HTTP middleware, claim
// extractors) don't need to change.
func (v *MultiIssuerValidator) ValidateAndParse(ctx context.Context, raw string) (*jwt.Token, error) {
	if raw == "" {
		return nil, errTokenEmpty
	}

	iss, err := unsafeExtractIssuer(raw)
	if err != nil {
		// Fall back to the legacy validator: it parses the token
		// itself, so a malformed-payload token still gets a
		// consistent error from there.
		if v.legacy != nil {
			return v.legacy.ValidateAndParse(ctx, raw)
		}
		return nil, fmt.Errorf("%w: %s", errTokenParsing, err)
	}

	if live, ok := v.manager.GetByIssuer(iss); ok {
		if live.Verifier == nil {
			// Configured provider doesn't issue ID tokens (e.g.
			// GitHub OAuth). Tokens claiming this issuer cannot be
			// verified through the OIDC path.
			return nil, fmt.Errorf("%w: provider for issuer %q does not issue verifiable id tokens",
				errTokenInvalid, iss)
		}
		idTok, err := live.Verifier.Verify(ctx, raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", errTokenInvalid, err)
		}
		var claims jwt.MapClaims
		if err := idTok.Claims(&claims); err != nil {
			return nil, fmt.Errorf("%w: %s", errTokenParsing, err)
		}
		return &jwt.Token{
			Raw:    raw,
			Claims: claims,
			Valid:  true,
		}, nil
	}

	if v.legacy != nil {
		return v.legacy.ValidateAndParse(ctx, raw)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownIssuer, iss)
}

// unsafeExtractIssuer parses the JWT payload WITHOUT verifying
// the signature, just to read the `iss` claim. The result is used
// only to pick which configured verifier handles the full
// signature verification. Marked "unsafe" to deter callers from
// trusting the issuer for anything beyond verifier selection.
func unsafeExtractIssuer(raw string) (string, error) {
	var claims jwt.MapClaims
	parser := jwt.NewParser()
	if _, _, err := parser.ParseUnverified(raw, &claims); err != nil {
		return "", err
	}
	iss, _ := claims["iss"].(string)
	if iss == "" {
		return "", errors.New("missing iss claim")
	}
	return iss, nil
}
