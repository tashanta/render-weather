// internal/background/jwks_loader.go
package background

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/lestrrat-go/jwx/v3/jwk"
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

	ctx := context.Background()

	// Fetch and parse JWKS using jwx library
	set, err := jwk.Fetch(ctx, url, jwk.WithHTTPClient(m.httpClient))
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}

	// Parse keys and store by kid
	keys := make(map[string]*rsa.PublicKey)
	for i := 0; i < set.Len(); i++ {
		key, ok := set.Key(i)
		if !ok {
			continue
		}

		// Skip non-signing keys or non-RS256
		use, _ := key.KeyUsage()
		if use != "sig" {
			continue
		}
		alg, _ := key.Algorithm()
		if algStr := alg.String(); algStr != "" && algStr != "RS256" {
			continue
		}

		kid, _ := key.KeyID()

		// Extract RSA public key
		var rsaKey rsa.PublicKey
		if err := jwk.Export(key, &rsaKey); err != nil {
			log.Warn().Err(err).Str("kid", kid).Msg("failed to export JWK to RSA, skipping")
			continue
		}
		keys[kid] = &rsaKey
	}

	m.mu.Lock()
	m.keys = keys
	m.mu.Unlock()

	return nil
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
