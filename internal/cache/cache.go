// internal/cache/cache.go
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/yourusername/render-weather/internal/models"
)

// HybridCache coordinates between L1 (memory) and L2 (Redis) caches
type HybridCache struct {
	l1  *MemoryCache
	l2  *RedisCache
	ttl time.Duration
}

func NewHybridCache(l1 *MemoryCache, l2 *RedisCache, ttl time.Duration) *HybridCache {
	return &HybridCache{
		l1:  l1,
		l2:  l2,
		ttl: ttl,
	}
}

// Get attempts L1 first, then L2, promoting L2 hits to L1
func (c *HybridCache) Get(ctx context.Context, key string) (*models.Weather, bool) {
	// Try L1 first
	if weather, found := c.l1.Get(key); found {
		return weather, true
	}

	// Try L2
	weather, err := c.l2.Get(ctx, key)
	if err != nil {
		return nil, false
	}

	// Promote to L1
	c.l1.Set(key, weather, c.ttl)

	return weather, true
}

// Set stores in both L1 and L2
func (c *HybridCache) Set(ctx context.Context, key string, value *models.Weather, ttl time.Duration) error {
	// Always set L1
	c.l1.Set(key, value, ttl)

	// Try to set L2, but don't fail if Redis is down
	if err := c.l2.Set(ctx, key, value, ttl); err != nil {
		// Degraded mode is acceptable - continue with L1 only
	}

	return nil
}

// PreloadFromRedis loads all weather keys from Redis into L1 (background initialization)
func (c *HybridCache) PreloadFromRedis(ctx context.Context) error {
	keys, err := c.l2.Keys(ctx, "weather:*")
	if err != nil {
		return fmt.Errorf("fetch redis keys: %w", err)
	}

	loaded := 0
	for _, key := range keys {
		weather, err := c.l2.Get(ctx, key)
		if err != nil {
			continue
		}
		c.l1.Set(key, weather, c.ttl)
		loaded++
	}

	log.Info().Int("loaded", loaded).Msg("preloaded entries from Redis to L1 cache")
	return nil
}
