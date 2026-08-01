// internal/middleware/prometheus.go
package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus returns a middleware that records HTTP metrics.
// It captures request counts and durations with method, path, and status code labels.
// Paths are normalized using Chi's RoutePattern to avoid cardinality explosion.
func Prometheus(registry *prometheus.Registry) func(http.Handler) http.Handler {
	requestsTotal := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "code"},
	)

	requestDuration := promauto.With(registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Call next handler
			next.ServeHTTP(wrapped, r)

			// Get normalized path from Chi route pattern
			path := r.URL.Path
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pattern := rctx.RoutePattern(); pattern != "" {
					path = pattern
				}
			}

			// Record metrics
			duration := time.Since(start).Seconds()
			code := strconv.Itoa(wrapped.statusCode)

			requestsTotal.WithLabelValues(r.Method, path, code).Inc()
			requestDuration.WithLabelValues(r.Method, path).Observe(duration)
		})
	}
}

// Note: responseWriter is defined in logging.go and shared across middlewares
