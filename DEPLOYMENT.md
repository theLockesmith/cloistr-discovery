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
- Deployment with 1 replica
- Service (ClusterIP on port 80)
- Ingress for `discover.cloistr.xyz`
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
  - GET /api/v1/pubkey/{pk}/relays - Find relays with pubkey's content
  - GET /api/v1/activity/streams - List active streams

NDP Event Kinds:
  - 30066: Relay Inventory
  - 30067: Activity Announcement
  - 30068: Discovery Query
  - 30069: Relay Directory Entry

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
  "timestamp": "2026-02-01T12:00:00Z",
  "cache": "connected",
  "relays_monitored": 7
}
```

### 4. Test External Access

Wait for TLS certificate to be issued (1-2 minutes):
```bash
kubectl -n coldforge-discovery get certificate
```

Test the public endpoint:
```bash
curl https://discover.cloistr.xyz/health
```

### 5. Test API Endpoints

```bash
# List monitored relays
curl https://discover.cloistr.xyz/api/v1/relays | jq .

# Query relays for a specific pubkey
curl https://discover.cloistr.xyz/api/v1/pubkey/<hex-pubkey>/relays | jq .

# List active streams
curl https://discover.cloistr.xyz/api/v1/activity/streams | jq .
```

### 6. Verify Prometheus Metrics

```bash
curl https://discover.cloistr.xyz/metrics
```

Should show metrics like:
```
# HELP discovery_relays_monitored Number of relays being monitored
# TYPE discovery_relays_monitored gauge
discovery_relays_monitored 7

# HELP discovery_cache_hits Total cache hits
# TYPE discovery_cache_hits counter
discovery_cache_hits 142
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
| `inventory_ttl` | 12 | Cache TTL for inventories (hours) |
| `activity_ttl` | 15 | Cache TTL for activities (minutes) |
| `log_level` | info | Logging level (debug, info, warn, error) |

### Scaling

To scale the number of replicas:

**Option 1: Via Atlas**
```bash
# Edit ~/Atlas/roles/kube/coldforge-discovery/vars/main.yml
coldforge_discovery_replicas: 3

# Re-deploy
atlas kube apply coldforge-discovery --kube-context atlantis
```

**Option 2: Via kubectl (temporary)**
```bash
kubectl -n coldforge-discovery scale deployment coldforge-discovery --replicas=3
```

### Resource Limits

Default resource limits:
- CPU: 100m (request) / 500m (limit)
- Memory: 128Mi (request) / 512Mi (limit)

To modify, edit `~/Atlas/roles/kube/coldforge-discovery/defaults/main.yml` and re-deploy.

## Troubleshooting

### Pod Not Starting

**Check pod status:**
```bash
kubectl -n coldforge-discovery describe pod <pod-name>
```

**Common issues:**
- **ImagePullBackOff**: Verify image exists in registry
- **CrashLoopBackOff**: Check logs for startup errors
- **Pending**: Check node resources and PVC binding

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

**Check DNS resolution:**
```bash
kubectl -n coldforge-discovery exec -it deployment/coldforge-discovery -- \
  nslookup dragonfly.dragonfly.svc.cluster.local
```

### Ingress Not Working

**Check ingress status:**
```bash
kubectl -n coldforge-discovery describe ingress coldforge-discovery
```

**Check TLS certificate:**
```bash
kubectl -n coldforge-discovery get certificate coldforge-discovery-tls
kubectl -n coldforge-discovery describe certificate coldforge-discovery-tls
```

**Check cert-manager logs:**
```bash
kubectl -n cert-manager logs deployment/cert-manager
```

**Verify Traefik is routing correctly:**
```bash
kubectl -n traefik logs deployment/traefik
```

### High Memory Usage

Check actual memory usage:
```bash
kubectl -n coldforge-discovery top pod
```

If consistently hitting limits, increase memory:
```yaml
# In ~/Atlas/roles/kube/coldforge-discovery/defaults/main.yml
coldforge_discovery_resources:
  limits:
    memory: "1Gi"  # Increased from 512Mi
```

### Relay Monitoring Not Working

**Check logs for relay connection errors:**
```bash
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i error
```

**Verify relay URLs are reachable:**
```bash
# Test from within cluster
kubectl -n coldforge-discovery exec -it deployment/coldforge-discovery -- \
  curl -v wss://relay.damus.io/
```

**Check if relays are being added to cache:**
```bash
# Port forward to Dragonfly
kubectl -n dragonfly port-forward svc/dragonfly 6379:6379

# In another terminal, use redis-cli
redis-cli -h localhost -p 6379 KEYS "relay:*"
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
- Ingress
- ConfigMap
- Namespace

**Note:** This does NOT remove the cluster-wide Dragonfly instance, as it may be used by other services.

## Monitoring & Observability

### Prometheus Metrics

Metrics are automatically scraped by Prometheus via the ServiceMonitor:
- Scrape interval: 30s
- Endpoint: `/metrics`

**View in Prometheus:**
```
# Query relay count
discovery_relays_monitored

# Query cache performance
rate(discovery_cache_hits[5m])
rate(discovery_cache_misses[5m])

# Query API request rate
rate(discovery_http_requests_total[5m])
```

### Grafana Dashboard

(TODO: Create Grafana dashboard for coldforge-discovery)

### Logging

Logs are collected by the cluster logging system (if configured).

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

### Network Policies

(TODO: Add network policies to restrict traffic)

### Pod Security

The deployment runs with default security context. Consider adding:
- Read-only root filesystem
- Non-root user
- Drop all capabilities

### TLS

TLS is automatically configured via cert-manager using Let's Encrypt.

**Certificate details:**
```bash
kubectl -n coldforge-discovery describe certificate coldforge-discovery-tls
```

## Performance Tuning

### Replica Scaling

For high-traffic scenarios:
```yaml
coldforge_discovery_replicas: 3  # Scale to 3 replicas
```

### Horizontal Pod Autoscaler

(TODO: Add HPA configuration for automatic scaling based on CPU/memory)

### Cache Configuration

Adjust TTLs based on usage patterns:
```yaml
inventory_ttl: 6   # Reduce to 6 hours for more frequent updates
activity_ttl: 30   # Increase to 30 minutes for less cache churn
```

## Related Documentation

- Project Documentation: `/home/forgemaster/Development/coldforge-discovery/CLAUDE.md`
- Service Documentation: `~/claude/coldforge/services/discovery/CLAUDE.md`
- NIP Draft: `~/claude/coldforge/research/nip-draft-discovery-protocol.md`
- Atlas Role: `~/Atlas/roles/kube/coldforge-discovery/`
- Kubernetes Manifests: `/home/forgemaster/Development/coldforge-discovery/deploy/k8s/`

## Support

For issues or questions:
1. Check logs: `kubectl -n coldforge-discovery logs`
2. Check events: `kubectl -n coldforge-discovery get events`
3. Review Atlas role configuration: `~/Atlas/roles/kube/coldforge-discovery/`
