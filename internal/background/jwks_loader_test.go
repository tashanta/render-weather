// internal/background/jwks_loader_test.go
package background

import (
	"crypto/rsa"
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
		jwks := `{
			"keys": [{
				"kty": "RSA",
				"kid": "test_key",
				"use": "sig",
				"alg": "RS256",
				"n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
				"e": "AQAB"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwks))
	}))
	defer server.Close()

	manager := NewJWKSManager(server.URL)
	manager.Start()

	// Wait for initial load
	time.Sleep(200 * time.Millisecond)

	assert.True(t, manager.IsReady())
	key, err := manager.GetKey("test_key")
	require.NoError(t, err)
	assert.NotNil(t, key)
}

func TestJWKSManager_Start_Retry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		jwks := `{
			"keys": [{
				"kty": "RSA",
				"kid": "test_key",
				"use": "sig",
				"alg": "RS256",
				"n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
				"e": "AQAB"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwks))
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
	_, err := manager.GetKey("some-kid")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrJWKSNotReady)
}

func TestJWKSManager_GetKey_NotReady(t *testing.T) {
	manager := NewJWKSManager("http://invalid.local")
	// Don't start - manager not ready

	_, err := manager.GetKey("some-kid")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJWKSNotReady)
}

func TestJWKSManager_GetKey_NotFound(t *testing.T) {
	// Create a server that returns valid JWKS
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks := `{
			"keys": [{
				"kty": "RSA",
				"kid": "test-key-1",
				"use": "sig",
				"alg": "RS256",
				"n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
				"e": "AQAB"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwks))
	}))
	defer server.Close()

	manager := NewJWKSManager(server.URL)
	manager.Start()

	// Wait for JWKS to load
	time.Sleep(200 * time.Millisecond)

	_, err := manager.GetKey("non-existent-kid")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

func TestJWKSManager_GetKey_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks := `{
			"keys": [{
				"kty": "RSA",
				"kid": "test-key-1",
				"use": "sig",
				"alg": "RS256",
				"n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
				"e": "AQAB"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwks))
	}))
	defer server.Close()

	manager := NewJWKSManager(server.URL)
	manager.Start()

	// Wait for JWKS to load
	time.Sleep(200 * time.Millisecond)

	key, err := manager.GetKey("test-key-1")

	require.NoError(t, err)
	assert.NotNil(t, key)
	assert.IsType(t, &rsa.PublicKey{}, key)
}
