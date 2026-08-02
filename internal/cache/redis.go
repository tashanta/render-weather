// internal/cache/redis.go
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yourusername/render-weather/internal/models"
)

// redisClient is an interface for Redis operations (for testing)
type redisClient interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) *redis.StatusCmd
	Keys(ctx context.Context, pattern string) *redis.StringSliceCmd
	Ping(ctx context.Context) *redis.StatusCmd
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd
}

type RedisCache struct {
	client redisClient
}

func NewRedisCache(redisURL string) (*RedisCache, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	// Test connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &RedisCache{client: client}, nil
}

func (c *RedisCache) Get(ctx context.Context, key string) (*models.Weather, error) {
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var weather models.Weather
	if err := json.Unmarshal([]byte(val), &weather); err != nil {
		return nil, fmt.Errorf("unmarshal weather: %w", err)
	}

	return &weather, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, value *models.Weather, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal weather: %w", err)
	}

	return c.client.Set(ctx, key, data, ttl).Err()
}

func (c *RedisCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	return c.client.Keys(ctx, pattern).Result()
}

// Client returns the underlying Redis client for rate limiting
func (c *RedisCache) Client() redisClient {
	return c.client
}
