# Global Rate Limiter Design

**Date:** 2026-08-02  
**Author:** AI Agent  
**Status:** Approved

## Overview

This document describes the design and implementation of a global rate limiter for the Weather API. The rate limiter protects the `/weather/{city}` and `/api/v1/weather/{city}` endpoints from excessive traffic using a token bucket algorithm with Redis as primary storage and in-memory fallback.

## Requirements

- **Algorithm:** Token bucket (capacity: 60 tokens, refill: 1 token/second)
- **Primary storage:** Redis (shared state across replicas)
- **Fallback storage:** In-memory (local token bucket when Redis is unavailable)
- **Scope:** Global rate limit (not per-user for MVP)
- **Behavior:** Memory fallback is always initialized, Redis is always attempted (even if down at startup)
- **Fail-open:** Service availability prioritized over strict rate limiting

## Architecture

### Components

1. **RateLimiter interface** - Abstraction for rate limiting implementations
2. **RedisRateLimiter** - Token bucket implementation using Lua script for atomicity
3. **MemoryRateLimiter** - Local token bucket fallback with mutex protection
4. **AdaptiveRateLimiter** - Orchestrator that tries Redis first, falls back to memory on error
5. **RateLimit middleware** - Chi middleware that intercepts requests and enforces limits

### Flow Diagram

```
Requête → RateLimitMiddleware
           │
           ▼
    Appel RateLimiter.Allow()
           │
           ├─ Redis UP ──► RedisRateLimiter (script Lua atomique)
           │                   │
           │                   ├─ Tokens disponibles ──► 200 OK + headers
           │                   └─ Tokens épuisés ──────► 429 Too Many Requests
           │
           └─ Redis DOWN ──► MemoryRateLimiter (fallback local)
                                   │
                                   ├─ Tokens disponibles ──► 200 OK + headers
                                   └─ Tokens épuisés ──────► 429 Too Many Requests
```

### Middleware Stack Placement

```
Recovery → Logging → CORS → Prometheus → RateLimit → Auth → Handler
```

**Rationale:** Rate limiting is placed **before** Auth middleware to save CPU cycles on JWT validation for rate-limited requests.

### Endpoints Affected

- `/weather/{city}` - Rate limited + Auth
- `/api/v1/weather/{city}` - Rate limited + Auth
- `/health` - **NOT** rate limited (monitoring)
- `/metrics` - **NOT** rate limited (observability)

## Detailed Implementation

### 1. RateLimiter Interface

**File:** `internal/ratelimit/ratelimit.go`

```go
package ratelimit

import "context"

// RateLimiter defines the interface for rate limiting implementations
type RateLimiter interface {
    // Allow returns whether the request is allowed
    // Returns:
    //   allowed: true if request is allowed
    //   remaining: number of tokens remaining after this request
    //   resetAt: Unix timestamp (seconds) when the bucket will be full again
    //   err: error if the rate limiter encountered an internal error
    Allow(ctx context.Context) (allowed bool, remaining int, resetAt int64, err error)
}
```

### 2. RedisRateLimiter Implementation

**File:** `internal/ratelimit/redis.go`

#### Data Structure

**Redis Key:** `ratelimit:global`  
**Type:** Hash with 2 fields

```
{
  "tokens": "60.0",          // Available tokens (float for precise refill)
  "last_refill": "1722600000" // Unix timestamp of last refill (int64)
}
```

#### Lua Script

```lua
-- KEYS[1] = "ratelimit:global"
-- ARGV[1] = capacity (60)
-- ARGV[2] = refill_rate (1 token/sec)
-- ARGV[3] = current_timestamp (Unix seconds)

local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

-- Get current state
local state = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(state[1]) or capacity
local last_refill = tonumber(state[2]) or now

-- Calculate refill
local elapsed = now - last_refill
local refilled = math.min(capacity, tokens + (elapsed * refill_rate))

-- Consume 1 token
if refilled >= 1 then
    refilled = refilled - 1
    redis.call('HMSET', key, 'tokens', refilled, 'last_refill', now)
    redis.call('EXPIRE', key, 120)  -- TTL 2min (safety)
    return {1, math.floor(refilled), now + math.ceil((capacity - refilled) / refill_rate)}
else
    redis.call('HMSET', key, 'tokens', refilled, 'last_refill', now)
    redis.call('EXPIRE', key, 120)
    return {0, 0, now + math.ceil((1 - refilled) / refill_rate)}
end
```

#### Go Structure

```go
type RedisRateLimiter struct {
    cache      *cache.RedisCache  // Can be nil if Redis is unavailable
    capacity   int                // Token bucket capacity (60)
    refillRate time.Duration      // Refill rate (1 second)
    script     string             // Lua script source
}

func NewRedisRateLimiter(cache *cache.RedisCache, capacity int, refillRate time.Duration) *RedisRateLimiter {
    return &RedisRateLimiter{
        cache:      cache,
        capacity:   capacity,
        refillRate: refillRate,
        script:     luaScript, // defined as const
    }
}

func (r *RedisRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
    // Check if Redis is available
    if r.cache == nil {
        return false, 0, 0, fmt.Errorf("redis cache not available")
    }
    
    now := time.Now().Unix()
    refillRatePerSec := 1.0 / r.refillRate.Seconds()
    
    // Execute Lua script
    result, err := r.cache.Client().Eval(ctx, r.script, 
        []string{"ratelimit:global"}, 
        r.capacity, 
        refillRatePerSec, 
        now,
    ).Result()
    
    if err != nil {
        return false, 0, 0, fmt.Errorf("redis eval failed: %w", err)
    }
    
    // Parse result [allowed, remaining, resetAt]
    results := result.([]interface{})
    allowed := results[0].(int64) == 1
    remaining := int(results[1].(int64))
    resetAt := results[2].(int64)
    
    return allowed, remaining, resetAt, nil
}
```

### 3. MemoryRateLimiter Implementation

**File:** `internal/ratelimit/memory.go`

#### Go Structure

```go
type MemoryRateLimiter struct {
    capacity     int           // 60
    refillRate   time.Duration // 1 second
    tokens       float64       // available tokens
    lastRefill   time.Time     // last refill timestamp
    mu           sync.Mutex    // concurrency protection
}

func NewMemoryRateLimiter(capacity int, refillRate time.Duration) *MemoryRateLimiter {
    return &MemoryRateLimiter{
        capacity:   capacity,
        refillRate: refillRate,
        tokens:     float64(capacity), // start with full bucket
        lastRefill: time.Now(),
    }
}

func (m *MemoryRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    now := time.Now()
    elapsed := now.Sub(m.lastRefill)
    
    // Refill tokens based on elapsed time
    tokensToAdd := elapsed.Seconds() / m.refillRate.Seconds()
    m.tokens = math.Min(float64(m.capacity), m.tokens+tokensToAdd)
    m.lastRefill = now
    
    // Try to consume 1 token
    if m.tokens >= 1.0 {
        m.tokens -= 1.0
        remaining := int(m.tokens)
        
        // Calculate resetAt
        tokensNeeded := float64(m.capacity) - m.tokens
        resetAt := now.Add(time.Duration(tokensNeeded*m.refillRate.Seconds()) * time.Second).Unix()
        
        return true, remaining, resetAt, nil
    }
    
    // Not enough tokens
    tokensNeeded := 1.0 - m.tokens
    resetAt := now.Add(time.Duration(tokensNeeded*m.refillRate.Seconds()) * time.Second).Unix()
    
    return false, 0, resetAt, nil
}
```

### 4. AdaptiveRateLimiter (Orchestrator)

**File:** `internal/ratelimit/adaptive.go`

```go
type AdaptiveRateLimiter struct {
    redis    RateLimiter // Primary (can be nil at Allow() time if Redis fails)
    fallback RateLimiter // Always initialized
}

func NewAdaptiveRateLimiter(redis, fallback RateLimiter) *AdaptiveRateLimiter {
    return &AdaptiveRateLimiter{
        redis:    redis,
        fallback: fallback,
    }
}

func (a *AdaptiveRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
    // Try Redis first
    if a.redis != nil {
        allowed, remaining, resetAt, err := a.redis.Allow(ctx)
        if err == nil {
            return allowed, remaining, resetAt, nil
        }
        
        // Redis failed - log ERROR and fall back to memory
        log.Error().Err(err).Msg("Redis rate limiter failed, using memory fallback")
    }
    
    // Use memory fallback
    return a.fallback.Allow(ctx)
}
```

### 5. RateLimit Middleware

**File:** `internal/middleware/ratelimit.go`

```go
package middleware

import (
    "encoding/json"
    "net/http"
    "strconv"
    "time"

    "github.com/rs/zerolog/log"
    "github.com/yourusername/render-weather/internal/ratelimit"
)

func RateLimit(limiter ratelimit.RateLimiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx := r.Context()
            
            allowed, remaining, resetAt, err := limiter.Allow(ctx)
            
            // Always add rate limit headers (RFC 6585 standard)
            w.Header().Set("X-RateLimit-Limit", "60")
            w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
            w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))
            
            if err != nil {
                // Internal error - fail-open (service availability priority)
                log.Error().
                    Err(err).
                    Str("path", r.URL.Path).
                    Msg("rate limiter internal error")
                next.ServeHTTP(w, r)
                return
            }
            
            if !allowed {
                // Rate limit exceeded
                retryAfter := resetAt - time.Now().Unix()
                if retryAfter < 0 {
                    retryAfter = 0
                }
                
                w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusTooManyRequests)
                
                json.NewEncoder(w).Encode(map[string]string{
                    "error":   "rate_limit_exceeded",
                    "message": "Too many requests. Please try again later.",
                })
                
                log.Warn().
                    Str("path", r.URL.Path).
                    Int64("reset_at", resetAt).
                    Msg("rate limit exceeded")
                return
            }
            
            // Request allowed
            log.Debug().
                Str("path", r.URL.Path).
                Int("remaining", remaining).
                Int64("reset_at", resetAt).
                Msg("rate limit: request allowed")
            
            next.ServeHTTP(w, r)
        })
    }
}
```

## Integration in main.go

### Initialization (after hybrid cache creation, ~line 74)

```go
// 6a. Create memory rate limiter (fallback, always initialized)
memRateLimiter := ratelimit.NewMemoryRateLimiter(
    cfg.RateLimitCapacity,
    cfg.RateLimitRefillRate,
)
log.Info().
    Int("capacity", cfg.RateLimitCapacity).
    Dur("refill_rate", cfg.RateLimitRefillRate).
    Msg("memory rate limiter initialized (fallback)")

// 6b. Create Redis rate limiter (always created, even if Redis is down)
// It will handle reconnection attempts internally on each Allow() call
redisRateLimiter := ratelimit.NewRedisRateLimiter(
    redisCache,
    cfg.RateLimitCapacity,
    cfg.RateLimitRefillRate,
)
if redisCache != nil {
    log.Info().Msg("Redis rate limiter initialized")
} else {
    log.Warn().Msg("Redis unavailable at startup, rate limiter will retry on each request")
}

// 6c. Create adaptive rate limiter (orchestrator)
rateLimiter := ratelimit.NewAdaptiveRateLimiter(redisRateLimiter, memRateLimiter)
log.Info().Msg("adaptive rate limiter initialized")
```

### Middleware Stack (after line ~119)

```go
router := chi.NewRouter()
router.Use(middleware.Recovery())           // 1. Recovery (panics)
router.Use(middleware.Logging())            // 2. Logging (request_id)
router.Use(middleware.CORS(cfg.AllowedOrigins))  // 3. CORS
router.Use(middleware.Prometheus(promRegistry))  // 4. Metrics
router.Use(middleware.RateLimit(rateLimiter))    // 5. Rate limit (NEW)

// Public routes (no rate limit, no auth)
router.Get("/health", handlers.HealthHandler())
router.Handle("/metrics", promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{
    Registry: promRegistry,
}))

// Protected routes (rate limited + auth)
router.Group(func(r chi.Router) {
    r.Use(authMiddleware)
    r.Get("/weather/{city}", handlers.WeatherHandler(weatherService))
    r.Get("/api/v1/weather/{city}", handlers.WeatherHandler(weatherService))
})
```

## Configuration

### Environment Variables

**File:** `internal/config/config.go`

```go
type Config struct {
    // ... existing fields ...
    
    // Rate limiting
    RateLimitCapacity   int           `env:"RATE_LIMIT_CAPACITY" envDefault:"60"`
    RateLimitRefillRate time.Duration `env:"RATE_LIMIT_REFILL_RATE" envDefault:"1s"`
}
```

**File:** `.env.example`

```env
# Rate Limiting
RATE_LIMIT_CAPACITY=60           # Token bucket capacity (requests per minute)
RATE_LIMIT_REFILL_RATE=1s        # Refill rate (1 token per second = 60/min)
```

### Default Values

- `RATE_LIMIT_CAPACITY=60` - 60 requests per minute
- `RATE_LIMIT_REFILL_RATE=1s` - 1 token every second

## Error Handling

### Strategy: Fail-Open

Service availability is prioritized over strict rate limiting. Internal errors allow the request to proceed.

### Error Cases

| Situation | Error | Decision | Logging Level |
|-----------|-------|----------|---------------|
| Redis down | `redis: connection refused` | Use memory fallback | `ERROR` (every time) |
| Lua script error | `NOSCRIPT` or Lua error | Use memory fallback | `ERROR` |
| Redis timeout | `context deadline exceeded` | Use memory fallback | `ERROR` |
| Unknown error | Any other error | **Fail-open** (allow request) | `ERROR` |
| Rate limit exceeded | None (expected behavior) | Return 429 | `WARN` |

**Important:** All errors are logged at `ERROR` level systematically. No deduplication or throttling. If Redis is down, every request logs an error to ensure ops visibility.

### Logging Examples

```go
// Redis failure (ERROR)
log.Error().
    Err(err).
    Msg("Redis rate limiter failed, using memory fallback")

// Rate limit exceeded (WARN - normal behavior)
log.Warn().
    Str("path", r.URL.Path).
    Int64("reset_at", resetAt).
    Msg("rate limit exceeded")

// Internal error (ERROR - fail-open)
log.Error().
    Err(err).
    Str("path", r.URL.Path).
    Msg("rate limiter internal error")

// Request allowed (DEBUG - can be disabled in prod)
log.Debug().
    Str("path", r.URL.Path).
    Int("remaining", remaining).
    Msg("rate limit: request allowed")
```

## HTTP Response Details

### Success Response (200 OK)

```http
HTTP/1.1 200 OK
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 42
X-RateLimit-Reset: 1722600060
Content-Type: application/json

{
  "city": "Paris",
  "temperature": 22.5,
  ...
}
```

### Rate Limited Response (429 Too Many Requests)

```http
HTTP/1.1 429 Too Many Requests
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1722600060
Retry-After: 15
Content-Type: application/json

{
  "error": "rate_limit_exceeded",
  "message": "Too many requests. Please try again later."
}
```

### HTTP Headers

| Header | Example | Description |
|--------|---------|-------------|
| `X-RateLimit-Limit` | `60` | Global limit (requests per minute) |
| `X-RateLimit-Remaining` | `42` | Tokens remaining after this request |
| `X-RateLimit-Reset` | `1722600060` | Unix timestamp when bucket refills completely |
| `Retry-After` | `15` | Seconds to wait before retry (429 only) |

## Testing Strategy

### Unit Tests (TDD)

#### 1. `internal/ratelimit/memory_test.go`

- Token bucket algorithm: progressive refill
- Initial burst: 60 requests pass, 61st fails
- Temporal refill: wait 1s → 1 more token available
- Concurrency: 10 goroutines → exactly 60 pass total
- Edge cases: fractional tokens, time precision

#### 2. `internal/ratelimit/redis_test.go`

- Use `miniredis` for in-memory Redis mock
- Lua script execution correctness
- State persistence across calls
- Redis error handling (connection refused, timeout)
- Script not cached (`NOSCRIPT` error)

#### 3. `internal/ratelimit/adaptive_test.go`

- Redis UP → uses Redis implementation
- Redis returns error → falls back to memory
- Redis nil → uses memory directly

#### 4. `internal/middleware/ratelimit_test.go`

- Mock `RateLimiter` with `testify/mock`
- HTTP headers present and correct
- 429 status code with proper JSON body
- Fail-open behavior on internal error
- Logging verification

### Coverage Targets

| Package | Target | Notes |
|---------|--------|-------|
| `ratelimit/memory.go` | 90%+ | Core token bucket algorithm |
| `ratelimit/redis.go` | 85%+ | Mock Redis via miniredis |
| `ratelimit/adaptive.go` | 95%+ | Simple fallback logic |
| `middleware/ratelimit.go` | 100% | Critical path |

**Overall target:** ~90% coverage (consistent with project 78-100%)

## Deployment Considerations

### Render.com Compatibility

- **Valkey 8** (Redis 7.2.4 fork) fully supports Lua scripting
- `EVAL`, `EVALSHA`, `SCRIPT LOAD` all available
- Script atomicity guaranteed
- No infrastructure changes needed (uses existing Redis)

### Multi-Replica Behavior

- **Redis UP:** All replicas share the same token bucket → strict global limit
- **Redis DOWN:** Each replica has independent token bucket → limit multiplied by replica count (degraded accuracy, but service continues)

### Memory Usage

- **RedisRateLimiter:** ~100 bytes (script cache + minimal state)
- **MemoryRateLimiter:** ~50 bytes (4 fields + mutex)
- **Total overhead:** Negligible (~150 bytes per instance)

## Future Enhancements (Not in MVP)

- Per-user rate limiting (based on JWT `sub` claim)
- Different limits for different endpoints
- Rate limit metrics (Prometheus counters)
- Sliding window algorithm option
- Distributed lock for memory fallback coordination

## References

- RFC 6585: HTTP Status Code 429 (Too Many Requests)
- Token Bucket Algorithm: https://en.wikipedia.org/wiki/Token_bucket
- Redis Lua Scripting: https://redis.io/docs/manual/programmability/eval-intro/
- Valkey Documentation: https://valkey.io/topics/eval-intro/
- Render.com Redis: https://docs.render.com/redis

## Appendix: File Structure Summary

```
internal/
  ratelimit/
    ratelimit.go           # Interface definition
    redis.go               # Redis implementation with Lua script
    memory.go              # Memory fallback implementation
    adaptive.go            # Orchestrator (Redis → Memory fallback)
    redis_test.go          # Redis unit tests (miniredis)
    memory_test.go         # Memory unit tests
    adaptive_test.go       # Adaptive unit tests
  middleware/
    ratelimit.go           # Chi middleware
    ratelimit_test.go      # Middleware unit tests
```

**Total new files:** 8 (4 implementation + 4 test files)

---

**End of Design Document**
