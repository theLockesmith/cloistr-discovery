# coldforge-discovery Quick Start

Fast deployment guide for coldforge-discovery on Atlantis.

## Prerequisites Check

```bash
# 1. Verify Dragonfly is deployed
kubectl -n dragonfly get pods

# If not deployed:
atlas kube apply dragonfly --kube-context atlantis

# 2. Verify Docker image exists (built automatically by CI/CD)
```

## Deploy

```bash
# Deploy to Atlantis cluster
atlas kube apply coldforge-discovery --kube-context atlantis
```

## Verify

```bash
# Check pod status
kubectl -n coldforge-discovery get pods

# Check logs
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery -f

# Test health endpoint (internal)
kubectl -n coldforge-discovery port-forward svc/coldforge-discovery 8080:80
curl http://localhost:8080/health

# Test external access (via Cloudflare Tunnel)
curl https://discover.cloistr.xyz/health
```

## Test API

```bash
# List relays
curl https://discover.cloistr.xyz/api/v1/relays | jq .

# Filter by health status
curl 'https://discover.cloistr.xyz/api/v1/relays?health=online' | jq .

# Get specific relay
curl 'https://discover.cloistr.xyz/api/v1/relay/wss%3A%2F%2Frelay.damus.io' | jq .

# Check metrics
curl https://discover.cloistr.xyz/metrics
```

## Update Configuration

```bash
# Edit configuration
vim ~/Atlas/roles/kube/coldforge-discovery/vars/main.yml

# Re-deploy
atlas kube apply coldforge-discovery --kube-context atlantis
```

## Scaling

**Note:** Current architecture is single-replica only. See DEPLOYMENT.md for details.

```bash
# Vertical scaling (recommended) - edit resource limits
vim ~/Atlas/roles/kube/coldforge-discovery/defaults/main.yml
# Update coldforge_discovery_resources section
atlas kube apply coldforge-discovery --kube-context atlantis
```

## Troubleshooting

```bash
# Describe pod
kubectl -n coldforge-discovery describe pod <pod-name>

# View events
kubectl -n coldforge-discovery get events --sort-by='.lastTimestamp'

# Check Dragonfly connection
kubectl -n coldforge-discovery exec -it deployment/coldforge-discovery -- \
  sh -c 'nc -zv dragonfly.dragonfly.svc.cluster.local 6379'

# Check background worker status
curl https://discover.cloistr.xyz/health | jq .workers
```

## Remove

```bash
atlas kube apply coldforge-discovery --kube-context atlantis --extra-vars "kube_state=absent"
```

## Documentation

- Full Deployment Guide: `DEPLOYMENT.md`
- Project Documentation: `CLAUDE.md`
- Atlas Role: `~/Atlas/roles/kube/coldforge-discovery/`
