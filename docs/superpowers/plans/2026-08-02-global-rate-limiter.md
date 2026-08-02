# Global Rate Limiter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a global rate limiter (60 req/min) for weather endpoints using token bucket algorithm with Redis primary storage and in-memory fallback.

**Architecture:** Token bucket algorithm with Redis Lua script for atomic operations, in-memory fallback for Redis failures, adaptive orchestrator for seamless failover, Chi middleware for HTTP integration.

**Tech Stack:** Go 1.26, Redis/Valkey (Lua scripting), Chi v5 router, zerolog, testify

## Global Constraints

- Go 1.26+ required
- TDD: Write tests first (RED → GREEN → REFACTOR)
- All new code must have test coverage (target: 90%+)
- Use zerolog for structured logging
- Follow existing project patterns (package structure, error handling, naming)
- Commit after each passing test
- No external dependencies beyond existing go.mod

---

## Task 1: RateLimiter Interface and Configuration

**Files:**
- Create: `internal/ratelimit/ratelimit.go`
- Modify: `internal/config/config.go:12-20` (add rate limit config fields)
- Modify: `.env.example:42-45` (add rate limit env vars)

**Interfaces:**
- Consumes: Nothing (foundation task)
- Produces: 
  - `RateLimiter` interface with method `Allow(ctx context.Context) (allowed bool, remaining int, resetAt int64, err error)`
  - `Config.RateLimitCapacity int` field
  - `Config.RateLimitRefillRate time.Duration` field

- [ ] **Step 1: Write test for RateLimiter interface**

Create `internal/ratelimit/ratelimit_test.go`:

```go
package ratelimit_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourusername/render-weather/internal/ratelimit"
)

// MockRateLimiter implements RateLimiter for testing
type MockRateLimiter struct {
	AllowFunc func(ctx context.Context) (bool, int, int64, error)
}

func (m *MockRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
	return m.AllowFunc(ctx)
}

func TestRateLimiterInterface(t *testing.T) {
	mock := &MockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return true, 59, 1722600060, nil
		},
	}

	allowed, remaining, resetAt, err := mock.Allow(context.Background())

	assert.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 59, remaining)
	assert.Equal(t, int64(1722600060), resetAt)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ratelimit -v`

Expected: Compilation error "package ratelimit does not exist"

- [ ] **Step 3: Create RateLimiter interface**

Create `internal/ratelimit/ratelimit.go`:

```go
// Package ratelimit provides rate limiting implementations for the Weather API.
package ratelimit

import "context"

// RateLimiter defines the interface for rate limiting implementations.
type RateLimiter interface {
	// Allow returns whether the request is allowed.
	// Returns:
	//   allowed: true if request is allowed
	//   remaining: number of tokens remaining after this request
	//   resetAt: Unix timestamp (seconds) when the bucket will be full again
	//   err: error if the rate limiter encountered an internal error
	Allow(ctx context.Context) (allowed bool, remaining int, resetAt int64, err error)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ratelimit -v`

Expected: PASS

- [ ] **Step 5: Add rate limit config fields with tests**

Add to `internal/config/config_test.go` (after existing tests):

```go
func TestConfigRateLimitDefaults(t *testing.T) {
	// Clear environment
	os.Clearenv()
	os.Setenv("PORT", "8080")
	os.Setenv("REDIS_URL", "redis://localhost:6379")
	os.Setenv("OPENWEATHER_API_KEY", "test-key")

	cfg, err := Load()
	require.NoError(t, err)

	// Check rate limit defaults
	assert.Equal(t, 60, cfg.RateLimitCapacity)
	assert.Equal(t, 1*time.Second, cfg.RateLimitRefillRate)
}

func TestConfigRateLimitCustom(t *testing.T) {
	os.Clearenv()
	os.Setenv("PORT", "8080")
	os.Setenv("REDIS_URL", "redis://localhost:6379")
	os.Setenv("OPENWEATHER_API_KEY", "test-key")
	os.Setenv("RATE_LIMIT_CAPACITY", "100")
	os.Setenv("RATE_LIMIT_REFILL_RATE", "2s")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 100, cfg.RateLimitCapacity)
	assert.Equal(t, 2*time.Second, cfg.RateLimitRefillRate)
}
```

- [ ] **Step 6: Run config tests to verify failure**

Run: `go test ./internal/config -v`

Expected: FAIL "undefined: Config.RateLimitCapacity"

- [ ] **Step 7: Add rate limit fields to Config struct**

Modify `internal/config/config.go` (add after existing fields):

```go
type Config struct {
	// ... existing fields ...

	// Rate limiting
	RateLimitCapacity   int           `env:"RATE_LIMIT_CAPACITY" envDefault:"60"`
	RateLimitRefillRate time.Duration `env:"RATE_LIMIT_REFILL_RATE" envDefault:"1s"`
}
```

- [ ] **Step 8: Run config tests to verify pass**

Run: `go test ./internal/config -v`

Expected: PASS

- [ ] **Step 9: Update .env.example**

Add to `.env.example` (after existing variables):

```env
# Rate Limiting
RATE_LIMIT_CAPACITY=60           # Token bucket capacity (requests per minute)
RATE_LIMIT_REFILL_RATE=1s        # Refill rate (1 token per second = 60/min)
```

- [ ] **Step 10: Commit**

```bash
git add internal/ratelimit/ratelimit.go internal/ratelimit/ratelimit_test.go internal/config/config.go internal/config/config_test.go .env.example
git commit -m "feat(ratelimit): add RateLimiter interface and configuration"
```

---

## Task 2: MemoryRateLimiter Implementation

**Files:**
- Create: `internal/ratelimit/memory.go`
- Create: `internal/ratelimit/memory_test.go`

**Interfaces:**
- Consumes: `RateLimiter` interface from Task 1
- Produces: `NewMemoryRateLimiter(capacity int, refillRate time.Duration) *MemoryRateLimiter` constructor

- [ ] **Step 1: Write failing test for token bucket burst**

Create `internal/ratelimit/memory_test.go`:

```go
package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourusername/render-weather/internal/ratelimit"
)

func TestMemoryRateLimiter_InitialBurst(t *testing.T) {
	limiter := ratelimit.NewMemoryRateLimiter(3, 1*time.Second)
	ctx := context.Background()

	// First 3 requests should pass
	for i := 0; i < 3; i++ {
		allowed, remaining, resetAt, err := limiter.Allow(ctx)
		require.NoError(t, err)
		assert.True(t, allowed, "request %d should be allowed", i+1)
		assert.Equal(t, 2-i, remaining)
		assert.Greater(t, resetAt, time.Now().Unix())
	}

	// 4th request should fail
	allowed, remaining, resetAt, err := limiter.Allow(ctx)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
	assert.Greater(t, resetAt, time.Now().Unix())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ratelimit -v -run TestMemoryRateLimiter_InitialBurst`

Expected: FAIL "undefined: ratelimit.NewMemoryRateLimiter"

- [ ] **Step 3: Implement MemoryRateLimiter**

Create `internal/ratelimit/memory.go`:

```go
package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"
)

// MemoryRateLimiter implements a token bucket rate limiter in memory.
type MemoryRateLimiter struct {
	capacity     int
	refillRate   time.Duration
	tokens       float64
	lastRefill   time.Time
	mu           sync.Mutex
}

// NewMemoryRateLimiter creates a new in-memory rate limiter.
func NewMemoryRateLimiter(capacity int, refillRate time.Duration) *MemoryRateLimiter {
	return &MemoryRateLimiter{
		capacity:   capacity,
		refillRate: refillRate,
		tokens:     float64(capacity), // start with full bucket
		lastRefill: time.Now(),
	}
}

// Allow implements RateLimiter.Allow.
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

		// Calculate resetAt (when bucket will be full again)
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ratelimit -v -run TestMemoryRateLimiter_InitialBurst`

Expected: PASS

- [ ] **Step 5: Write test for temporal refill**

Add to `internal/ratelimit/memory_test.go`:

```go
func TestMemoryRateLimiter_TemporalRefill(t *testing.T) {
	limiter := ratelimit.NewMemoryRateLimiter(2, 100*time.Millisecond)
	ctx := context.Background()

	// Consume all tokens
	limiter.Allow(ctx)
	limiter.Allow(ctx)

	// Should be rate limited now
	allowed, _, _, err := limiter.Allow(ctx)
	require.NoError(t, err)
	assert.False(t, allowed)

	// Wait for 1 token to refill
	time.Sleep(110 * time.Millisecond)

	// Should have 1 token available now
	allowed, remaining, _, err := limiter.Allow(ctx)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 0, remaining)
}
```

- [ ] **Step 6: Run temporal refill test**

Run: `go test ./internal/ratelimit -v -run TestMemoryRateLimiter_TemporalRefill`

Expected: PASS

- [ ] **Step 7: Write test for concurrency**

Add to `internal/ratelimit/memory_test.go`:

```go
func TestMemoryRateLimiter_Concurrency(t *testing.T) {
	capacity := 10
	limiter := ratelimit.NewMemoryRateLimiter(capacity, 1*time.Second)
	ctx := context.Background()

	var wg sync.WaitGroup
	var allowedCount int32
	goroutines := 20

	// Launch 20 goroutines trying to consume tokens
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, _, _, err := limiter.Allow(ctx)
			if err == nil && allowed {
				atomic.AddInt32(&allowedCount, 1)
			}
		}()
	}

	wg.Wait()

	// Exactly `capacity` requests should have been allowed
	assert.Equal(t, int32(capacity), allowedCount)
}
```

Add import: `"sync/atomic"`

- [ ] **Step 8: Run concurrency test**

Run: `go test ./internal/ratelimit -v -run TestMemoryRateLimiter_Concurrency`

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/ratelimit/memory.go internal/ratelimit/memory_test.go
git commit -m "feat(ratelimit): implement MemoryRateLimiter with token bucket"
```

---

## Task 3: RedisRateLimiter Implementation

**Files:**
- Create: `internal/ratelimit/redis.go`
- Create: `internal/ratelimit/redis_test.go`

**Interfaces:**
- Consumes: 
  - `RateLimiter` interface from Task 1
  - `*cache.RedisCache` from existing codebase
- Produces: `NewRedisRateLimiter(cache *cache.RedisCache, capacity int, refillRate time.Duration) *RedisRateLimiter` constructor

- [ ] **Step 1: Write failing test with miniredis**

Create `internal/ratelimit/redis_test.go`:

```go
package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourusername/render-weather/internal/cache"
	"github.com/yourusername/render-weather/internal/ratelimit"
)

func setupTestRedis(t *testing.T) (*cache.RedisCache, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	redisCache := &cache.RedisCache{}
	// Use reflection or expose Client() method to inject test client
	// For now, we'll assume cache.NewRedisCache accepts a client
	// This is a simplification - adjust based on actual cache.RedisCache implementation

	return redisCache, mr
}

func TestRedisRateLimiter_InitialBurst(t *testing.T) {
	redisCache, mr := setupTestRedis(t)
	defer mr.Close()

	limiter := ratelimit.NewRedisRateLimiter(redisCache, 3, 1*time.Second)
	ctx := context.Background()

	// First 3 requests should pass
	for i := 0; i < 3; i++ {
		allowed, remaining, resetAt, err := limiter.Allow(ctx)
		require.NoError(t, err)
		assert.True(t, allowed, "request %d should be allowed", i+1)
		assert.Equal(t, 2-i, remaining)
		assert.Greater(t, resetAt, time.Now().Unix())
	}

	// 4th request should fail
	allowed, remaining, resetAt, err := limiter.Allow(ctx)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
	assert.Greater(t, resetAt, time.Now().Unix())
}
```

**Note:** You may need to install miniredis: `go get github.com/alicebob/miniredis/v2`

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ratelimit -v -run TestRedisRateLimiter_InitialBurst`

Expected: FAIL "undefined: ratelimit.NewRedisRateLimiter"

- [ ] **Step 3: Implement RedisRateLimiter with Lua script**

Create `internal/ratelimit/redis.go`:

```go
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/yourusername/render-weather/internal/cache"
)

const luaScript = `
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
    redis.call('EXPIRE', key, 120)
    return {1, math.floor(refilled), now + math.ceil((capacity - refilled) / refill_rate)}
else
    redis.call('HMSET', key, 'tokens', refilled, 'last_refill', now)
    redis.call('EXPIRE', key, 120)
    return {0, 0, now + math.ceil((1 - refilled) / refill_rate)}
end
`

// RedisRateLimiter implements a token bucket rate limiter using Redis.
type RedisRateLimiter struct {
	cache      *cache.RedisCache
	capacity   int
	refillRate time.Duration
	script     string
}

// NewRedisRateLimiter creates a new Redis-backed rate limiter.
func NewRedisRateLimiter(cache *cache.RedisCache, capacity int, refillRate time.Duration) *RedisRateLimiter {
	return &RedisRateLimiter{
		cache:      cache,
		capacity:   capacity,
		refillRate: refillRate,
		script:     luaScript,
	}
}

// Allow implements RateLimiter.Allow.
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

**Note:** This assumes `cache.RedisCache` has a `Client() *redis.Client` method. Adjust based on actual implementation.

- [ ] **Step 4: Expose Client() method in RedisCache if needed**

Check `internal/cache/redis.go`. If `Client()` method doesn't exist, add it:

```go
// Client returns the underlying Redis client
func (c *RedisCache) Client() *redis.Client {
	return c.client
}
```

- [ ] **Step 5: Update redis_test.go setup to work with actual cache**

Modify `setupTestRedis` in `internal/ratelimit/redis_test.go`:

```go
func setupTestRedis(t *testing.T) (*cache.RedisCache, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)
	
	// Create RedisCache with test URL
	redisCache, err := cache.NewRedisCache("redis://" + mr.Addr())
	require.NoError(t, err)
	
	return redisCache, mr
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/ratelimit -v -run TestRedisRateLimiter_InitialBurst`

Expected: PASS

- [ ] **Step 7: Write test for state persistence**

Add to `internal/ratelimit/redis_test.go`:

```go
func TestRedisRateLimiter_StatePersistence(t *testing.T) {
	redisCache, mr := setupTestRedis(t)
	defer mr.Close()

	limiter1 := ratelimit.NewRedisRateLimiter(redisCache, 5, 1*time.Second)
	ctx := context.Background()

	// Consume 3 tokens with first limiter instance
	for i := 0; i < 3; i++ {
		allowed, _, _, err := limiter1.Allow(ctx)
		require.NoError(t, err)
		assert.True(t, allowed)
	}

	// Create new limiter instance (simulates different replica)
	limiter2 := ratelimit.NewRedisRateLimiter(redisCache, 5, 1*time.Second)

	// Should see state from first limiter (2 tokens remaining)
	allowed, remaining, _, err := limiter2.Allow(ctx)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 1, remaining) // 5 - 3 - 1 = 1
}
```

- [ ] **Step 8: Run persistence test**

Run: `go test ./internal/ratelimit -v -run TestRedisRateLimiter_StatePersistence`

Expected: PASS

- [ ] **Step 9: Write test for Redis unavailable**

Add to `internal/ratelimit/redis_test.go`:

```go
func TestRedisRateLimiter_RedisUnavailable(t *testing.T) {
	// Create limiter with nil cache
	limiter := ratelimit.NewRedisRateLimiter(nil, 60, 1*time.Second)
	ctx := context.Background()

	allowed, remaining, resetAt, err := limiter.Allow(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis cache not available")
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
	assert.Equal(t, int64(0), resetAt)
}
```

- [ ] **Step 10: Run Redis unavailable test**

Run: `go test ./internal/ratelimit -v -run TestRedisRateLimiter_RedisUnavailable`

Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add internal/ratelimit/redis.go internal/ratelimit/redis_test.go internal/cache/redis.go
git commit -m "feat(ratelimit): implement RedisRateLimiter with Lua script"
```

---

## Task 4: AdaptiveRateLimiter Implementation

**Files:**
- Create: `internal/ratelimit/adaptive.go`
- Create: `internal/ratelimit/adaptive_test.go`

**Interfaces:**
- Consumes: `RateLimiter` interface from Task 1
- Produces: `NewAdaptiveRateLimiter(redis, fallback RateLimiter) *AdaptiveRateLimiter` constructor

- [ ] **Step 1: Write failing test for Redis success path**

Create `internal/ratelimit/adaptive_test.go`:

```go
package ratelimit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourusername/render-weather/internal/ratelimit"
)

func TestAdaptiveRateLimiter_RedisSuccess(t *testing.T) {
	redisLimiter := &MockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return true, 59, 1722600060, nil
		},
	}

	fallbackLimiter := &MockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			t.Fatal("fallback should not be called when Redis succeeds")
			return false, 0, 0, nil
		},
	}

	limiter := ratelimit.NewAdaptiveRateLimiter(redisLimiter, fallbackLimiter)
	ctx := context.Background()

	allowed, remaining, resetAt, err := limiter.Allow(ctx)

	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 59, remaining)
	assert.Equal(t, int64(1722600060), resetAt)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ratelimit -v -run TestAdaptiveRateLimiter_RedisSuccess`

Expected: FAIL "undefined: ratelimit.NewAdaptiveRateLimiter"

- [ ] **Step 3: Implement AdaptiveRateLimiter**

Create `internal/ratelimit/adaptive.go`:

```go
package ratelimit

import (
	"context"

	"github.com/rs/zerolog/log"
)

// AdaptiveRateLimiter tries Redis first, falls back to memory on error.
type AdaptiveRateLimiter struct {
	redis    RateLimiter
	fallback RateLimiter
}

// NewAdaptiveRateLimiter creates a new adaptive rate limiter.
func NewAdaptiveRateLimiter(redis, fallback RateLimiter) *AdaptiveRateLimiter {
	return &AdaptiveRateLimiter{
		redis:    redis,
		fallback: fallback,
	}
}

// Allow implements RateLimiter.Allow.
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ratelimit -v -run TestAdaptiveRateLimiter_RedisSuccess`

Expected: PASS

- [ ] **Step 5: Write test for Redis failure fallback**

Add to `internal/ratelimit/adaptive_test.go`:

```go
func TestAdaptiveRateLimiter_RedisFailureFallback(t *testing.T) {
	redisLimiter := &MockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return false, 0, 0, errors.New("redis connection refused")
		},
	}

	fallbackLimiter := &MockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return true, 45, 1722600070, nil
		},
	}

	limiter := ratelimit.NewAdaptiveRateLimiter(redisLimiter, fallbackLimiter)
	ctx := context.Background()

	allowed, remaining, resetAt, err := limiter.Allow(ctx)

	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 45, remaining)
	assert.Equal(t, int64(1722600070), resetAt)
}
```

- [ ] **Step 6: Run fallback test**

Run: `go test ./internal/ratelimit -v -run TestAdaptiveRateLimiter_RedisFailureFallback`

Expected: PASS

- [ ] **Step 7: Write test for nil Redis (uses fallback directly)**

Add to `internal/ratelimit/adaptive_test.go`:

```go
func TestAdaptiveRateLimiter_NilRedis(t *testing.T) {
	fallbackLimiter := &MockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return false, 0, 1722600080, nil
		},
	}

	limiter := ratelimit.NewAdaptiveRateLimiter(nil, fallbackLimiter)
	ctx := context.Background()

	allowed, remaining, resetAt, err := limiter.Allow(ctx)

	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
	assert.Equal(t, int64(1722600080), resetAt)
}
```

- [ ] **Step 8: Run nil Redis test**

Run: `go test ./internal/ratelimit -v -run TestAdaptiveRateLimiter_NilRedis`

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/ratelimit/adaptive.go internal/ratelimit/adaptive_test.go
git commit -m "feat(ratelimit): implement AdaptiveRateLimiter with failover"
```

---

## Task 5: RateLimit Middleware

**Files:**
- Create: `internal/middleware/ratelimit.go`
- Create: `internal/middleware/ratelimit_test.go`

**Interfaces:**
- Consumes: `RateLimiter` interface from Task 1
- Produces: `RateLimit(limiter ratelimit.RateLimiter) func(http.Handler) http.Handler` middleware function

- [ ] **Step 1: Write failing test for allowed request**

Create `internal/middleware/ratelimit_test.go`:

```go
package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourusername/render-weather/internal/middleware"
	"github.com/yourusername/render-weather/internal/ratelimit"
)

type mockRateLimiter struct {
	AllowFunc func(ctx context.Context) (bool, int, int64, error)
}

func (m *mockRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
	return m.AllowFunc(ctx)
}

func TestRateLimitMiddleware_Allowed(t *testing.T) {
	limiter := &mockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return true, 42, 1722600060, nil
		},
	}

	handler := middleware.RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/weather/Paris", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "60", rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "42", rec.Header().Get("X-RateLimit-Remaining"))
	assert.Equal(t, "1722600060", rec.Header().Get("X-RateLimit-Reset"))
	assert.Equal(t, "OK", rec.Body.String())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/middleware -v -run TestRateLimitMiddleware_Allowed`

Expected: FAIL "undefined: middleware.RateLimit"

- [ ] **Step 3: Implement RateLimit middleware**

Create `internal/middleware/ratelimit.go`:

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

// RateLimit returns a middleware that enforces rate limiting.
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/middleware -v -run TestRateLimitMiddleware_Allowed`

Expected: PASS

- [ ] **Step 5: Write test for rate limit exceeded (429)**

Add to `internal/middleware/ratelimit_test.go`:

```go
func TestRateLimitMiddleware_Exceeded(t *testing.T) {
	limiter := &mockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return false, 0, 1722600090, nil
		},
	}

	handler := middleware.RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called when rate limited")
	}))

	req := httptest.NewRequest(http.MethodGet, "/weather/Paris", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "60", rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
	assert.Equal(t, "1722600090", rec.Header().Get("X-RateLimit-Reset"))
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	assert.Contains(t, rec.Body.String(), "rate_limit_exceeded")
}
```

- [ ] **Step 6: Run 429 test**

Run: `go test ./internal/middleware -v -run TestRateLimitMiddleware_Exceeded`

Expected: PASS

- [ ] **Step 7: Write test for fail-open behavior**

Add to `internal/middleware/ratelimit_test.go`:

```go
func TestRateLimitMiddleware_FailOpen(t *testing.T) {
	limiter := &mockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return false, 0, 0, errors.New("internal limiter error")
		},
	}

	handler := middleware.RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/weather/Paris", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Request should pass despite limiter error (fail-open)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK", rec.Body.String())
}
```

Add import: `"errors"`

- [ ] **Step 8: Run fail-open test**

Run: `go test ./internal/middleware -v -run TestRateLimitMiddleware_FailOpen`

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/middleware/ratelimit.go internal/middleware/ratelimit_test.go
git commit -m "feat(middleware): implement RateLimit middleware with fail-open"
```

---

## Task 6: Integration with main.go

**Files:**
- Modify: `cmd/api/main.go:74-90` (add rate limiter initialization)
- Modify: `cmd/api/main.go:124` (add rate limit middleware to stack)

**Interfaces:**
- Consumes: All previous tasks (Config, MemoryRateLimiter, RedisRateLimiter, AdaptiveRateLimiter, RateLimit middleware)
- Produces: Fully integrated rate limiting in the application

- [ ] **Step 1: Add rate limiter initialization after hybrid cache**

Modify `cmd/api/main.go` (after line ~74 where hybrid cache is initialized):

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

Add import: `"github.com/yourusername/render-weather/internal/ratelimit"`

- [ ] **Step 2: Add RateLimit middleware to router stack**

Modify `cmd/api/main.go` (after line ~123 where middleware is configured):

```go
	router := chi.NewRouter()
	router.Use(middleware.Recovery())
	router.Use(middleware.Logging())
	router.Use(middleware.CORS(cfg.AllowedOrigins))
	router.Use(middleware.Prometheus(promRegistry))
	router.Use(middleware.RateLimit(rateLimiter)) // NEW: Rate limit before auth
```

- [ ] **Step 3: Build and verify compilation**

Run: `go build -o bin/api cmd/api/main.go`

Expected: Build succeeds with no errors

- [ ] **Step 4: Run all tests to verify integration**

Run: `go test ./...`

Expected: All tests PASS

- [ ] **Step 5: Start the server and test manually**

Run: `go run cmd/api/main.go` (requires .env with valid credentials)

In another terminal:
```bash
# Send 60 requests - all should pass
for i in {1..60}; do
  curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/weather/Paris
done

# 61st request should return 429
curl -i http://localhost:8080/weather/Paris
```

Expected: First 60 return 200, 61st returns 429 with rate limit headers

- [ ] **Step 6: Commit**

```bash
git add cmd/api/main.go
git commit -m "feat(main): integrate rate limiter with middleware stack"
```

---

## Task 7: Documentation Updates

**Files:**
- Modify: `README.md:90-100` (add rate limit to status codes)
- Modify: `README.md:35-43` (add rate limit env vars)
- Modify: `AGENTS.md:65` (add rate limit to error handling section)

**Interfaces:**
- Consumes: All implementation work from previous tasks
- Produces: Updated user-facing documentation

- [ ] **Step 1: Update README status codes section**

Modify `README.md` (around line 90-100, in the status codes table):

```markdown
**Status Codes:**
- `200` - Success
- `400` - Invalid city parameter
- `404` - City not found
- `429` - Rate limited (60 requests/minute global limit)
- `503` - Service unavailable (circuit breaker open or JWKS not ready)
- `500` - Internal error
```

- [ ] **Step 2: Add rate limit response example**

Add after the existing weather response example in README.md:

```markdown
**Rate Limit Response (429):**
```http
HTTP/1.1 429 Too Many Requests
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1722600060
Retry-After: 15

{
  "error": "rate_limit_exceeded",
  "message": "Too many requests. Please try again later."
}
```

- [ ] **Step 3: Update environment variables section**

Modify `README.md` (around line 35-43, add to env vars list):

```env
RATE_LIMIT_CAPACITY=60           # Token bucket capacity (requests per minute)
RATE_LIMIT_REFILL_RATE=1s        # Refill rate (1 token per second)
```

- [ ] **Step 4: Update AGENTS.md error handling**

Modify `AGENTS.md` (around line 65, add to error handling section):

```markdown
### Error Handling
- Provider errors → Service sentinel errors → Handler HTTP status codes
- Circuit breaker open → 503 Service Unavailable
- JWKS not ready → 503 Service Unavailable
- Rate limit exceeded → 429 Too Many Requests (global limit: 60 req/min)
- City not found → 404 Not Found
- Panics → 500 Internal Server Error (recovered, logged with stack trace)
```

- [ ] **Step 5: Verify documentation renders correctly**

Read through the updated sections in README.md and AGENTS.md to ensure:
- Markdown formatting is correct
- Code blocks are properly fenced
- Examples are accurate
- No typos

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document rate limiting feature and endpoints"
```

---

## Task 8: Final Testing and Verification

**Files:**
- No new files (verification task)

**Interfaces:**
- Consumes: All completed implementation and documentation
- Produces: Verified working system

- [ ] **Step 1: Run full test suite with coverage**

Run: `go test ./... -cover`

Expected output should show:
```
internal/ratelimit    coverage: XX% (target: 90%+)
internal/middleware   coverage: XX% (target: 100%)
```

Verify all tests PASS.

- [ ] **Step 2: Run tests with race detector**

Run: `go test ./... -race`

Expected: PASS with no race conditions detected

- [ ] **Step 3: Run linter**

Run: `golangci-lint run`

Expected: No errors or warnings

- [ ] **Step 4: Test Redis failover behavior**

1. Start server with Redis running: `go run cmd/api/main.go`
2. Send request: `curl -i http://localhost:8080/weather/Paris`
3. Stop Redis: `docker stop weather-redis`
4. Send request again: `curl -i http://localhost:8080/weather/Paris`
5. Check logs for: "Redis rate limiter failed, using memory fallback"
6. Verify request still succeeds (fail-open behavior)
7. Restart Redis: `docker start weather-redis`
8. Send request: `curl -i http://localhost:8080/weather/Paris`
9. Verify Redis is used again (no more error logs)

- [ ] **Step 5: Load test rate limiter**

Create simple load test script `test-ratelimit.sh`:

```bash
#!/bin/bash
echo "Sending 65 requests..."
for i in {1..65}; do
  status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/weather/Paris)
  echo "Request $i: $status"
done
```

Run: `chmod +x test-ratelimit.sh && ./test-ratelimit.sh`

Expected: First 60 requests return 200, last 5 return 429

- [ ] **Step 6: Verify rate limit headers**

Run: `curl -i http://localhost:8080/weather/Paris`

Verify headers present:
- `X-RateLimit-Limit: 60`
- `X-RateLimit-Remaining: <number>`
- `X-RateLimit-Reset: <timestamp>`

- [ ] **Step 7: Verify logs**

Check server logs for:
- Initialization: "memory rate limiter initialized (fallback)"
- Initialization: "Redis rate limiter initialized"
- Initialization: "adaptive rate limiter initialized"
- Rate limit exceeded: "rate limit exceeded" (WARN level)
- Redis failure: "Redis rate limiter failed, using memory fallback" (ERROR level)

- [ ] **Step 8: Final commit**

```bash
git add .
git commit -m "test: verify rate limiter end-to-end functionality"
```

- [ ] **Step 9: Create summary of changes**

Run: `git log --oneline | head -10`

Verify commits follow pattern:
- feat(ratelimit): ...
- feat(middleware): ...
- feat(main): ...
- docs: ...
- test: ...

---

## Verification Checklist

After completing all tasks, verify:

- [ ] All tests pass: `go test ./...`
- [ ] Coverage meets targets (90%+ for ratelimit, 100% for middleware)
- [ ] No race conditions: `go test ./... -race`
- [ ] Linter clean: `golangci-lint run`
- [ ] Rate limiting works (60 req/min enforced)
- [ ] Redis failover works (automatic fallback to memory)
- [ ] Fail-open works (service continues on internal errors)
- [ ] Rate limit headers present in all responses
- [ ] Documentation updated (README + AGENTS.md)
- [ ] All environment variables documented
- [ ] Logs show correct behavior (ERROR for Redis failures, WARN for rate limits)

---

**End of Implementation Plan**
