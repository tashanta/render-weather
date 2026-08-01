# JWT Authentication Design

**Date**: 2026-08-01  
**Status**: Approved  
**Scope**: Add JWT token validation to the Weather API with optional disable via environment variable

## Context

Currently, the authentication middleware only checks if the JWKS is loaded but does not validate JWT tokens. The project needs:

- Full JWT validation (signature, issuer, audience, expiration)
- OAuth 2.0 client_credentials flow support (no login/callback routes)
- Ability to disable authentication entirely via `AUTH_ENABLED=false`
- Clear separation between JWKS management and token validation logic

## Decisions

- **Library**: `github.com/golang-jwt/jwt/v5` (standard, no vendor lock-in)
- **Algorithm**: RS256 only (OAuth/Auth0 standard)
- **Error responses**: Generic 401 Unauthorized (no details exposed to attackers)
- **Architecture**: Separate `JWTValidator` component from `JWKSManager`

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         main.go                                  │
│                                                                  │
│  if AUTH_ENABLED=true:                                          │
│    ├── JWKSManager.Start()    → fetch + refresh JWKS            │
│    ├── JWTValidator(jwksManager, issuer, audience)              │
│    └── router.Use(Auth(validator))                              │
│                                                                  │
│  if AUTH_ENABLED=false:                                         │
│    └── (no auth middleware, no JWKS fetch)                      │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────┐      ┌─────────────────────┐
│    JWKSManager      │      │    JWTValidator     │
│  (background/)      │◄─────│  (auth/)            │
├─────────────────────┤      ├─────────────────────┤
│ - Fetch JWKS        │      │ - Parse token       │
│ - Periodic refresh  │      │ - Validate claims   │
│ - Store keys        │      │   (iss, aud, exp)   │
│ - GetKey(kid)       │      │ - Verify signature  │
│ - Ready()           │      │ - Validate(token)   │
└─────────────────────┘      └─────────────────────┘
           │                           │
           │                           ▼
           │                 ┌─────────────────────┐
           │                 │   Auth Middleware   │
           │                 │   (middleware/)     │
           │                 ├─────────────────────┤
           │                 │ - Extract Bearer    │
           │                 │ - Call Validate()   │
           │                 │ - 401 if invalid    │
           │                 └─────────────────────┘
```

**New package**: `internal/auth/` for `JWTValidator` (separate from `background/` which manages JWKS lifecycle).

## Components

### 1. Configuration (`internal/config/config.go`)

New fields:

```go
type Config struct {
    // ... existing ...
    
    AuthEnabled   bool   // AUTH_ENABLED (default: true)
    Auth0Domain   string // AUTH0_DOMAIN (required if AuthEnabled)
    Auth0Audience string // AUTH0_AUDIENCE (required if AuthEnabled)
}
```

Loading logic:
- `AUTH_ENABLED` not set or `"true"` → `AuthEnabled = true`, AUTH0_* required
- `AUTH_ENABLED = "false"` → `AuthEnabled = false`, AUTH0_* ignored

### 2. JWKSManager Evolution (`internal/background/jwks_loader.go`)

New internal structure (keys stored as parsed RSA):

```go
type JWKSManager struct {
    auth0Domain string
    keys        map[string]*rsa.PublicKey  // kid → parsed RSA key
    mu          sync.RWMutex
    ready       bool
    httpClient  *http.Client
}
```

New method:

```go
func (m *JWKSManager) GetKey(kid string) (*rsa.PublicKey, error)
```

Errors:

```go
var (
    ErrJWKSNotReady = errors.New("JWKS not loaded yet")
    ErrKeyNotFound  = errors.New("key not found for kid")
)
```

Interface for decoupling:

```go
type KeyProvider interface {
    GetKey(kid string) (*rsa.PublicKey, error)
    Ready() bool
}
```

### 3. JWTValidator (`internal/auth/validator.go`)

New file:

```go
type JWTValidator struct {
    keyProvider KeyProvider
    issuer      string
    audience    string
}

func NewJWTValidator(keyProvider KeyProvider, issuer, audience string) *JWTValidator

func (v *JWTValidator) Validate(tokenString string) error
```

Validation steps:
1. Parse token without signature verification (extract header for `kid`)
2. Get key via `keyProvider.GetKey(kid)`
3. Verify RS256 signature with public key
4. Validate claims:
   - `iss` == expected issuer
   - `aud` contains expected audience (can be array)
   - `exp` > now (not expired)
   - `iat` <= now (not issued in future)

Internal errors (logged, not exposed):

```go
var (
    ErrTokenMalformed   = errors.New("malformed token")
    ErrTokenExpired     = errors.New("token expired")
    ErrInvalidSignature = errors.New("invalid signature")
    ErrInvalidIssuer    = errors.New("invalid issuer")
    ErrInvalidAudience  = errors.New("invalid audience")
)
```

Interface for middleware:

```go
type TokenValidator interface {
    Validate(tokenString string) error
}
```

### 4. Auth Middleware (`internal/middleware/auth.go`)

New signature:

```go
func Auth(validator TokenValidator) func(http.Handler) http.Handler
```

Logic:
1. Extract `Authorization: Bearer <token>` header
2. Call `validator.Validate(token)`
3. Handle errors:
   - `ErrJWKSNotReady` → 503 Service Unavailable
   - Any other error → 401 Unauthorized
4. If valid, call next handler

HTTP responses:

| Case | Status | Body |
|------|--------|------|
| No header / no Bearer | 401 | `{"error":"unauthorized"}` |
| Invalid token | 401 | `{"error":"unauthorized"}` |
| JWKS not ready | 503 | `{"error":"service_unavailable"}` |

### 5. Wiring (`cmd/api/main.go`)

Conditional auth setup:

```go
var authMiddleware func(http.Handler) http.Handler

if cfg.AuthEnabled {
    jwksManager := background.NewJWKSManager(cfg.Auth0Domain)
    jwksManager.Start()
    
    issuer := fmt.Sprintf("https://%s/", cfg.Auth0Domain)
    validator := auth.NewJWTValidator(jwksManager, issuer, cfg.Auth0Audience)
    
    authMiddleware = middleware.Auth(validator)
} else {
    log.Warn().Msg("authentication disabled (AUTH_ENABLED=false)")
    authMiddleware = func(next http.Handler) http.Handler {
        return next
    }
}
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AUTH_ENABLED` | No | `true` | Enable/disable authentication |
| `AUTH0_DOMAIN` | If auth enabled | - | OAuth provider domain |
| `AUTH0_AUDIENCE` | If auth enabled | - | Expected token audience |

Issuer derived automatically: `https://{AUTH0_DOMAIN}/`

## Testing Strategy

### JWTValidator (`internal/auth/validator_test.go`)

| Test | Description |
|------|-------------|
| `TestValidate_ValidToken` | Valid token with correct RS256 signature |
| `TestValidate_ExpiredToken` | Expired token → error |
| `TestValidate_InvalidSignature` | Wrong signature → error |
| `TestValidate_InvalidIssuer` | Wrong issuer → error |
| `TestValidate_InvalidAudience` | Wrong audience → error |
| `TestValidate_MalformedToken` | Malformed token → error |
| `TestValidate_KeyNotFound` | Unknown kid → error |
| `TestValidate_JWKSNotReady` | KeyProvider not ready → ErrJWKSNotReady |

Mock: `mockKeyProvider` implementing `KeyProvider` interface.

### Auth Middleware (`internal/middleware/auth_test.go`)

| Test | Description |
|------|-------------|
| `TestAuth_NoHeader` | No Authorization header → 401 |
| `TestAuth_InvalidBearerFormat` | Invalid Bearer format → 401 |
| `TestAuth_ValidToken` | Valid token → passes to handler |
| `TestAuth_InvalidToken` | Invalid token → 401 |
| `TestAuth_JWKSNotReady` | Validator returns ErrJWKSNotReady → 503 |

Mock: `mockTokenValidator` implementing `TokenValidator` interface.

### JWKSManager (`internal/background/jwks_loader_test.go`)

| Test (new) | Description |
|------------|-------------|
| `TestGetKey_Success` | Get existing key by kid |
| `TestGetKey_NotReady` | Manager not ready → ErrJWKSNotReady |
| `TestGetKey_NotFound` | Unknown kid → ErrKeyNotFound |

### Config (`internal/config/config_test.go`)

| Test (new) | Description |
|------------|-------------|
| `TestLoad_AuthEnabled_RequiresVars` | AUTH_ENABLED=true requires AUTH0_* |
| `TestLoad_AuthDisabled_NoVarsRequired` | AUTH_ENABLED=false doesn't require AUTH0_* |
| `TestLoad_AuthEnabled_Default` | Without AUTH_ENABLED, default = true |

Test tokens generated with RSA keys created on-the-fly.

## Documentation

README.md will include a new **Authentication** section covering:
- Configuration variables
- Token validation rules
- How to disable authentication for development
- Error responses

## Dependencies

Add to `go.mod`:

```
github.com/golang-jwt/jwt/v5
```

## Files to Create/Modify

**New files**:
- `internal/auth/validator.go`
- `internal/auth/validator_test.go`
- `internal/auth/errors.go`

**Modified files**:
- `internal/config/config.go` - Add AuthEnabled field
- `internal/config/config_test.go` - Add tests for AuthEnabled
- `internal/background/jwks_loader.go` - Add GetKey method, parse keys as RSA
- `internal/background/jwks_loader_test.go` - Add GetKey tests
- `internal/middleware/auth.go` - Full JWT validation
- `internal/middleware/auth_test.go` - Update tests
- `cmd/api/main.go` - Conditional auth wiring
- `go.mod` - Add jwt dependency
- `README.md` - Authentication documentation
