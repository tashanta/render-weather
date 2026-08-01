// internal/background/jwks_loader.go
package background

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog/log"
)

// Errors for JWKS operations
var (
	ErrJWKSNotReady = errors.New("JWKS not loaded yet")
	ErrKeyNotFound  = errors.New("key not found for kid")
)

// KeyProvider interface for retrieving signing keys
// Allows mocking JWKSManager in validator tests
type KeyProvider interface {
	GetKey(kid string) (*rsa.PublicKey, error)
	Ready() bool
}

// JWK represents a JSON Web Key
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JSONWebKeySet represents JWKS structure
type JSONWebKeySet struct {
	Keys []JWK `json:"keys"`
}

type JWKSManager struct {
	auth0Domain string
	keys        map[string]*rsa.PublicKey
	mu          sync.RWMutex
	ready       bool
	httpClient  *http.Client
}

func NewJWKSManager(auth0Domain string) *JWKSManager {
	return &JWKSManager{
		auth0Domain: auth0Domain,
		keys:        make(map[string]*rsa.PublicKey),
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
		b.MaxElapsedTime = 0 // Infinite retries

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
	domain := m.auth0Domain
	// Add https:// if no protocol specified
	if !strings.HasPrefix(domain, "http://") && !strings.HasPrefix(domain, "https://") {
		domain = "https://" + domain
	}
	url := fmt.Sprintf("%s/.well-known/jwks.json", domain)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var jwks JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	// Parse keys and store by kid
	keys := make(map[string]*rsa.PublicKey)
	for _, jwk := range jwks.Keys {
		if jwk.Use != "sig" || jwk.Alg != "RS256" {
			continue // Skip non-signing keys
		}
		pubKey, err := parseRSAPublicKey(jwk)
		if err != nil {
			log.Warn().Err(err).Str("kid", jwk.Kid).Msg("failed to parse JWK, skipping")
			continue
		}
		keys[jwk.Kid] = pubKey
	}

	m.mu.Lock()
	m.keys = keys
	m.mu.Unlock()

	return nil
}

// parseRSAPublicKey converts JWK to *rsa.PublicKey
func parseRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	if jwk.Kty != "RSA" {
		return nil, fmt.Errorf("unsupported key type: %s", jwk.Kty)
	}

	// Decode modulus (n)
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)

	// Decode exponent (e)
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}
	// Convert exponent bytes to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

// GetKey returns the RSA public key for the given key ID
func (m *JWKSManager) GetKey(kid string) (*rsa.PublicKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.ready {
		return nil, ErrJWKSNotReady
	}

	key, exists := m.keys[kid]
	if !exists {
		return nil, ErrKeyNotFound
	}

	return key, nil
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
