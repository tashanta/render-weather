// internal/handlers/health_test.go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthHandler(t *testing.T) {
	t.Run("returns 200 with status ok", func(t *testing.T) {
		handler := HealthHandler()

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	})
}
