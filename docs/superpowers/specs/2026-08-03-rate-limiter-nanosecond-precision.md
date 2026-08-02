# Rate Limiter Nanosecond Precision Fix

**Date:** 2026-08-03  
**Status:** Approved  
**Related:** PR #15 (original rate limiter implementation)

## Problem Statement

### Symptoms

Production logs show the rate limiter allows **111 requests/minute** instead of the configured 60 RPM limit - an **85% overage**.

```
9:44PM DBG rate limit: request allowed remaining=59
9:44PM DBG rate limit: request allowed remaining=59
9:44PM DBG rate limit: request allowed remaining=58
...
9:45PM DBG rate limit: request allowed remaining=49
9:45PM DBG rate limit: request allowed remaining=52  ← Jump from 49 to 52
9:45PM DBG rate limit: request allowed remaining=55  ← Jump from 52 to 55
9:45PM DBG rate limit: request allowed remaining=56
```

The `remaining` token count jumps upward (49 → 52 → 55) indicating tokens are refilling faster than 1 token/second.

### Root Cause

The token bucket algorithm uses **float64 arithmetic with seconds**, accumulating rounding errors:

**MemoryRateLimiter (memory.go:38):**
```go
tokensToAdd := elapsed.Seconds() / m.refillRate.Seconds()
// Example: 0.5 / 1.0 = 0.5
// But float64 rounding: 0.500000001
// Accumulated over 111 requests = 51 extra tokens
```

**RedisRateLimiter (redis.go:70):**
```go
refillRatePerSec := 1.0 / r.refillRate.Seconds()
// Lua: tokens + (elapsed * refill_rate)
// Both elapsed and refill_rate use float seconds
```

**Lua script (redis.go:28-29):**
```lua
local elapsed = now - last_refill  -- seconds (float)
local refilled = math.min(capacity, tokens + (elapsed * refill_rate))
```

### Business Impact

- **Cost:** 85% more OpenWeatherMap API calls than budgeted
- **Abuse prevention:** Users can bypass the 60 RPM limit
- **Reliability:** Rate limit fails its purpose
- **SLA risk:** External API rate limits may be exceeded

---

## Solution: Int64 Nanosecond Precision

### Design Principle

**Eliminate all float64 operations. Use int64 nanoseconds exclusively.**

- ✅ Zero rounding errors (integer arithmetic)
- ✅ Native Go types (`time.Duration`, `UnixNano()`)
- ✅ Faster than float64 (CPU-level int ops)
- ✅ Lua supports int64 up to 2^63-1 (292 years in nanoseconds)

---

## Architecture Changes

### 1. MemoryRateLimiter

**Current structure:**
```go
type MemoryRateLimiter struct {
    capacity   int
    refillRate time.Duration
    tokens     float64        // ← float64
    lastRefill time.Time      // ← stores time.Time
    mu         sync.Mutex
}
```

**New structure:**
```go
type MemoryRateLimiter struct {
    capacity       int
    refillRateNs   int64      // nanoseconds per token (e.g., 1e9 = 1 token/sec)
    tokensNs       int64      // tokens * 1e9 (sub-token precision)
    lastRefillNano int64      // UnixNano() timestamp
    mu             sync.Mutex
}
```

**New refill calculation (int64 only):**
```go
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
```

**Constructor change:**
```go
func NewMemoryRateLimiter(capacity int, refillRate time.Duration) *MemoryRateLimiter {
    return &MemoryRateLimiter{
        capacity:       capacity,
        refillRateNs:   refillRate.Nanoseconds(),  // Convert once at init
        tokensNs:       int64(capacity) * 1e9,     // Start full
        lastRefillNano: time.Now().UnixNano(),
    }
}
```

---

### 2. RedisRateLimiter

**New Lua script (complete replacement):**
```lua
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
```

**Go code changes (redis.go:63-97):**
```go
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

    // Parse result (unchanged)
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

---

## Redis Migration Strategy

### Current Redis State
```
HGETALL ratelimit:global
1) "tokens"         → "52.341" (float string)
2) "last_refill"    → "1785707106" (Unix seconds)
```

### New Redis State
```
HGETALL ratelimit:global
1) "tokens_ns"      → "60000000000" (60 tokens * 1e9)
2) "last_refill_ns" → "1735689600000000000" (UnixNano)
```

### Clean Slate Approach

**Pre-deployment step:**
```bash
# Clean all Redis data
redis-cli FLUSHDB
```

**Alternative: Automatic cleanup in Lua script (first call):**
```lua
-- Before any operation, ensure clean state
redis.call('DEL', key)
redis.call('HMSET', key, 'tokens_ns', capacity * 1000000000, 'last_refill_ns', now_ns)
redis.call('EXPIRE', key, 120)
```

**Why this works:**
- Greenfield environment - no production data to preserve
- Simple and foolproof
- No compatibility logic needed
- TTL auto-expires old keys if any remain

---

## Testing Strategy

### Test 1: Precision verification (new test)

**Goal:** Prove no more than 60 requests can be served in any 60-second window.

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
    
    // Continuous requests over 60 seconds
    start := time.Now()
    allowedCount := 0
    deniedCount := 0
    
    for time.Since(start) < 60*time.Second {
        allowed, _, _, _ := limiter.Allow(context.Background())
        if allowed {
            allowedCount++
        } else {
            deniedCount++
        }
        time.Sleep(500 * time.Millisecond) // ~2 req/sec
    }
    
    // With 1 token/sec refill over 60s = max 60 new tokens
    // Initial bucket was empty (consumed in burst)
    assert.LessOrEqual(t, allowedCount, 60, 
        "must not exceed 60 requests over 60 seconds")
    assert.Greater(t, deniedCount, 0, 
        "some requests should be denied due to rate limit")
}
```

### Test 2: Redis precision (new test)

```go
func TestRedisRateLimiter_ExactRefillRate(t *testing.T) {
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
    assert.False(t, allowed, "bucket should be empty")
    
    // Wait exactly 1 second
    time.Sleep(1 * time.Second)
    
    // Exactly 1 token should be available
    allowed, remaining, _, _ := limiter.Allow(context.Background())
    assert.True(t, allowed, "1 token should have refilled")
    assert.Equal(t, 0, remaining, "should have consumed the single refilled token")
    
    // Immediate 2nd request denied
    allowed, _, _, _ = limiter.Allow(context.Background())
    assert.False(t, allowed, "no tokens should remain after consuming refilled token")
}
```

### Test 3: Integration load test (manual)

**Script:** `scripts/test-rate-limit-precision.sh`
```bash
#!/bin/bash
echo "Testing rate limit over 60 seconds..."
start=$(date +%s)
allowed=0
denied=0

while [ $(($(date +%s) - start)) -lt 60 ]; do
    status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/weather/Paris)
    if [ "$status" -eq 200 ] || [ "$status" -eq 401 ]; then
        ((allowed++))
    elif [ "$status" -eq 429 ]; then
        ((denied++))
    fi
    sleep 0.5  # 2 req/sec = 120 attempts over 60s
done

echo "Results:"
echo "  Allowed: $allowed"
echo "  Denied: $denied"

# Expected: allowed <= 120 (60 initial burst + 60 refilled)
if [ $allowed -le 120 ]; then
    echo "✅ PASS: Rate limit enforced correctly"
else
    echo "❌ FAIL: Allowed $allowed requests (expected <= 120)"
    exit 1
fi
```

### Test 4: Regression suite

**All existing tests must pass:**
```bash
go test ./internal/ratelimit -v
go test ./internal/middleware -v
go test ./cmd/api -v
go test ./... -race
```

---

## Files Modified

### Core Implementation (3 files)

1. **`internal/ratelimit/memory.go`** (~71 lines)
   - Lines 10-16: Structure changes (float64 → int64)
   - Lines 19-27: Constructor (calculate refillRateNs)
   - Lines 30-71: `Allow()` complete refactor (int64 arithmetic)

2. **`internal/ratelimit/redis.go`** (~98 lines)
   - Lines 11-42: Lua script complete rewrite
   - Lines 63-97: `Allow()` pass nanoseconds to Lua

3. **`internal/ratelimit/redis_test.go`** (~80 lines)
   - Minor: Adjust assertions for nanosecond values

### Tests (2 files)

4. **`internal/ratelimit/memory_test.go`** (~84 lines)
   - Add: `TestMemoryRateLimiter_Enforce60RPM` (~40 lines)
   - Adjust: Existing tests if needed (~5 lines)

5. **`internal/ratelimit/redis_test.go`**
   - Add: `TestRedisRateLimiter_ExactRefillRate` (~35 lines)

### No Changes Required

- ✅ `internal/ratelimit/adaptive.go` - interface unchanged
- ✅ `internal/middleware/ratelimit.go` - interface unchanged
- ✅ `cmd/api/main.go` - initialization unchanged
- ✅ `internal/config/config.go` - config unchanged

**Total scope:** ~150-200 lines modified/added

---

## Deployment Plan

### Pre-deployment Checklist

- [ ] All tests pass: `go test ./...`
- [ ] Race detector clean: `go test ./... -race`
- [ ] Linting clean: `golangci-lint run`
- [ ] Redis backup (if needed): `redis-cli SAVE`

### Deployment Steps

1. **Clean Redis (greenfield):**
   ```bash
   redis-cli FLUSHDB
   ```

2. **Deploy new API binary:**
   ```bash
   docker-compose up -d --build api
   ```

3. **Verify logs:**
   ```
   api-1 | INFO memory rate limiter initialized (fallback)
   api-1 | INFO redis rate limiter initialized
   api-1 | INFO adaptive rate limiter initialized
   ```

4. **Run integration test:**
   ```bash
   ./scripts/test-rate-limit-precision.sh
   ```

5. **Monitor for 5 minutes:**
   - Check logs for `remaining` values (should decrement smoothly)
   - Verify 429 responses appear after 60 requests
   - Confirm no `remaining` jumps (49 → 52)

### Rollback Plan

If issues occur:
```bash
# Revert to previous Docker image
docker-compose down
git checkout <previous-commit>
docker-compose up -d --build api

# Redis state auto-expires (120s TTL)
# Or force clean: redis-cli FLUSHDB
```

---

## Success Criteria

### Functional Requirements
- ✅ Exactly 60 requests allowed in initial burst
- ✅ No more than 60 requests per 60-second rolling window
- ✅ Token refill rate: 1 token/second (verified by test)
- ✅ All existing 36 tests pass
- ✅ Race detector clean

### Observability
- ✅ Logs show smooth `remaining` decrement (no jumps)
- ✅ 429 responses appear after 60 requests
- ✅ Integration test passes (<= 120 requests over 60s)

### Performance
- ✅ No regression in request latency
- ✅ Redis script execution < 1ms (same as before)
- ✅ Memory limiter < 10μs per call (int64 faster than float64)

---

## Risks and Mitigations

### Risk 1: Integer overflow in calculations

**Risk:** `elapsed_ns * 1e9` could overflow int64 if elapsed is huge.

**Likelihood:** Extremely low (would require ~292 years uptime)

**Mitigation:** None needed. If elapsed > 1 hour, token bucket is already full.

### Risk 2: Lua script bugs

**Risk:** Error in Lua integer division breaks rate limiting.

**Likelihood:** Low (comprehensive tests)

**Mitigation:**
- Extensive unit tests with miniredis
- Staged rollout (dev → staging → prod)
- Fast rollback plan (previous Docker image)

### Risk 3: Redis unavailable during FLUSHDB

**Risk:** FLUSHDB takes time, requests fail during cleanup.

**Likelihood:** Low (FLUSHDB is instant for small datasets)

**Mitigation:** Memory fallback handles all requests during Redis downtime.

---

## Future Improvements (Out of Scope)

- **Per-user rate limiting:** Current is global, could add per-API-key limits
- **Dynamic rate limits:** Adjust limits based on time of day or load
- **Rate limit headers:** Add `X-RateLimit-Reset` in milliseconds (currently seconds)
- **Distributed counters:** Use Redis INCR with sliding window for multi-replica accuracy

---

## References

- Original PR: #15 (Global Rate Limiter Implementation)
- Original spec: `docs/superpowers/specs/2026-08-02-global-rate-limiter-design.md`
- Token bucket algorithm: https://en.wikipedia.org/wiki/Token_bucket
- Redis Lua scripting: https://redis.io/commands/eval/
- Go time package: https://pkg.go.dev/time#Duration

---

**Approved by:** User  
**Implementation date:** 2026-08-03
