// internal/middleware/recovery.go
package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/rs/zerolog/log"
)

// Recovery middleware recovers from panics and returns 500
func Recovery() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					stack := debug.Stack()
					log.Error().
						Interface("panic", err).
						Str("stack", string(stack)).
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Msg("panic recovered")

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = fmt.Fprintf(w, `{"error":"internal_error"}`)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
