// internal/cache/redis_test.go
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

// Mock Redis client for testing
type mockRedisClient struct {
	data map[string]string
}

func newMockRedisClient() *mockRedisClient {
	return &mockRedisClient{
		data: make(map[string]string),
	}
}

func (m *mockRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	val, exists := m.data[key]
	cmd := redis.NewStringCmd(ctx)
	if !exists {
		cmd.SetErr(redis.Nil)
	} else {
		cmd.SetVal(val)
	}
	return cmd
}

func (m *mockRedisClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) *redis.StatusCmd {
	var strVal string
	switch v := value.(type) {
	case string:
		strVal = v
	case []byte:
		strVal = string(v)
	default:
		strVal = ""
	}
	m.data[key] = strVal
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetVal("OK")
	return cmd
}

func (m *mockRedisClient) Keys(ctx context.Context, pattern string) *redis.StringSliceCmd {
	keys := make([]string, 0)
	for k := range m.data {
		keys = append(keys, k)
	}
	cmd := redis.NewStringSliceCmd(ctx)
	cmd.SetVal(keys)
	return cmd
}

func (m *mockRedisClient) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	cmd.SetVal("PONG")
	return cmd
}

func (m *mockRedisClient) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	// Not needed for cache tests, return dummy command
	cmd := redis.NewCmd(ctx)
	cmd.SetVal(nil)
	return cmd
}

func TestRedisCache_SetAndGet(t *testing.T) {
	mock := newMockRedisClient()
	cache := &RedisCache{client: mock}

	weather := &models.Weather{
		City:        "Paris",
		Temperature: 18.5,
	}

	err := cache.Set(context.Background(), "weather:paris", weather, 1*time.Hour)
	require.NoError(t, err)

	retrieved, err := cache.Get(context.Background(), "weather:paris")

	require.NoError(t, err)
	assert.Equal(t, "Paris", retrieved.City)
	assert.Equal(t, 18.5, retrieved.Temperature)
}

func TestRedisCache_GetNotFound(t *testing.T) {
	mock := newMockRedisClient()
	cache := &RedisCache{client: mock}

	_, err := cache.Get(context.Background(), "nonexistent")

	assert.Error(t, err)
	assert.Equal(t, redis.Nil, err)
}

func TestRedisCache_Keys(t *testing.T) {
	mock := newMockRedisClient()
	cache := &RedisCache{client: mock}

	cache.Set(context.Background(), "weather:paris", &models.Weather{City: "Paris"}, 1*time.Hour)
	cache.Set(context.Background(), "weather:london", &models.Weather{City: "London"}, 1*time.Hour)

	keys, err := cache.Keys(context.Background(), "weather:*")

	require.NoError(t, err)
	assert.Len(t, keys, 2)
}
