// internal/cache/memory_test.go
package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/yourusername/render-weather/internal/models"
)

func TestMemoryCache_SetAndGet(t *testing.T) {
	cache := NewMemoryCache(10)

	weather := &models.Weather{
		City:        "Paris",
		Temperature: 18.5,
	}

	cache.Set("weather:paris", weather, 1*time.Hour)

	retrieved, found := cache.Get("weather:paris")

	assert.True(t, found)
	assert.Equal(t, "Paris", retrieved.City)
	assert.Equal(t, 18.5, retrieved.Temperature)
}

func TestMemoryCache_GetNotFound(t *testing.T) {
	cache := NewMemoryCache(10)

	_, found := cache.Get("nonexistent")

	assert.False(t, found)
}

func TestMemoryCache_TTLExpiration(t *testing.T) {
	cache := NewMemoryCache(10)

	weather := &models.Weather{City: "Paris"}
	cache.Set("weather:paris", weather, 50*time.Millisecond)

	// Should exist immediately
	_, found := cache.Get("weather:paris")
	assert.True(t, found)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	_, found = cache.Get("weather:paris")
	assert.False(t, found)
}

func TestMemoryCache_LRUEviction(t *testing.T) {
	cache := NewMemoryCache(2) // Max 2 items

	cache.Set("key1", &models.Weather{City: "Paris"}, 1*time.Hour)
	cache.Set("key2", &models.Weather{City: "London"}, 1*time.Hour)
	cache.Set("key3", &models.Weather{City: "Berlin"}, 1*time.Hour)

	// key1 should be evicted (least recently used)
	_, found := cache.Get("key1")
	assert.False(t, found)

	_, found = cache.Get("key2")
	assert.True(t, found)

	_, found = cache.Get("key3")
	assert.True(t, found)
}

func TestMemoryCache_Keys(t *testing.T) {
	cache := NewMemoryCache(10)

	cache.Set("key1", &models.Weather{City: "Paris"}, 1*time.Hour)
	cache.Set("key2", &models.Weather{City: "London"}, 1*time.Hour)

	keys := cache.Keys()

	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "key1")
	assert.Contains(t, keys, "key2")
}

func TestMemoryCache_Len(t *testing.T) {
	cache := NewMemoryCache(10)

	assert.Equal(t, 0, cache.Len())

	cache.Set("key1", &models.Weather{City: "Paris"}, 1*time.Hour)
	assert.Equal(t, 1, cache.Len())

	cache.Set("key2", &models.Weather{City: "London"}, 1*time.Hour)
	assert.Equal(t, 2, cache.Len())
}
