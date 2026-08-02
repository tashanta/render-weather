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
