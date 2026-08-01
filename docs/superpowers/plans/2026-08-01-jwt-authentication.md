# JWT Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add JWT token validation with RS256 signature verification, configurable via AUTH_ENABLED environment variable.

**Architecture:** Separate JWTValidator component consumes keys from JWKSManager via KeyProvider interface. Auth middleware delegates validation to JWTValidator. When AUTH_ENABLED=false, no JWKS fetch and no token validation.

**Tech Stack:** Go 1.26, github.com/golang-jwt/jwt/v5, Chi router, zerolog

## Global Constraints

- Algorithm: RS256 only
- Error responses: Generic 401 "unauthorized" (no details exposed)
- TDD: Write failing test first, then implementation
- Commits: After each task completion

---

### Task 1: Add JWT Dependency

**Files:**
- Modify: `go.mod`

**Interfaces:**
- Consumes: None
- Produces: `github.com/golang-jwt/jwt/v5` available for import

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/golang-jwt/jwt/v5
```

- [ ] **Step 2: Verify installation**

```bash
go mod tidy
grep "golang-jwt/jwt/v5" go.mod
```

Expected: Line showing `github.com/golang-jwt/jwt/v5 v5.x.x`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add golang-jwt/jwt/v5 dependency"
```

---

### Task 2: Add AuthEnabled to Config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: None
- Produces: `Config.AuthEnabled bool` field, conditional validation of AUTH0_DOMAIN and AUTH0_AUDIENCE

- [ ] **Step 1: Write failing tests for AuthEnabled config**

Add to `internal/config/config_test.go`:

```go
func TestLoad_AuthEnabled_Default(t *testing.T) {
	// Set required vars for auth enabled (default)
	t.Setenv("AUTH0_DOMAIN", "test.auth0.com")
	t.Setenv("AUTH0_AUDIENCE", "https://api.test.com")
	t.Setenv("OPENWEATHER_API_KEY", "test-key")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	// AUTH_ENABLED not set - should default to true

	cfg, err := Load()

	require.NoError(t, err)
	assert.True(t, cfg.AuthEnabled)
}

func TestLoad_AuthEnabled_True(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("AUTH0_DOMAIN", "test.auth0.com")
	t.Setenv("AUTH0_AUDIENCE", "https://api.test.com")
	t.Setenv("OPENWEATHER_API_KEY", "test-key")
	t.Setenv("REDIS_URL", "redis://localhost:6379")

	cfg, err := Load()

	require.NoError(t, err)
	assert.True(t, cfg.AuthEnabled)
	assert.Equal(t, "test.auth0.com", cfg.Auth0Domain)
	assert.Equal(t, "https://api.test.com", cfg.Auth0Audience)
}

func TestLoad_AuthEnabled_RequiresAuth0Domain(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "true")
	// AUTH0_DOMAIN not set
	t.Setenv("AUTH0_AUDIENCE", "https://api.test.com")
	t.Setenv("OPENWEATHER_API_KEY", "test-key")
	t.Setenv("REDIS_URL", "redis://localhost:6379")

	_, err := Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AUTH0_DOMAIN")
}

func TestLoad_AuthEnabled_RequiresAuth0Audience(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("AUTH0_DOMAIN", "test.auth0.com")
	// AUTH0_AUDIENCE not set
	t.Setenv("OPENWEATHER_API_KEY", "test-key")
	t.Setenv("REDIS_URL", "redis://localhost:6379")

	_, err := Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AUTH0_AUDIENCE")
}

func TestLoad_AuthDisabled_NoAuth0VarsRequired(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "false")
	// AUTH0_DOMAIN and AUTH0_AUDIENCE not set
	t.Setenv("OPENWEATHER_API_KEY", "test-key")
	t.Setenv("REDIS_URL", "redis://localhost:6379")

	cfg, err := Load()

	require.NoError(t, err)
	assert.False(t, cfg.AuthEnabled)
	assert.Empty(t, cfg.Auth0Domain)
	assert.Empty(t, cfg.Auth0Audience)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/... -v -run "TestLoad_Auth"
```

Expected: FAIL - `AuthEnabled` field does not exist

- [ ] **Step 3: Implement AuthEnabled in config**

Modify `internal/config/config.go`:

Add `AuthEnabled` field to struct (after line 13):

```go
type Config struct {
	// Server
	Port string
	Env  string

	// Auth
	AuthEnabled   bool
	Auth0Domain   string
	Auth0Audience string

	// OpenWeatherMap
	OpenWeatherAPIKey string

	// Redis
	RedisURL string

	// CORS
	AllowedOrigins []string

	// Cache
	CacheTTL       time.Duration
	CacheL1MaxSize int

	// Circuit Breaker
	CBTimeout      time.Duration
	CBMaxFailures  int
	CBOpenDuration time.Duration

	// Logging
	LogLevel string
}
```

Replace the `Load()` function with conditional auth validation:

```go
func Load() (*Config, error) {
	cfg := &Config{
		Port:     getEnv("PORT", "8080"),
		Env:      getEnv("ENV", "production"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
	}

	// Auth configuration
	authEnabled := getEnv("AUTH_ENABLED", "true")
	cfg.AuthEnabled = authEnabled != "false"

	if cfg.AuthEnabled {
		cfg.Auth0Domain = os.Getenv("AUTH0_DOMAIN")
		if cfg.Auth0Domain == "" {
			return nil, fmt.Errorf("AUTH0_DOMAIN is required when AUTH_ENABLED=true")
		}

		cfg.Auth0Audience = os.Getenv("AUTH0_AUDIENCE")
		if cfg.Auth0Audience == "" {
			return nil, fmt.Errorf("AUTH0_AUDIENCE is required when AUTH_ENABLED=true")
		}
	}

	cfg.OpenWeatherAPIKey = os.Getenv("OPENWEATHER_API_KEY")
	if cfg.OpenWeatherAPIKey == "" {
		return nil, fmt.Errorf("OPENWEATHER_API_KEY is required")
	}

	cfg.RedisURL = os.Getenv("REDIS_URL")
	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}

	// CORS
	allowedOrigins := getEnv("ALLOWED_ORIGINS", "")
	if allowedOrigins != "" {
		cfg.AllowedOrigins = strings.Split(allowedOrigins, ",")
	}

	// Cache settings
	cacheTTL := getEnvInt("CACHE_TTL", 3600)
	cfg.CacheTTL = time.Duration(cacheTTL) * time.Second
	cfg.CacheL1MaxSize = getEnvInt("CACHE_L1_MAX_SIZE", 1000)

	// Circuit Breaker settings
	cbTimeout := getEnvInt("CB_TIMEOUT", 1000)
	cfg.CBTimeout = time.Duration(cbTimeout) * time.Millisecond
	cfg.CBMaxFailures = getEnvInt("CB_MAX_FAILURES", 5)
	cfg.CBOpenDuration = time.Duration(getEnvInt("CB_OPEN_DURATION", 30)) * time.Second

	return cfg, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/config/... -v -run "TestLoad_Auth"
```

Expected: All 5 new tests PASS

- [ ] **Step 5: Run all config tests**

```bash
go test ./internal/config/... -v
```

Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add AuthEnabled with conditional AUTH0_* validation"
```

---

### Task 3: Add Errors and KeyProvider Interface to JWKSManager

**Files:**
- Modify: `internal/background/jwks_loader.go`

**Interfaces:**
- Consumes: None
- Produces: 
  - `ErrJWKSNotReady` error
  - `ErrKeyNotFound` error  
  - `KeyProvider` interface with `GetKey(kid string) (*rsa.PublicKey, error)` and `Ready() bool`

- [ ] **Step 1: Add errors and interface**

Add at the top of `internal/background/jwks_loader.go` after the imports:

```go
import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog/log"
)

// Errors for JWKS operations
var (
	ErrJWKSNotReady = errors.New("JWKS not loaded yet")
	ErrKeyNotFound  = errors.New("key not found for kid")
)

// KeyProvider interface for retrieving signing keys
// Allows mocking JWKSManager in validator tests
type KeyProvider interface {
	GetKey(kid string) (*rsa.PublicKey, error)
	Ready() bool
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./internal/background/...
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/background/jwks_loader.go
git commit -m "feat(background): add KeyProvider interface and JWKS errors"
```

---

### Task 4: Implement GetKey with RSA Key Parsing

**Files:**
- Modify: `internal/background/jwks_loader.go`
- Modify: `internal/background/jwks_loader_test.go`

**Interfaces:**
- Consumes: `ErrJWKSNotReady`, `ErrKeyNotFound` from Task 3
- Produces: `JWKSManager.GetKey(kid string) (*rsa.PublicKey, error)` method, `JWKSManager` implements `KeyProvider`

- [ ] **Step 1: Write failing tests for GetKey**

Add to `internal/background/jwks_loader_test.go`:

```go
func TestJWKSManager_GetKey_NotReady(t *testing.T) {
	manager := NewJWKSManager("http://invalid.local")
	// Don't start - manager not ready

	_, err := manager.GetKey("some-kid")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWKSNotReady)
}

func TestJWKSManager_GetKey_NotFound(t *testing.T) {
	// Create a server that returns valid JWKS
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks := `{
			"keys": [{
				"kty": "RSA",
				"kid": "test-key-1",
				"use": "sig",
				"alg": "RS256",
				"n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
				"e": "AQAB"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwks))
	}))
	defer server.Close()

	manager := NewJWKSManager(server.URL)
	manager.Start()

	// Wait for JWKS to load
	time.Sleep(200 * time.Millisecond)

	_, err := manager.GetKey("non-existent-kid")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

func TestJWKSManager_GetKey_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks := `{
			"keys": [{
				"kty": "RSA",
				"kid": "test-key-1",
				"use": "sig",
				"alg": "RS256",
				"n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
				"e": "AQAB"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwks))
	}))
	defer server.Close()

	manager := NewJWKSManager(server.URL)
	manager.Start()

	// Wait for JWKS to load
	time.Sleep(200 * time.Millisecond)

	key, err := manager.GetKey("test-key-1")

	require.NoError(t, err)
	assert.NotNil(t, key)
	assert.IsType(t, &rsa.PublicKey{}, key)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/background/... -v -run "TestJWKSManager_GetKey"
```

Expected: FAIL - `GetKey` method doesn't exist or doesn't return `*rsa.PublicKey`

- [ ] **Step 3: Update JWKSManager struct to store parsed keys**

Modify `internal/background/jwks_loader.go`. Replace the struct and related code:

```go
// JWK represents a JSON Web Key
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JSONWebKeySet represents JWKS structure
type JSONWebKeySet struct {
	Keys []JWK `json:"keys"`
}

type JWKSManager struct {
	auth0Domain string
	keys        map[string]*rsa.PublicKey
	mu          sync.RWMutex
	ready       bool
	httpClient  *http.Client
}

func NewJWKSManager(auth0Domain string) *JWKSManager {
	return &JWKSManager{
		auth0Domain: auth0Domain,
		keys:        make(map[string]*rsa.PublicKey),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}
```

- [ ] **Step 4: Add RSA key parsing helper function**

Add to `internal/background/jwks_loader.go`:

```go
// parseRSAPublicKey converts JWK to *rsa.PublicKey
func parseRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	if jwk.Kty != "RSA" {
		return nil, fmt.Errorf("unsupported key type: %s", jwk.Kty)
	}

	// Decode modulus (n)
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)

	// Decode exponent (e)
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}
	// Convert exponent bytes to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}
```

- [ ] **Step 5: Update fetchJWKS to parse and store RSA keys**

Replace `fetchJWKS` method in `internal/background/jwks_loader.go`:

```go
func (m *JWKSManager) fetchJWKS() error {
	domain := m.auth0Domain
	// Add https:// if no protocol specified
	if !strings.HasPrefix(domain, "http://") && !strings.HasPrefix(domain, "https://") {
		domain = "https://" + domain
	}
	url := fmt.Sprintf("%s/.well-known/jwks.json", domain)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var jwks JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	// Parse keys and store by kid
	keys := make(map[string]*rsa.PublicKey)
	for _, jwk := range jwks.Keys {
		if jwk.Use != "sig" || jwk.Alg != "RS256" {
			continue // Skip non-signing keys
		}
		pubKey, err := parseRSAPublicKey(jwk)
		if err != nil {
			log.Warn().Err(err).Str("kid", jwk.Kid).Msg("failed to parse JWK, skipping")
			continue
		}
		keys[jwk.Kid] = pubKey
	}

	m.mu.Lock()
	m.keys = keys
	m.mu.Unlock()

	return nil
}
```

- [ ] **Step 6: Implement GetKey method**

Add to `internal/background/jwks_loader.go`:

```go
// GetKey returns the RSA public key for the given key ID
func (m *JWKSManager) GetKey(kid string) (*rsa.PublicKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.ready {
		return nil, ErrJWKSNotReady
	}

	key, exists := m.keys[kid]
	if !exists {
		return nil, ErrKeyNotFound
	}

	return key, nil
}
```

- [ ] **Step 7: Remove old GetJWKS method (no longer needed)**

Delete the `GetJWKS` method from `internal/background/jwks_loader.go` as it's replaced by `GetKey`.

- [ ] **Step 8: Run GetKey tests to verify they pass**

```bash
go test ./internal/background/... -v -run "TestJWKSManager_GetKey"
```

Expected: All 3 tests PASS

- [ ] **Step 9: Run all background tests**

```bash
go test ./internal/background/... -v
```

Expected: All tests PASS

- [ ] **Step 10: Commit**

```bash
git add internal/background/jwks_loader.go internal/background/jwks_loader_test.go
git commit -m "feat(background): implement GetKey with RSA key parsing"
```

---

### Task 5: Create JWTValidator with Errors

**Files:**
- Create: `internal/auth/errors.go`
- Create: `internal/auth/validator.go`
- Create: `internal/auth/validator_test.go`

**Interfaces:**
- Consumes: `KeyProvider` interface from Task 3, `ErrJWKSNotReady` from Task 3
- Produces: 
  - `JWTValidator` struct
  - `NewJWTValidator(keyProvider KeyProvider, issuer, audience string) *JWTValidator`
  - `Validate(tokenString string) error` method
  - `TokenValidator` interface

- [ ] **Step 1: Create errors file**

Create `internal/auth/errors.go`:

```go
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
```

- [ ] **Step 2: Write failing tests for JWTValidator**

Create `internal/auth/validator_test.go`:

```go
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
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/auth/... -v
```

Expected: FAIL - `NewJWTValidator` not defined

- [ ] **Step 4: Implement JWTValidator**

Create `internal/auth/validator.go`:

```go
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
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/auth/... -v
```

Expected: All 9 tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/auth/
git commit -m "feat(auth): add JWTValidator with RS256 signature verification"
```

---

### Task 6: Update Auth Middleware for JWT Validation

**Files:**
- Modify: `internal/middleware/auth.go`
- Modify: `internal/middleware/auth_test.go`

**Interfaces:**
- Consumes: `TokenValidator` interface from Task 5, `ErrJWKSNotReady` from Task 3
- Produces: Updated `Auth(validator TokenValidator) func(http.Handler) http.Handler`

- [ ] **Step 1: Write failing tests for updated Auth middleware**

Replace content of `internal/middleware/auth_test.go`:

```go
// internal/middleware/auth_test.go
package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/yourusername/render-weather/internal/background"
)

// mockTokenValidator implements auth.TokenValidator for testing
type mockTokenValidator struct {
	err error
}

func (m *mockTokenValidator) Validate(tokenString string) error {
	return m.err
}

func TestAuth_NoHeader(t *testing.T) {
	validator := &mockTokenValidator{err: nil}
	router := chi.NewRouter()
	router.Use(Auth(validator))
	router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No Authorization header
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "unauthorized")
}

func TestAuth_InvalidBearerFormat(t *testing.T) {
	validator := &mockTokenValidator{err: nil}
	router := chi.NewRouter()
	router.Use(Auth(validator))
	router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz") // Not Bearer
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "unauthorized")
}

func TestAuth_EmptyBearerToken(t *testing.T) {
	validator := &mockTokenValidator{err: nil}
	router := chi.NewRouter()
	router.Use(Auth(validator))
	router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "unauthorized")
}

func TestAuth_ValidToken(t *testing.T) {
	validator := &mockTokenValidator{err: nil} // Validation succeeds
	router := chi.NewRouter()
	router.Use(Auth(validator))
	router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid.jwt.token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `{"status":"ok"}`, rec.Body.String())
}

func TestAuth_InvalidToken(t *testing.T) {
	validator := &mockTokenValidator{err: errors.New("invalid token")}
	router := chi.NewRouter()
	router.Use(Auth(validator))
	router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid.jwt.token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "unauthorized")
}

func TestAuth_JWKSNotReady(t *testing.T) {
	validator := &mockTokenValidator{err: background.ErrJWKSNotReady}
	router := chi.NewRouter()
	router.Use(Auth(validator))
	router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer some.jwt.token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "service_unavailable")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/middleware/... -v -run "TestAuth_"
```

Expected: FAIL - Function signature mismatch

- [ ] **Step 3: Implement updated Auth middleware**

Replace content of `internal/middleware/auth.go`:

```go
// internal/middleware/auth.go
package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/yourusername/render-weather/internal/background"
)

// TokenValidator interface for validating JWT tokens
type TokenValidator interface {
	Validate(tokenString string) error
}

// Auth middleware validates JWT Bearer tokens
func Auth(validator TokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				unauthorized(w)
				return
			}

			// Check Bearer prefix
			if !strings.HasPrefix(authHeader, "Bearer ") {
				unauthorized(w)
				return
			}

			// Extract token
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == "" {
				unauthorized(w)
				return
			}

			// Validate token
			err := validator.Validate(token)
			if err != nil {
				if errors.Is(err, background.ErrJWKSNotReady) {
					serviceUnavailable(w)
					return
				}
				// Log the actual error but don't expose it
				log.Debug().Err(err).Msg("token validation failed")
				unauthorized(w)
				return
			}

			// Token valid, continue
			next.ServeHTTP(w, r)
		})
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, `{"error":"unauthorized"}`)
}

func serviceUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprint(w, `{"error":"service_unavailable"}`)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/middleware/... -v -run "TestAuth_"
```

Expected: All 6 tests PASS

- [ ] **Step 5: Run all middleware tests**

```bash
go test ./internal/middleware/... -v
```

Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/middleware/auth.go internal/middleware/auth_test.go
git commit -m "feat(middleware): implement JWT Bearer token validation"
```

---

### Task 7: Update main.go with Conditional Auth Wiring

**Files:**
- Modify: `cmd/api/main.go`

**Interfaces:**
- Consumes: 
  - `Config.AuthEnabled` from Task 2
  - `JWKSManager` from Task 4
  - `JWTValidator` from Task 5
  - `Auth` middleware from Task 6
- Produces: Conditional auth middleware setup based on AUTH_ENABLED

- [ ] **Step 1: Update imports in main.go**

Add the auth import to `cmd/api/main.go`:

```go
import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/yourusername/render-weather/internal/auth"
	"github.com/yourusername/render-weather/internal/background"
	"github.com/yourusername/render-weather/internal/cache"
	"github.com/yourusername/render-weather/internal/config"
	"github.com/yourusername/render-weather/internal/handlers"
	"github.com/yourusername/render-weather/internal/middleware"
	"github.com/yourusername/render-weather/internal/providers"
	"github.com/yourusername/render-weather/internal/services"
)
```

- [ ] **Step 2: Replace JWKS and auth setup with conditional logic**

Replace the JWKS manager and auth setup section (around lines 66-103) in `cmd/api/main.go` with:

```go
	// 6. Start background cache loader goroutine
	background.StartCacheLoader(hybridCache)
	log.Info().Msg("background cache loader started")

	// 7. Setup auth middleware (conditional)
	var authMiddleware func(http.Handler) http.Handler

	if cfg.AuthEnabled {
		// Create JWKS manager
		jwksManager := background.NewJWKSManager(cfg.Auth0Domain)
		log.Info().Str("domain", cfg.Auth0Domain).Msg("JWKS manager initialized")

		// Start JWKS loader goroutine
		jwksManager.Start()
		log.Info().Msg("JWKS loader started")

		// Create JWT validator
		issuer := fmt.Sprintf("https://%s/", cfg.Auth0Domain)
		validator := auth.NewJWTValidator(jwksManager, issuer, cfg.Auth0Audience)
		log.Info().Str("issuer", issuer).Str("audience", cfg.Auth0Audience).Msg("JWT validator initialized")

		// Create auth middleware
		authMiddleware = middleware.Auth(validator)
	} else {
		log.Warn().Msg("authentication disabled (AUTH_ENABLED=false)")
		// Pass-through middleware (no-op)
		authMiddleware = func(next http.Handler) http.Handler {
			return next
		}
	}

	// 8. Create OpenWeatherMap provider
	owmProvider := providers.NewOpenWeatherMapProvider(cfg.OpenWeatherAPIKey, 5*time.Second)
	log.Info().Msg("OpenWeatherMap provider initialized")

	// 9. Create weather service with circuit breaker
	weatherService := services.NewWeatherService(
		owmProvider,
		hybridCache,
		5*time.Second,  // timeout
		5,              // max failures
		30*time.Second, // open duration
		1*time.Hour,    // cache TTL
	)
	log.Info().Msg("weather service initialized with circuit breaker")

	// 10. Setup Chi router with middleware
	router := chi.NewRouter()
	router.Use(middleware.Recovery())
	router.Use(middleware.Logging())
	router.Use(middleware.CORS(cfg.AllowedOrigins))

	// 11. Register public routes (no auth)
	router.Get("/health", handlers.HealthHandler())

	// 12. Register protected routes (with auth)
	router.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		r.Get("/weather/{city}", handlers.WeatherHandler(weatherService))
		r.Get("/api/v1/weather/{city}", handlers.WeatherHandler(weatherService))
	})

	log.Info().Msg("routes registered: /health, /weather/{city}, /api/v1/weather/{city}")

	// 13. Start HTTP server
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./cmd/api/...
```

Expected: No errors

- [ ] **Step 4: Run all tests to ensure nothing broke**

```bash
go test ./... -v
```

Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/api/main.go
git commit -m "feat(main): add conditional auth wiring based on AUTH_ENABLED"
```

---

### Task 8: Update README Documentation

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: None
- Produces: Documentation for authentication configuration

- [ ] **Step 1: Read current README**

```bash
head -100 README.md
```

- [ ] **Step 2: Add Authentication section to README**

Add the following section after the existing configuration/environment variables section in `README.md`:

```markdown
## Authentication

The API uses JWT Bearer tokens for authentication (OAuth 2.0 client_credentials flow).

### Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AUTH_ENABLED` | No | `true` | Enable/disable authentication |
| `AUTH0_DOMAIN` | If auth enabled | - | OAuth provider domain (e.g., `tenant.eu.auth0.com`) |
| `AUTH0_AUDIENCE` | If auth enabled | - | Expected token audience |

### Token Validation

When enabled, the middleware validates:
- **Signature** (RS256) against JWKS public keys
- **Issuer** (`iss`) matches `https://{AUTH0_DOMAIN}/`
- **Audience** (`aud`) contains the expected value
- **Expiration** (`exp`) is in the future

### Disabling Authentication (Development)

Set `AUTH_ENABLED=false` to bypass authentication entirely:

```bash
AUTH_ENABLED=false go run cmd/api/main.go
```

> **Warning:** Never disable authentication in production.

### Error Responses

| Status | Meaning |
|--------|---------|
| 401 Unauthorized | Missing or invalid token |
| 503 Service Unavailable | JWKS not loaded yet (retry later) |

### Usage Example

```bash
# Get a token from your OAuth provider (client_credentials flow)
TOKEN=$(curl -s --request POST \
  --url "https://${AUTH0_DOMAIN}/oauth/token" \
  --header 'content-type: application/json' \
  --data "{\"client_id\":\"${CLIENT_ID}\",\"client_secret\":\"${CLIENT_SECRET}\",\"audience\":\"${AUTH0_AUDIENCE}\",\"grant_type\":\"client_credentials\"}" \
  | jq -r '.access_token')

# Call the API with the token
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/weather/paris
```
```

- [ ] **Step 3: Update environment variables section**

Add `AUTH_ENABLED` to the existing environment variables table in `README.md` if there is one.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: add authentication section to README"
```

---

### Task 9: Update .env.example

**Files:**
- Modify: `.env.example`

**Interfaces:**
- Consumes: None
- Produces: Updated example environment file with AUTH_ENABLED

- [ ] **Step 1: Read current .env.example**

```bash
cat .env.example
```

- [ ] **Step 2: Add AUTH_ENABLED to .env.example**

Add the following line to `.env.example` (near the AUTH0 variables):

```bash
# Authentication (set to false to disable JWT validation in development)
AUTH_ENABLED=true
```

- [ ] **Step 3: Commit**

```bash
git add .env.example
git commit -m "docs: add AUTH_ENABLED to .env.example"
```

---

### Task 10: Final Verification

**Files:**
- None (verification only)

**Interfaces:**
- Consumes: All previous tasks
- Produces: Verified working implementation

- [ ] **Step 1: Run all tests**

```bash
go test ./... -v
```

Expected: All tests PASS

- [ ] **Step 2: Run tests with race detector**

```bash
go test ./... -race
```

Expected: No race conditions detected

- [ ] **Step 3: Run linter**

```bash
golangci-lint run
```

Expected: No errors

- [ ] **Step 4: Build the application**

```bash
go build -o bin/api cmd/api/main.go
```

Expected: Binary created successfully

- [ ] **Step 5: Verify auth disabled mode works**

```bash
AUTH_ENABLED=false OPENWEATHER_API_KEY=test REDIS_URL=redis://localhost:6379 go run cmd/api/main.go &
sleep 2
curl -s http://localhost:8080/health
pkill -f "go run cmd/api"
```

Expected: Health endpoint responds without auth

- [ ] **Step 6: Final commit (if any uncommitted changes)**

```bash
git status
# If clean, skip this step
```
