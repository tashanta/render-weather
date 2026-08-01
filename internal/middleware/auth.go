// internal/middleware/auth.go
package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/yourusername/render-weather/internal/background"
)

// TokenValidator interface for validating JWT tokens
type TokenValidator interface {
	Validate(tokenString string) error
}

// Auth middleware validates JWT Bearer tokens
func Auth(validator TokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				unauthorized(w)
				return
			}

			// Check Bearer prefix
			if !strings.HasPrefix(authHeader, "Bearer ") {
				unauthorized(w)
				return
			}

			// Extract token
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == "" {
				unauthorized(w)
				return
			}

			// Validate token
			err := validator.Validate(token)
			if err != nil {
				if errors.Is(err, background.ErrJWKSNotReady) {
					serviceUnavailable(w)
					return
				}
				// Log the actual error but don't expose it
				log.Debug().Err(err).Msg("token validation failed")
				unauthorized(w)
				return
			}

			// Token valid, continue
			next.ServeHTTP(w, r)
		})
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, `{"error":"unauthorized"}`)
}

func serviceUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprint(w, `{"error":"service_unavailable"}`)
}
