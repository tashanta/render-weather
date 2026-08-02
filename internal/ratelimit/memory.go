package ratelimit

import (
	"context"
	"sync"
	"time"
)

// MemoryRateLimiter implements a token bucket rate limiter in memory.
type MemoryRateLimiter struct {
	capacity       int
	refillRateNs   int64 // nanoseconds per token
	tokensNs       int64 // tokens * 1e9 (sub-token precision)
	lastRefillNano int64 // UnixNano() timestamp
	mu             sync.Mutex
}

// NewMemoryRateLimiter creates a new in-memory rate limiter.
func NewMemoryRateLimiter(capacity int, refillRate time.Duration) *MemoryRateLimiter {
	return &MemoryRateLimiter{
		capacity:       capacity,
		refillRateNs:   refillRate.Nanoseconds(),
		tokensNs:       int64(capacity) * 1e9, // start with full bucket
		lastRefillNano: time.Now().UnixNano(),
	}
}

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
	m.tokensNs = minInt64(capacityNs, m.tokensNs+tokensToAddNs)
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

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
