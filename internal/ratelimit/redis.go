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
