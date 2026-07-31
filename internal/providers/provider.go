// internal/providers/provider.go
package providers

import (
	"context"

	"github.com/yourusername/render-weather/internal/models"
)

// WeatherProvider defines the interface for weather data providers
type WeatherProvider interface {
	GetCurrentWeather(ctx context.Context, city string) (*models.Weather, error)
}
