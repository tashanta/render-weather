# Prometheus Metrics Instrumentation - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a public `/metrics` endpoint exposing HTTP and Go runtime metrics for Prometheus/OpenTelemetry scraping.

**Architecture:** Chi middleware captures request duration and counts, normalizes paths using `chi.RouteContext().RoutePattern()`, and exposes metrics via `promhttp.Handler()`. Go runtime metrics are included automatically via default Prometheus collectors.

**Tech Stack:** Go 1.26, Chi v5, prometheus/client_golang v1.22.0

## Global Constraints

- Go 1.26 minimum
- TDD: write tests first, then implementation
- Follow existing project patterns (zerolog logging, testify assertions)
- Endpoint `/metrics` must be public (no auth)
- Path labels must be normalized to avoid cardinality explosion

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/middleware/prometheus.go` | Prometheus middleware: captures metrics per request |
| `internal/middleware/prometheus_test.go` | Tests for the middleware |
| `cmd/api/main.go` | Wire middleware + add `/metrics` route |
| `go.mod` | Add prometheus/client_golang dependency |

---

### Task 1: Add Prometheus dependency

**Files:**
- Modify: `go.mod`

**Interfaces:**
- Consumes: nothing
- Produces: `github.com/prometheus/client_golang` available for import

- [ ] **Step 1: Add dependency**

```bash
go get github.com/prometheus/client_golang@v1.22.0
```

- [ ] **Step 2: Verify dependency added**

Run: `grep prometheus go.mod`
Expected: `github.com/prometheus/client_golang v1.22.0`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add prometheus/client_golang v1.22.0"
```

---

### Task 2: Create Prometheus middleware with tests

**Files:**
- Create: `internal/middleware/prometheus.go`
- Create: `internal/middleware/prometheus_test.go`

**Interfaces:**
- Consumes: `github.com/prometheus/client_golang/prometheus`, `github.com/go-chi/chi/v5`
- Produces: `func Prometheus(registry *prometheus.Registry) func(http.Handler) http.Handler`

- [ ] **Step 1: Write the failing test for counter increment**

Create `internal/middleware/prometheus_test.go`:

```go
// internal/middleware/prometheus_test.go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
	req := httptest.NewRequest("GET", "/test", nil)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/middleware -run TestPrometheusMiddleware_IncrementsCounter -v`
Expected: FAIL with "undefined: Prometheus"

- [ ] **Step 3: Write minimal implementation**

Create `internal/middleware/prometheus.go`:

```go
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

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/middleware -run TestPrometheusMiddleware_IncrementsCounter -v`
Expected: PASS

- [ ] **Step 5: Write test for duration histogram**

Add to `internal/middleware/prometheus_test.go`:

```go
func TestPrometheusMiddleware_RecordsDuration(t *testing.T) {
	registry := prometheus.NewRegistry()

	r := chi.NewRouter()
	r.Use(Prometheus(registry))
	r.Get("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/slow", nil)
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
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/middleware -run TestPrometheusMiddleware_RecordsDuration -v`
Expected: PASS

- [ ] **Step 7: Write test for path normalization**

Add to `internal/middleware/prometheus_test.go`:

```go
func TestPrometheusMiddleware_NormalizesPath(t *testing.T) {
	registry := prometheus.NewRegistry()

	r := chi.NewRouter()
	r.Use(Prometheus(registry))
	r.Get("/weather/{city}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Make requests with different cities
	for _, city := range []string{"Paris", "London", "Tokyo"} {
		req := httptest.NewRequest("GET", "/weather/"+city, nil)
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
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/middleware -run TestPrometheusMiddleware_NormalizesPath -v`
Expected: PASS

- [ ] **Step 9: Write test for correct labels on error responses**

Add to `internal/middleware/prometheus_test.go`:

```go
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
	req1 := httptest.NewRequest("GET", "/error", nil)
	r.ServeHTTP(httptest.NewRecorder(), req1)

	// POST /created -> 201
	req2 := httptest.NewRequest("POST", "/created", nil)
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
```

- [ ] **Step 10: Run test to verify it passes**

Run: `go test ./internal/middleware -run TestPrometheusMiddleware_LabelsErrorResponses -v`
Expected: PASS

- [ ] **Step 11: Run all middleware tests**

Run: `go test ./internal/middleware -v`
Expected: All tests PASS

- [ ] **Step 12: Commit**

```bash
git add internal/middleware/prometheus.go internal/middleware/prometheus_test.go
git commit -m "feat(middleware): add Prometheus metrics middleware with tests"
```

---

### Task 3: Wire middleware and add /metrics endpoint

**Files:**
- Modify: `cmd/api/main.go:1-140`

**Interfaces:**
- Consumes: `middleware.Prometheus(registry)`, `promhttp.HandlerFor(registry, opts)`
- Produces: `/metrics` endpoint, metrics on all HTTP requests

- [ ] **Step 1: Add imports to main.go**

Add to imports in `cmd/api/main.go`:

```go
import (
	// ... existing imports ...
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)
```

- [ ] **Step 2: Create Prometheus registry after config loading**

Add after line 42 (after "configuration loaded" log) in `cmd/api/main.go`:

```go
	// 2b. Create Prometheus registry with Go collectors
	promRegistry := prometheus.NewRegistry()
	promRegistry.MustRegister(collectors.NewGoCollector())
	promRegistry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	log.Info().Msg("Prometheus registry initialized with Go collectors")
```

- [ ] **Step 3: Add Prometheus middleware to router**

Modify the middleware stack in `cmd/api/main.go` (after line 93, after CORS):

```go
	// 11. Setup Chi router with middleware
	router := chi.NewRouter()
	router.Use(middleware.Recovery())
	router.Use(middleware.Logging())
	router.Use(middleware.CORS(cfg.AllowedOrigins))
	router.Use(middleware.Prometheus(promRegistry))
```

- [ ] **Step 4: Add /metrics route**

Modify the public routes section in `cmd/api/main.go` (after line 96):

```go
	// 12. Register public routes (no auth)
	router.Get("/health", handlers.HealthHandler())
	router.Get("/metrics", promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{
		Registry: promRegistry,
	}))
```

- [ ] **Step 5: Update routes log message**

Update log message at line 105:

```go
	log.Info().Msg("routes registered: /health, /metrics, /weather/{city}, /api/v1/weather/{city}")
```

- [ ] **Step 6: Verify build succeeds**

Run: `go build ./cmd/api`
Expected: No errors

- [ ] **Step 7: Run all tests**

Run: `go test ./... -v`
Expected: All tests PASS

- [ ] **Step 8: Commit**

```bash
git add cmd/api/main.go
git commit -m "feat: wire Prometheus middleware and add /metrics endpoint"
```

---

### Task 4: Manual verification

**Files:**
- None (verification only)

**Interfaces:**
- Consumes: Running application
- Produces: Verified working metrics endpoint

- [ ] **Step 1: Start the API**

Run: `go run cmd/api/main.go`
Expected: Server starts with logs showing Prometheus registry initialized

- [ ] **Step 2: Make test requests**

In another terminal:
```bash
curl http://localhost:8080/health
curl http://localhost:8080/health
curl http://localhost:8080/health
```

- [ ] **Step 3: Verify metrics endpoint**

```bash
curl -s http://localhost:8080/metrics | grep http_requests_total
```

Expected output (similar to):
```
# HELP http_requests_total Total number of HTTP requests.
# TYPE http_requests_total counter
http_requests_total{code="200",method="GET",path="/health"} 3
```

- [ ] **Step 4: Verify Go runtime metrics**

```bash
curl -s http://localhost:8080/metrics | grep go_goroutines
```

Expected output (similar to):
```
# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 8
```

- [ ] **Step 5: Verify histogram metrics**

```bash
curl -s http://localhost:8080/metrics | grep http_request_duration_seconds
```

Expected: Histogram buckets with `_bucket`, `_sum`, `_count` suffixes

- [ ] **Step 6: Stop server and commit verification**

Stop server with Ctrl+C.

```bash
git add -A
git commit -m "feat: complete Prometheus metrics instrumentation" --allow-empty
```

---

## Summary

After completing all tasks:
- `/metrics` endpoint exposed publicly (no auth)
- `http_requests_total` counter with method/path/code labels
- `http_request_duration_seconds` histogram with method/path labels
- Go runtime metrics (goroutines, memory, GC)
- Paths normalized to avoid cardinality explosion
- Full test coverage for middleware
