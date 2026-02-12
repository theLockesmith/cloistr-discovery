# Atlas Role Configuration Summary

This document summarizes the Atlas role configuration for deploying coldforge-discovery to Kubernetes.

## Location

The Atlas role is located at:
```
~/Atlas/roles/kube/coldforge-discovery/
```

## Changes Made

### 1. Updated Dragonfly Configuration

**Before:**
- Deployed a dedicated Dragonfly instance in the coldforge-discovery namespace
- Used local PVC for Dragonfly data
- Consumed additional cluster resources

**After:**
- Uses cluster-wide Dragonfly instance at `dragonfly.dragonfly.svc.cluster.local:6379`
- No local Dragonfly deployment
- Shared caching infrastructure across services
- High availability with 3 replicas and automatic failover

**Files Modified:**
- `defaults/main.yml` - Removed Dragonfly deployment vars, added connection vars
- `tasks/shared_task_file.yml` - Removed PVC, Deployment, and Service tasks for Dragonfly
- `tasks/shared_task_file.yml` - Updated ConfigMap to use cluster-wide Dragonfly host

### 2. Updated Domain Configuration

**Before:**
- Domain: `discovery.cloistr.xyz`
- Ingress disabled by default

**After:**
- Domain: `discovery.cloistr.xyz`
- Ingress enabled by default

**Files Modified:**
- `defaults/main.yml` - Changed `discovery_domain` and set `ingress_enabled: true`
- `vars/main.yml` - Updated domain to coldforge.xyz

### 3. Updated Documentation

**Files Modified:**
- `README.md` - Updated architecture diagram, removed local Dragonfly references
- `tasks/main.yml` - Updated deployment success message

## Atlas Role Structure

```
~/Atlas/roles/kube/coldforge-discovery/
├── README.md                    # Role documentation
├── defaults/
│   └── main.yml                 # Default configuration variables
├── vars/
│   └── main.yml                 # Production overrides
└── tasks/
    ├── main.yml                 # Main orchestration task
    └── shared_task_file.yml     # Kubernetes manifest definitions
```

## Key Configuration Variables

### defaults/main.yml

```yaml
# Namespace
namespace: coldforge-discovery

# Application
discovery_port: 8080
coldforge_discovery_replicas: 1

# Image
coldforge_discovery_image: registry.coldforge.xyz/coldforge/coldforge-discovery
coldforge_discovery_image_tag: latest

# Resources
coldforge_discovery_resources:
  requests:
    cpu: "100m"
    memory: "128Mi"
  limits:
    cpu: "500m"
    memory: "512Mi"

# Dragonfly Connection (cluster-wide)
dragonfly_host: dragonfly.dragonfly.svc.cluster.local
dragonfly_port: 6379
use_cluster_dragonfly: true

# Discovery Configuration
seed_relays:
  - wss://relay.damus.io
  - wss://nos.lol
  - wss://relay.nostr.band
  - wss://relay.cloistr.xyz
relay_check_interval: 300
nip11_timeout: 10
inventory_ttl: 12
activity_ttl: 15
publish_relay: wss://relay.cloistr.xyz
log_level: info

# Ingress
ingress_enabled: true
ingress_class: traefik
cert_issuer: letsencrypt-production
discovery_domain: discovery.cloistr.xyz

# Kubernetes State
kube_state: present
```

### vars/main.yml

```yaml
# Production overrides
discovery_domain: discovery.cloistr.xyz

seed_relays:
  - wss://relay.cloistr.xyz
  - wss://relay.damus.io
  - wss://nos.lol
  - wss://relay.nostr.band
  - wss://relay.snort.social
  - wss://relay.primal.net
  - wss://purplepag.es

publish_relay: wss://relay.cloistr.xyz
log_level: info
```

## Kubernetes Resources Deployed

| Resource Type | Name | Namespace | Purpose |
|--------------|------|-----------|---------|
| Namespace | coldforge-discovery | - | Service isolation |
| ConfigMap | coldforge-discovery-config | coldforge-discovery | Environment variables |
| Deployment | coldforge-discovery | coldforge-discovery | Main application |
| Service | coldforge-discovery | coldforge-discovery | Internal networking |
| Ingress | coldforge-discovery | coldforge-discovery | External access |
| ServiceMonitor | coldforge-discovery | coldforge-discovery | Prometheus metrics |

## Deployment Architecture

```
┌─────────────────────────────────────────────────────────┐
│                       Internet                          │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│              Traefik Ingress Controller                 │
│            (discovery.cloistr.xyz)                    │
│                 TLS via cert-manager                    │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│         Namespace: coldforge-discovery                  │
│                                                         │
│  ┌──────────────────────────────────────────┐           │
│  │  Deployment: coldforge-discovery         │           │
│  │  ├─ Container: discovery (port 8080)     │           │
│  │  ├─ ConfigMap: environment variables     │           │
│  │  ├─ Resources: 100m CPU / 128Mi RAM      │           │
│  │  ├─ Liveness probe: /health              │           │
│  │  └─ Readiness probe: /health             │           │
│  └──────────────────────────────────────────┘           │
│                     │                                   │
│                     ▼                                   │
│  ┌──────────────────────────────────────────┐           │
│  │  Service: coldforge-discovery            │           │
│  │  Type: ClusterIP                         │           │
│  │  Port: 80 -> 8080                        │           │
│  └──────────────────────────────────────────┘           │
│                     │                                   │
│                     ▼                                   │
│  ┌──────────────────────────────────────────┐           │
│  │  ServiceMonitor (Prometheus)             │           │
│  │  Scrape interval: 30s                    │           │
│  │  Path: /metrics                          │           │
│  └──────────────────────────────────────────┘           │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│              Namespace: dragonfly                       │
│          (Cluster-wide cache instance)                  │
│                                                         │
│  ┌──────────────────────────────────────────┐           │
│  │  Dragonfly Cluster                       │           │
│  │  ├─ Replicas: 3 (HA)                     │           │
│  │  ├─ Operator-managed failover            │           │
│  │  ├─ Redis-compatible API                 │           │
│  │  └─ Service: dragonfly:6379              │           │
│  └──────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────┘
```

## Environment Variables (via ConfigMap)

| Variable | Value | Purpose |
|----------|-------|---------|
| DISCOVERY_PORT | 8080 | HTTP server port |
| LOG_LEVEL | info | Logging verbosity |
| CACHE_URL | redis://dragonfly.dragonfly.svc.cluster.local:6379 | Dragonfly connection |
| SEED_RELAYS | (comma-separated URLs) | Initial relays to monitor |
| RELAY_CHECK_INTERVAL | 300 | Health check interval (seconds) |
| NIP11_TIMEOUT | 10 | NIP-11 fetch timeout (seconds) |
| INVENTORY_TTL | 12 | Inventory cache TTL (hours) |
| ACTIVITY_TTL | 15 | Activity cache TTL (minutes) |
| PUBLISH_RELAY | wss://relay.cloistr.xyz | NDP event publishing target |

## Deployment Command

```bash
# Deploy
atlas kube apply coldforge-discovery --kube-context atlantis

# Remove
atlas kube apply coldforge-discovery --kube-context atlantis --extra-vars "kube_state=absent"
```

## Verification Commands

```bash
# Check deployment status
kubectl -n coldforge-discovery get all

# View logs
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery -f

# Test internal health endpoint
kubectl -n coldforge-discovery port-forward svc/coldforge-discovery 8080:80
curl http://localhost:8080/health

# Test external access
curl https://discovery.cloistr.xyz/health
curl https://discovery.cloistr.xyz/api/v1/relays

# Check Prometheus metrics
curl https://discovery.cloistr.xyz/metrics

# Verify Dragonfly connection
kubectl -n coldforge-discovery exec -it deployment/coldforge-discovery -- \
  sh -c 'nc -zv dragonfly.dragonfly.svc.cluster.local 6379'
```

## Prerequisites

Before deploying coldforge-discovery, ensure:

1. **Cluster-wide Dragonfly is deployed:**
   ```bash
   atlas kube apply dragonfly --kube-context atlantis
   kubectl -n dragonfly wait --for=condition=ready pod -l app.kubernetes.io/name=dragonfly --timeout=30m
   ```

2. **Docker image is available in registry:**
   ```bash
   cd ~/Development/coldforge-discovery
   make docker-publish
   ```

3. **DNS is configured:**
   - `discovery.cloistr.xyz` points to cluster ingress

4. **Cluster components are ready:**
   - Traefik ingress controller
   - cert-manager for TLS
   - Prometheus operator (for ServiceMonitor)

## Related Files in Project

```
coldforge-discovery/
├── CLAUDE.md                    # Project documentation (updated)
├── DEPLOYMENT.md                # Full deployment guide (new)
├── deploy/
│   ├── QUICK-START.md           # Quick reference (new)
│   ├── ATLAS-ROLE-SUMMARY.md    # This file (new)
│   └── k8s/                     # Reference manifests (new)
│       ├── README.md
│       ├── namespace.yaml
│       ├── configmap.yaml
│       ├── deployment.yaml
│       ├── service.yaml
│       ├── ingress.yaml
│       └── servicemonitor.yaml
```

## Why Use Atlas?

The Atlas role provides:
- **Idempotency** - Safe to run multiple times
- **Version control** - Configuration tracked in git
- **Repeatability** - Consistent deployments
- **Rollback** - Easy to revert changes
- **Documentation** - Self-documenting infrastructure

**Do NOT use ad hoc kubectl commands for production changes!**

## Customization

To customize the deployment:

1. **Edit configuration:**
   ```bash
   vim ~/Atlas/roles/kube/coldforge-discovery/vars/main.yml
   ```

2. **Common customizations:**
   - Change replica count
   - Modify resource limits
   - Add/remove seed relays
   - Adjust TTL values
   - Change log level

3. **Apply changes:**
   ```bash
   atlas kube apply coldforge-discovery --kube-context atlantis
   ```

## Monitoring

### Prometheus Metrics

Metrics are automatically scraped by Prometheus:
- **Endpoint:** `/metrics`
- **Interval:** 30s
- **Labels:** Automatically tagged with namespace, pod, etc.

### Key Metrics

- `discovery_relays_monitored` - Number of relays being tracked
- `discovery_cache_hits` - Cache hit rate
- `discovery_cache_misses` - Cache miss rate
- `discovery_http_requests_total` - API request count
- `discovery_relay_health_checks` - Health check statistics

### Grafana

(TODO: Create Grafana dashboard for coldforge-discovery metrics)

## Support

For issues or questions:
1. Check logs: `kubectl -n coldforge-discovery logs`
2. Check events: `kubectl -n coldforge-discovery get events`
3. Review Atlas role: `~/Atlas/roles/kube/coldforge-discovery/`
4. Consult DEPLOYMENT.md for troubleshooting steps
