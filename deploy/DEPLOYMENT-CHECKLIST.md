# Deployment Checklist - coldforge-discovery

Use this checklist when deploying coldforge-discovery to production (Atlantis cluster).

## Pre-Deployment

### 1. Code Ready
- [ ] All code changes committed
- [ ] Unit tests passing (`make test` - 357 tests)
- [ ] Code reviewed
- [ ] Docker Compose local testing completed

### 2. Docker Image
- [ ] CI/CD pipeline completed successfully (images built and pushed automatically on merge to main)

### 3. Dragonfly Cluster-Wide Instance
- [ ] Dragonfly namespace exists
  ```bash
  kubectl get namespace dragonfly
  ```
- [ ] Dragonfly pods running
  ```bash
  kubectl -n dragonfly get pods
  ```
- [ ] Dragonfly service accessible
  ```bash
  kubectl -n dragonfly get svc dragonfly
  ```
- [ ] Test Dragonfly connection
  ```bash
  kubectl -n dragonfly exec -it dragonfly-0 -- redis-cli ping
  # Expected: PONG
  ```

**If Dragonfly not deployed:**
```bash
atlas kube apply dragonfly --kube-context atlantis
kubectl -n dragonfly wait --for=condition=ready pod -l app.kubernetes.io/name=dragonfly --timeout=30m
```

### 4. Cluster Prerequisites
- [ ] Prometheus operator installed (for ServiceMonitor)
  ```bash
  kubectl -n monitoring get prometheus
  ```
- [ ] Cloudflare Tunnel configured (`cloistr-tunnel` role)

### 5. Atlas Role Configuration
- [ ] Review defaults/main.yml
  ```bash
  cat ~/Atlas/roles/kube/coldforge-discovery/defaults/main.yml
  ```
- [ ] Review vars/main.yml
  ```bash
  cat ~/Atlas/roles/kube/coldforge-discovery/vars/main.yml
  ```
- [ ] Verify seed relays are correct
- [ ] Verify domain is `discover.cloistr.xyz`
- [ ] Verify Dragonfly host is `dragonfly.dragonfly.svc.cluster.local`

### 6. Secrets Configuration
- [ ] Vault secrets configured in `vars/vault.yml`
  - `nostr_private_key` - For signing Kind 30072 events
  - `admin_api_key` - For admin endpoints
  - `dragonfly_password` - For cache authentication

## Deployment

### 7. Execute Deployment
- [ ] Run Atlas deployment command
  ```bash
  atlas kube apply coldforge-discovery --kube-context atlantis
  ```
- [ ] Deployment completes without errors
- [ ] Note deployment timestamp: _______________

### 8. Verify Namespace and Resources
- [ ] Namespace created
  ```bash
  kubectl get namespace coldforge-discovery
  ```
- [ ] All resources created
  ```bash
  kubectl -n coldforge-discovery get all
  ```
  Expected:
  - Deployment: coldforge-discovery
  - ReplicaSet: coldforge-discovery-xxxxx
  - Pod: coldforge-discovery-xxxxx-xxxxx
  - Service: coldforge-discovery

### 9. Verify Pod Status
- [ ] Pod is running
  ```bash
  kubectl -n coldforge-discovery get pods
  # Status should be: Running
  ```
- [ ] Pod ready (1/1)
- [ ] No restarts or errors
- [ ] Check pod details if issues
  ```bash
  kubectl -n coldforge-discovery describe pod <pod-name>
  ```

### 10. Verify Logs
- [ ] View startup logs
  ```bash
  kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery --tail=100
  ```
- [ ] No ERROR level messages
- [ ] Dragonfly connection successful
- [ ] HTTP server started on port 8080
- [ ] Relay monitoring initialized
- [ ] Seed relays being contacted
- [ ] NIP-65 crawler started (if enabled)
- [ ] NIP-66 consumer connected (if enabled)
- [ ] Publisher started (if enabled)

## Post-Deployment Verification

### 11. Internal Health Check
- [ ] Port forward to service
  ```bash
  kubectl -n coldforge-discovery port-forward svc/coldforge-discovery 8080:80
  ```
- [ ] Test health endpoint (in another terminal)
  ```bash
  curl http://localhost:8080/health
  ```
- [ ] Response is 200 OK
- [ ] Response shows healthy status with workers
  ```json
  {
    "status": "healthy",
    "workers": {
      "relay-monitor": {"status": "healthy"},
      "nip65-crawler": {"status": "healthy"},
      "publisher": {"status": "healthy"}
    }
  }
  ```

### 12. External Access
- [ ] Access via Cloudflare Tunnel
  ```bash
  curl https://discover.cloistr.xyz/health
  ```
- [ ] Returns 200 OK
- [ ] Response shows healthy status
- [ ] TLS certificate valid (Cloudflare)

### 13. API Endpoints
- [ ] List relays endpoint works
  ```bash
  curl https://discover.cloistr.xyz/api/v1/relays | jq .
  ```
- [ ] Returns JSON with relay array
- [ ] Contains monitored relays
- [ ] Filter endpoints work
  ```bash
  curl 'https://discover.cloistr.xyz/api/v1/relays?health=online' | jq .
  ```
- [ ] Get relay endpoint works
  ```bash
  curl 'https://discover.cloistr.xyz/api/v1/relay/wss%3A%2F%2Frelay.damus.io' | jq .
  ```

### 14. Prometheus Metrics
- [ ] Metrics endpoint accessible
  ```bash
  curl https://discover.cloistr.xyz/metrics
  ```
- [ ] Returns Prometheus format metrics
- [ ] Contains coldforge-discovery specific metrics:
  - `discovery_relays_total`
  - `discovery_relays_online`
  - `discovery_nip65_relays_found`
  - `discovery_publisher_events_published`

### 15. ServiceMonitor
- [ ] ServiceMonitor created
  ```bash
  kubectl -n coldforge-discovery get servicemonitor coldforge-discovery
  ```
- [ ] Prometheus scraping metrics (check Prometheus UI)
- [ ] No scrape errors in Prometheus

### 16. Dragonfly Connection
- [ ] Verify from pod
  ```bash
  kubectl -n coldforge-discovery exec -it deployment/coldforge-discovery -- \
    sh -c 'nc -zv dragonfly.dragonfly.svc.cluster.local 6379'
  ```
- [ ] Connection successful
- [ ] Check cache data (optional)
  ```bash
  kubectl -n dragonfly port-forward svc/dragonfly 6379:6379
  # In another terminal:
  redis-cli -h localhost -p 6379 KEYS "relay:*"
  ```

### 17. Resource Usage
- [ ] Check CPU/memory usage
  ```bash
  kubectl -n coldforge-discovery top pod
  ```
- [ ] Within expected limits (< 500m CPU, < 512Mi memory)
- [ ] No resource throttling

### 18. Events and Errors
- [ ] Check events for issues
  ```bash
  kubectl -n coldforge-discovery get events --sort-by='.lastTimestamp'
  ```
- [ ] No Error or Warning events
- [ ] All events are normal operations

## Monitoring Setup

### 19. Prometheus
- [ ] Service discovered by Prometheus
- [ ] Metrics being scraped (check Prometheus targets)
- [ ] Query test metrics:
  ```promql
  discovery_relays_total
  discovery_relays_online
  rate(discovery_publisher_events_published[5m])
  ```

### 20. Logging
- [ ] Verify logs in cluster logging system (if configured)
- [ ] Set log level appropriately (info for production)

## Functional Testing

### 21. Background Workers
- [ ] Relay monitoring functioning
  ```bash
  kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i "checking relay"
  ```
- [ ] NIP-65 crawling working (if enabled)
  ```bash
  kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i "nip65"
  ```
- [ ] NIP-66 consuming events (if enabled)
  ```bash
  kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i "nip66"
  ```
- [ ] Publisher running (if enabled)
  ```bash
  kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i "publish"
  ```

### 22. Kind 30072 Publishing
- [ ] Publisher connected to target relays
- [ ] Events being published
- [ ] NIP-42 auth working (if relay requires it)

### 23. Cache Performance
- [ ] Verify cache hit/miss rates in metrics
  ```bash
  curl https://discover.cloistr.xyz/metrics | grep cache
  ```
- [ ] Monitor cache memory usage in Dragonfly

### 24. API Performance
- [ ] Test API response times
  ```bash
  time curl https://discover.cloistr.xyz/api/v1/relays
  ```
- [ ] Should be < 1 second for typical queries
- [ ] Test with different query parameters

## Documentation

### 25. Update Documentation
- [ ] Note any configuration changes made
- [ ] Document any issues encountered and resolutions
- [ ] Update runbook if needed

## Rollback Plan (If Needed)

### 26. Rollback Procedure
**Only if deployment has critical issues:**

- [ ] Identify issue severity
- [ ] Determine if rollback necessary
- [ ] Execute rollback:
  ```bash
  # Rollback to previous revision
  kubectl -n coldforge-discovery rollout undo deployment/coldforge-discovery

  # Or remove entirely
  atlas kube apply coldforge-discovery --kube-context atlantis --extra-vars "kube_state=absent"
  ```
- [ ] Verify rollback successful
- [ ] Document rollback reason and issue

## Post-Deployment Monitoring

### 27. First 24 Hours
- [ ] Monitor pod stability (no crashes/restarts)
- [ ] Monitor resource usage trends
- [ ] Monitor error rates in logs
- [ ] Monitor cache hit rates
- [ ] Monitor API response times
- [ ] Check for any unexpected behavior

### 28. First Week
- [ ] Review Prometheus metrics
- [ ] Check for memory leaks
- [ ] Verify relay health checks occurring regularly
- [ ] Monitor publisher activity
- [ ] Review logs for patterns

## Sign-Off

**Deployment completed by:** _______________________

**Date:** _______________________

**Time:** _______________________

**Deployment successful:** [ ] Yes [ ] No

**Issues encountered:** _______________________

**Resolution:** _______________________

**Ready for production traffic:** [ ] Yes [ ] No

**Notes:**
_______________________________________________
_______________________________________________

---

## Quick Reference Commands

### View Status
```bash
kubectl -n coldforge-discovery get all
kubectl -n coldforge-discovery get pods
kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery -f
```

### Test Endpoints
```bash
curl https://discover.cloistr.xyz/health
curl https://discover.cloistr.xyz/api/v1/relays | jq .
curl https://discover.cloistr.xyz/metrics
```

### Troubleshooting
```bash
kubectl -n coldforge-discovery describe pod <pod-name>
kubectl -n coldforge-discovery get events --sort-by='.lastTimestamp'
kubectl -n coldforge-discovery logs <pod-name> --previous
```

### Remove
```bash
atlas kube apply coldforge-discovery --kube-context atlantis --extra-vars "kube_state=absent"
```
