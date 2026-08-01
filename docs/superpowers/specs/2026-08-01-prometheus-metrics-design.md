# Design: Instrumentation Prometheus Metrics

**Date:** 2026-08-01  
**Status:** Approuvé  
**Auteur:** OpenCode

## Objectif

Instrumenter l'application Weather API Go avec un endpoint public `/metrics` exposant des métriques HTTP et Go runtime, compatible Prometheus et OpenTelemetry Collector.

## Décision

**Option choisie:** Middleware Chi global avec `prometheus/client_golang`

### Pourquoi cette option

- Simple à implémenter (une seule modification dans main.go)
- Métriques uniformes pour tous les endpoints
- `promhttp` gère automatiquement les collectors Go runtime
- Pattern standard reconnu par la communauté
- Librairie officielle Prometheus avec réputation "High"

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Chi Router                           │
├─────────────────────────────────────────────────────────────┤
│  Middleware Stack:                                          │
│    1. Recovery                                              │
│    2. Logging                                               │
│    3. CORS                                                  │
│    4. Prometheus Metrics  ← NOUVEAU                         │
├─────────────────────────────────────────────────────────────┤
│  Routes publiques:                                          │
│    GET /health     → HealthHandler                          │
│    GET /metrics    → promhttp.Handler()  ← NOUVEAU          │
├─────────────────────────────────────────────────────────────┤
│  Routes protégées (Auth middleware):                        │
│    GET /weather/{city}                                      │
│    GET /api/v1/weather/{city}                               │
└─────────────────────────────────────────────────────────────┘
```

### Flux d'une requête

1. Requête arrive → middleware Prometheus démarre le timer
2. Passe par les autres middlewares et le handler
3. Response écrite → middleware Prometheus enregistre:
   - Incrémente `http_requests_total{method, path, code}`
   - Observe la durée dans `http_request_duration_seconds{method, path}`

### Normalisation des paths

Pour éviter l'explosion de cardinalité des labels:
- `/weather/Paris` → `/weather/{city}`
- `/api/v1/weather/London` → `/api/v1/weather/{city}`

Utilisation de `chi.RouteContext(r.Context()).RoutePattern()` pour obtenir le pattern au lieu du path réel.

## Composants

### Nouveau fichier: `internal/middleware/prometheus.go`

Middleware Chi qui:
- Capture le temps de début de requête
- Appelle le handler suivant
- Enregistre les métriques après la réponse
- Normalise les paths via Chi RouteContext

### Modifications: `cmd/api/main.go`

1. Import de `prometheus/client_golang/prometheus` et `promhttp`
2. Ajout du middleware Prometheus dans la stack (après CORS)
3. Nouvelle route publique: `GET /metrics` → `promhttp.Handler()`

### Dépendance

```
github.com/prometheus/client_golang v1.22.0
```

## Métriques exposées

### Métriques HTTP

| Nom | Type | Labels | Description |
|-----|------|--------|-------------|
| `http_requests_total` | Counter | method, path, code | Nombre total de requêtes HTTP |
| `http_request_duration_seconds` | Histogram | method, path | Latence des requêtes HTTP |

**Buckets histogram:** 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 secondes

### Métriques Go runtime (collectors par défaut)

| Nom | Type | Description |
|-----|------|-------------|
| `go_goroutines` | Gauge | Nombre de goroutines actives |
| `go_threads` | Gauge | Nombre de threads OS |
| `go_memstats_alloc_bytes` | Gauge | Mémoire allouée |
| `go_memstats_heap_*` | Gauge | Statistiques heap |
| `go_gc_duration_seconds` | Summary | Durée des pauses GC |

## Gestion d'erreurs

- **Endpoint `/metrics`**: `promhttp.Handler()` gère tout en interne, pas d'erreur possible
- **Middleware Prometheus**: ne peut pas échouer (simple observation)
- **Registry Prometheus**: si échec d'enregistrement → panic au démarrage (fail-fast)

## Tests

### Fichier: `internal/middleware/prometheus_test.go`

| Test | Description |
|------|-------------|
| `TestPrometheusMiddleware_IncrementsCounter` | Vérifie que `http_requests_total` est incrémenté |
| `TestPrometheusMiddleware_RecordsDuration` | Vérifie que `http_request_duration_seconds` est observé |
| `TestPrometheusMiddleware_NormalizesPath` | Vérifie que `/weather/Paris` devient `/weather/{city}` |
| `TestPrometheusMiddleware_LabelsCorrectly` | Vérifie les labels method, path, code |

### Pattern de test

```go
// Créer un registry isolé pour chaque test
// Appeler le handler via httptest.NewRecorder
// Vérifier les métriques via prometheus.Gather()
```

### Vérification manuelle

```bash
# Démarrer l'API
go run cmd/api/main.go

# Faire quelques requêtes
curl http://localhost:8080/health

# Vérifier les métriques
curl http://localhost:8080/metrics | grep http_requests_total
```

## Sécurité

- Endpoint `/metrics` **public** (sans authentification)
- Pas de données sensibles exposées (uniquement compteurs et latences)
- Compatible avec le scraping Prometheus/OpenTelemetry standard

## Hors scope

- Rate limiting sur `/metrics`
- Métriques custom business (ex: cache hit ratio) - pourra être ajouté plus tard
- Métriques par tenant/user
