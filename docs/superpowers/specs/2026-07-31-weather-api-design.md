# Weather API - Specification de Conception

**Date:** 2026-07-31  
**Statut:** Approuvé  
**Type:** MVP  

## Vue d'Ensemble

Service API REST en Go qui expose les données météorologiques actuelles via OpenWeatherMap, avec authentification Auth0, cache hybride (mémoire + Redis), circuit breaker, et déploiement sur Render.com (plan Hobby).

## Objectifs

- Fournir une API REST simple pour obtenir la météo actuelle d'une ville par son nom
- Optimiser les coûts via un cache hybride L1 (mémoire) + L2 (Redis)
- Garantir la résilience avec circuit breaker sur les appels OpenWeatherMap
- Sécuriser l'accès via Auth0 (OAuth2/JWT)
- Déployer sur Render.com avec contraintes plan Hobby
- Logging structuré pour observabilité

## Architecture Globale

### Structure du Projet

```
render-weather/
├── cmd/
│   └── api/
│       └── main.go              # Point d'entrée, initialisation
├── internal/
│   ├── handlers/                # Endpoints HTTP (Chi)
│   │   ├── weather.go
│   │   └── health.go
│   ├── services/                # Logique métier
│   │   └── weather_service.go
│   ├── providers/               # Interface + implémentations météo
│   │   ├── provider.go          # Interface WeatherProvider
│   │   └── openweathermap.go    # Implémentation OpenWeatherMap
│   ├── cache/                   # Cache hybride
│   │   ├── cache.go             # Interface + logique
│   │   ├── memory.go            # Cache LRU L1
│   │   └── redis.go             # Cache Redis L2
│   ├── middleware/              # Auth, logging, CORS
│   │   ├── auth.go
│   │   ├── logging.go
│   │   ├── cors.go
│   │   └── recovery.go
│   ├── models/                  # Structures de données
│   │   └── weather.go
│   └── config/                  # Configuration
│       └── config.go
├── docs/
│   └── superpowers/
│       └── specs/
├── go.mod
├── go.sum
├── Dockerfile
├── .env.example
├── README.md
└── AGENTS.md
```

### Interface WeatherProvider

Permet de changer de fournisseur météo facilement :

```go
type WeatherProvider interface {
    GetCurrentWeather(ctx context.Context, city string) (*Weather, error)
}
```

## Flux de Données

### Requête Typique avec Circuit Breaker

```
Client → API (/weather/{city})
  ↓
1. Middleware Auth (validation JWT Auth0)
  ↓
2. Middleware Logging (log requête)
  ↓
3. Handler weather.go
  ↓
4. WeatherService.GetWeather("Paris")
  ↓
5. Cache.Get("weather:paris")
  ├─ HIT L1 (mémoire) → Retour immédiat
  │
  ├─ MISS L1 → Check L2 (Redis)
  │   ├─ HIT L2 → Stockage en L1 + retour
  │   └─ MISS L2 → Circuit Breaker Check
  │       │
  │       ├─ OPEN (trop d'échecs)
  │       │   → 503 Service Unavailable + Retry-After: 30
  │       │
  │       ├─ HALF-OPEN (test récupération)
  │       │   → 1 tentative test
  │       │
  │       └─ CLOSED (normal)
  │           → WeatherProvider.GetCurrentWeather("Paris")
  │           → Timeout 1s
  │           → Appel OpenWeatherMap API
  │           ├─ Succès → Cache L1+L2 (TTL 1h) → Réponse
  │           ├─ 429/5xx/timeout → Incrémente erreurs
  │           └─ Seuil atteint → Circuit OPEN
```

### Configuration Circuit Breaker

- **Timeout:** 1 seconde (requête OpenWeatherMap)
- **Seuil d'ouverture:** 5 échecs consécutifs OU 10 échecs en 1 minute
- **Conditions d'échec:**
  - HTTP 429 (rate limit OpenWeatherMap : 60 req/min)
  - HTTP 5xx (erreur serveur)
  - Timeout >1s
  - Erreur réseau
- **Durée état OPEN:** 30 secondes
- **État HALF-OPEN:** Après 30s, teste 1 requête
- **Retour à CLOSED:** Si requête HALF-OPEN réussit
- **Bibliothèque:** `github.com/sony/gobreaker` ou implémentation custom

## API Endpoints

### Endpoint Météo

```
GET /api/v1/weather/{city}    # Version explicite
GET /weather/{city}             # Alias vers dernière version (v1)
```

**Authentification:** Bearer token JWT (Auth0) requis  
**Header:** `Authorization: Bearer <jwt_token>`

**Paramètres:**
- `city` (path parameter, string) : Nom de la ville (ex: "Paris", "Lyon", "New York")

**Exemples:**
```
GET /api/v1/weather/Paris
GET /weather/Lyon
GET /api/v1/weather/New%20York
```

**Réponse succès (200):**
```json
{
  "city": "Paris",
  "country": "FR",
  "temperature": 18.5,
  "feels_like": 17.2,
  "humidity": 65,
  "pressure": 1013,
  "wind_speed": 3.5,
  "description": "Partly cloudy",
  "icon": "02d",
  "timestamp": "2026-07-31T14:30:00Z"
}
```

**Headers de réponse:**
```
Cache-Control: public, max-age=3600
Content-Type: application/json
X-Cache-Hit: true
```

### Endpoint Health Check

```
GET /health
```

**Pas d'authentification requise**

**Réponse (200):**
```json
{
  "status": "ok"
}
```

**Principe:** Le healthcheck indique si le service peut traiter des requêtes, pas l'état des dépendances. Si Redis est down mais l'API fonctionne en mode dégradé (L1 + OpenWeatherMap), le healthcheck retourne 200.

### Codes d'Erreur

| Code | error_code | Contexte |
|------|-----------|----------|
| 400 | `invalid_city` | Paramètre city vide ou format invalide |
| 401 | `unauthorized` | Auth JWT échoué (message générique pour sécurité) |
| 404 | `city_not_found` | Ville introuvable par OpenWeatherMap |
| 429 | `rate_limited` | Rate limit atteint |
| 503 | `service_unavailable` | Circuit breaker ouvert |
| 500 | `internal_error` | Erreur serveur inattendue |

**Format standardisé:**
```json
{
  "error": "error_code"
}
```

**Exemple 503 avec circuit breaker:**
```json
{
  "error": "service_unavailable"
}
```
Header: `Retry-After: 30`

## Authentification et Sécurité

### Auth0 Integration

- Validation JWT via Auth0 public keys (JWKS endpoint)
- Middleware d'authentification sur `/weather/*` et `/api/v1/weather/*`
- Healthcheck `/health` non protégé

**Variables de configuration:**
- `AUTH0_DOMAIN` : Domain Auth0 (ex: `tenant.eu.auth0.com`)
- `AUTH0_AUDIENCE` : API identifier configuré dans Auth0

**Validation du JWT:**
- Vérification signature avec clés publiques Auth0
- Vérification expiration (`exp` claim)
- Vérification audience (`aud` claim)
- Vérification issuer (`iss` claim)

**Réponse en cas d'échec (401):**
```json
{
  "error": "unauthorized"
}
```

**Principe de sécurité:** Message générique pour ne pas donner d'indices aux attaquants. Pas de détail sur la cause (token manquant, expiré, invalide, etc.).

### Sécurité Supplémentaire

- **CORS:** Configuré via `ALLOWED_ORIGINS` (domaines autorisés)
- **Headers de sécurité:** X-Content-Type-Options, X-Frame-Options, etc.
- **Secrets:** Clé OpenWeatherMap stockée en secret Render.com (`OPENWEATHER_API_KEY`)
- **Rate limiting:** Pas de rate limiting applicatif (plan Hobby n'a pas de CDN). À ajouter côté CDN si upgrade vers plan payant.
- **Pas de stack traces:** Réponses minimalistes, logs détaillés serveur uniquement

## Gestion du Cache

### Stratégie Cache Hybride (L1 + L2)

**L1 - Cache mémoire (in-process):**
- Implémentation : LRU cache
- Taille maximale : 1000 entrées (configurable via `CACHE_L1_MAX_SIZE`)
- TTL : 1 heure (configurable via `CACHE_TTL`)
- Scope : Instance locale uniquement
- Avantage : Latence ultra-faible (<1ms)

**L2 - Redis (Render.com):**
- Type : Redis managed by Render (plan gratuit pour MVP)
- TTL : 1 heure (même que L1)
- Scope : Partagé entre instances
- Avantage : Persistance et partage

### Préchargement au Démarrage

**Séquence d'initialisation:**

```
1. Application démarre
2. Connexion à Redis
   ├─ Succès → Continue
   └─ Échec → Log warning, continue avec L1 uniquement
3. Scan des clés `weather:*` dans Redis
4. Chargement de toutes les entrées valides (non expirées) dans L1
5. Log du nombre d'entrées préchargées
6. Démarrage du serveur HTTP
```

**Avantages:**
- Réduction drastique des appels Redis après démarrage
- Temps de réponse optimal dès la première requête
- Économie de coûts Redis (plan gratuit limité)
- Meilleure résilience (données chaudes déjà en mémoire)

### Logique de Récupération

```
1. Check L1 (mémoire) 
   → HIT = retour immédiat
   
2. MISS L1 → Check L2 (Redis)
   → HIT L2 = stockage en L1 + retour
   → MISS L2 = appel OpenWeatherMap (via circuit breaker)
              → stockage L2 + L1 + retour
```

### Format de Clé Cache

Format : `weather:{city_lowercase}`

Exemples :
- `weather:paris`
- `weather:new york`
- `weather:lyon`

### Mode Dégradé

**Si Redis inaccessible:**
- Au démarrage : Log warning, L1 vide, service démarre normalement
- En runtime : Log warning, fonctionnement avec L1 uniquement + appels OpenWeatherMap

**Principe:** Le service reste opérationnel même sans Redis. Le cache L1 et OpenWeatherMap suffisent pour assurer le service.

## Logging et Métriques

### Format de Logs

- Logs structurés en JSON vers stdout
- Capturés automatiquement par Render.com
- Bibliothèque : `github.com/rs/zerolog` ou `go.uber.org/zap`

### Niveaux de Logs

- `DEBUG` : Détails techniques (désactivé en production)
- `INFO` : Requêtes HTTP, cache hits/miss, démarrage/arrêt
- `WARN` : Redis inaccessible, circuit breaker OPEN, timeouts
- `ERROR` : Erreurs critiques, panics récupérés

### Champs Standard

```json
{
  "timestamp": "2026-07-31T14:30:00Z",
  "level": "info",
  "message": "Request processed",
  "request_id": "uuid",
  "method": "GET",
  "path": "/weather/Paris",
  "status": 200,
  "duration_ms": 45,
  "cache_hit": true,
  "cache_level": "L1",
  "user_id": "auth0|123"
}
```

### Événements à Logger

- **Chaque requête HTTP** (middleware avec request_id unique)
- **Cache hit/miss** (L1 et L2 séparément)
- **Appels OpenWeatherMap** (durée, statut, ville)
- **Circuit breaker** : Transitions d'état (CLOSED → OPEN → HALF-OPEN)
- **Erreurs Redis** (connexion, read, write)
- **Démarrage** : 
  - Configuration (sans secrets)
  - Nombre d'entrées préchargées depuis Redis
  - État des connexions (Redis, Auth0)
- **Erreurs authentification** (sans détails sensibles)

### Métriques Basiques

Via logs structurés, analysables dans Render dashboard ou export futur :

- Nombre de requêtes par endpoint
- Taux de cache hit (L1 vs L2 vs miss)
- Latences (p50, p95, p99)
- Nombre d'appels OpenWeatherMap
- État du circuit breaker (durée en OPEN)
- Erreurs par type (401, 404, 429, 503, 500)

## Gestion des Erreurs

### Format Standardisé

```json
{
  "error": "error_code"
}
```

**Principe:** Réponses minimalistes pour le client, logs détaillés côté serveur.

### Catalogue des Erreurs

| HTTP | error_code | Description | Action Interne |
|------|-----------|-------------|----------------|
| 400 | `invalid_city` | City manquant/vide/invalide | Log warn avec détails |
| 401 | `unauthorized` | Auth échoué | Log warn (sans token) |
| 404 | `city_not_found` | Ville introuvable par OpenWeatherMap | Log info |
| 429 | `rate_limited` | Rate limit atteint | Log warn, incrémente circuit breaker |
| 503 | `service_unavailable` | Circuit breaker ouvert | Log warn avec état CB |
| 500 | `internal_error` | Erreur serveur inattendue | Log error avec stack trace |

### Comportement Interne

- **Panic recovery middleware** : Capture les panics, log complet avec stack trace, retourne 500
- **Timeout global par requête** : 2 secondes (abort si dépassé)
- **Erreurs Redis non-critiques** : Log warning, continue sans L2
- **Pas de stack traces dans les réponses** : Seulement dans les logs serveur
- **Request ID** : UUID généré par requête pour traçabilité

## Configuration et Variables d'Environnement

### Variables Requises (Secrets Render.com)

```bash
# Auth0
AUTH0_DOMAIN=tenant.eu.auth0.com
AUTH0_AUDIENCE=https://api.weather.example.com

# OpenWeatherMap
OPENWEATHER_API_KEY=your_secret_key_here

# Redis (fourni automatiquement par Render)
REDIS_URL=redis://user:pass@host:port
```

### Variables Optionnelles (Environment Variables Render.com)

```bash
# Serveur
PORT=8080                          # Port d'écoute (défaut: 8080)
ENV=production                     # Environment (dev/staging/production)

# CORS
ALLOWED_ORIGINS=https://app.example.com,https://www.example.com

# Cache
CACHE_TTL=3600                     # Durée en secondes (défaut: 3600 = 1h)
CACHE_L1_MAX_SIZE=1000            # Taille max cache L1 (défaut: 1000)

# Circuit Breaker
CB_TIMEOUT=1000                    # Timeout en ms (défaut: 1000)
CB_MAX_FAILURES=5                 # Échecs avant ouverture (défaut: 5)
CB_OPEN_DURATION=30               # Durée état OPEN en secondes (défaut: 30)

# Logging
LOG_LEVEL=info                     # debug/info/warn/error (défaut: info)
```

### Validation au Démarrage

**Séquence de validation:**

1. Chargement des variables d'environnement
2. Vérification présence des variables requises
3. Échec immédiat si variables critiques manquantes (exit code 1)
4. Log de la configuration chargée (sans secrets)
5. Test de connexion Redis (non-bloquant si échec)
6. Chargement clés publiques Auth0 JWKS
7. Préchargement cache depuis Redis
8. Démarrage serveur HTTP

**Fichier `.env.example`:**

Template à créer avec toutes les variables pour faciliter le setup local.

## Déploiement sur Render.com

### Plan Render : Hobby

**Contraintes:**
- 512 MB RAM
- CPU partagé
- Service peut hiberner après 15 min d'inactivité
- Pas de CDN edge
- Redis gratuit limité

**Adaptations:**
- Cache L1 avec préchargement crucial pour performance
- Taille cache L1 ajustable si contrainte mémoire
- Headers Cache-Control émis pour upgrade futur vers CDN

### Ressources Render à Créer

**1. Redis Instance**
- Nom : `render-weather-redis`
- Plan : **Gratuit**
- Variable `REDIS_URL` automatiquement injectée dans le web service

**2. Web Service**
- Nom : `render-weather-api`
- Type : Web Service
- Runtime : **Docker**
- Branch : `main`
- Auto-deploy : Activé
- Health check path : `/health`
- Lien avec Redis instance créée

### Dockerfile Multi-Stage Rootless

```dockerfile
# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o api cmd/api/main.go

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS calls
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /home/appuser

# Copy binary from builder
COPY --from=builder --chown=appuser:appuser /app/api .

# Switch to non-root user
USER appuser

EXPOSE 8080

CMD ["./api"]
```

**Principe de sécurité:** Container rootless avec utilisateur non-privilégié (UID/GID 1000).

### Secrets à Configurer Manuellement

Via Render dashboard, section "Environment" du web service :

```
AUTH0_DOMAIN
AUTH0_AUDIENCE
OPENWEATHER_API_KEY
ALLOWED_ORIGINS (si CORS nécessaire)
```

### Déploiement Continu

- Push sur `main` → Build Docker automatique → Déploiement
- Rollback disponible via Render dashboard (historique des déploiements)
- Zero-downtime deployment géré par Render (sauf plan Hobby)

## Stratégie de Tests

### Tests Unitaires

**Bibliothèque:**
- `testing` (standard library)
- `github.com/stretchr/testify` (assertions, mocks)

**Couverture cible:** >80% sur logique métier

**Fichiers de test:**

```
internal/
├── cache/
│   ├── cache_test.go         # Test logique L1+L2
│   ├── memory_test.go        # Test LRU
│   └── redis_test.go         # Test avec redis mock
├── providers/
│   └── openweathermap_test.go # Test avec HTTP mock
├── services/
│   └── weather_service_test.go # Test logique métier
├── handlers/
│   └── weather_test.go       # Test handlers HTTP
└── middleware/
    ├── auth_test.go
    ├── logging_test.go
    └── recovery_test.go
```

### Tests d'Intégration

**Avec mocks uniquement (pas d'appels réels) :**

- Test du circuit breaker avec serveur HTTP mock lent/down/429
- Test du flow complet : requête → cache miss → OpenWeatherMap mock → cache store
- Test mode dégradé (Redis down via mock)
- Test préchargement cache au démarrage (Redis mock avec données)
- Test authentification avec JWT mock (pas d'appel Auth0 réel)
- Test timeout 1s avec serveur mock lent

**Principe:** Auth0 et OpenWeatherMap ne doivent JAMAIS être appelés dans les tests. Tous les services externes sont mockés.

### Tests End-to-End

**Pour MVP:** Tests manuels avec curl/Postman suffisants

**Tests manuels à effectuer:**
- Requête avec token Auth0 valide
- Requête sans token (401)
- Requête ville valide (cache miss → OpenWeatherMap)
- Requête ville valide (cache hit L1)
- Requête ville invalide (404)
- Healthcheck

### Commandes de Test

```bash
# Tous les tests
go test ./...

# Avec couverture
go test -cover ./...

# Coverage détaillé HTML
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Verbose
go test -v ./...

# Test spécifique
go test ./internal/cache -v

# Race detector
go test -race ./...
```

### CI Future (hors scope MVP)

Commandes à automatiser plus tard :

```bash
# Lint
golangci-lint run

# Format check
gofmt -s -w .

# Security scan
gosec ./...

# Tests avec race detector
go test -race ./...
```

## Stack Technique

### Backend
- **Langage:** Go 1.21+
- **Framework HTTP:** Chi (`github.com/go-chi/chi/v5`)
- **Auth:** Auth0 + JWT validation (`github.com/auth0/go-jwt-middleware`)
- **Cache Redis:** `github.com/redis/go-redis/v9`
- **Circuit Breaker:** `github.com/sony/gobreaker`
- **Logging:** `github.com/rs/zerolog` ou `go.uber.org/zap`
- **Tests:** `testing` + `github.com/stretchr/testify`

### Infrastructure
- **Hosting:** Render.com (plan Hobby)
- **Cache:** Redis managed by Render (plan gratuit)
- **Auth:** Auth0 (plan gratuit suffisant pour MVP)
- **Météo:** OpenWeatherMap API (plan gratuit : 60 req/min, 1M req/mois)

## Contraintes et Limitations

### Plan Render Hobby
- Service peut hiberner après inactivité (cold start ~10-30s)
- 512 MB RAM (ajuster taille cache L1 si nécessaire)
- Pas de CDN edge (cache L1+L2 crucial)

### OpenWeatherMap Gratuit
- Rate limit : 60 requêtes/minute
- Quota : 1,000,000 requêtes/mois
- Mitigation : Cache 1h + circuit breaker

### Redis Gratuit Render
- Capacité limitée (quelques MB)
- Pas de persistence garantie
- Mitigation : Préchargement L1 au démarrage

## Prochaines Étapes (Hors Scope MVP)

**Évolutions possibles :**

1. **Upgrade Render vers plan payant:**
   - CDN edge pour cache distribué
   - Rate limiting CDN
   - Plus de RAM pour cache L1 plus grand

2. **Features supplémentaires:**
   - Prévisions météo (3-5 jours)
   - Recherche par coordonnées GPS
   - Historique météo
   - Webhooks pour alertes météo

3. **Observabilité avancée:**
   - Export logs vers service externe (Datadog, New Relic)
   - Dashboards métriques temps réel
   - Alerting automatique

4. **CI/CD:**
   - GitHub Actions pour tests automatiques
   - Déploiement multi-environnements (dev/staging/prod)
   - Tests de charge

## Annexes

### Exemple de Requête cURL

```bash
# Obtenir météo de Paris
curl -H "Authorization: Bearer YOUR_JWT_TOKEN" \
     https://render-weather-api.onrender.com/weather/Paris

# Health check
curl https://render-weather-api.onrender.com/health
```

### Exemple de Réponse Complète

```http
HTTP/1.1 200 OK
Cache-Control: public, max-age=3600
Content-Type: application/json
X-Cache-Hit: true
X-Request-ID: 550e8400-e29b-41d4-a716-446655440000

{
  "city": "Paris",
  "country": "FR",
  "temperature": 18.5,
  "feels_like": 17.2,
  "humidity": 65,
  "pressure": 1013,
  "wind_speed": 3.5,
  "description": "Partly cloudy",
  "icon": "02d",
  "timestamp": "2026-07-31T14:30:00Z"
}
```

### Diagramme de Séquence Complet

```
Client              API                Auth0       Cache L1    Cache L2    Circuit Breaker    OpenWeatherMap
  |                  |                   |             |           |              |                   |
  |-- GET /weather/Paris --------------->|             |           |              |                   |
  |                  |-- Validate JWT -->|             |           |              |                   |
  |                  |<-- JWT Valid -----|             |           |              |                   |
  |                  |-- Check L1 ------>|             |           |              |                   |
  |                  |<-- MISS -----------|             |           |              |                   |
  |                  |-- Check L2 ---------------------->|           |              |                   |
  |                  |<-- MISS --------------------------|           |              |                   |
  |                  |-- Check State -------------------------------->|              |                   |
  |                  |<-- CLOSED ------------------------------------|              |                   |
  |                  |-- GetWeather(Paris) ------------------------------------------->|                   |
  |                  |                   |             |           |              |-- HTTP GET ------->|
  |                  |                   |             |           |              |<-- 200 OK ---------|
  |                  |<-- Weather Data --------------------------------------------|                   |
  |                  |-- Store L2 ---------------------->|           |              |                   |
  |                  |-- Store L1 ------>|             |           |              |                   |
  |<-- 200 OK + JSON |                   |             |           |              |                   |
```

---

**Document de conception validé et prêt pour implémentation.**
