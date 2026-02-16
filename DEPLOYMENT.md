# coldforge-discovery Deployment Guide

Complete guide for deploying coldforge-discovery to the Atlantis Kubernetes cluster.

## Prerequisites

### 1. Cluster-Wide Dragonfly

The discovery service requires the cluster-wide Dragonfly instance for caching.

**Check if Dragonfly is deployed:**
```bash
kubectl -n dragonfly get pods
```

**Deploy Dragonfly if needed:**
```bash
atlas kube apply dragonfly --kube-context atlantis
```

Wait for Dragonfly to be ready (may take 10-15 minutes on Atlantis due to Ceph):
```bash
kubectl -n dragonfly wait --for=condition=ready pod -l app.kubernetes.io/name=dragonfly --timeout=30m
```

**Verify Dragonfly is accessible:**
```bash
kubectl -n dragonfly exec -it dragonfly-0 -- redis-cli ping
# Should output: PONG
```

### 2. Docker Image

Images are built and pushed automatically by the CI/CD pipeline on merge to main.

## Deployment via Atlas

### Deploy to Atlantis

```bash
# Deploy with default configuration
atlas kube apply coldforge-discovery --kube-context atlantis
```

The Atlas role will create:
- Namespace: `coldforge-discovery`
- ConfigMap with environment variables
- Secret for NOSTR_PRIVATE_KEY and ADMIN_API_KEY
- Deployment with 1 replica
- Service (ClusterIP on port 80)
- ServiceMonitor for Prometheus metrics

### Deployment Output

You should see output similar to:
```
============================================
Coldforge Discovery Deployed Successfully!
============================================

Namespace: coldforge-discovery
Replicas: 1

Internal Endpoints:
  - Health: http://coldforge-discovery.coldforge-discovery.svc.cluster.local:8080/health
  - Metrics: http://coldforge-discovery.coldforge-discovery.svc.cluster.local:8080/metrics
  - API: http://coldforge-discovery.coldforge-discovery.svc.cluster.local:8080/api/v1/

API Endpoints:
  - GET /api/v1/relays - List relays (filter: health, nips, location)
  - GET /api/v1/relay/{url} - Get relay details

NDP Event Kind:
  - 30072: Relay Directory Entry (published)

Cache: Cluster-wide Dragonfly (Redis-compatible)
  - dragonfly.dragonfly.svc.cluster.local:6379

External Access:
  - https://discover.cloistr.xyz

============================================
```

## Verification

### 1. Check Pod Status

```bash
kubectl -n coldforge-discovery get pods
```

Expected output:
```
NAME                                   READY   STATUS    RESTARTS   AGE
coldforge-discovery-xxxxxxxxxx-xxxxx   1/1     Running   0          2m
```

### 2. Check Logs

```bash
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery -f
```

Look for:
- Successful Dragonfly connection
- Relay monitoring starting
- HTTP server listening on port 8080
- NIP-65 crawler starting (if enabled)
- NIP-66 consumer connecting (if enabled)
- Publisher starting (if enabled)

### 3. Test Health Endpoint (Internal)

```bash
# Port forward to local machine
kubectl -n coldforge-discovery port-forward svc/coldforge-discovery 8080:80

# In another terminal
curl http://localhost:8080/health
```

Expected response:
```json
{
  "status": "healthy",
  "workers": {
    "relay-monitor": {"status": "healthy", "last_check": "2026-02-14T12:00:00Z"},
    "nip65-crawler": {"status": "healthy", "last_check": "2026-02-14T12:00:00Z"},
    "publisher": {"status": "healthy", "last_check": "2026-02-14T12:00:00Z"}
  }
}
```

### 4. Test External Access

The service is exposed via Cloudflare Tunnel at `discover.cloistr.xyz`:
```bash
curl https://discover.cloistr.xyz/health
```

### 5. Test API Endpoints

```bash
# List monitored relays
curl https://discover.cloistr.xyz/api/v1/relays | jq .

# Filter by health status
curl https://discover.cloistr.xyz/api/v1/relays?health=online | jq .

# Get specific relay
curl https://discover.cloistr.xyz/api/v1/relay/wss%3A%2F%2Frelay.damus.io | jq .
```

### 6. Verify Prometheus Metrics

```bash
curl https://discover.cloistr.xyz/metrics
```

Should show metrics like:
```
# HELP discovery_relays_total Total number of relays tracked
# TYPE discovery_relays_total gauge
discovery_relays_total 912

# HELP discovery_relays_online Number of online relays
# TYPE discovery_relays_online gauge
discovery_relays_online 423
...
```

## Configuration

### Environment Variables

Configuration is managed via the ConfigMap. To modify:

1. Edit `~/Atlas/roles/kube/coldforge-discovery/vars/main.yml`
2. Re-deploy:
   ```bash
   atlas kube apply coldforge-discovery --kube-context atlantis
   ```

Key variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `seed_relays` | (list) | Initial relays to monitor |
| `relay_check_interval` | 300 | Health check interval (seconds) |
| `publish_enabled` | true | Enable Kind 30072 publishing |
| `publish_interval` | 10 | Minutes between publish cycles |
| `nip65_crawl_enabled` | true | Enable NIP-65 discovery |
| `nip65_crawl_interval` | 30 | Minutes between crawls |
| `nip66_enabled` | true | Consume NIP-66 events |
| `log_level` | info | Logging level (debug, info, warn, error) |

### Secrets

Secrets are managed via Ansible Vault in `vars/vault.yml`:
- `nostr_private_key` - Hex or nsec key for signing Kind 30072 events
- `admin_api_key` - API key for admin endpoints
- `dragonfly_password` - Password for Dragonfly connection

## Scaling

### Current Architecture: Single Replica

**Important:** The current architecture is designed for single-replica deployment. Background workers would run on ALL replicas, causing:

- **Duplicate relay health checks**
- **Duplicate NIP-65 crawls**
- **Duplicate NIP-66 subscriptions**
- **Duplicate Kind 30072 event publishing** (critical issue)

### Vertical Scaling (Recommended)

For increased load, scale vertically by increasing resource limits:

```yaml
# In ~/Atlas/roles/kube/coldforge-discovery/defaults/main.yml
coldforge_discovery_resources:
  requests:
    cpu: "200m"
    memory: "256Mi"
  limits:
    cpu: "1000m"
    memory: "1Gi"
```

### Horizontal Pod Autoscaler (Future)

HPA requires architectural changes before implementation:

1. **Leader Election** - Only one replica runs background workers
2. **Work Distribution** - Partition relay URLs across replicas
3. **Distributed Locking** - Use Redis SETNX for publish deduplication

**Current recommendation:** Keep single replica. The service handles 900+ relays efficiently with minimal resources (100m CPU, 128Mi memory).

### Manual Replica Scaling (Not Recommended)

If you must scale replicas temporarily:

```bash
# Via kubectl (temporary, will be overwritten on redeploy)
kubectl -n coldforge-discovery scale deployment coldforge-discovery --replicas=2
```

**Warning:** This WILL cause duplicate publishing of Kind 30072 events.

## Troubleshooting

### Pod Not Starting

**Check pod status:**
```bash
kubectl -n coldforge-discovery describe pod <pod-name>
```

**Common issues:**
- **ImagePullBackOff**: Verify image exists in registry
- **CrashLoopBackOff**: Check logs for startup errors
- **Pending**: Check node resources

### Cannot Connect to Dragonfly

**Verify Dragonfly is running:**
```bash
kubectl -n dragonfly get pods
kubectl -n dragonfly logs dragonfly-0
```

**Test connection from discovery pod:**
```bash
kubectl -n coldforge-discovery exec -it deployment/coldforge-discovery -- \
  sh -c 'nc -zv dragonfly.dragonfly.svc.cluster.local 6379'
```

### Background Workers Not Running

**Check health endpoint:**
```bash
curl https://discover.cloistr.xyz/health
```

Look for workers with status "unhealthy" or "initializing".

**Check logs for worker errors:**
```bash
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -E "(nip65|nip66|publisher|monitor)"
```

### Publishing Not Working

**Verify publisher is enabled:**
```bash
kubectl -n coldforge-discovery get configmap coldforge-discovery-config -o yaml | grep PUBLISH
```

**Check for NIP-42 auth issues:**
```bash
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i auth
```

**Verify private key is set:**
```bash
kubectl -n coldforge-discovery get secret coldforge-discovery-secrets -o yaml
```

## Updating the Deployment

### Update Image Tag

To deploy a specific version:

```bash
# Edit ~/Atlas/roles/kube/coldforge-discovery/defaults/main.yml
coldforge_discovery_image_tag: v1.0.0  # Change from 'latest'

# Re-deploy
atlas kube apply coldforge-discovery --kube-context atlantis
```

### Rolling Update

Atlas handles rolling updates automatically. When you re-deploy:
1. New pods are created with updated configuration
2. Old pods are terminated after new pods are ready
3. Zero-downtime deployment

**Monitor the rollout:**
```bash
kubectl -n coldforge-discovery rollout status deployment/coldforge-discovery
```

**Rollback if needed:**
```bash
kubectl -n coldforge-discovery rollout undo deployment/coldforge-discovery
```

## Removal

To completely remove the deployment:

```bash
atlas kube apply coldforge-discovery --kube-context atlantis --extra-vars "kube_state=absent"
```

This will remove:
- All pods
- Service
- ConfigMap
- Secret
- Namespace

**Note:** This does NOT remove the cluster-wide Dragonfly instance, as it may be used by other services.

## Monitoring & Observability

### Prometheus Metrics

Metrics are automatically scraped by Prometheus via the ServiceMonitor:
- Scrape interval: 30s
- Endpoint: `/metrics`

**Key metrics:**
```promql
# Total relays tracked
discovery_relays_total

# Relays by health status
discovery_relays_online
discovery_relays_degraded
discovery_relays_offline

# Discovery source counts
discovery_nip65_relays_found
discovery_nip66_events_received

# Publisher activity
discovery_publisher_events_published
discovery_publisher_last_publish_timestamp
```

### Logging

**View live logs:**
```bash
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery -f
```

**Filter for errors:**
```bash
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i error
```

**Change log level:**
```yaml
# In vars/main.yml
log_level: debug  # For more verbose logging
```

## Security

### External Access

External access is via Cloudflare Tunnel (`cloistr-tunnel` role), not direct ingress:
- Domain: `discover.cloistr.xyz`
- Path-based routing: `/api/*` → backend, `/*` → UI frontend
- TLS termination at Cloudflare

### Admin Authentication

Admin endpoints require authentication:
- API Key via `X-API-Key` header or `?api_key=` query param
- Basic auth with configured username/password

## Related Documentation

- [CLAUDE.md](CLAUDE.md) - Full project documentation
- [README.md](README.md) - Project overview
- Atlas Role: `~/Atlas/roles/kube/coldforge-discovery/`
