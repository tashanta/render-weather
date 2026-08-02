package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/yourusername/render-weather/internal/middleware"
	"github.com/yourusername/render-weather/internal/ratelimit"
)

type mockRateLimiter struct {
	AllowFunc func(ctx context.Context) (bool, int, int64, error)
}

func (m *mockRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
	return m.AllowFunc(ctx)
}

// Compile-time check that mockRateLimiter implements ratelimit.RateLimiter
var _ ratelimit.RateLimiter = (*mockRateLimiter)(nil)

func TestRateLimitMiddleware_Allowed(t *testing.T) {
	limiter := &mockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return true, 42, 1722600060, nil
		},
	}

	handler := middleware.RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/weather/Paris", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "60", rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "42", rec.Header().Get("X-RateLimit-Remaining"))
	assert.Equal(t, "1722600060", rec.Header().Get("X-RateLimit-Reset"))
	assert.Equal(t, "OK", rec.Body.String())
}

func TestRateLimitMiddleware_Exceeded(t *testing.T) {
	limiter := &mockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return false, 0, 1722600090, nil
		},
	}

	handler := middleware.RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called when rate limited")
	}))

	req := httptest.NewRequest(http.MethodGet, "/weather/Paris", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "60", rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
	assert.Equal(t, "1722600090", rec.Header().Get("X-RateLimit-Reset"))
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	assert.Contains(t, rec.Body.String(), "rate_limit_exceeded")
}

func TestRateLimitMiddleware_FailOpen(t *testing.T) {
	limiter := &mockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return false, 0, 0, errors.New("internal limiter error")
		},
	}

	handler := middleware.RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/weather/Paris", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Request should pass despite limiter error (fail-open)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK", rec.Body.String())
}
