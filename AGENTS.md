# AGENTS.md - AI Agent Development Guide

This document contains information for AI coding agents working on the Weather API project.

## Project Overview

Weather API service built with Go 1.26, featuring Auth0 authentication, hybrid caching (memory + Redis), circuit breaker resilience, and background goroutines for cache preloading and JWKS management.

## Technology Stack

- **Language**: Go 1.26
- **Router**: Chi v5
- **Logging**: zerolog (structured JSON)
- **Cache**: In-memory LRU + Redis
- **Circuit Breaker**: sony/gobreaker
- **Retry Logic**: cenk alti/backoff
- **Testing**: testify (assert, require, mock)
- **External APIs**: OpenWeatherMap, Auth0 JWKS

## Key Commands

### Testing
```bash
go test ./...                    # Run all tests (36 tests)
go test ./... -cover             # With coverage (78-100% per package)
go test ./... -race              # Race detector (Note: background tests slow)
go test ./internal/handlers -v   # Specific package with verbose output
```

### Building
```bash
go build -o bin/api cmd/api/main.go  # Local binary
docker build -t weather-api .        # Docker image
```

### Linting
```bash
golangci-lint run    # Run linters (.golangci.yml config)
golangci-lint fmt    # Auto-format (gofumpt + goimports)
```

### Running
```bash
go run cmd/api/main.go               # Local development
docker run --env-file .env -p 8080:8080 weather-api  # Docker
```

## Architecture

### Component Flow
1. HTTP Request → Middleware Stack (Recovery → Logging → CORS → Auth)
2. Handler extracts parameters, validates input
3. Service checks hybrid cache (L1 → L2)
4. On miss: Circuit breaker → Provider (with retry/backoff)
5. Cache result, return to client with headers

### Background Goroutines
- **Cache Loader**: Preloads popular cities from Redis to memory
- **JWKS Loader**: Fetches and refreshes Auth0 JWT keys continuously

### Error Handling
- Provider errors → Service sentinel errors → Handler HTTP status codes
- Circuit breaker open → 503 Service Unavailable
- JWKS not ready → 503 Service Unavailable
- Rate limit exceeded → 429 Too Many Requests (global limit: 60 req/min)
- City not found → 404 Not Found
- Panics → 500 Internal Server Error (recovered, logged with stack trace)

## Testing Strategy

### Approach
- **TDD**: All features built test-first (RED → GREEN → REFACTOR)
- **Mocks**: No real API calls (OpenWeatherMap, Redis, Auth0)
- **Isolation**: Each package tests independently
- **Coverage**: Handlers and middleware at 100%, others 78-88%

### Mock Patterns
- `httptest.Server` for provider tests
- Interface mocks for services (e.g., `mockWeatherService`)
- `sync.Map` for in-memory Redis mock
- `testify/assert` for assertions

### Race Conditions
- Background goroutine tests use mutexes in mocks
- Sleep delays (100-500ms) for async verification
- Race detector enabled (`go test -race`)

## Environment Variables

Required in `.env`:
```env
PORT=8080
REDIS_URL=redis://localhost:6379
OPENWEATHER_API_KEY=your_key
AUTH0_DOMAIN=https://tenant.auth0.com
AUTH0_AUDIENCE=https://api-audience
ALLOWED_ORIGINS=https://example.com
```

## Deployment (Render.com)

1. Connect repo to Render
2. Create Web Service (uses Dockerfile)
3. Add Redis service (Internal Connection)
4. Configure environment variables
5. Deploy

**Important**: 
- Server starts immediately (non-blocking)
- Background loaders complete asynchronously
- Redis failures don't prevent startup (memory-only fallback)

## Common Tasks for Agents

### Adding a New Endpoint
1. Define handler in `internal/handlers/`
2. Write tests first (TDD)
3. Add route in `cmd/api/main.go`
4. Update README with endpoint documentation

### Adding a New Provider
1. Implement `providers.WeatherProvider` interface
2. Add retry logic with `backoff`
3. Define `ClientError` for 4xx (no retry)
4. Write tests with `httptest.Server`

### Modifying Cache Behavior
1. Check interface `CacheGetter` in `services/weather_service.go`
2. Ensure L1, L2, and hybrid all satisfy interface
3. Update TTL values in `cmd/api/main.go` if needed
4. Test cache hits/misses

### Debugging
- Logs are structured JSON (grep/jq friendly)
- Request IDs in context for tracing
- Circuit breaker state changes logged
- Provider errors wrapped with context

## Code Style

- **Error handling**: Wrap errors with `fmt.Errorf(..., %w, err)`
- **Logging**: Use structured fields `.Str()`, `.Int()`, `.Err()`
- **Tests**: Table-driven with `t.Run()` subtests
- **Interfaces**: Define near usage, keep minimal
- **Names**: `Get`, `Set`, `New` prefixes; avoid abbreviations

## Dependencies

Managed via `go.mod`:
- `github.com/go-chi/chi/v5` - HTTP routing
- `github.com/rs/zerolog` - Structured logging
- `github.com/redis/go-redis/v9` - Redis client
- `github.com/sony/gobreaker` - Circuit breaker
- `github.com/cenkalti/backoff/v4` - Retry with backoff
- `github.com/google/uuid` - Request ID generation
- `github.com/stretchr/testify` - Testing utilities

## Known Issues

- Race detector on background tests is slow (500ms sleep delays)
- Redis connection errors logged but don't fail startup
- JWKS validation placeholder (auth middleware checks ready state only)

## Future Enhancements (Not Implemented)

- Actual JWT token validation (currently checks JWKS ready only)
- Prometheus metrics endpoint
- Rate limiting per-client
- More sophisticated cache eviction strategies
- WebSocket support for real-time updates

## Contact

For questions about architecture decisions or design choices, review git commits and task reports in `.superpowers/sdd/`.

---

**For AI Agents**: This project follows TDD, uses mocks extensively, and emphasizes clean error handling. All background tasks are non-blocking. When making changes, run `go test ./...` before committing.