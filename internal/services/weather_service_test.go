// internal/services/weather_service_test.go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourusername/render-weather/internal/models"
)

type mockWeatherProvider struct {
	weather   *models.Weather
	err       error
	callCount int
}

func (m *mockWeatherProvider) GetCurrentWeather(ctx context.Context, city string) (*models.Weather, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return m.weather, nil
}

type mockCache struct {
	storage map[string]*models.Weather
}

func newMockCache() *mockCache {
	return &mockCache{storage: make(map[string]*models.Weather)}
}

func (m *mockCache) Get(ctx context.Context, key string) (*models.Weather, bool) {
	weather, found := m.storage[key]
	return weather, found
}

func (m *mockCache) Set(ctx context.Context, key string, value *models.Weather, ttl time.Duration) error {
	m.storage[key] = value
	return nil
}

func TestWeatherService_GetWeather_CacheHit(t *testing.T) {
	mockProvider := &mockWeatherProvider{}
	mockCache := newMockCache()
	
	// Pre-populate cache
	cachedWeather := &models.Weather{City: "Paris", Temperature: 18.5}
	mockCache.Set(context.Background(), "weather:paris", cachedWeather, 1*time.Hour)

	service := NewWeatherService(mockProvider, mockCache, 1*time.Second, 5, 30*time.Second, 1*time.Hour)

	weather, cacheHit, err := service.GetWeather(context.Background(), "Paris")

	require.NoError(t, err)
	assert.True(t, cacheHit)
	assert.Equal(t, "Paris", weather.City)
	assert.Equal(t, 0, mockProvider.callCount, "Should not call provider on cache hit")
}

func TestWeatherService_GetWeather_CacheMiss(t *testing.T) {
	mockProvider := &mockWeatherProvider{
		weather: &models.Weather{City: "London", Temperature: 15.0},
	}
	mockCache := newMockCache()

	service := NewWeatherService(mockProvider, mockCache, 1*time.Second, 5, 30*time.Second, 1*time.Hour)

	weather, cacheHit, err := service.GetWeather(context.Background(), "London")

	require.NoError(t, err)
	assert.False(t, cacheHit)
	assert.Equal(t, "London", weather.City)
	assert.Equal(t, 1, mockProvider.callCount)

	// Should now be cached
	cached, found := mockCache.Get(context.Background(), "weather:london")
	assert.True(t, found)
	assert.Equal(t, "London", cached.City)
}

func TestWeatherService_GetWeather_CircuitBreakerOpens(t *testing.T) {
	mockProvider := &mockWeatherProvider{
		err: assert.AnError,
	}
	mockCache := newMockCache()

	service := NewWeatherService(mockProvider, mockCache, 100*time.Millisecond, 2, 1*time.Second, 1*time.Hour)

	// First 2 calls should fail and open circuit
	for i := 0; i < 3; i++ {
		_, _, err := service.GetWeather(context.Background(), "TestCity")
		assert.Error(t, err)
	}

	// Circuit should now be open - provider should not be called
	initialCount := mockProvider.callCount
	_, _, err := service.GetWeather(context.Background(), "TestCity")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrCircuitBreakerOpen)
	assert.Equal(t, initialCount, mockProvider.callCount, "Circuit breaker should prevent call")
}
