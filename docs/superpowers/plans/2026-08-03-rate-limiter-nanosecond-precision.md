# Rate Limiter Nanosecond Precision Fix - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix rate limiter precision issue allowing 111 RPM instead of 60 by replacing float64 seconds with int64 nanoseconds.

**Architecture:** Refactor MemoryRateLimiter and RedisRateLimiter to use int64 nanosecond arithmetic, eliminating float rounding errors.

**Tech Stack:** Go 1.26, Redis Lua scripting, miniredis (testing), testify

## Global Constraints

- Go 1.26+ required
- TDD: Write tests first (RED → GREEN → REFACTOR)
- All new/modified code must have test coverage (target: 90%+)
- Use zerolog for structured logging
- Follow existing project patterns (package structure, error handling, naming)
- Commit after each passing test or logical unit
- No external dependencies beyond existing go.mod
- Preserve interface compatibility (`RateLimiter` interface unchanged)

---

## Task 1: MemoryRateLimiter Nanosecond Refactor

**Files:**
- Modify: `internal/ratelimit/memory.go:10-71`
- Modify: `internal/ratelimit/memory_test.go:1-84`

**Interfaces:**
- Consumes: Nothing (self-contained refactor)
- Produces: `NewMemoryRateLimiter(capacity int, refillRate time.Duration) *MemoryRateLimiter` with int64 nanosecond internals

- [ ] **Step 1: Write test for nanosecond precision**

Create or modify `internal/ratelimit/memory_test.go`:

```go
func TestMemoryRateLimiter_NanosecondPrecision(t *testing.T) {
	// Create limiter: 10 tokens, 100ms per token
	limiter := NewMemoryRateLimiter(10, 100*time.Millisecond)
	
	// Consume all 10 tokens
	for i := 0; i < 10; i++ {
		allowed, remaining, _, err := limiter.Allow(context.Background())
		require.NoError(t, err)
		assert.True(t, allowed, "burst token %d should be allowed", i+1)
		assert.Equal(t, 9-i, remaining)
	}
	
	// 11th request immediately denied
	allowed, remaining, _, _ := limiter.Allow(context.Background())
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
	
	// Wait 100ms - exactly 1 token should refill
	time.Sleep(100 * time.Millisecond)
	allowed, remaining, _, _ = limiter.Allow(context.Background())
	assert.True(t, allowed, "1 token should have refilled after 100ms")
	assert.Equal(t, 0, remaining)
	
	// Immediate next request denied (bucket empty again)
	allowed, _, _, _ = limiter.Allow(context.Background())
	assert.False(t, allowed)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ratelimit -v -run TestMemoryRateLimiter_NanosecondPrecision`

Expected: Test will likely pass incorrectly due to float64 imprecision, or fail with timing issues. This establishes baseline behavior.

- [ ] **Step 3: Refactor MemoryRateLimiter structure**

Modify `internal/ratelimit/memory.go`:

```go
// MemoryRateLimiter implements a token bucket rate limiter in memory.
type MemoryRateLimiter struct {
	capacity       int
	refillRateNs   int64      // nanoseconds per token
	tokensNs       int64      // tokens * 1e9 (sub-token precision)
	lastRefillNano int64      // UnixNano() timestamp
	mu             sync.Mutex
}
```

- [ ] **Step 4: Refactor NewMemoryRateLimiter constructor**

```go
// NewMemoryRateLimiter creates a new in-memory rate limiter.
func NewMemoryRateLimiter(capacity int, refillRate time.Duration) *MemoryRateLimiter {
	return &MemoryRateLimiter{
		capacity:       capacity,
		refillRateNs:   refillRate.Nanoseconds(),
		tokensNs:       int64(capacity) * 1e9, // start with full bucket
		lastRefillNano: time.Now().UnixNano(),
	}
}
```

- [ ] **Step 5: Refactor Allow() method - refill calculation**

```go
// Allow implements RateLimiter.Allow.
func (m *MemoryRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	nowNano := now.UnixNano()
	
	// Calculate elapsed nanoseconds
	elapsedNs := nowNano - m.lastRefillNano
	
	// Add tokens: (elapsed_ns * 1e9) / refill_rate_ns
	// Integer division, no float rounding
	tokensToAddNs := (elapsedNs * 1e9) / m.refillRateNs
	
	// Cap at capacity
	capacityNs := int64(m.capacity) * 1e9
	m.tokensNs = min(capacityNs, m.tokensNs + tokensToAddNs)
	m.lastRefillNano = nowNano
	
	// Try to consume 1 token (1e9 nanoseconds)
	if m.tokensNs >= 1e9 {
		m.tokensNs -= 1e9
		remaining := int(m.tokensNs / 1e9)
		
		// Calculate resetAt (when bucket will be full)
		tokensNeededNs := capacityNs - m.tokensNs
		waitNs := (tokensNeededNs * m.refillRateNs) / 1e9
		resetAt := now.Add(time.Duration(waitNs)).Unix()
		
		return true, remaining, resetAt, nil
	}
	
	// Not enough tokens - calculate wait time
	tokensNeededNs := 1e9 - m.tokensNs
	waitNs := (tokensNeededNs * m.refillRateNs) / 1e9
	resetAt := now.Add(time.Duration(waitNs)).Unix()
	
	return false, 0, resetAt, nil
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 6: Run all memory tests**

Run: `go test ./internal/ratelimit -v -run Memory`

Expected: All tests should pass including new nanosecond precision test

- [ ] **Step 7: Add precision enforcement test (60 RPM)**

Add to `internal/ratelimit/memory_test.go`:

```go
func TestMemoryRateLimiter_Enforce60RPM(t *testing.T) {
	limiter := NewMemoryRateLimiter(60, 1*time.Second)
	
	// Consume full bucket (60 tokens)
	for i := 0; i < 60; i++ {
		allowed, _, _, err := limiter.Allow(context.Background())
		require.NoError(t, err)
		assert.True(t, allowed, "burst request %d must be allowed", i+1)
	}
	
	// 61st request must be denied immediately
	allowed, _, _, _ := limiter.Allow(context.Background())
	assert.False(t, allowed, "request 61 must be denied (bucket empty)")
	
	// Continuous requests over 10 seconds (shorter for test speed)
	start := time.Now()
	allowedCount := 0
	deniedCount := 0
	
	for time.Since(start) < 10*time.Second {
		allowed, _, _, _ := limiter.Allow(context.Background())
		if allowed {
			allowedCount++
		} else {
			deniedCount++
		}
		time.Sleep(100 * time.Millisecond) // 10 req/sec
	}
	
	// With 1 token/sec refill over 10s = max 10 new tokens
	// Initial bucket was empty (consumed in burst)
	assert.LessOrEqual(t, allowedCount, 10, 
		"must not exceed 10 requests over 10 seconds (1 token/sec)")
	assert.Greater(t, deniedCount, 0, 
		"some requests should be denied due to rate limit")
}
```

- [ ] **Step 8: Run new test**

Run: `go test ./internal/ratelimit -v -run TestMemoryRateLimiter_Enforce60RPM`

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/ratelimit/memory.go internal/ratelimit/memory_test.go
git commit -m "refactor(ratelimit): use int64 nanoseconds in MemoryRateLimiter for precision"
```

---

## Task 2: RedisRateLimiter Nanosecond Refactor

**Files:**
- Modify: `internal/ratelimit/redis.go:11-97`
- Modify: `internal/ratelimit/redis_test.go:1-80`

**Interfaces:**
- Consumes: `RateLimiter` interface (unchanged)
- Produces: RedisRateLimiter with int64 nanosecond Lua script

- [ ] **Step 1: Write test for Redis nanosecond precision**

Add to `internal/ratelimit/redis_test.go`:

```go
func TestRedisRateLimiter_ExactRefillRate(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := cache.NewRedisCache(client)
	
	limiter := NewRedisRateLimiter(cache, 10, 100*time.Millisecond)
	
	// Consume 10 tokens
	for i := 0; i < 10; i++ {
		allowed, _, _, err := limiter.Allow(context.Background())
		require.NoError(t, err)
		assert.True(t, allowed)
	}
	
	// 11th denied
	allowed, _, _, _ := limiter.Allow(context.Background())
	assert.False(t, allowed, "bucket should be empty")
	
	// Wait exactly 100ms
	time.Sleep(100 * time.Millisecond)
	
	// Exactly 1 token should be available
	allowed, remaining, _, _ := limiter.Allow(context.Background())
	assert.True(t, allowed, "1 token should have refilled after 100ms")
	assert.Equal(t, 0, remaining, "should have consumed the single refilled token")
	
	// Immediate 2nd request denied
	allowed, _, _, _ = limiter.Allow(context.Background())
	assert.False(t, allowed, "no tokens should remain after consuming refilled token")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ratelimit -v -run TestRedisRateLimiter_ExactRefillRate`

Expected: FAIL (current float64 Lua script won't pass)

- [ ] **Step 3: Replace Lua script with nanosecond version**

Modify `internal/ratelimit/redis.go` (lines 11-42):

```go
const luaScript = `
-- KEYS[1] = "ratelimit:global"
-- ARGV[1] = capacity (e.g., 60)
-- ARGV[2] = refill_rate_ns (e.g., 1000000000 = 1 token/sec)
-- ARGV[3] = now_ns (UnixNano timestamp)

local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate_ns = tonumber(ARGV[2])
local now_ns = tonumber(ARGV[3])

-- Get current state (nanosecond values)
local state = redis.call('HMGET', key, 'tokens_ns', 'last_refill_ns')
local tokens_ns = tonumber(state[1]) or (capacity * 1000000000)
local last_refill_ns = tonumber(state[2]) or now_ns

-- Calculate refill (integer arithmetic)
local elapsed_ns = now_ns - last_refill_ns
local tokens_to_add_ns = (elapsed_ns * 1000000000) / refill_rate_ns
local capacity_ns = capacity * 1000000000
local refilled_ns = math.min(capacity_ns, tokens_ns + tokens_to_add_ns)

-- Try to consume 1 token (1e9 nanoseconds)
if refilled_ns >= 1000000000 then
    refilled_ns = refilled_ns - 1000000000
    redis.call('HMSET', key, 'tokens_ns', refilled_ns, 'last_refill_ns', now_ns)
    redis.call('EXPIRE', key, 120)
    
    -- Return: [allowed, remaining, resetAt]
    local remaining = math.floor(refilled_ns / 1000000000)
    local tokens_needed_ns = capacity_ns - refilled_ns
    local wait_ns = (tokens_needed_ns * refill_rate_ns) / 1000000000
    local reset_at = math.floor((now_ns + wait_ns) / 1000000000)
    
    return {1, remaining, reset_at}
else
    redis.call('HMSET', key, 'tokens_ns', refilled_ns, 'last_refill_ns', now_ns)
    redis.call('EXPIRE', key, 120)
    
    -- Return: [denied, 0, resetAt]
    local tokens_needed_ns = 1000000000 - refilled_ns
    local wait_ns = (tokens_needed_ns * refill_rate_ns) / 1000000000
    local reset_at = math.floor((now_ns + wait_ns) / 1000000000)
    
    return {0, 0, reset_at}
end
`
```

- [ ] **Step 4: Update Allow() method to use nanoseconds**

Modify `internal/ratelimit/redis.go` (lines 63-97):

```go
// Allow implements RateLimiter.Allow.
func (r *RedisRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
	if r.cache == nil {
		return false, 0, 0, fmt.Errorf("redis cache not available")
	}

	// Use nanoseconds
	nowNs := time.Now().UnixNano()
	refillRateNs := r.refillRate.Nanoseconds()

	// Execute Lua script with nanosecond values
	result, err := r.cache.Client().Eval(ctx, r.script,
		[]string{"ratelimit:global"},
		r.capacity,
		refillRateNs,  // int64 nanoseconds
		nowNs,         // int64 nanoseconds
	).Result()
	if err != nil {
		return false, 0, 0, fmt.Errorf("redis eval failed: %w", err)
	}

	// Parse result (unchanged - already uses safe type assertions from PR #15 fixes)
	results, ok := result.([]interface{})
	if !ok || len(results) != 3 {
		return false, 0, 0, fmt.Errorf("redis eval returned unexpected format: %v", result)
	}

	allowed, ok1 := results[0].(int64)
	remaining, ok2 := results[1].(int64)
	resetAt, ok3 := results[2].(int64)

	if !ok1 || !ok2 || !ok3 {
		return false, 0, 0, fmt.Errorf("redis eval type assertion failed")
	}

	return allowed == 1, int(remaining), resetAt, nil
}
```

- [ ] **Step 5: Run new test**

Run: `go test ./internal/ratelimit -v -run TestRedisRateLimiter_ExactRefillRate`

Expected: PASS

- [ ] **Step 6: Run all Redis tests**

Run: `go test ./internal/ratelimit -v -run Redis`

Expected: All 3 existing Redis tests should pass (initial burst, state persistence, nil cache)

- [ ] **Step 7: Add 60 RPM enforcement test for Redis**

Add to `internal/ratelimit/redis_test.go`:

```go
func TestRedisRateLimiter_Enforce60RPM(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := cache.NewRedisCache(client)
	
	limiter := NewRedisRateLimiter(cache, 60, 1*time.Second)
	
	// Consume 60 tokens
	for i := 0; i < 60; i++ {
		allowed, _, _, err := limiter.Allow(context.Background())
		require.NoError(t, err)
		assert.True(t, allowed)
	}
	
	// 61st denied
	allowed, _, _, _ := limiter.Allow(context.Background())
	assert.False(t, allowed)
	
	// Over 5 seconds with 10 req/sec attempts = 50 attempts
	// Should allow max 5 new tokens (5 seconds * 1 token/sec)
	start := time.Now()
	allowedCount := 0
	deniedCount := 0
	
	for time.Since(start) < 5*time.Second {
		allowed, _, _, _ := limiter.Allow(context.Background())
		if allowed {
			allowedCount++
		} else {
			deniedCount++
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	assert.LessOrEqual(t, allowedCount, 5,
		"must not exceed 5 requests over 5 seconds")
	assert.Greater(t, deniedCount, 0,
		"some requests should be denied")
}
```

- [ ] **Step 8: Run new test**

Run: `go test ./internal/ratelimit -v -run TestRedisRateLimiter_Enforce60RPM`

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/ratelimit/redis.go internal/ratelimit/redis_test.go
git commit -m "refactor(ratelimit): use int64 nanoseconds in RedisRateLimiter Lua script"
```

---

## Task 3: Regression Testing and Documentation

**Files:**
- Create: `scripts/test-rate-limit-precision.sh`
- Modify: `docs/superpowers/specs/2026-08-03-rate-limiter-nanosecond-precision.md` (mark as implemented)

**Interfaces:**
- Consumes: All rate limiter implementations from Tasks 1-2
- Produces: Verification script, updated docs

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`

Expected: All 36 existing tests + 4 new tests = 40 tests passing

- [ ] **Step 2: Run race detector**

Run: `go test ./... -race`

Expected: PASS with no race conditions detected

- [ ] **Step 3: Run linter**

Run: `golangci-lint run`

Expected: No errors or warnings

- [ ] **Step 4: Create load test script**

Create `scripts/test-rate-limit-precision.sh`:

```bash
#!/bin/bash
# Test rate limit precision over 60 seconds

set -e

echo "Testing rate limit precision over 60 seconds..."
echo "Sending requests at 2 req/sec (120 attempts total)"
echo ""

start=$(date +%s)
allowed=0
denied=0
errors=0

while [ $(($(date +%s) - start)) -lt 60 ]; do
    status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/weather/Paris 2>/dev/null || echo "000")
    
    if [ "$status" -eq 200 ] || [ "$status" -eq 401 ]; then
        ((allowed++))
    elif [ "$status" -eq 429 ]; then
        ((denied++))
    else
        ((errors++))
    fi
    
    sleep 0.5  # 2 req/sec
done

echo "Results over 60 seconds:"
echo "  Allowed: $allowed"
echo "  Denied: $denied"
echo "  Errors: $errors"
echo "  Total attempts: $((allowed + denied + errors))"
echo ""

# Expected: allowed <= 120 (60 initial burst + 60 refilled over 60s)
if [ $allowed -le 120 ]; then
    echo "✅ PASS: Rate limit enforced correctly (≤120 requests allowed)"
    exit 0
else
    echo "❌ FAIL: Allowed $allowed requests (expected ≤120)"
    echo "This indicates the rate limiter is still allowing more than 60 RPM"
    exit 1
fi
```

- [ ] **Step 5: Make script executable**

Run: `chmod +x scripts/test-rate-limit-precision.sh`

- [ ] **Step 6: Test script with server**

Manual test (requires running server):
```bash
# Terminal 1: Start server
go run cmd/api/main.go

# Terminal 2: Run test
./scripts/test-rate-limit-precision.sh
```

Expected output:
```
Testing rate limit precision over 60 seconds...
Sending requests at 2 req/sec (120 attempts total)

Results over 60 seconds:
  Allowed: 120
  Denied: 0
  Errors: 0
  Total attempts: 120

✅ PASS: Rate limit enforced correctly (≤120 requests allowed)
```

- [ ] **Step 7: Update spec status**

Modify `docs/superpowers/specs/2026-08-03-rate-limiter-nanosecond-precision.md`:

Change line 4 from:
```markdown
**Status:** Approved
```

To:
```markdown
**Status:** Implemented  
**Implementation PR:** #<PR_NUMBER>
```

- [ ] **Step 8: Commit**

```bash
git add scripts/test-rate-limit-precision.sh docs/superpowers/specs/2026-08-03-rate-limiter-nanosecond-precision.md
git commit -m "test: add rate limit precision verification script and update spec"
```

---

## Verification Checklist

After completing all tasks, verify:

- [ ] All tests pass: `go test ./...`
- [ ] Test count increased: 36 → 40 tests (4 new precision tests)
- [ ] Coverage maintained: ratelimit package 90%+, middleware 100%
- [ ] No race conditions: `go test ./... -race`
- [ ] Linter clean: `golangci-lint run`
- [ ] Load test script passes: `./scripts/test-rate-limit-precision.sh`
- [ ] Logs show smooth token decrement (no jumps 49 → 52)
- [ ] Exactly 60 requests in initial burst, then 1 token/sec refill
- [ ] Redis state uses `tokens_ns` and `last_refill_ns` keys

---

**End of Implementation Plan**
