# Kubernetes Deployment

This directory contains reference Kubernetes manifests for coldforge-discovery.

**IMPORTANT:** These are reference manifests only. The actual deployment is managed via Atlas roles.

## Deployment via Atlas (Recommended)

The coldforge-discovery service is deployed using the Atlas automation system:

```bash
# Deploy to Atlantis cluster
atlas kube apply coldforge-discovery --kube-context atlantis

# Remove deployment
atlas kube apply coldforge-discovery --kube-context atlantis --extra-vars "kube_state=absent"
```

### Atlas Role Location

The Atlas role is located at:
```
~/Atlas/roles/kube/coldforge-discovery/
```

### Configuration

Configuration is managed via Ansible variables in the Atlas role:

- `defaults/main.yml` - Default configuration
- `vars/main.yml` - Production overrides
- `vars/vault.yml` - Secrets (NOSTR_PRIVATE_KEY, ADMIN_API_KEY)
- `tasks/shared_task_file.yml` - Kubernetes manifest definitions

## Prerequisites

Before deploying coldforge-discovery, ensure:

1. **Cluster-wide Dragonfly is deployed:**
   ```bash
   atlas kube apply dragonfly --kube-context atlantis
   ```

2. **Cloudflare Tunnel is configured** (`cloistr-tunnel` role)

3. **Prometheus operator is installed** for ServiceMonitor

## Architecture

```
Internet
   │
   ▼
Cloudflare Tunnel (discover.cloistr.xyz)
   │
   ├── /api/* → coldforge-discovery (backend)
   └── /* → coldforge-discovery-ui (frontend)
   │
   ▼
coldforge-discovery namespace
   │
   ├─ Deployment: coldforge-discovery
   │  └─ Container: discovery (port 8080)
   ├─ Service: coldforge-discovery (ClusterIP, port 80)
   ├─ ConfigMap: coldforge-discovery-config
   ├─ Secret: coldforge-discovery-secrets
   └─ ServiceMonitor: Prometheus metrics
      │
      ▼
   Dragonfly (cluster-wide)
   dragonfly.dragonfly.svc.cluster.local:6379
```

## Resources Deployed

| Resource | Name | Namespace | Purpose |
|----------|------|-----------|---------|
| Namespace | coldforge-discovery | - | Isolation |
| ConfigMap | coldforge-discovery-config | coldforge-discovery | Environment config |
| Secret | coldforge-discovery-secrets | coldforge-discovery | Private key, API key |
| Deployment | coldforge-discovery | coldforge-discovery | Main service |
| Service | coldforge-discovery | coldforge-discovery | Internal routing |
| ServiceMonitor | coldforge-discovery | coldforge-discovery | Prometheus scraping |

## Configuration Variables

Key environment variables (set via ConfigMap):

| Variable | Default | Description |
|----------|---------|-------------|
| DISCOVERY_PORT | 8080 | HTTP server port |
| LOG_LEVEL | info | Logging level |
| CACHE_URL | redis://dragonfly...:6379 | Dragonfly connection |
| SEED_RELAYS | (comma-separated) | Initial relays to monitor |
| RELAY_CHECK_INTERVAL | 300 | Health check interval (seconds) |
| NIP11_TIMEOUT | 10 | NIP-11 fetch timeout (seconds) |
| PUBLISH_ENABLED | true | Enable Kind 30072 publishing |
| PUBLISH_RELAYS | (comma-separated) | Relays to publish events to |
| PUBLISH_INTERVAL | 10 | Minutes between publish cycles |
| NIP65_CRAWL_ENABLED | true | Enable NIP-65 discovery |
| NIP66_ENABLED | true | Consume NIP-66 events |
| ADMIN_ENABLED | true | Enable admin endpoints |

## Resource Limits

Default resource configuration:

```yaml
requests:
  cpu: 100m
  memory: 128Mi
limits:
  cpu: 500m
  memory: 512Mi
```

## Health Checks

- **Liveness Probe:** HTTP GET /health (port 8080)
  - Initial delay: 10s
  - Period: 30s
  - Timeout: 5s

- **Readiness Probe:** HTTP GET /health (port 8080)
  - Initial delay: 5s
  - Period: 10s
  - Timeout: 3s

## Monitoring

### Prometheus Metrics

Metrics are exposed at `/metrics` and scraped via ServiceMonitor:
- Interval: 30s
- Port: http (80)
- Path: /metrics

### Access Endpoints

**Internal (cluster):**
- Health: http://coldforge-discovery.coldforge-discovery.svc.cluster.local/health
- Metrics: http://coldforge-discovery.coldforge-discovery.svc.cluster.local/metrics
- API: http://coldforge-discovery.coldforge-discovery.svc.cluster.local/api/v1/

**External (public):**
- All endpoints: https://discover.cloistr.xyz/

## Troubleshooting

### Check deployment status

```bash
# View pods
kubectl -n coldforge-discovery get pods

# View logs
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery -f

# Check events
kubectl -n coldforge-discovery get events --sort-by='.lastTimestamp'
```

### Verify Dragonfly connection

```bash
# Port forward to discovery service
kubectl -n coldforge-discovery port-forward svc/coldforge-discovery 8080:80

# Check health endpoint
curl http://localhost:8080/health
```

### Verify Dragonfly cluster-wide instance

```bash
# Check Dragonfly pods
kubectl -n dragonfly get pods

# Test connection from discovery pod
kubectl -n coldforge-discovery exec -it deployment/coldforge-discovery -- \
  sh -c 'nc -zv dragonfly.dragonfly.svc.cluster.local 6379'
```

## Scaling

**Note:** Current architecture supports single replica only. See DEPLOYMENT.md for details on why horizontal scaling requires code changes.

## Related Documentation

- [CLAUDE.md](../../CLAUDE.md) - Project documentation
- [DEPLOYMENT.md](../../DEPLOYMENT.md) - Full deployment guide
- Atlas Role: `~/Atlas/roles/kube/coldforge-discovery/`
- Dragonfly Role: `~/Atlas/roles/kube/dragonfly/`
