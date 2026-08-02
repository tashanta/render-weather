package ratelimit_test

import (
	"context"
	"sync"
	"sync/atomic"
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
