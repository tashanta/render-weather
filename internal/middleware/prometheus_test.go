// internal/middleware/prometheus_test.go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrometheusMiddleware_IncrementsCounter(t *testing.T) {
	// Create isolated registry for test
	registry := prometheus.NewRegistry()

	// Create router with middleware
	r := chi.NewRouter()
	r.Use(Prometheus(registry))
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Verify counter was incremented
	metrics, err := registry.Gather()
	require.NoError(t, err)

	var requestsTotal *dto.MetricFamily
	for _, m := range metrics {
		if m.GetName() == "http_requests_total" {
			requestsTotal = m
			break
		}
	}

	require.NotNil(t, requestsTotal, "http_requests_total metric not found")
	require.Len(t, requestsTotal.GetMetric(), 1)

	metric := requestsTotal.GetMetric()[0]
	assert.Equal(t, float64(1), metric.GetCounter().GetValue())

	// Verify labels
	labels := make(map[string]string)
	for _, label := range metric.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	assert.Equal(t, "GET", labels["method"])
	assert.Equal(t, "/test", labels["path"])
	assert.Equal(t, "200", labels["code"])
}

func TestPrometheusMiddleware_RecordsDuration(t *testing.T) {
	registry := prometheus.NewRegistry()

	r := chi.NewRouter()
	r.Use(Prometheus(registry))
	r.Get("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	metrics, err := registry.Gather()
	require.NoError(t, err)

	var duration *dto.MetricFamily
	for _, m := range metrics {
		if m.GetName() == "http_request_duration_seconds" {
			duration = m
			break
		}
	}

	require.NotNil(t, duration, "http_request_duration_seconds metric not found")
	require.Len(t, duration.GetMetric(), 1)

	histogram := duration.GetMetric()[0].GetHistogram()
	assert.GreaterOrEqual(t, histogram.GetSampleSum(), 0.01) // At least 10ms
	assert.Equal(t, uint64(1), histogram.GetSampleCount())
}

func TestPrometheusMiddleware_NormalizesPath(t *testing.T) {
	registry := prometheus.NewRegistry()

	r := chi.NewRouter()
	r.Use(Prometheus(registry))
	r.Get("/weather/{city}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Make requests with different cities
	for _, city := range []string{"Paris", "London", "Tokyo"} {
		req := httptest.NewRequest(http.MethodGet, "/weather/"+city, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}

	metrics, err := registry.Gather()
	require.NoError(t, err)

	var requestsTotal *dto.MetricFamily
	for _, m := range metrics {
		if m.GetName() == "http_requests_total" {
			requestsTotal = m
			break
		}
	}

	require.NotNil(t, requestsTotal)
	// Should have only ONE metric series (normalized path), not three
	require.Len(t, requestsTotal.GetMetric(), 1)

	metric := requestsTotal.GetMetric()[0]
	assert.Equal(t, float64(3), metric.GetCounter().GetValue())

	// Verify path is normalized
	labels := make(map[string]string)
	for _, label := range metric.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	assert.Equal(t, "/weather/{city}", labels["path"])
}

func TestPrometheusMiddleware_LabelsErrorResponses(t *testing.T) {
	registry := prometheus.NewRegistry()

	r := chi.NewRouter()
	r.Use(Prometheus(registry))
	r.Get("/error", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	r.Post("/created", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	// GET /error -> 500
	req1 := httptest.NewRequest(http.MethodGet, "/error", nil)
	r.ServeHTTP(httptest.NewRecorder(), req1)

	// POST /created -> 201
	req2 := httptest.NewRequest(http.MethodPost, "/created", nil)
	r.ServeHTTP(httptest.NewRecorder(), req2)

	metrics, err := registry.Gather()
	require.NoError(t, err)

	var requestsTotal *dto.MetricFamily
	for _, m := range metrics {
		if m.GetName() == "http_requests_total" {
			requestsTotal = m
			break
		}
	}

	require.NotNil(t, requestsTotal)
	require.Len(t, requestsTotal.GetMetric(), 2)

	// Collect all label combinations
	labelSets := make([]map[string]string, 0)
	for _, metric := range requestsTotal.GetMetric() {
		labels := make(map[string]string)
		for _, label := range metric.GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}
		labelSets = append(labelSets, labels)
	}

	// Verify both combinations exist
	assert.Contains(t, labelSets, map[string]string{"method": "GET", "path": "/error", "code": "500"})
	assert.Contains(t, labelSets, map[string]string{"method": "POST", "path": "/created", "code": "201"})
}
