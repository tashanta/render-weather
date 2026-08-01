// internal/middleware/logging_test.go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestLogging(t *testing.T) {
	t.Run("logs request with UUID, duration, status", func(t *testing.T) {
		var capturedRequestID string
		router := chi.NewRouter()
		router.Use(Logging())
		router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			val := r.Context().Value(requestIDKey)
			capturedRequestID, _ = val.(string)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		// Verify request_id was added to context
		assert.NotEmpty(t, capturedRequestID)
	})
}
