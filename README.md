# Weather API

Production-ready weather API powered by OpenWeatherMap with Auth0 authentication, hybrid caching, and circuit breaker resilience.

## Features

- **Auth0 JWT Authentication** - Secure API access with background JWKS loading
- **Hybrid Cache (L1 + L2)** - Memory (LRU) + Redis for high performance
- **Circuit Breaker** - Automatic failover with retry/backoff logic
- **Background Loaders** - Non-blocking cache preload and JWKS refresh
- **Structured Logging** - JSON logging with zerolog (request IDs, duration, status)
- **RESTful API** - Clean endpoints with proper HTTP status codes
- **Graceful Shutdown** - Signal handling with 10-second timeout
- **Docker Ready** - Multi-stage rootless build for Render.com

## Requirements

- Go 1.26+
- Redis (for L2 cache)
- OpenWeatherMap API key
- Auth0 domain and audience

## Quick Start

### 1. Environment Setup

Copy `.env.example` to `.env` and configure:

```bash
cp .env.example .env
```

Required variables:
```env
PORT=8080
REDIS_URL=redis://localhost:6379
OPENWEATHER_API_KEY=your_api_key_here
AUTH0_DOMAIN=https://your-tenant.auth0.com
AUTH0_AUDIENCE=https://your-api-audience
ALLOWED_ORIGINS=https://example.com,https://app.example.com
AUTH_ENABLED=true  # Set to false to disable authentication (dev only)
RATE_LIMIT_CAPACITY=60           # Token bucket capacity (requests per minute)
RATE_LIMIT_REFILL_RATE=1s        # Refill rate (1 token per second)
```

### 2. Start Redis

```bash
docker run -d -p 6379:6379 --name weather-redis redis:alpine
```

### 3. Run API

```bash
go run cmd/api/main.go
```

The server starts immediately on port 8080. Background goroutines (cache loader, JWKS loader) initialize asynchronously.

## API Endpoints

### Health Check
```bash
GET /health
```

Returns: `{"status":"ok"}`

### Get Weather
```bash
GET /weather/{city}
GET /api/v1/weather/{city}
```

Example:
```bash
curl http://localhost:8080/weather/London
```

Response:
```json
{
  "city": "London",
  "temperature": 15.5,
  "description": "Cloudy",
  "humidity": 70,
  "windSpeed": 5.2
}
```

**Rate Limit Response (429):**
```http
HTTP/1.1 429 Too Many Requests
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1722600060
Retry-After: 15

{
  "error": "rate_limit_exceeded",
  "message": "Too many requests. Please try again later."
}
```

**Headers:**
- `Cache-Control: public, max-age=3600`
- `X-Cache-Hit: true/false`

**Status Codes:**
- `200` - Success
- `400` - Invalid city parameter
- `404` - City not found
- `429` - Rate limited (60 requests/minute global limit)
- `503` - Service unavailable (circuit breaker open or JWKS not ready)
- `500` - Internal error

## Authentication

The API uses JWT Bearer tokens for authentication (OAuth 2.0 client_credentials flow).

### Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AUTH_ENABLED` | No | `true` | Enable/disable authentication |
| `AUTH0_DOMAIN` | If auth enabled | - | OAuth provider domain (e.g., `tenant.eu.auth0.com`) |
| `AUTH0_AUDIENCE` | If auth enabled | - | Expected token audience |

### Token Validation

When enabled, the middleware validates:
- **Signature** (RS256) against JWKS public keys
- **Issuer** (`iss`) matches `https://{AUTH0_DOMAIN}/`
- **Audience** (`aud`) contains the expected value
- **Expiration** (`exp`) is in the future

### Disabling Authentication (Development)

Set `AUTH_ENABLED=false` to bypass authentication entirely:

```bash
AUTH_ENABLED=false go run cmd/api/main.go
```

> **Warning:** Never disable authentication in production.

### Error Responses

| Status | Meaning |
|--------|---------|
| 401 Unauthorized | Missing or invalid token |
| 503 Service Unavailable | JWKS not loaded yet (retry later) |

### Usage Example

```bash
# Get a token from your OAuth provider (client_credentials flow)
TOKEN=$(curl -s --request POST \
  --url "https://${AUTH0_DOMAIN}/oauth/token" \
  --header 'content-type: application/json' \
  --data "{\"client_id\":\"${CLIENT_ID}\",\"client_secret\":\"${CLIENT_SECRET}\",\"audience\":\"${AUTH0_AUDIENCE}\",\"grant_type\":\"client_credentials\"}" \
  | jq -r '.access_token')

# Call the API with the token
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/weather/paris
```

## Testing

### Run All Tests
```bash
go test ./...
```

### With Coverage
```bash
go test ./... -cover
```

Coverage by package:
- handlers: 100%
- middleware: 100%
- providers: 88.5%
- background: 85.7%
- config: 80.6%
- services: 78.6%
- cache: 78.3%

### With Race Detector (Note: background tests are slow)
```bash
go test ./... -race
```

### Run Specific Package
```bash
go test ./internal/handlers -v
```

## Building

### Local Binary
```bash
go build -o bin/api cmd/api/main.go
./bin/api
```

### Docker Image
```bash
docker build -t weather-api .
docker run --env-file .env -p 8080:8080 weather-api
```

### Docker Compose (local stack)

Runs the API with a local Redis, equivalent to Render's free Redis service:

```bash
cp .env.example .env   # fill in real credentials
docker compose up      # builds and starts the API (8080) + Redis
```

- `api`: builds from the Dockerfile, maps `8080:8080`, reads `.env` (`REDIS_URL` overridden to `redis://redis:6379`), starts after the Redis healthcheck passes
- `redis`: `redis:7-alpine`, no volume (in-memory cache), not exposed on the host
- `app_network` (bridge): dedicated named network shared by both services, for fine-grained control over local networking resources

## Project Structure

```
├── cmd/api/main.go          # Application entry point
├── internal/
│   ├── background/          # Cache & JWKS loaders (goroutines)
│   ├── cache/               # L1 (memory), L2 (Redis), hybrid
│   ├── config/              # Environment variable loading
│   ├── handlers/            # HTTP handlers (weather, health)
│   ├── middleware/          # Auth, logging, CORS, recovery
│   ├── models/              # Weather data model
│   ├── providers/           # OpenWeatherMap API client
│   └── services/            # Weather service with circuit breaker
├── Dockerfile               # Multi-stage rootless build
├── docker-compose.yml       # Local stack (API + Redis)
├── go.mod                   # Go dependencies
└── .env.example             # Environment template
```

## Architecture

### Overview

1. **Chi Router** with middleware stack (Recovery → Logging → CORS → Prometheus → Auth)
2. **Weather Service** wraps provider with circuit breaker
3. **Hybrid Cache** checks L1 (memory), then L2 (Redis)
4. **Background Loaders** preload cache and refresh JWKS keys
5. **OpenWeatherMap Provider** with exponential backoff retry

### Startup Initialization (Non-Blocking)

The server starts immediately without waiting for external dependencies. Background goroutines handle async initialization with graceful degradation:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Application Startup                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. Load environment variables                                              │
│  2. Validate required config (exit if missing)                              │
│  3. Log configuration (secrets redacted)                                    │
│                                                                             │
│  ┌─────────────────────────────────────┐  ┌──────────────────────────────┐  │
│  │  Goroutine: Cache Preload          │  │  Goroutine: JWKS Loader      │  │
│  │                                     │  │                              │  │
│  │  Connect to Redis (3 retries)       │  │  Fetch Auth0 public keys     │  │
│  │  ├─ Success: Scan weather:* keys    │  │  (3 retries: 1s, 2s, 4s)     │  │
│  │  │           Load into L1 cache     │  │  ├─ Success: JWT validation  │  │
│  │  │           Log entries count      │  │  │           ready           │  │
│  │  └─ Failure: Log warning            │  │  └─ Failure: Retry every 30s │  │
│  │              L1 starts empty        │  │              until success   │  │
│  │              Service continues      │  │                              │  │
│  └─────────────────────────────────────┘  └──────────────────────────────┘  │
│                                                                             │
│  4. Start HTTP server immediately (port 8080)                               │
│     └─ /health returns 200 immediately                                      │
│     └─ /weather/* returns 503 until JWKS ready                              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Graceful Degradation:**
- **Redis unavailable**: Service operates with L1 cache only + direct OpenWeatherMap calls
- **JWKS not loaded**: Protected endpoints return 503 (Retry-After: 30)
- **L1 preload failed**: Cache starts empty, populates on demand

### Request Flow with Circuit Breaker

```
Client → API (/weather/{city})
  │
  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  1. Middleware Stack                                                        │
│     Recovery → Logging (request_id) → CORS → Prometheus → Auth (JWT)        │
└─────────────────────────────────────────────────────────────────────────────┘
  │
  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  2. Handler: Extract city parameter, validate input                         │
└─────────────────────────────────────────────────────────────────────────────┘
  │
  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  3. WeatherService.GetWeather("Paris")                                      │
└─────────────────────────────────────────────────────────────────────────────┘
  │
  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  4. Cache Lookup                                                            │
│                                                                             │
│     Cache.Get("weather:paris")                                              │
│     ├─ HIT L1 (memory) ──────────────────────────────► Return immediately   │
│     │                                                  X-Cache-Hit: true    │
│     └─ MISS L1 → Check L2 (Redis)                                           │
│         ├─ HIT L2 ───► Store in L1 ──────────────────► Return               │
│         │                                              X-Cache-Hit: true    │
│         └─ MISS L2 → Continue to Circuit Breaker                            │
└─────────────────────────────────────────────────────────────────────────────┘
  │
  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  5. Circuit Breaker Check                                                   │
│                                                                             │
│     ┌──────────────────────────────────────────────────────────────────┐    │
│     │ OPEN (too many failures)                                         │    │
│     │ └─► 503 Service Unavailable + Retry-After: 30                    │    │
│     ├──────────────────────────────────────────────────────────────────┤    │
│     │ HALF-OPEN (recovery test after 30s)                              │    │
│     │ └─► Allow 1 test request through                                 │    │
│     ├──────────────────────────────────────────────────────────────────┤    │
│     │ CLOSED (normal operation)                                        │    │
│     │ └─► Proceed to OpenWeatherMap                                    │    │
│     └──────────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
  │
  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  6. OpenWeatherMap Provider (with Retry + Backoff)                          │
│                                                                             │
│     Retry strategy: Exponential backoff with jitter (max 3 attempts)        │
│     Delays: 100ms → 300ms → 900ms (±20% jitter)                             │
│     Global timeout: 1 second (including all retries)                        │
│                                                                             │
│     ┌─────────────────────────────────────────────────────────────────┐     │
│     │ Retryable errors:                                               │     │
│     │   • HTTP 5xx (transient server error)                           │     │
│     │   • Timeout / Network error                                     │     │
│     ├─────────────────────────────────────────────────────────────────┤     │
│     │ Non-retryable errors (fail immediately):                        │     │
│     │   • HTTP 429 (rate limit) → Circuit breaker handles             │     │
│     │   • HTTP 4xx (client error, e.g., city not found)               │     │
│     └─────────────────────────────────────────────────────────────────┘     │
│                                                                             │
│     Results:                                                                │
│     ├─ Success ───► Store L1 + L2 (TTL 1h) ───► Return 200 + JSON           │
│     ├─ 404 ───────► Return 404 city_not_found                               │
│     ├─ 429/5xx/timeout ───► Increment failure counter                       │
│     └─ Threshold reached ───► Circuit → OPEN                                │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Circuit Breaker Configuration

| Parameter | Value | Description |
|-----------|-------|-------------|
| Timeout | 1 second | Max time for OpenWeatherMap request |
| Failure threshold | 5 consecutive failures | Triggers OPEN state |
| Open duration | 30 seconds | Time before HALF-OPEN test |
| Failure conditions | 429, 5xx, timeout, network error | What counts as failure |

### Retry & Backoff Configuration

**OpenWeatherMap Provider:**
| Parameter | Value |
|-----------|-------|
| Max attempts | 3 |
| Initial delay | 100ms |
| Multiplier | 3x |
| Jitter | ±20% |
| Global timeout | 1 second |

**Auth0 JWKS Loader:**
| Parameter | Value |
|-----------|-------|
| Initial attempts | 3 (at startup) |
| Initial delays | 1s, 2s, 4s |
| Continuous retry | Every 30s until success |
| Timeout per attempt | 5 seconds |

## Monitoring

### Logs (stdout)

All requests are logged to stdout in structured JSON format via zerolog. Each log entry contains:

| Field | Type | Description |
|-------|------|-------------|
| `request_id` | string | UUID unique per request (for debugging/tracing) |
| `method` | string | HTTP method (GET, POST, etc.) |
| `path` | string | Request path (e.g., `/weather/Paris`) |
| `status` | int | HTTP status code (200, 404, 500, etc.) |
| `duration` | duration | Response time (e.g., `12.345ms`) |
| `time` | timestamp | Request timestamp |

Example log output:
```json
{"level":"info","request_id":"550e8400-e29b-41d4-a716-446655440000","method":"GET","path":"/weather/Paris","status":200,"duration":45.123,"time":"2026-08-01T12:00:00Z","message":"request completed"}
```

**Use cases:**
- Filter by `status >= 500` for error alerting
- Aggregate `duration` by `path` for latency analysis
- Correlate issues using `request_id` across services
- Parse with `jq` for quick analysis: `cat logs.json | jq 'select(.status >= 400)'`

### Metrics Endpoint (`/metrics`)

Prometheus-compatible metrics are exposed on `GET /metrics` (public, no auth).

#### HTTP Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_requests_total` | Counter | `method`, `path`, `code` | Total HTTP requests |
| `http_request_duration_seconds` | Histogram | `method`, `path` | Request latency distribution |

**Path normalization:** Dynamic segments are normalized to avoid cardinality explosion:
- `/weather/Paris` → `/weather/{city}`
- `/api/v1/weather/London` → `/api/v1/weather/{city}`

#### Go Runtime Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `go_goroutines` | Gauge | Number of active goroutines |
| `go_threads` | Gauge | Number of OS threads |
| `go_memstats_alloc_bytes` | Gauge | Bytes allocated (heap) |
| `go_memstats_heap_inuse_bytes` | Gauge | Heap memory in use |
| `go_gc_duration_seconds` | Summary | GC pause duration |
| `process_resident_memory_bytes` | Gauge | Process memory (RSS) |
| `process_cpu_seconds_total` | Counter | CPU time consumed |

#### Quick Dashboard (Poor Man's Solution)

No Prometheus/Grafana? Poll metrics and write to a file for simple analysis:

```bash
# Poll every 15s and append to file
while true; do
  echo "=== $(date -Iseconds) ===" >> metrics.log
  curl -s http://localhost:8080/metrics | grep -E "^(http_|go_goroutines|go_memstats_alloc)" >> metrics.log
  sleep 15
done
```

Extract key metrics with grep/awk:
```bash
# Request count by endpoint
grep http_requests_total metrics.log | tail -20

# Current goroutine count trend
grep go_goroutines metrics.log | awk '{print $2}'

# Memory usage over time
grep go_memstats_alloc_bytes metrics.log | awk '{print $2/1024/1024 " MB"}'
```

#### Prometheus Integration

For production, configure Prometheus to scrape the `/metrics` endpoint:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'weather-api'
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:8080']
```

Example PromQL queries:
```promql
# Request rate (requests/second)
rate(http_requests_total[5m])

# 95th percentile latency
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))

# Error rate (4xx + 5xx)
sum(rate(http_requests_total{code=~"4..|5.."}[5m])) / sum(rate(http_requests_total[5m]))

# Memory growth
increase(go_memstats_alloc_bytes[1h])
```

## CI/CD

### CI - GitHub Actions (`.github/workflows/build.yml`)

The CI workflow runs on every push and pull request to `main`. It includes three jobs:

#### Job `test` - Code Quality

| Step | Description |
|------|-------------|
| **Checkout** | Clones the repository |
| **Setup Go** | Installs Go 1.26 with dependency caching |
| **Download dependencies** | `go mod download` - fetches modules |
| **Verify dependencies** | `go mod verify` - checks module integrity |
| **Check formatting** | Validates formatting with `gofmt -s` |
| **golangci-lint** | Static analysis (linters configured in `.golangci.yml`) |
| **gosec** | Security scanner to detect Go vulnerabilities |
| **Tests + race detector** | `go test -race -v ./...` - detects data races |
| **Tests + coverage** | Generates `coverage.out` and `coverage.html` |
| **Upload artifacts** | Saves coverage report as artifact |

#### Job `coverage-pages` - Coverage Publishing

Runs only on push to `main` (after the `test` job).

| Step | Description |
|------|-------------|
| **Download artifact** | Retrieves coverage report |
| **Deploy to GitHub Pages** | Publishes HTML report to GitHub Pages |

#### Job `docker` - Build & Scan Image

Runs after the `test` job.

| Step | Description |
|------|-------------|
| **Setup Buildx** | Configures Docker Buildx for optimized builds |
| **Login GHCR** | Authenticates to GitHub Container Registry |
| **Extract metadata** | Generates tags (`latest`, `sha`, `branch`, `pr-N`) |
| **Build & Push** | Multi-stage build, pushes only on `main` |
| **Trivy scan** | Vulnerability scanner (blocks on CRITICAL/HIGH) |

**Generated Docker tags:**
- `ghcr.io/{owner}/{repo}:latest` - main branch
- `ghcr.io/{owner}/{repo}:{sha}` - full commit SHA
- `ghcr.io/{owner}/{repo}:main` - branch name
- `ghcr.io/{owner}/{repo}:pr-N` - pull requests

### CD - GitOps with Render (`render.yaml`)

The `render.yaml` file defines infrastructure as code for Render.com. Render automatically detects this file and provisions services.

#### Deployed Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Render.com (Frankfurt)               │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────────────┐    ┌─────────────────────────┐ │
│  │  render-weather-api │───▶│  render-weather-redis   │ │
│  │  (Web Service)      │    │  (Redis)                │ │
│  │  - Docker runtime   │    │  - Plan: free           │ │
│  │  - Plan: free       │    │  - Internal only        │ │
│  │  - Auto-deploy: on  │    └─────────────────────────┘ │
│  │  - Health: /health  │                                │
│  └─────────────────────┘                                │
└─────────────────────────────────────────────────────────┘
```

#### Defined Services

| Service | Type | Configuration |
|---------|------|---------------|
| `render-weather-redis` | Redis | Free plan, Frankfurt region, internal access only |
| `render-weather-api` | Web (Docker) | Free plan, auto-deploy from `main`, health check `/health` |

#### Environment Variables

| Variable | Source | Description |
|----------|--------|-------------|
| `PORT` | Fixed value | Listening port (8080) |
| `ENV` | Fixed value | Environment (`production`) |
| `REDIS_URL` | Linked service | Auto-generated connection string from `render-weather-redis` |
| `AUTH0_DOMAIN` | Dashboard (secret) | Auth0 domain - configure manually |
| `AUTH0_AUDIENCE` | Dashboard (secret) | Auth0 audience - configure manually |
| `OPENWEATHER_API_KEY` | Dashboard (secret) | OpenWeatherMap API key - configure manually |
| `ALLOWED_ORIGINS` | Fixed value | CORS origins (empty by default) |
| `CACHE_TTL` | Fixed value | Cache TTL in seconds (3600) |
| `CACHE_L1_MAX_SIZE` | Fixed value | Max memory cache size (1000) |
| `CB_TIMEOUT` | Fixed value | Circuit breaker timeout in ms (1000) |
| `CB_MAX_FAILURES` | Fixed value | Failures before opening (5) |
| `CB_OPEN_DURATION` | Fixed value | Open circuit duration in seconds (30) |
| `LOG_LEVEL` | Fixed value | Log level (`info`) |

#### GitOps Deployment Flow

1. **Push to `main`** → Render detects the change
2. **Docker build** → Uses the repo's `Dockerfile`
3. **Deploy** → Deploys the new container
4. **Health check** → Verifies `/health` before routing traffic
5. **Auto rollback** → Reverts to previous version if health check fails

**One-time manual configuration:**
1. Connect GitHub repo to Render
2. Create a Blueprint from `render.yaml`
3. Configure secrets in Render dashboard:
   - `AUTH0_DOMAIN`
   - `AUTH0_AUDIENCE`
   - `OPENWEATHER_API_KEY`

**Important**: Server starts immediately. Background tasks (cache/JWKS loading) complete asynchronously.

## Production Readiness

This section describes what's missing to make the application fully production-ready.

**Legend:** `MUST` = Required for production | `SHOULD` = Recommended improvement

### Application

| Priority | Item | Description |
|----------|------|-------------|
| SHOULD | **Per-user rate limiting** | Currently not implemented. A 429 from this API would have the same user impact as a 429 from OpenWeatherMap. If needed, implement rate limiting per user (based on JWT `sub` claim) with limits lower than OpenWeatherMap's. However, additional caching layers (see Infrastructure) provide better value. |

### Infrastructure

| Priority | Item | Description |
|----------|------|-------------|
| MUST | **Edge / CDN** | Add an edge layer (e.g., Cloudflare) for HTTP caching on top of existing L1 (memory) and L2 (Redis) caches. This provides geographic distribution and reduces origin load. |
| MUST | **Vertical scaling** | Enable CPU-based autoscaling for the web service (and optionally request-based). Currently requires a paid Render plan. |
| SHOULD | **WAF rules** | Add specific WAF rules if Render's built-in Cloudflare integration is insufficient for security requirements. |
| SHOULD | **Redis scaling** | Consider Redis clustering for higher throughput and availability. |
| SHOULD | **Multi-AZ deployment** | Deploy web service and Redis across multiple availability zones for resilience. |

### Observability / Monitoring / Alerting

| Priority | Item | Description |
|----------|------|-------------|
| MUST | **Metrics collection** | Set up OpenTelemetry or Prometheus scraping with storage backend (e.g., Grafana Cloud, Datadog, or self-hosted). |
| MUST | **Dashboards** | Build operational dashboards for request rates, latencies, error rates, cache hit ratios, circuit breaker state. |
| MUST | **Alerting** | Configure alerts on key metrics: error rate spikes, latency degradation, circuit breaker trips, memory/CPU thresholds. |
| SHOULD | **Distributed tracing** | Instrument endpoints with spans/traces (OpenTelemetry) for request flow visibility across services. |

### CI/CD

| Priority | Item | Description |
|----------|------|-------------|
| MUST | **Staging environment** | Add staging/prod environment separation with mandatory review before production deployment. Can use release tags or a dedicated branch (e.g., `release`) to trigger prod deploys. |
| MUST | **Dependency updates** | Configure Dependabot or Renovate for automated dependency update PRs with security alerts. |
| MUST | **Preview environments** | Deploy preview environments for feature branches (verify Render Blueprint support for this). |
| SHOULD | **Release management** | If trunk-based development isn't sufficient, implement release-please for semantic versioning and automated CHANGELOG generation. |

### Target Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Production Target                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Users ──▶ CDN/Edge (Cloudflare) ──▶ WAF ──▶ Load Balancer                │
│                    │                              │                         │
│                    │ (HTTP Cache)                 │                         │
│                    ▼                              ▼                         │
│              Cache HIT?                  ┌───────────────┐                  │
│               Yes → Return               │  Web Service  │ (Auto-scaled)    │
│                                          │  (Multi-AZ)   │                  │
│                                          └───────┬───────┘                  │
│                                                  │                          │
│                            ┌─────────────────────┼─────────────────────┐    │
│                            │                     │                     │    │
│                            ▼                     ▼                     ▼    │
│                      ┌──────────┐         ┌──────────┐         ┌──────────┐ │
│                      │ L1 Cache │         │ L2 Cache │         │ Metrics  │ │
│                      │ (Memory) │         │ (Redis   │         │ (OTel/   │ │
│                      │          │         │ Cluster) │         │ Prom)    │ │
│                      └──────────┘         └──────────┘         └────┬─────┘ │
│                                                                     │       │
│                                                                     ▼       │
│                                                              ┌──────────┐   │
│                                                              │ Alerting │   │
│                                                              │ Dashboard│   │
│                                                              └──────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Development

See `AGENTS.md` for AI agent development guidelines and architecture decisions.

## License

MIT
