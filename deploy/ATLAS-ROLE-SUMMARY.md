# Atlas Role Configuration Summary

This document summarizes the Atlas role configuration for deploying coldforge-discovery to Kubernetes.

## Location

The Atlas role is located at:
```
~/Atlas/roles/kube/coldforge-discovery/
```

## Role Purpose

Deploys the Nostr Relay Discovery Service which:
- Monitors relay health via NIP-11
- Discovers relays from NIP-65 lists and NIP-66 events
- Publishes Kind 30072 (Relay Directory Entry) events

## Atlas Role Structure

```
~/Atlas/roles/kube/coldforge-discovery/
├── defaults/
│   └── main.yml                 # Default configuration variables
├── vars/
│   ├── main.yml                 # Production overrides
│   └── vault.yml                # Secrets (encrypted)
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

# Image (set by CI/CD)
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
log_level: info

# Publishing Configuration
publish_enabled: true
publish_relays:
  - wss://relay.cloistr.xyz
publish_interval: 10

# Discovery Sources
nip65_crawl_enabled: true
nip65_crawl_interval: 30
nip66_enabled: true
peer_discovery_enabled: true

# Admin Interface
admin_enabled: true

# External Access (via Cloudflare Tunnel)
ingress_enabled: false
discovery_domain: discover.cloistr.xyz

# Kubernetes State
kube_state: present
```

### vars/vault.yml (encrypted)

```yaml
# Secrets for signing Kind 30072 events and admin access
nostr_private_key: <encrypted>
admin_api_key: <encrypted>
dragonfly_password: <encrypted>
```

## Kubernetes Resources Deployed

| Resource Type | Name | Namespace | Purpose |
|--------------|------|-----------|---------|
| Namespace | coldforge-discovery | - | Service isolation |
| Secret | coldforge-discovery-secrets | coldforge-discovery | Private keys, API keys |
| ConfigMap | coldforge-discovery-config | coldforge-discovery | Environment variables |
| Deployment | coldforge-discovery | coldforge-discovery | Main application |
| Service | coldforge-discovery | coldforge-discovery | Internal networking |
| ServiceMonitor | coldforge-discovery | coldforge-discovery | Prometheus metrics |

## Deployment Architecture

```
┌─────────────────────────────────────────────────────────┐
│                       Internet                          │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│              Cloudflare Tunnel                          │
│            (discover.cloistr.xyz)                       │
│     Path-based routing: /api/* → backend                │
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
│  │  ├─ Secret: NOSTR_PRIVATE_KEY, API_KEY   │           │
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
│  │  ├─ Redis-compatible API                 │           │
│  │  ├─ Authenticated access                 │           │
│  │  └─ Service: dragonfly:6379              │           │
│  └──────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────┘
```

## Environment Variables (via ConfigMap)

| Variable | Value | Purpose |
|----------|-------|---------|
| DISCOVERY_PORT | 8080 | HTTP server port |
| LOG_LEVEL | info | Logging verbosity |
| CACHE_URL | redis://...:6379 | Dragonfly connection |
| SEED_RELAYS | (comma-separated URLs) | Initial relays to monitor |
| RELAY_CHECK_INTERVAL | 300 | Health check interval (seconds) |
| NIP11_TIMEOUT | 10 | NIP-11 fetch timeout (seconds) |
| PUBLISH_ENABLED | true | Enable Kind 30072 publishing |
| PUBLISH_RELAYS | (comma-separated) | Relays to publish to |
| PUBLISH_INTERVAL | 10 | Minutes between publish cycles |
| NIP65_CRAWL_ENABLED | true | Enable NIP-65 discovery |
| NIP66_ENABLED | true | Consume NIP-66 events |
| ADMIN_ENABLED | true | Enable admin endpoints |

## Deployment Commands

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
curl https://discover.cloistr.xyz/health
curl https://discover.cloistr.xyz/api/v1/relays

# Check Prometheus metrics
curl https://discover.cloistr.xyz/metrics

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

2. **Docker image is available in registry** (built automatically by CI/CD on merge to main)

3. **Cloudflare Tunnel is configured:**
   - `cloistr-tunnel` role deployed
   - `discover.cloistr.xyz` configured for path-based routing

4. **Cluster components are ready:**
   - Prometheus operator (for ServiceMonitor)

## Scaling Considerations

**Important:** Current architecture is single-replica only.

Background workers (relay monitor, NIP-65 crawler, NIP-66 consumer, publisher) run on ALL replicas, which would cause:
- Duplicate relay health checks
- Duplicate NIP-65 crawls
- Duplicate Kind 30072 event publishing

See DEPLOYMENT.md for requirements to enable horizontal scaling.

## Monitoring

### Prometheus Metrics

Metrics are automatically scraped by Prometheus:
- **Endpoint:** `/metrics`
- **Interval:** 30s
- **Labels:** Automatically tagged with namespace, pod, etc.

### Key Metrics

- `discovery_relays_total` - Total relays tracked
- `discovery_relays_online` - Online relay count
- `discovery_nip65_relays_found` - Relays discovered via NIP-65
- `discovery_publisher_events_published` - Kind 30072 events published

## Customization

To customize the deployment:

1. **Edit configuration:**
   ```bash
   vim ~/Atlas/roles/kube/coldforge-discovery/vars/main.yml
   ```

2. **Common customizations:**
   - Modify resource limits
   - Add/remove seed relays
   - Adjust check intervals
   - Change log level

3. **Apply changes:**
   ```bash
   atlas kube apply coldforge-discovery --kube-context atlantis
   ```

## Related Documentation

- [CLAUDE.md](../../CLAUDE.md) - Project documentation
- [DEPLOYMENT.md](../../DEPLOYMENT.md) - Full deployment guide
- [README.md](../../README.md) - Project overview
