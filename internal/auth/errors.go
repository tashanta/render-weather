// internal/auth/errors.go
package auth

import "errors"

// Validation errors (logged internally, not exposed to clients)
var (
	ErrTokenMalformed   = errors.New("malformed token")
	ErrTokenExpired     = errors.New("token expired")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrInvalidIssuer    = errors.New("invalid issuer")
	ErrInvalidAudience  = errors.New("invalid audience")
	ErrInvalidClaims    = errors.New("invalid claims")
	ErrMissingKid       = errors.New("missing kid in token header")
)

// TokenValidator interface for the auth middleware
type TokenValidator interface {
	Validate(tokenString string) error
}
