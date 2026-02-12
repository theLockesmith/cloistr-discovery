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
- `tasks/shared_task_file.yml` - Kubernetes manifest definitions

## Prerequisites

Before deploying coldforge-discovery, ensure:

1. **Cluster-wide Dragonfly is deployed:**
   ```bash
   atlas kube apply dragonfly --kube-context atlantis
   ```

2. **cert-manager is installed** for TLS certificates

3. **Traefik ingress controller is configured**

## Architecture

```
Internet
   │
   ▼
Traefik Ingress (discovery.cloistr.xyz)
   │
   ▼
coldforge-discovery namespace
   │
   ├─ Deployment: coldforge-discovery
   │  └─ Container: discovery (port 8080)
   ├─ Service: coldforge-discovery (ClusterIP, port 80)
   ├─ ConfigMap: coldforge-discovery-config
   ├─ Ingress: discovery.cloistr.xyz (TLS via cert-manager)
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
| Deployment | coldforge-discovery | coldforge-discovery | Main service |
| Service | coldforge-discovery | coldforge-discovery | Internal routing |
| Ingress | coldforge-discovery | coldforge-discovery | External access |
| ServiceMonitor | coldforge-discovery | coldforge-discovery | Prometheus scraping |

## Configuration Variables

Key environment variables (set via ConfigMap):

| Variable | Default | Description |
|----------|---------|-------------|
| DISCOVERY_PORT | 8080 | HTTP server port |
| LOG_LEVEL | info | Logging level |
| CACHE_URL | redis://dragonfly.dragonfly.svc.cluster.local:6379 | Dragonfly connection |
| SEED_RELAYS | (comma-separated) | Initial relays to monitor |
| RELAY_CHECK_INTERVAL | 300 | Health check interval (seconds) |
| NIP11_TIMEOUT | 10 | NIP-11 fetch timeout (seconds) |
| INVENTORY_TTL | 12 | Cache TTL for inventories (hours) |
| ACTIVITY_TTL | 15 | Cache TTL for activities (minutes) |
| PUBLISH_RELAY | wss://relay.cloistr.xyz | Relay for publishing NDP events |

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
- All endpoints: https://discovery.cloistr.xyz/

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

### Check ingress and TLS

```bash
# View ingress
kubectl -n coldforge-discovery get ingress

# Check certificate
kubectl -n coldforge-discovery get certificate

# Test external access
curl -I https://discovery.cloistr.xyz/health
```

### Verify Dragonfly cluster-wide instance

```bash
# Check Dragonfly pods
kubectl -n dragonfly get pods

# Test connection from discovery pod
kubectl -n coldforge-discovery exec -it deployment/coldforge-discovery -- \
  sh -c 'nc -zv dragonfly.dragonfly.svc.cluster.local 6379'
```

## Manual Deployment (Not Recommended)

If you need to deploy manually for testing, you can generate the manifests:

```bash
# This would require extracting from the Atlas role
# Not recommended - use Atlas instead
```

## Related Documentation

- Service Documentation: `~/claude/coldforge/services/discovery/CLAUDE.md`
- NIP Draft: `~/claude/coldforge/research/nip-draft-discovery-protocol.md`
- Atlas Role: `~/Atlas/roles/kube/coldforge-discovery/`
- Dragonfly Role: `~/Atlas/roles/kube/dragonfly/`
