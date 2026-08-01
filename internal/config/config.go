package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Server
	Port string
	Env  string

	// Auth
	AuthEnabled   bool
	Auth0Domain   string
	Auth0Audience string

	// OpenWeatherMap
	OpenWeatherAPIKey string

	// Redis
	RedisURL string

	// CORS
	AllowedOrigins []string

	// Cache
	CacheTTL       time.Duration
	CacheL1MaxSize int

	// Circuit Breaker
	CBTimeout      time.Duration
	CBMaxFailures  int
	CBOpenDuration time.Duration

	// Logging
	LogLevel string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:     getEnv("PORT", "8080"),
		Env:      getEnv("ENV", "production"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
	}

	// Auth configuration
	authEnabled := getEnv("AUTH_ENABLED", "true")
	cfg.AuthEnabled = authEnabled != "false"

	if cfg.AuthEnabled {
		cfg.Auth0Domain = os.Getenv("AUTH0_DOMAIN")
		if cfg.Auth0Domain == "" {
			return nil, fmt.Errorf("AUTH0_DOMAIN is required when AUTH_ENABLED=true")
		}

		cfg.Auth0Audience = os.Getenv("AUTH0_AUDIENCE")
		if cfg.Auth0Audience == "" {
			return nil, fmt.Errorf("AUTH0_AUDIENCE is required when AUTH_ENABLED=true")
		}
	}

	cfg.OpenWeatherAPIKey = os.Getenv("OPENWEATHER_API_KEY")
	if cfg.OpenWeatherAPIKey == "" {
		return nil, fmt.Errorf("OPENWEATHER_API_KEY is required")
	}

	cfg.RedisURL = os.Getenv("REDIS_URL")
	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}

	// CORS
	allowedOrigins := getEnv("ALLOWED_ORIGINS", "")
	if allowedOrigins != "" {
		cfg.AllowedOrigins = strings.Split(allowedOrigins, ",")
	}

	// Cache settings
	cacheTTL := getEnvInt("CACHE_TTL", 3600)
	cfg.CacheTTL = time.Duration(cacheTTL) * time.Second
	cfg.CacheL1MaxSize = getEnvInt("CACHE_L1_MAX_SIZE", 1000)

	// Circuit Breaker settings
	cbTimeout := getEnvInt("CB_TIMEOUT", 1000)
	cfg.CBTimeout = time.Duration(cbTimeout) * time.Millisecond
	cfg.CBMaxFailures = getEnvInt("CB_MAX_FAILURES", 5)
	cfg.CBOpenDuration = time.Duration(getEnvInt("CB_OPEN_DURATION", 30)) * time.Second

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
