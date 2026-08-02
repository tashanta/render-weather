package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/yourusername/render-weather/internal/cache"
)

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
