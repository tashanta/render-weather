// internal/background/jwks_loader_test.go
package background

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWKSManager_Start_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/.well-known/jwks.json", r.URL.Path)
		jwks := map[string]interface{}{
			"keys": []map[string]string{
				{"kid": "test_key", "kty": "RSA", "use": "sig"},
			},
		}
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	manager := NewJWKSManager(server.URL)
	manager.Start()

	// Wait for initial load
	time.Sleep(200 * time.Millisecond)

	assert.True(t, manager.IsReady())
	jwks, err := manager.GetJWKS()
	require.NoError(t, err)
	assert.NotNil(t, jwks)
}

func TestJWKSManager_Start_Retry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		jwks := map[string]interface{}{
			"keys": []map[string]string{
				{"kid": "test_key", "kty": "RSA", "use": "sig"},
			},
		}
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	manager := NewJWKSManager(server.URL)
	manager.Start()

	// Wait for retries (backoff: 1s + 2s = 3s total)
	time.Sleep(4 * time.Second)

	assert.True(t, manager.IsReady())
	assert.GreaterOrEqual(t, attempts, 2)
}

func TestJWKSManager_NotReady(t *testing.T) {
	manager := NewJWKSManager("http://invalid.local")

	assert.False(t, manager.IsReady())
	_, err := manager.GetJWKS()
	assert.Error(t, err)
}
