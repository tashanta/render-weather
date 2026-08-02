package ratelimit_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/yourusername/render-weather/internal/ratelimit"
)

// MockRateLimiter implements RateLimiter for testing
type MockRateLimiter struct {
	AllowFunc func(ctx context.Context) (bool, int, int64, error)
}

func (m *MockRateLimiter) Allow(ctx context.Context) (bool, int, int64, error) {
	return m.AllowFunc(ctx)
}

// Compile-time check that MockRateLimiter implements RateLimiter
var _ ratelimit.RateLimiter = (*MockRateLimiter)(nil)

func TestRateLimiterInterface(t *testing.T) {
	mock := &MockRateLimiter{
		AllowFunc: func(ctx context.Context) (bool, int, int64, error) {
			return true, 59, 1722600060, nil
		},
	}

	allowed, remaining, resetAt, err := mock.Allow(context.Background())

	assert.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 59, remaining)
	assert.Equal(t, int64(1722600060), resetAt)
}
