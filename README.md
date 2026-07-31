# Weather API

Production-ready weather API powered by OpenWeatherMap with Auth0 authentication.

## Features

- Auth0 JWT authentication
- Hybrid cache (memory + Redis)
- Circuit breaker with retry logic
- Structured JSON logging
- Docker deployment on Render.com

## Quick Start

1. Copy `.env.example` to `.env` and configure
2. Run Redis: `docker run -d -p 6379:6379 redis:alpine`
3. Run API: `go run cmd/api/main.go`

## Testing

```bash
go test ./...
go test -cover ./...
```

## Deployment

See `Dockerfile` for containerized deployment on Render.com.
