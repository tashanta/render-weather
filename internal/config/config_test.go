package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_Success(t *testing.T) {
	// Set environment variables
	os.Setenv("PORT", "8080")
	os.Setenv("ENV", "test")
	os.Setenv("AUTH0_DOMAIN", "test.eu.auth0.com")
	os.Setenv("AUTH0_AUDIENCE", "https://api.test.com")
	os.Setenv("OPENWEATHER_API_KEY", "test_key")
	os.Setenv("REDIS_URL", "redis://localhost:6379")
	defer func() {
		os.Clearenv()
	}()

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, "test", cfg.Env)
	assert.Equal(t, "test.eu.auth0.com", cfg.Auth0Domain)
	assert.Equal(t, "https://api.test.com", cfg.Auth0Audience)
	assert.Equal(t, "test_key", cfg.OpenWeatherAPIKey)
	assert.Equal(t, "redis://localhost:6379", cfg.RedisURL)
}

func TestLoadConfig_MissingRequired(t *testing.T) {
	os.Clearenv()

	_, err := Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Set only required vars
	os.Setenv("AUTH0_DOMAIN", "test.eu.auth0.com")
	os.Setenv("AUTH0_AUDIENCE", "https://api.test.com")
	os.Setenv("OPENWEATHER_API_KEY", "test_key")
	os.Setenv("REDIS_URL", "redis://localhost:6379")
	defer os.Clearenv()

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, "production", cfg.Env)
	assert.Equal(t, 3600*time.Second, cfg.CacheTTL)
	assert.Equal(t, 1000, cfg.CacheL1MaxSize)
	assert.Equal(t, 1000*time.Millisecond, cfg.CBTimeout)
}
