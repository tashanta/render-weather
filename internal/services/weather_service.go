// internal/services/weather_service.go
package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sony/gobreaker"
	"github.com/yourusername/render-weather/internal/models"
	"github.com/yourusername/render-weather/internal/providers"
)

type CacheGetter interface {
	Get(ctx context.Context, key string) (*models.Weather, bool)
	Set(ctx context.Context, key string, value *models.Weather, ttl time.Duration) error
}

type WeatherService struct {
	provider providers.WeatherProvider
	cache    CacheGetter
	cb       *gobreaker.CircuitBreaker
	cacheTTL time.Duration
}

func NewWeatherService(
	provider providers.WeatherProvider,
	cache CacheGetter,
	timeout time.Duration,
	maxFailures int,
	openDuration time.Duration,
	cacheTTL time.Duration,
) *WeatherService {
	cbSettings := gobreaker.Settings{
		Name:        "weather-provider",
		MaxRequests: 1,
		Interval:    60 * time.Second,
		Timeout:     openDuration,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(maxFailures)
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Info().
				Str("circuit_breaker", name).
				Str("from", from.String()).
				Str("to", to.String()).
				Msg("circuit breaker state changed")
		},
	}

	return &WeatherService{
		provider: provider,
		cache:    cache,
		cb:       gobreaker.NewCircuitBreaker(cbSettings),
		cacheTTL: cacheTTL,
	}
}

func (s *WeatherService) GetWeather(ctx context.Context, city string) (*models.Weather, error) {
	key := fmt.Sprintf("weather:%s", strings.ToLower(city))

	// Check cache first
	if weather, found := s.cache.Get(ctx, key); found {
		log.Debug().Str("city", city).Str("key", key).Msg("cache hit")
		return weather, nil
	}

	log.Debug().Str("city", city).Str("key", key).Msg("cache miss")

	// Fetch from provider via circuit breaker
	result, err := s.cb.Execute(func() (interface{}, error) {
		return s.provider.GetCurrentWeather(ctx, city)
	})

	if err != nil {
		if err == gobreaker.ErrOpenState {
			log.Warn().Str("city", city).Msg("circuit breaker open")
			return nil, fmt.Errorf("weather service temporarily unavailable")
		}
		return nil, fmt.Errorf("fetch weather: %w", err)
	}

	weather := result.(*models.Weather)

	// Store in cache
	if err := s.cache.Set(ctx, key, weather, s.cacheTTL); err != nil {
		log.Warn().Err(err).Str("key", key).Msg("failed to cache weather")
		// Don't fail the request if caching fails
	}

	return weather, nil
}
