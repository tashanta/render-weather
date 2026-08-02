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

func TestMemoryRateLimiter_NanosecondPrecision(t *testing.T) {
	// Create limiter: 10 tokens, 100ms per token
	limiter := ratelimit.NewMemoryRateLimiter(10, 100*time.Millisecond)
	
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

func TestMemoryRateLimiter_Enforce60RPM(t *testing.T) {
	limiter := ratelimit.NewMemoryRateLimiter(60, 1*time.Second)
	
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
