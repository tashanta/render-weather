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

**Headers:**
- `Cache-Control: public, max-age=3600`
- `X-Cache-Hit: true/false`

**Status Codes:**
- `200` - Success
- `400` - Invalid city parameter
- `404` - City not found
- `429` - Rate limited
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

1. **Chi Router** with middleware stack (Recovery → Logging → CORS → Prometheus → Auth)
2. **Weather Service** wraps provider with circuit breaker
3. **Hybrid Cache** checks L1 (memory), then L2 (Redis)
4. **Background Loaders** preload cache and refresh JWKS keys
5. **OpenWeatherMap Provider** with exponential backoff retry

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

## Deployment (Render.com)

1. Connect GitHub repo to Render
2. Create new Web Service
3. Set environment variables in Render dashboard
4. Add Redis service (Internal Connection)
5. Deploy - Render will use Dockerfile

**Important**: Server starts immediately. Background tasks (cache/JWKS loading) complete asynchronously.

## Development

See `AGENTS.md` for AI agent development guidelines and architecture decisions.

## License

MIT
