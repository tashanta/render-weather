// Package ratelimit provides rate limiting implementations for the Weather API.
package ratelimit

import "context"

// RateLimiter defines the interface for rate limiting implementations.
type RateLimiter interface {
	// Allow returns whether the request is allowed.
	// Returns:
	//   allowed: true if request is allowed
	//   remaining: number of tokens remaining after this request
	//   resetAt: Unix timestamp (seconds) when the bucket will be full again
	//   err: error if the rate limiter encountered an internal error
	Allow(ctx context.Context) (allowed bool, remaining int, resetAt int64, err error)
}
