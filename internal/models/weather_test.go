// internal/models/weather_test.go
package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWeather_JSONSerialization(t *testing.T) {
	now := time.Now().UTC()
	weather := Weather{
		City:        "Paris",
		Country:     "FR",
		Temperature: 18.5,
		FeelsLike:   17.2,
		Humidity:    65,
		Pressure:    1013,
		WindSpeed:   3.5,
		Description: "Partly cloudy",
		Icon:        "02d",
		Timestamp:   now,
	}

	// Serialize
	data, err := json.Marshal(weather)
	require.NoError(t, err)

	// Deserialize
	var decoded Weather
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, weather.City, decoded.City)
	assert.Equal(t, weather.Country, decoded.Country)
	assert.Equal(t, weather.Temperature, decoded.Temperature)
	assert.Equal(t, weather.FeelsLike, decoded.FeelsLike)
	assert.Equal(t, weather.Humidity, decoded.Humidity)
	assert.Equal(t, weather.Pressure, decoded.Pressure)
	assert.Equal(t, weather.WindSpeed, decoded.WindSpeed)
	assert.Equal(t, weather.Description, decoded.Description)
	assert.Equal(t, weather.Icon, decoded.Icon)
}
