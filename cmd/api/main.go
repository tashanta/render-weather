// cmd/api/main.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/yourusername/render-weather/internal/auth"
	"github.com/yourusername/render-weather/internal/background"
	"github.com/yourusername/render-weather/internal/cache"
	"github.com/yourusername/render-weather/internal/config"
	"github.com/yourusername/render-weather/internal/handlers"
	"github.com/yourusername/render-weather/internal/middleware"
	"github.com/yourusername/render-weather/internal/providers"
	"github.com/yourusername/render-weather/internal/services"
)

func main() {
	// 1. Initialize zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().Msg("starting weather API server")

	// 2. Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	log.Info().
		Str("port", cfg.Port).
		Str("redis", cfg.RedisURL).
		Msg("configuration loaded")

	// 2b. Create Prometheus registry with Go collectors
	promRegistry := prometheus.NewRegistry()
	promRegistry.MustRegister(collectors.NewGoCollector())
	promRegistry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	log.Info().Msg("Prometheus registry initialized with Go collectors")

	// 3. Create L1 memory cache
	memCache := cache.NewMemoryCache(1000)
	log.Info().Msg("L1 memory cache initialized")

	// 4. Create L2 Redis cache
	redisCache, err := cache.NewRedisCache(cfg.RedisURL)
	if err != nil {
		log.Warn().Err(err).Msg("failed to create Redis cache, continuing without L2")
		// Use memory cache only
		redisCache = nil
	} else {
		log.Info().Msg("L2 Redis cache initialized")
	}

	// 5. Create hybrid cache
	hybridCache := cache.NewHybridCache(memCache, redisCache, 1*time.Hour)
	log.Info().Msg("hybrid cache (L1+L2) initialized")

	// 6. Start background cache loader goroutine
	background.StartCacheLoader(hybridCache)
	log.Info().Msg("background cache loader started")

	// 7. Setup auth middleware (conditional)
	var authMiddleware func(http.Handler) http.Handler

	if cfg.AuthEnabled {
		// Create JWKS manager
		jwksManager := background.NewJWKSManager(cfg.Auth0Domain)
		log.Info().Str("domain", cfg.Auth0Domain).Msg("JWKS manager initialized")

		// Start JWKS loader goroutine
		jwksManager.Start()
		log.Info().Msg("JWKS loader started")

		// Create JWT validator
		issuer := fmt.Sprintf("https://%s/", cfg.Auth0Domain)
		validator := auth.NewJWTValidator(jwksManager, issuer, cfg.Auth0Audience)
		log.Info().Str("issuer", issuer).Str("audience", cfg.Auth0Audience).Msg("JWT validator initialized")

		// Create auth middleware
		authMiddleware = middleware.Auth(validator)
	} else {
		log.Warn().Msg("authentication disabled (AUTH_ENABLED=false)")
		// Pass-through middleware (no-op)
		authMiddleware = func(next http.Handler) http.Handler {
			return next
		}
	}

	// 8. Create OpenWeatherMap provider
	owmProvider := providers.NewOpenWeatherMapProvider(cfg.OpenWeatherAPIKey, 5*time.Second)
	log.Info().Msg("OpenWeatherMap provider initialized")

	// 9. Create weather service with circuit breaker
	weatherService := services.NewWeatherService(
		owmProvider,
		hybridCache,
		5*time.Second,  // timeout
		5,              // max failures
		30*time.Second, // open duration
		1*time.Hour,    // cache TTL
	)
	log.Info().Msg("weather service initialized with circuit breaker")

	// 10. Setup Chi router with middleware
	router := chi.NewRouter()
	router.Use(middleware.Recovery())
	router.Use(middleware.Logging())
	router.Use(middleware.CORS(cfg.AllowedOrigins))
	router.Use(middleware.Prometheus(promRegistry))

	// 11. Register public routes (no auth)
	router.Get("/health", handlers.HealthHandler())
	router.Handle("/metrics", promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{
		Registry: promRegistry,
	}))

	// 12. Register protected routes (with auth)
	router.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		r.Get("/weather/{city}", handlers.WeatherHandler(weatherService))
		r.Get("/api/v1/weather/{city}", handlers.WeatherHandler(weatherService))
	})

	log.Info().Msg("routes registered: /health, /metrics, /weather/{city}, /api/v1/weather/{city}")

	// 13. Start HTTP server
	addr := fmt.Sprintf(":%s", cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Info().Str("addr", addr).Msg("HTTP server listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	// 14. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("server forced to shutdown")
	}

	log.Info().Msg("server stopped gracefully")
}
