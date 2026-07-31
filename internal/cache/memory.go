// internal/cache/memory.go
package cache

import (
	"container/list"
	"sync"
	"time"

	"github.com/yourusername/render-weather/internal/models"
)

type cacheEntry struct {
	key       string
	value     *models.Weather
	expiresAt time.Time
}

// MemoryCache is an LRU cache with TTL support
type MemoryCache struct {
	maxSize int
	mu      sync.RWMutex
	items   map[string]*list.Element
	lru     *list.List
}

func NewMemoryCache(maxSize int) *MemoryCache {
	return &MemoryCache{
		maxSize: maxSize,
		items:   make(map[string]*list.Element),
		lru:     list.New(),
	}
}

func (c *MemoryCache) Get(key string) (*models.Weather, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, found := c.items[key]
	if !found {
		return nil, false
	}

	entry := elem.Value.(*cacheEntry)

	// Check TTL
	if time.Now().After(entry.expiresAt) {
		c.removeElement(elem)
		return nil, false
	}

	// Move to front (most recently used)
	c.lru.MoveToFront(elem)

	return entry.value, true
}

func (c *MemoryCache) Set(key string, value *models.Weather, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing
	if elem, found := c.items[key]; found {
		entry := elem.Value.(*cacheEntry)
		entry.value = value
		entry.expiresAt = time.Now().Add(ttl)
		c.lru.MoveToFront(elem)
		return
	}

	// Evict if at capacity
	if c.lru.Len() >= c.maxSize {
		oldest := c.lru.Back()
		if oldest != nil {
			c.removeElement(oldest)
		}
	}

	// Add new entry
	entry := &cacheEntry{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	elem := c.lru.PushFront(entry)
	c.items[key] = elem
}

func (c *MemoryCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.items))
	for key := range c.items {
		keys = append(keys, key)
	}
	return keys
}

func (c *MemoryCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *MemoryCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	delete(c.items, entry.key)
	c.lru.Remove(elem)
}
