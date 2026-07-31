// internal/models/weather.go
package models

import "time"

// Weather represents current weather data for a city
type Weather struct {
	City        string    `json:"city"`
	Country     string    `json:"country"`
	Temperature float64   `json:"temperature"`
	FeelsLike   float64   `json:"feels_like"`
	Humidity    int       `json:"humidity"`
	Pressure    int       `json:"pressure"`
	WindSpeed   float64   `json:"wind_speed"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	Timestamp   time.Time `json:"timestamp"`
}
