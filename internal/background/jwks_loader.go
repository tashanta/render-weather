// internal/background/jwks_loader.go
package background

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog/log"
)

// JSONWebKeySet represents JWKS structure
type JSONWebKeySet struct {
	Keys []json.RawMessage `json:"keys"`
}

type JWKSManager struct {
	auth0Domain string
	jwks        *JSONWebKeySet
	mu          sync.RWMutex
	ready       bool
	httpClient  *http.Client
}

func NewJWKSManager(auth0Domain string) *JWKSManager {
	return &JWKSManager{
		auth0Domain: auth0Domain,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Start begins background JWKS loading with retry
func (m *JWKSManager) Start() {
	go func() {
		log.Info().Str("domain", m.auth0Domain).Msg("starting JWKS loader")

		// Initial load with retries
		b := backoff.NewExponentialBackOff()
		b.InitialInterval = 100 * time.Millisecond
		b.Multiplier = 2.0
		b.MaxInterval = 1 * time.Second
		b.MaxElapsedTime = 5 * time.Second

		operation := func() error {
			if err := m.fetchJWKS(); err != nil {
				log.Warn().Err(err).Msg("JWKS fetch failed, will retry")
				return err
			}
			return nil
		}

		// Retry until success
		ctx := context.Background()
		if err := backoff.Retry(operation, backoff.WithContext(b, ctx)); err != nil {
			log.Error().Err(err).Msg("JWKS loading failed critically")
			return
		}

		m.mu.Lock()
		m.ready = true
		m.mu.Unlock()

		log.Info().Msg("JWKS loaded successfully, service ready")

		// Refresh every 1 hour
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			if err := m.fetchJWKS(); err != nil {
				log.Warn().Err(err).Msg("JWKS refresh failed, keeping old keys")
			} else {
				log.Info().Msg("JWKS refreshed")
			}
		}
	}()
}

func (m *JWKSManager) fetchJWKS() error {
	url := fmt.Sprintf("%s/.well-known/jwks.json", m.auth0Domain)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var jwks JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	m.mu.Lock()
	m.jwks = &jwks
	m.mu.Unlock()

	return nil
}

func (m *JWKSManager) GetJWKS() (*JSONWebKeySet, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.jwks == nil {
		return nil, fmt.Errorf("JWKS not loaded yet")
	}

	return m.jwks, nil
}

func (m *JWKSManager) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ready
}

// Ready is an alias for IsReady() to match middleware interface
func (m *JWKSManager) Ready() bool {
	return m.IsReady()
}
