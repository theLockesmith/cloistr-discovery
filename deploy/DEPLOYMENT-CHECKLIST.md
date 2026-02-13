# Deployment Checklist - coldforge-discovery

Use this checklist when deploying coldforge-discovery to production (Atlantis cluster).

## Pre-Deployment

### 1. Code Ready
- [ ] All code changes committed
- [ ] Unit tests passing (`make test`)
- [ ] Code reviewed
- [ ] Docker Compose local testing completed

### 2. Docker Image
- [ ] CI/CD pipeline completed successfully (images built and pushed automatically on merge to main)

### 3. Dragonfly Cluster-Wide Instance
- [ ] Dragonfly namespace exists
  ```bash
  kubectl get namespace dragonfly
  ```
- [ ] Dragonfly pods running (3 replicas)
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
- [ ] Traefik ingress controller running
  ```bash
  kubectl -n traefik get pods
  ```
- [ ] cert-manager installed
  ```bash
  kubectl -n cert-manager get pods
  ```
- [ ] Prometheus operator installed (for ServiceMonitor)
  ```bash
  kubectl -n monitoring get prometheus
  ```

### 5. DNS Configuration
- [ ] DNS record created for `discovery.cloistr.xyz`
- [ ] Points to cluster ingress IP/CNAME
- [ ] DNS propagation verified
  ```bash
  dig discovery.cloistr.xyz
  ```

### 6. Atlas Role Configuration
- [ ] Review defaults/main.yml
  ```bash
  cat ~/Atlas/roles/kube/coldforge-discovery/defaults/main.yml
  ```
- [ ] Review vars/main.yml
  ```bash
  cat ~/Atlas/roles/kube/coldforge-discovery/vars/main.yml
  ```
- [ ] Verify seed relays are correct
- [ ] Verify domain is `discovery.cloistr.xyz`
- [ ] Verify Dragonfly host is `dragonfly.dragonfly.svc.cluster.local`

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
- [ ] Response shows healthy status
  ```json
  {
    "status": "healthy",
    "cache": "connected",
    "relays_monitored": 7
  }
  ```

### 12. Ingress Configuration
- [ ] Ingress resource created
  ```bash
  kubectl -n coldforge-discovery get ingress coldforge-discovery
  ```
- [ ] Ingress has IP/host assigned
- [ ] TLS configuration present

### 13. TLS Certificate
- [ ] Certificate resource created
  ```bash
  kubectl -n coldforge-discovery get certificate coldforge-discovery-tls
  ```
- [ ] Certificate status is Ready
  ```bash
  kubectl -n coldforge-discovery describe certificate coldforge-discovery-tls
  ```
- [ ] No errors in cert-manager logs
  ```bash
  kubectl -n cert-manager logs deployment/cert-manager | grep -i coldforge-discovery
  ```
- [ ] Wait for certificate issuance (1-2 minutes)

### 14. External Access - Health Endpoint
- [ ] Access via HTTPS
  ```bash
  curl https://discovery.cloistr.xyz/health
  ```
- [ ] Returns 200 OK
- [ ] Response shows healthy status
- [ ] TLS certificate valid (no warnings)
  ```bash
  curl -v https://discovery.cloistr.xyz/health 2>&1 | grep -i "subject\|issuer"
  ```

### 15. API Endpoints
- [ ] List relays endpoint works
  ```bash
  curl https://discovery.cloistr.xyz/api/v1/relays | jq .
  ```
- [ ] Returns JSON array
- [ ] Contains monitored relays
- [ ] Pubkey query endpoint works
  ```bash
  curl "https://discovery.cloistr.xyz/api/v1/pubkey/$(openssl rand -hex 32)/relays" | jq .
  ```
- [ ] Activity streams endpoint works
  ```bash
  curl https://discovery.cloistr.xyz/api/v1/activity/streams | jq .
  ```

### 16. Prometheus Metrics
- [ ] Metrics endpoint accessible
  ```bash
  curl https://discovery.cloistr.xyz/metrics
  ```
- [ ] Returns Prometheus format metrics
- [ ] Contains coldforge-discovery specific metrics:
  - `discovery_relays_monitored`
  - `discovery_cache_hits`
  - `discovery_http_requests_total`

### 17. ServiceMonitor
- [ ] ServiceMonitor created
  ```bash
  kubectl -n coldforge-discovery get servicemonitor coldforge-discovery
  ```
- [ ] Prometheus scraping metrics (check Prometheus UI)
- [ ] No scrape errors in Prometheus

### 18. Dragonfly Connection
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
  redis-cli -h localhost -p 6379 KEYS "inventory:*"
  ```

### 19. Resource Usage
- [ ] Check CPU/memory usage
  ```bash
  kubectl -n coldforge-discovery top pod
  ```
- [ ] Within expected limits (< 500m CPU, < 512Mi memory)
- [ ] No resource throttling

### 20. Events and Errors
- [ ] Check events for issues
  ```bash
  kubectl -n coldforge-discovery get events --sort-by='.lastTimestamp'
  ```
- [ ] No Error or Warning events
- [ ] All events are normal operations

## Monitoring Setup

### 21. Prometheus
- [ ] Service discovered by Prometheus
- [ ] Metrics being scraped (check Prometheus targets)
- [ ] Query test metrics:
  ```promql
  discovery_relays_monitored
  rate(discovery_http_requests_total[5m])
  ```

### 22. Grafana (Optional)
- [ ] Import coldforge-discovery dashboard (if available)
- [ ] Verify panels showing data
- [ ] Set up alerts if needed

### 23. Logging
- [ ] Verify logs in cluster logging system (if configured)
- [ ] Set up log aggregation (if needed)
- [ ] Configure log retention policy

## Functional Testing

### 24. NDP Protocol Testing
- [ ] Relay monitoring functioning
  ```bash
  # Check logs for relay health checks
  kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i "relay"
  ```
- [ ] Inventory indexing working
  ```bash
  # Check for inventory events in logs
  kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i "inventory"
  ```
- [ ] Activity tracking operational
  ```bash
  # Check for activity events
  kubectl -n coldforge-discovery logs -l app.kubernetes.io/name=coldforge-discovery | grep -i "activity"
  ```

### 25. Cache Performance
- [ ] Verify cache hit/miss rates in metrics
  ```bash
  curl https://discovery.cloistr.xyz/metrics | grep cache
  ```
- [ ] Check TTL behavior (inventories: 12h, activities: 15m)
- [ ] Monitor cache memory usage in Dragonfly

### 26. API Performance
- [ ] Test API response times
  ```bash
  time curl https://discovery.cloistr.xyz/api/v1/relays
  ```
- [ ] Should be < 1 second for typical queries
- [ ] Test with different query parameters
- [ ] Verify pagination (if implemented)

## Documentation

### 27. Update Documentation
- [ ] Update deployment date in CLAUDE.md
- [ ] Note any configuration changes made
- [ ] Document any issues encountered and resolutions
- [ ] Update runbook if needed

### 28. Team Communication
- [ ] Notify team of deployment
- [ ] Share access URLs:
  - API: https://discovery.cloistr.xyz/api/v1/
  - Health: https://discovery.cloistr.xyz/health
  - Metrics: https://discovery.cloistr.xyz/metrics
- [ ] Document any changes to standard procedures

## Rollback Plan (If Needed)

### 29. Rollback Procedure
**Only if deployment has critical issues:**

- [ ] Identify issue severity
- [ ] Determine if rollback necessary
- [ ] Execute rollback:
  ```bash
  # If deployment failed, remove it
  atlas kube apply coldforge-discovery --kube-context atlantis --extra-vars "kube_state=absent"

  # If need to rollback to previous image version
  # Update image tag in ~/Atlas/roles/kube/coldforge-discovery/defaults/main.yml
  # Then re-deploy with previous version
  ```
- [ ] Verify rollback successful
- [ ] Document rollback reason and issue

## Post-Deployment Monitoring

### 30. First 24 Hours
- [ ] Monitor pod stability (no crashes/restarts)
- [ ] Monitor resource usage trends
- [ ] Monitor error rates in logs
- [ ] Monitor cache hit rates
- [ ] Monitor API response times
- [ ] Check for any unexpected behavior

### 31. First Week
- [ ] Review Prometheus metrics
- [ ] Check for memory leaks
- [ ] Verify relay health checks occurring regularly
- [ ] Verify inventory updates happening
- [ ] Monitor activity stream performance
- [ ] Review logs for patterns

### 32. Optimization (If Needed)
- [ ] Adjust resource limits if needed
- [ ] Tune cache TTLs if needed
- [ ] Adjust health check intervals if needed
- [ ] Scale replicas if needed
- [ ] Consider HPA if traffic varies

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
curl https://discovery.cloistr.xyz/health
curl https://discovery.cloistr.xyz/api/v1/relays | jq .
curl https://discovery.cloistr.xyz/metrics
```

### Troubleshooting
```bash
kubectl -n coldforge-discovery describe pod <pod-name>
kubectl -n coldforge-discovery get events --sort-by='.lastTimestamp'
kubectl -n coldforge-discovery logs <pod-name> --previous
```

### Scale
```bash
kubectl -n coldforge-discovery scale deployment coldforge-discovery --replicas=3
```

### Remove
```bash
atlas kube apply coldforge-discovery --kube-context atlantis --extra-vars "kube_state=absent"
```
