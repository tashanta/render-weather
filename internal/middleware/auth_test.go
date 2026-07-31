// internal/middleware/auth_test.go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// Mock JWKSManager
type mockJWKSManager struct {
	ready bool
}

func (m *mockJWKSManager) Ready() bool {
	return m.ready
}

func TestAuth(t *testing.T) {
	t.Run("returns 503 when JWKS not ready", func(t *testing.T) {
		mgr := &mockJWKSManager{ready: false}
		router := chi.NewRouter()
		router.Use(Auth(mgr, "https://example.com/api"))
		router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Contains(t, rec.Body.String(), "service_unavailable")
	})

	t.Run("passes through when JWKS ready", func(t *testing.T) {
		mgr := &mockJWKSManager{ready: true}
		router := chi.NewRouter()
		router.Use(Auth(mgr, "https://example.com/api"))
		router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, `{"status":"ok"}`, rec.Body.String())
	})
}
