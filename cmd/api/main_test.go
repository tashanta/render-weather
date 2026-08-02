// cmd/api/main_test.go
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/render-weather/internal/cache"
	"github.com/yourusername/render-weather/internal/config"
	"github.com/yourusername/render-weather/internal/handlers"
	"github.com/yourusername/render-weather/internal/middleware"
	"github.com/yourusername/render-weather/internal/providers"
	"github.com/yourusername/render-weather/internal/ratelimit"
	"github.com/yourusername/render-weather/internal/services"
)

// TestRateLimiterIntegration verifies that rate limiting middleware works when integrated
// This test validates the middleware integration pattern used in main.go
func TestRateLimiterIntegration(t *testing.T) {
	// Setup: Create a minimal server stack similar to main.go

	// 1. Create memory cache (no Redis for test)
	memCache := cache.NewMemoryCache(100)
	hybridCache := cache.NewHybridCache(memCache, nil, 1*time.Hour)

	// 2. Create rate limiters
	cfg := &config.Config{
		RateLimitCapacity:   5, // Low capacity for testing
		RateLimitRefillRate: 1 * time.Second,
	}

	memRateLimiter := ratelimit.NewMemoryRateLimiter(
		cfg.RateLimitCapacity,
		cfg.RateLimitRefillRate,
	)

	redisRateLimiter := ratelimit.NewRedisRateLimiter(
		nil, // No Redis for test
		cfg.RateLimitCapacity,
		cfg.RateLimitRefillRate,
	)

	rateLimiter := ratelimit.NewAdaptiveRateLimiter(redisRateLimiter, memRateLimiter)

	// 3. Create weather service
	owmProvider := providers.NewOpenWeatherMapProvider("test-key", 5*time.Second)
	weatherService := services.NewWeatherService(
		owmProvider,
		hybridCache,
		5*time.Second,
		5,
		30*time.Second,
		1*time.Hour,
	)

	// 4. Create router with middleware stack (same order as main.go)
	router := chi.NewRouter()
	router.Use(middleware.Recovery())
	router.Use(middleware.Logging())
	router.Use(middleware.CORS([]string{"*"}))
	router.Use(middleware.Prometheus(prometheus.NewRegistry()))
	router.Use(middleware.RateLimit(rateLimiter)) // Rate limit BEFORE auth

	// 5. Register protected route (no auth for test)
	router.Get("/weather/{city}", handlers.WeatherHandler(weatherService))

	// Test: Send capacity+1 requests, expect last one to be rate limited
	testServer := httptest.NewServer(router)
	defer testServer.Close()

	// Send requests up to capacity
	for i := 0; i < cfg.RateLimitCapacity; i++ {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/weather/Paris", testServer.URL), nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		// First 5 should pass (may be 401/200 depending on auth, but not 429)
		assert.NotEqual(t, http.StatusTooManyRequests, resp.StatusCode,
			"Request %d should not be rate limited", i+1)
	}

	// Next request should be rate limited
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/weather/Paris", testServer.URL), nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode,
		"Request beyond capacity should be rate limited")

	// Verify rate limit headers are present
	assert.NotEmpty(t, resp.Header.Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, resp.Header.Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, resp.Header.Get("X-RateLimit-Reset"))
}

// TestMain ensures test environment is set up
func TestMain(m *testing.M) {
	// Set required env vars for tests
	os.Setenv("PORT", "8080")
	os.Setenv("REDIS_URL", "redis://localhost:6379")
	os.Setenv("OPENWEATHER_API_KEY", "test-key")
	os.Setenv("AUTH_ENABLED", "false")
	os.Setenv("ALLOWED_ORIGINS", "*")

	os.Exit(m.Run())
}
