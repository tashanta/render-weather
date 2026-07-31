// internal/providers/openweathermap_test.go
package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenWeatherMapProvider_GetCurrentWeather_Success(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/data/2.5/weather", r.URL.Path)
		assert.Equal(t, "Paris", r.URL.Query().Get("q"))
		assert.Equal(t, "test_key", r.URL.Query().Get("appid"))
		assert.Equal(t, "metric", r.URL.Query().Get("units"))

		response := map[string]interface{}{
			"name": "Paris",
			"sys":  map[string]string{"country": "FR"},
			"main": map[string]interface{}{
				"temp":       18.5,
				"feels_like": 17.2,
				"humidity":   65,
				"pressure":   1013,
			},
			"wind": map[string]float64{
				"speed": 3.5,
			},
			"weather": []map[string]string{
				{"description": "partly cloudy", "icon": "02d"},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewOpenWeatherMapProvider("test_key", 1*time.Second)
	provider.baseURL = server.URL // Override for testing

	weather, err := provider.GetCurrentWeather(context.Background(), "Paris")

	require.NoError(t, err)
	assert.Equal(t, "Paris", weather.City)
	assert.Equal(t, "FR", weather.Country)
	assert.Equal(t, 18.5, weather.Temperature)
	assert.Equal(t, 17.2, weather.FeelsLike)
	assert.Equal(t, 65, weather.Humidity)
	assert.Equal(t, 1013, weather.Pressure)
	assert.Equal(t, 3.5, weather.WindSpeed)
	assert.Equal(t, "partly cloudy", weather.Description)
	assert.Equal(t, "02d", weather.Icon)
}

func TestOpenWeatherMapProvider_GetCurrentWeather_Retry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Success on 2nd attempt
		response := map[string]interface{}{
			"name": "Paris",
			"sys":  map[string]string{"country": "FR"},
			"main": map[string]interface{}{
				"temp":       18.5,
				"feels_like": 17.2,
				"humidity":   65,
				"pressure":   1013,
			},
			"wind": map[string]float64{"speed": 3.5},
			"weather": []map[string]string{
				{"description": "clear", "icon": "01d"},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewOpenWeatherMapProvider("test_key", 1*time.Second)
	provider.baseURL = server.URL

	weather, err := provider.GetCurrentWeather(context.Background(), "Paris")

	require.NoError(t, err)
	assert.Equal(t, "Paris", weather.City)
	assert.GreaterOrEqual(t, attempts, 2, "Should have retried")
}

func TestOpenWeatherMapProvider_GetCurrentWeather_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "city not found"})
	}))
	defer server.Close()

	provider := NewOpenWeatherMapProvider("test_key", 1*time.Second)
	provider.baseURL = server.URL

	_, err := provider.GetCurrentWeather(context.Background(), "InvalidCity")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "city not found")
}
