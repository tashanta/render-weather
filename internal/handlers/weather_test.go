// internal/handlers/weather_test.go
package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/yourusername/render-weather/internal/models"
	"github.com/yourusername/render-weather/internal/services"
)

// Mock WeatherService
type mockWeatherService struct {
	weather  *models.Weather
	err      error
	cacheHit bool
}

func (m *mockWeatherService) GetWeather(ctx context.Context, city string) (*models.Weather, bool, error) {
	return m.weather, m.cacheHit, m.err
}

func TestWeatherHandler(t *testing.T) {
	t.Run("returns 200 with weather data on success", func(t *testing.T) {
		svc := &mockWeatherService{
			weather: &models.Weather{
				City:        "London",
				Temperature: 15.5,
				Description: "Cloudy",
				Humidity:    70,
				WindSpeed:   5.2,
			},
			cacheHit: true,
		}

		handler := WeatherHandler(svc)
		router := chi.NewRouter()
		router.Get("/weather/{city}", handler)

		req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "London")
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))
		assert.Equal(t, "true", rec.Header().Get("X-Cache-Hit"))
	})

	t.Run("returns 400 for empty city", func(t *testing.T) {
		svc := &mockWeatherService{}
		handler := WeatherHandler(svc)

		// Call handler directly without Chi routing to test empty city validation
		req := httptest.NewRequest(http.MethodGet, "/weather/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid_city")
	})

	t.Run("returns 404 for city not found", func(t *testing.T) {
		svc := &mockWeatherService{
			err: services.ErrCityNotFound,
		}
		handler := WeatherHandler(svc)
		router := chi.NewRouter()
		router.Get("/weather/{city}", handler)

		req := httptest.NewRequest(http.MethodGet, "/weather/InvalidCity", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Contains(t, rec.Body.String(), "city_not_found")
	})

	t.Run("returns 429 for rate limit", func(t *testing.T) {
		svc := &mockWeatherService{
			err: services.ErrRateLimited,
		}
		handler := WeatherHandler(svc)
		router := chi.NewRouter()
		router.Get("/weather/{city}", handler)

		req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		assert.Contains(t, rec.Body.String(), "rate_limited")
	})

	t.Run("returns 503 for circuit breaker open", func(t *testing.T) {
		svc := &mockWeatherService{
			err: services.ErrCircuitBreakerOpen,
		}
		handler := WeatherHandler(svc)
		router := chi.NewRouter()
		router.Get("/weather/{city}", handler)

		req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Contains(t, rec.Body.String(), "service_unavailable")
	})

	t.Run("returns 500 for unexpected error", func(t *testing.T) {
		svc := &mockWeatherService{
			err: errors.New("unexpected error"),
		}
		handler := WeatherHandler(svc)
		router := chi.NewRouter()
		router.Get("/weather/{city}", handler)

		req := httptest.NewRequest(http.MethodGet, "/weather/London", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "internal_error")
	})

	t.Run("sets X-Cache-Hit false for cache miss", func(t *testing.T) {
		svc := &mockWeatherService{
			weather: &models.Weather{
				City:        "Paris",
				Temperature: 18.0,
				Description: "Sunny",
			},
			cacheHit: false,
		}

		handler := WeatherHandler(svc)
		router := chi.NewRouter()
		router.Get("/weather/{city}", handler)

		req := httptest.NewRequest(http.MethodGet, "/weather/Paris", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "false", rec.Header().Get("X-Cache-Hit"))
	})
}
