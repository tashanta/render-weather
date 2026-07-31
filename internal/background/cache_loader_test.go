// internal/background/cache_loader_test.go
package background

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockHybridCache struct {
	mu            sync.Mutex
	preloadCalled bool
	shouldFail    bool
}

func (m *mockHybridCache) PreloadFromRedis(ctx context.Context) error {
	m.mu.Lock()
	m.preloadCalled = true
	shouldFail := m.shouldFail
	m.mu.Unlock()

	if shouldFail {
		return assert.AnError
	}
	return nil
}

func (m *mockHybridCache) getPreloadCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.preloadCalled
}

func TestStartCacheLoader_Success(t *testing.T) {
	mock := &mockHybridCache{}

	StartCacheLoader(mock)

	// Give goroutine time to execute
	time.Sleep(100 * time.Millisecond)

	assert.True(t, mock.getPreloadCalled())
}

func TestStartCacheLoader_Retries(t *testing.T) {
	mock := &mockHybridCache{shouldFail: true}

	StartCacheLoader(mock)

	// Should retry, give it time
	time.Sleep(500 * time.Millisecond)

	assert.True(t, mock.getPreloadCalled())
}
