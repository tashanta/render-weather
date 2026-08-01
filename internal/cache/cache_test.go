// internal/cache/cache_test.go
package cache

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/render-weather/internal/models"
)

func TestHybridCache_Get_L1Hit(t *testing.T) {
	mockRedis := newMockRedisClient()
	redisCache := &RedisCache{client: mockRedis}
	memCache := NewMemoryCache(10)
	hybrid := NewHybridCache(memCache, redisCache, 1*time.Hour)

	weather := &models.Weather{City: "Paris", Temperature: 18.5}
	memCache.Set("weather:paris", weather, 1*time.Hour)

	retrieved, found := hybrid.Get(context.Background(), "weather:paris")

	assert.True(t, found)
	assert.Equal(t, "Paris", retrieved.City)
}

func TestHybridCache_Get_L1Miss_L2Hit(t *testing.T) {
	mockRedis := newMockRedisClient()
	redisCache := &RedisCache{client: mockRedis}
	memCache := NewMemoryCache(10)
	hybrid := NewHybridCache(memCache, redisCache, 1*time.Hour)

	// Store only in Redis
	weather := &models.Weather{City: "London", Temperature: 15.0}
	redisCache.Set(context.Background(), "weather:london", weather, 1*time.Hour)

	retrieved, found := hybrid.Get(context.Background(), "weather:london")

	assert.True(t, found)
	assert.Equal(t, "London", retrieved.City)

	// Should now be in L1
	l1Retrieved, l1Found := memCache.Get("weather:london")
	assert.True(t, l1Found)
	assert.Equal(t, "London", l1Retrieved.City)
}

func TestHybridCache_Get_BothMiss(t *testing.T) {
	mockRedis := newMockRedisClient()
	redisCache := &RedisCache{client: mockRedis}
	memCache := NewMemoryCache(10)
	hybrid := NewHybridCache(memCache, redisCache, 1*time.Hour)

	_, found := hybrid.Get(context.Background(), "nonexistent")

	assert.False(t, found)
}

func TestHybridCache_Set(t *testing.T) {
	mockRedis := newMockRedisClient()
	redisCache := &RedisCache{client: mockRedis}
	memCache := NewMemoryCache(10)
	hybrid := NewHybridCache(memCache, redisCache, 1*time.Hour)

	weather := &models.Weather{City: "Berlin", Temperature: 20.0}

	err := hybrid.Set(context.Background(), "weather:berlin", weather, 1*time.Hour)

	require.NoError(t, err)

	// Check both caches
	l1Weather, l1Found := memCache.Get("weather:berlin")
	assert.True(t, l1Found)
	assert.Equal(t, "Berlin", l1Weather.City)

	l2Weather, l2Err := redisCache.Get(context.Background(), "weather:berlin")
	require.NoError(t, l2Err)
	assert.Equal(t, "Berlin", l2Weather.City)
}

func TestHybridCache_Set_RedisFailure(t *testing.T) {
	// Redis that always fails
	failingRedis := &mockFailingRedis{}
	redisCache := &RedisCache{client: failingRedis}
	memCache := NewMemoryCache(10)
	hybrid := NewHybridCache(memCache, redisCache, 1*time.Hour)

	weather := &models.Weather{City: "Paris", Temperature: 18.5}

	err := hybrid.Set(context.Background(), "weather:paris", weather, 1*time.Hour)

	// Should not error, just log
	require.NoError(t, err)

	// L1 should still work
	l1Weather, found := memCache.Get("weather:paris")
	assert.True(t, found)
	assert.Equal(t, "Paris", l1Weather.City)
}

func TestHybridCache_PreloadFromRedis(t *testing.T) {
	mockRedis := newMockRedisClient()
	redisCache := &RedisCache{client: mockRedis}
	memCache := NewMemoryCache(10)
	hybrid := NewHybridCache(memCache, redisCache, 1*time.Hour)

	// Preload Redis with some data
	redisCache.Set(context.Background(), "weather:paris", &models.Weather{City: "Paris", Temperature: 18.5}, 1*time.Hour)
	redisCache.Set(context.Background(), "weather:london", &models.Weather{City: "London", Temperature: 15.0}, 1*time.Hour)

	err := hybrid.PreloadFromRedis(context.Background())

	require.NoError(t, err)

	// Verify L1 has the data
	assert.Equal(t, 2, memCache.Len())

	parisWeather, found := memCache.Get("weather:paris")
	assert.True(t, found)
	assert.Equal(t, "Paris", parisWeather.City)

	londonWeather, found := memCache.Get("weather:london")
	assert.True(t, found)
	assert.Equal(t, "London", londonWeather.City)
}

type mockFailingRedis struct{}

func (m *mockFailingRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx)
	cmd.SetErr(redis.Nil)
	return cmd
}

func (m *mockFailingRedis) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetErr(redis.ErrClosed)
	return cmd
}

func (m *mockFailingRedis) Keys(ctx context.Context, pattern string) *redis.StringSliceCmd {
	cmd := redis.NewStringSliceCmd(ctx)
	cmd.SetErr(redis.ErrClosed)
	return cmd
}

func (m *mockFailingRedis) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetErr(redis.ErrClosed)
	return cmd
}
