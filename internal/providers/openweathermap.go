// internal/providers/openweathermap.go
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/yourusername/render-weather/internal/models"
)

type OpenWeatherMapProvider struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

type owmResponse struct {
	Name string `json:"name"`
	Sys  struct {
		Country string `json:"country"`
	} `json:"sys"`
	Main struct {
		Temp      float64 `json:"temp"`
		FeelsLike float64 `json:"feels_like"`
		Humidity  int     `json:"humidity"`
		Pressure  int     `json:"pressure"`
	} `json:"main"`
	Wind struct {
		Speed float64 `json:"speed"`
	} `json:"wind"`
	Weather []struct {
		Description string `json:"description"`
		Icon        string `json:"icon"`
	} `json:"weather"`
}

func NewOpenWeatherMapProvider(apiKey string, timeout time.Duration) *OpenWeatherMapProvider {
	return &OpenWeatherMapProvider{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		baseURL: "https://api.openweathermap.org",
	}
}

func (p *OpenWeatherMapProvider) GetCurrentWeather(ctx context.Context, city string) (*models.Weather, error) {
	var weather *models.Weather

	// Exponential backoff retry strategy
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 100 * time.Millisecond
	b.Multiplier = 3.0
	b.MaxInterval = 900 * time.Millisecond
	b.MaxElapsedTime = p.httpClient.Timeout

	operation := func() error {
		w, err := p.fetchWeather(ctx, city)
		if err != nil {
			// Don't retry on 4xx client errors
			if isClientError(err) {
				return backoff.Permanent(err)
			}
			return err
		}
		weather = w
		return nil
	}

	if err := backoff.Retry(operation, backoff.WithContext(b, ctx)); err != nil {
		return nil, err
	}

	return weather, nil
}

func (p *OpenWeatherMapProvider) fetchWeather(ctx context.Context, city string) (*models.Weather, error) {
	u, err := url.Parse(fmt.Sprintf("%s/data/2.5/weather", p.baseURL))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	q := u.Query()
	q.Set("q", city)
	q.Set("appid", p.apiKey)
	q.Set("units", "metric")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, &ClientError{Message: "city not found", StatusCode: resp.StatusCode}
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limit exceeded: %d", resp.StatusCode)
	}

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return nil, &ClientError{Message: fmt.Sprintf("client error: %d", resp.StatusCode), StatusCode: resp.StatusCode}
	}

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("server error: %d", resp.StatusCode)
	}

	var owm owmResponse
	if err := json.NewDecoder(resp.Body).Decode(&owm); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	weather := &models.Weather{
		City:        owm.Name,
		Country:     owm.Sys.Country,
		Temperature: owm.Main.Temp,
		FeelsLike:   owm.Main.FeelsLike,
		Humidity:    owm.Main.Humidity,
		Pressure:    owm.Main.Pressure,
		WindSpeed:   owm.Wind.Speed,
		Timestamp:   time.Now().UTC(),
	}

	if len(owm.Weather) > 0 {
		weather.Description = owm.Weather[0].Description
		weather.Icon = owm.Weather[0].Icon
	}

	return weather, nil
}

// ClientError represents a 4xx error that should not be retried
type ClientError struct {
	Message    string
	StatusCode int
}

func (e *ClientError) Error() string {
	return e.Message
}

func isClientError(err error) bool {
	var clientErr *ClientError
	return errors.As(err, &clientErr)
}
