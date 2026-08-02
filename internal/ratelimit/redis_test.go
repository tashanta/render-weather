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
