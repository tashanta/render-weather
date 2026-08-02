package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/render-weather/internal/cache"
	"github.com/yourusername/render-weather/internal/ratelimit"
)

func setupTestRedis(t *testing.T) (*cache.RedisCache, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)

	// Create RedisCache with test URL
	redisCache, err := cache.NewRedisCache("redis://" + mr.Addr())
	require.NoError(t, err)

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

func TestRedisRateLimiter_ExactRefillRate(t *testing.T) {
	redisCache, mr := setupTestRedis(t)
	defer mr.Close()

	limiter := ratelimit.NewRedisRateLimiter(redisCache, 10, 100*time.Millisecond)
	ctx := context.Background()

	// Consume 10 tokens
	for i := 0; i < 10; i++ {
		allowed, _, _, err := limiter.Allow(ctx)
		require.NoError(t, err)
		assert.True(t, allowed)
	}

	// 11th denied
	allowed, _, _, _ := limiter.Allow(ctx)
	assert.False(t, allowed, "bucket should be empty")

	// Wait exactly 100ms
	time.Sleep(100 * time.Millisecond)

	// Exactly 1 token should be available
	allowed, remaining, _, _ := limiter.Allow(ctx)
	assert.True(t, allowed, "1 token should have refilled after 100ms")
	assert.Equal(t, 0, remaining, "should have consumed the single refilled token")

	// Immediate 2nd request denied
	allowed, _, _, _ = limiter.Allow(ctx)
	assert.False(t, allowed, "no tokens should remain after consuming refilled token")
}

func TestRedisRateLimiter_Enforce60RPM(t *testing.T) {
	redisCache, mr := setupTestRedis(t)
	defer mr.Close()

	limiter := ratelimit.NewRedisRateLimiter(redisCache, 60, 1*time.Second)
	ctx := context.Background()

	// Consume 60 tokens
	for i := 0; i < 60; i++ {
		allowed, _, _, err := limiter.Allow(ctx)
		require.NoError(t, err)
		assert.True(t, allowed)
	}

	// 61st denied
	allowed, _, _, _ := limiter.Allow(ctx)
	assert.False(t, allowed)

	// Over 5 seconds with 10 req/sec attempts = 50 attempts
	// Should allow max 5 new tokens (5 seconds * 1 token/sec)
	start := time.Now()
	allowedCount := 0
	deniedCount := 0

	for time.Since(start) < 5*time.Second {
		allowed, _, _, _ := limiter.Allow(ctx)
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
