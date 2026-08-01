// internal/middleware/auth.go
package middleware

import (
	"fmt"
	"net/http"
)

// JWKSReadyChecker interface for checking if JWKS is ready
type JWKSReadyChecker interface {
	Ready() bool
}

// Auth middleware checks if JWKS is ready before allowing requests
func Auth(jwksManager JWKSReadyChecker, audience string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if JWKS is ready
			if !jwksManager.Ready() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprintf(w, `{"error":"service_unavailable","message":"authentication service initializing"}`)
				return
			}

			// In production, would validate JWT token here
			// For now, just pass through if JWKS is ready
			next.ServeHTTP(w, r)
		})
	}
}
