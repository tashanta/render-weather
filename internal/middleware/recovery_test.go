// internal/middleware/recovery_test.go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestRecoveryMiddleware(t *testing.T) {
	t.Run("catches panic and returns 500", func(t *testing.T) {
		router := chi.NewRouter()
		router.Use(Recovery())
		router.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		})

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "internal_error")
	})

	t.Run("normal request passes through", func(t *testing.T) {
		router := chi.NewRouter()
		router.Use(Recovery())
		router.Get("/normal", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})

		req := httptest.NewRequest(http.MethodGet, "/normal", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, `{"status":"ok"}`, rec.Body.String())
	})
}
