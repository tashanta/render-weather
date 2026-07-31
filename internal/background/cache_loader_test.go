// internal/background/cache_loader_test.go
package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockHybridCache struct {
	preloadCalled bool
	shouldFail    bool
}

func (m *mockHybridCache) PreloadFromRedis(ctx context.Context) error {
	m.preloadCalled = true
	if m.shouldFail {
		return assert.AnError
	}
	return nil
}

func TestStartCacheLoader_Success(t *testing.T) {
	mock := &mockHybridCache{}

	StartCacheLoader(mock)

	// Give goroutine time to execute
	time.Sleep(100 * time.Millisecond)

	assert.True(t, mock.preloadCalled)
}

func TestStartCacheLoader_Retries(t *testing.T) {
	mock := &mockHybridCache{shouldFail: true}

	StartCacheLoader(mock)

	// Should retry, give it time
	time.Sleep(500 * time.Millisecond)

	assert.True(t, mock.preloadCalled)
}
