// internal/handlers/weather.go
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/yourusername/render-weather/internal/models"
	"github.com/yourusername/render-weather/internal/services"
)

type WeatherServiceGetter interface {
	GetWeather(ctx context.Context, city string) (*models.Weather, bool, error)
}

// WeatherHandler returns a handler for weather requests
func WeatherHandler(svc WeatherServiceGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		city := chi.URLParam(r, "city")

		// Validate city parameter
		if city == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"error":"invalid_city"}`)
			return
		}

		// Get weather from service
		weather, cacheHit, err := svc.GetWeather(r.Context(), city)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")

			// Map service errors to HTTP status codes
			switch {
			case errors.Is(err, services.ErrCityNotFound):
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprintf(w, `{"error":"city_not_found"}`)
			case errors.Is(err, services.ErrRateLimited):
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = fmt.Fprintf(w, `{"error":"rate_limited"}`)
			case errors.Is(err, services.ErrCircuitBreakerOpen):
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprintf(w, `{"error":"service_unavailable"}`)
			default:
				log.Error().Err(err).Str("city", city).Msg("unexpected error fetching weather")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprintf(w, `{"error":"internal_error"}`)
			}
			return
		}

		// Set cache headers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if cacheHit {
			w.Header().Set("X-Cache-Hit", "true")
		} else {
			w.Header().Set("X-Cache-Hit", "false")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(weather)
	}
}
