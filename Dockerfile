# Multi-stage build: golang builder + alpine runtime
# Rootless with non-root user (UID/GID 1000)

FROM golang:1.26-alpine AS builder

# Install ca-certificates for HTTPS calls
RUN apk add --no-cache ca-certificates

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . ./

# Build static binary with CGO disabled
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o api cmd/api/main.go

# Runtime stage
FROM alpine:latest

# Install ca-certificates
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

# Copy binary from builder and set ownership
COPY --from=builder --chown=appuser:appuser /build/api ./api

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Run binary
CMD ["./api"]
