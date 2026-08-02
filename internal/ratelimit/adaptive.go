package ratelimit

import (
	"context"

	"github.com/rs/zerolog/log"
)

// AdaptiveRateLimiter tries Redis first, falls back to memory on error.
type AdaptiveRateLimiter struct {
	redis    RateLimiter
	fallback RateLimiter
}

// NewAdaptiveRateLimiter creates a new adaptive rate limiter.
func NewAdaptiveRateLimiter(redis, fallback RateLimiter) *AdaptiveRateLimiter {
	return &AdaptiveRateLimiter{
		redis:    redis,
		fallback: fallback,
	}
}

// Allow implements RateLimiter.Allow.
func (a *AdaptiveRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
	// Try Redis first
	if a.redis != nil {
		allowed, remaining, resetAt, err := a.redis.Allow(ctx)
		if err == nil {
			return allowed, remaining, resetAt, nil
		}

		// Redis failed - log ERROR and fall back to memory
		log.Error().Err(err).Msg("Redis rate limiter failed, using memory fallback")
	}

	// Use memory fallback
	return a.fallback.Allow(ctx)
}
