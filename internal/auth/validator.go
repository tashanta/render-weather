// internal/auth/validator.go
package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/yourusername/render-weather/internal/background"
)

// KeyProvider interface for retrieving signing keys
type KeyProvider interface {
	GetKey(kid string) (*rsa.PublicKey, error)
	Ready() bool
}

// JWTValidator validates JWT tokens
type JWTValidator struct {
	keyProvider KeyProvider
	issuer      string
	audience    string
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(keyProvider KeyProvider, issuer, audience string) *JWTValidator {
	return &JWTValidator{
		keyProvider: keyProvider,
		issuer:      issuer,
		audience:    audience,
	}
}

// Validate parses and validates a JWT token string
func (v *JWTValidator) Validate(tokenString string) error {
	// Parse token with custom key function
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Extract kid from header
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, ErrMissingKid
		}

		// Get the key from provider
		key, err := v.keyProvider.GetKey(kid)
		if err != nil {
			return nil, err
		}

		return key, nil
	}, jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
	)

	if err != nil {
		return v.mapError(err)
	}

	if !token.Valid {
		return ErrInvalidClaims
	}

	return nil
}

// mapError converts jwt library errors to our domain errors
func (v *JWTValidator) mapError(err error) error {
	// Check for our custom errors first (they're wrapped)
	if errors.Is(err, ErrMissingKid) {
		return ErrMissingKid
	}
	if errors.Is(err, background.ErrJWKSNotReady) {
		return background.ErrJWKSNotReady
	}
	if errors.Is(err, background.ErrKeyNotFound) {
		return background.ErrKeyNotFound
	}

	// Check for jwt library errors
	if errors.Is(err, jwt.ErrTokenMalformed) {
		return fmt.Errorf("%w: %v", ErrTokenMalformed, err)
	}
	if errors.Is(err, jwt.ErrTokenExpired) {
		return fmt.Errorf("%w: %v", ErrTokenExpired, err)
	}
	if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	if errors.Is(err, jwt.ErrTokenInvalidIssuer) {
		return fmt.Errorf("%w: %v", ErrInvalidIssuer, err)
	}
	if errors.Is(err, jwt.ErrTokenInvalidAudience) {
		return fmt.Errorf("%w: %v", ErrInvalidAudience, err)
	}

	// For signature validation failures not caught above
	if errors.Is(err, rsa.ErrVerification) {
		return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}

	// Default: treat as malformed
	return fmt.Errorf("%w: %v", ErrTokenMalformed, err)
}
