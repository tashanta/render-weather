package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"
)

// MemoryRateLimiter implements a token bucket rate limiter in memory.
type MemoryRateLimiter struct {
	capacity     int
	refillRate   time.Duration
	tokens       float64
	lastRefill   time.Time
	mu           sync.Mutex
}

// NewMemoryRateLimiter creates a new in-memory rate limiter.
func NewMemoryRateLimiter(capacity int, refillRate time.Duration) *MemoryRateLimiter {
	return &MemoryRateLimiter{
		capacity:   capacity,
		refillRate: refillRate,
		tokens:     float64(capacity), // start with full bucket
		lastRefill: time.Now(),
	}
}

// Allow implements RateLimiter.Allow.
func (m *MemoryRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(m.lastRefill)

	// Refill tokens based on elapsed time
	tokensToAdd := elapsed.Seconds() / m.refillRate.Seconds()
	m.tokens = math.Min(float64(m.capacity), m.tokens+tokensToAdd)
	m.lastRefill = now

	// Try to consume 1 token
	if m.tokens >= 1.0 {
		m.tokens -= 1.0
		remaining := int(m.tokens)

		// Calculate resetAt (when bucket will be full again)
		tokensNeeded := float64(m.capacity) - m.tokens
		waitDuration := time.Duration(tokensNeeded * m.refillRate.Seconds() * float64(time.Second))
		resetAt := now.Add(waitDuration).Unix()
		
		// Ensure resetAt is always in the future
		if resetAt <= now.Unix() {
			resetAt = now.Unix() + 1
		}

		return true, remaining, resetAt, nil
	}

	// Not enough tokens
	tokensNeeded := 1.0 - m.tokens
	waitDuration := time.Duration(tokensNeeded * m.refillRate.Seconds() * float64(time.Second))
	resetAt := now.Add(waitDuration).Unix()
	
	// Ensure resetAt is always in the future
	if resetAt <= now.Unix() {
		resetAt = now.Unix() + 1
	}

	return false, 0, resetAt, nil
}
