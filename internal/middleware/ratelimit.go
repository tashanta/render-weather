package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/yourusername/render-weather/internal/ratelimit"
)

// RateLimit returns a middleware that enforces rate limiting.
func RateLimit(limiter ratelimit.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			allowed, remaining, resetAt, err := limiter.Allow(ctx)

			// Always add rate limit headers (RFC 6585 standard)
			w.Header().Set("X-RateLimit-Limit", "60")
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

			if err != nil {
				// Internal error - fail-open (service availability priority)
				log.Error().
					Err(err).
					Str("path", r.URL.Path).
					Msg("rate limiter internal error")
				next.ServeHTTP(w, r)
				return
			}

			if !allowed {
				// Rate limit exceeded
				retryAfter := resetAt - time.Now().Unix()
				if retryAfter < 0 {
					retryAfter = 0
				}

				w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)

				json.NewEncoder(w).Encode(map[string]string{
					"error":   "rate_limit_exceeded",
					"message": "Too many requests. Please try again later.",
				})

				log.Warn().
					Str("path", r.URL.Path).
					Int64("reset_at", resetAt).
					Msg("rate limit exceeded")
				return
			}

			// Request allowed
			log.Debug().
				Str("path", r.URL.Path).
				Int("remaining", remaining).
				Int64("reset_at", resetAt).
				Msg("rate limit: request allowed")

			next.ServeHTTP(w, r)
		})
	}
}
