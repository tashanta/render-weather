// internal/auth/validator_test.go
package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourusername/render-weather/internal/background"
)

// mockKeyProvider implements background.KeyProvider for testing
type mockKeyProvider struct {
	keys  map[string]*rsa.PublicKey
	ready bool
}

func (m *mockKeyProvider) GetKey(kid string) (*rsa.PublicKey, error) {
	if !m.ready {
		return nil, background.ErrJWKSNotReady
	}
	key, exists := m.keys[kid]
	if !exists {
		return nil, background.ErrKeyNotFound
	}
	return key, nil
}

func (m *mockKeyProvider) Ready() bool {
	return m.ready
}

// generateTestKey creates an RSA key pair for testing
func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

// createTestToken creates a signed JWT for testing
func createTestToken(t *testing.T, key *rsa.PrivateKey, kid, issuer string, audience []string, expiry time.Time) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
		"exp": expiry.Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	tokenString, err := token.SignedString(key)
	require.NoError(t, err)
	return tokenString
}

func TestValidate_ValidToken(t *testing.T) {
	privateKey := generateTestKey(t)
	kid := "test-key-1"
	issuer := "https://test.auth0.com/"
	audience := "https://api.test.com"

	provider := &mockKeyProvider{
		keys:  map[string]*rsa.PublicKey{kid: &privateKey.PublicKey},
		ready: true,
	}

	validator := NewJWTValidator(provider, issuer, audience)
	token := createTestToken(t, privateKey, kid, issuer, []string{audience}, time.Now().Add(1*time.Hour))

	err := validator.Validate(token)

	assert.NoError(t, err)
}

func TestValidate_ExpiredToken(t *testing.T) {
	privateKey := generateTestKey(t)
	kid := "test-key-1"
	issuer := "https://test.auth0.com/"
	audience := "https://api.test.com"

	provider := &mockKeyProvider{
		keys:  map[string]*rsa.PublicKey{kid: &privateKey.PublicKey},
		ready: true,
	}

	validator := NewJWTValidator(provider, issuer, audience)
	token := createTestToken(t, privateKey, kid, issuer, []string{audience}, time.Now().Add(-1*time.Hour))

	err := validator.Validate(token)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestValidate_InvalidSignature(t *testing.T) {
	privateKey := generateTestKey(t)
	otherKey := generateTestKey(t) // Different key
	kid := "test-key-1"
	issuer := "https://test.auth0.com/"
	audience := "https://api.test.com"

	provider := &mockKeyProvider{
		keys:  map[string]*rsa.PublicKey{kid: &otherKey.PublicKey}, // Wrong public key
		ready: true,
	}

	validator := NewJWTValidator(provider, issuer, audience)
	token := createTestToken(t, privateKey, kid, issuer, []string{audience}, time.Now().Add(1*time.Hour))

	err := validator.Validate(token)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidSignature)
}

func TestValidate_InvalidIssuer(t *testing.T) {
	privateKey := generateTestKey(t)
	kid := "test-key-1"
	issuer := "https://test.auth0.com/"
	wrongIssuer := "https://wrong.auth0.com/"
	audience := "https://api.test.com"

	provider := &mockKeyProvider{
		keys:  map[string]*rsa.PublicKey{kid: &privateKey.PublicKey},
		ready: true,
	}

	validator := NewJWTValidator(provider, issuer, audience)
	token := createTestToken(t, privateKey, kid, wrongIssuer, []string{audience}, time.Now().Add(1*time.Hour))

	err := validator.Validate(token)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidIssuer)
}

func TestValidate_InvalidAudience(t *testing.T) {
	privateKey := generateTestKey(t)
	kid := "test-key-1"
	issuer := "https://test.auth0.com/"
	audience := "https://api.test.com"
	wrongAudience := "https://wrong.api.com"

	provider := &mockKeyProvider{
		keys:  map[string]*rsa.PublicKey{kid: &privateKey.PublicKey},
		ready: true,
	}

	validator := NewJWTValidator(provider, issuer, audience)
	token := createTestToken(t, privateKey, kid, issuer, []string{wrongAudience}, time.Now().Add(1*time.Hour))

	err := validator.Validate(token)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAudience)
}

func TestValidate_MalformedToken(t *testing.T) {
	provider := &mockKeyProvider{
		keys:  map[string]*rsa.PublicKey{},
		ready: true,
	}

	validator := NewJWTValidator(provider, "https://test.auth0.com/", "https://api.test.com")

	err := validator.Validate("not.a.valid.token")

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenMalformed)
}

func TestValidate_KeyNotFound(t *testing.T) {
	privateKey := generateTestKey(t)
	kid := "unknown-key"
	issuer := "https://test.auth0.com/"
	audience := "https://api.test.com"

	provider := &mockKeyProvider{
		keys:  map[string]*rsa.PublicKey{}, // No keys
		ready: true,
	}

	validator := NewJWTValidator(provider, issuer, audience)
	token := createTestToken(t, privateKey, kid, issuer, []string{audience}, time.Now().Add(1*time.Hour))

	err := validator.Validate(token)

	assert.Error(t, err)
	assert.ErrorIs(t, err, background.ErrKeyNotFound)
}

func TestValidate_JWKSNotReady(t *testing.T) {
	privateKey := generateTestKey(t)
	kid := "test-key-1"
	issuer := "https://test.auth0.com/"
	audience := "https://api.test.com"

	provider := &mockKeyProvider{
		keys:  map[string]*rsa.PublicKey{kid: &privateKey.PublicKey},
		ready: false, // Not ready
	}

	validator := NewJWTValidator(provider, issuer, audience)
	token := createTestToken(t, privateKey, kid, issuer, []string{audience}, time.Now().Add(1*time.Hour))

	err := validator.Validate(token)

	assert.Error(t, err)
	assert.ErrorIs(t, err, background.ErrJWKSNotReady)
}

func TestValidate_MissingKid(t *testing.T) {
	privateKey := generateTestKey(t)
	issuer := "https://test.auth0.com/"
	audience := "https://api.test.com"

	provider := &mockKeyProvider{
		keys:  map[string]*rsa.PublicKey{"test-key": &privateKey.PublicKey},
		ready: true,
	}

	// Create token without kid in header
	claims := jwt.MapClaims{
		"iss": issuer,
		"aud": []string{audience},
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// Don't set kid: token.Header["kid"] = kid
	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)

	validator := NewJWTValidator(provider, issuer, audience)

	err = validator.Validate(tokenString)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrMissingKid)
}
