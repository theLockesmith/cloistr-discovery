# DNS Load Issue from Relay Health Checks

**Date:** 2026-03-08
**Reported in:** cloistr-drive session
**Severity:** Medium - causes intermittent timeouts for co-located services

## Summary

The discovery service's relay health checking generates high DNS query volume, causing intermittent DNS timeouts that affect other services (Drive, Blossom) on the same Kubernetes node.

## Findings

### Symptoms

- Users experiencing timeouts connecting to Drive
- Cloudflare tunnel showing WebSocket disconnections
- Signer showing "connection closed" errors when publishing to relay

### Root Cause Analysis

1. **DNS errors concentrated on one node:**
   ```
   dns-default-84gkl on atlantis-d7b4z-worker-0-pm4vf: 250 errors (5 min)
   All other nodes: 1-17 errors (5 min)
   ```

2. **That node hosts:**
   - cloistr-discovery
   - cloistr-drive
   - cloistr-blossom
   - cloistr-tasks-frontend
   - cloistr-testclient

3. **Discovery is doing relay health checks causing DNS load:**
   ```
   relay.nostr.jabber.ch - timeout
   nostr.cruncher.com - timeout
   umbrel.tail6ee2a9.ts.net - timeout
   *.onion addresses - no such host
   malformed URLs - parse errors
   ```

4. **Many lookups are for:**
   - Dead/non-existent relays
   - .onion addresses (can't resolve via regular DNS)
   - Malformed URLs from the relay list
   - Slow external domains

### Evidence

DNS server `10.0.4.10` is healthy - responds fine when queried directly. The issue is volume/contention from discovery's many concurrent DNS lookups.

Discovery logs show continuous relay checking:
```json
{"level":"WARN","msg":"relay check failed","url":"wss://relay.nostr.lighting","error":"dial tcp: lookup relay.nostr.lighting on 172.30.0.10:53: no such host"}
{"level":"WARN","msg":"relay check failed","url":"ws://oxz6arhmx7dxrgxveqccafilm4lbkaqegxvgrufw4tbzrdkg22bnt7id.onion","error":"no such host"}
{"level":"WARN","msg":"relay check failed","url":"wss://nostr.garden","error":"context deadline exceeded"}
```

## Impact

- ~250 DNS timeouts per 5 minutes from one node
- Intermittent connection failures for Drive and Blossom users
- WebSocket connections dropping through Cloudflare tunnel
- NIP-46 signer responses delayed

## Proposed Solutions

### Option 1: Rate Limit DNS Lookups (Recommended)

Add rate limiting to relay health checks:
- Limit concurrent DNS lookups (e.g., max 10 at a time)
- Add delay between checks (e.g., 100ms)
- Cache DNS failures with TTL to avoid repeated lookups for dead relays

### Option 2: DNS Result Caching

Cache DNS results in-process:
- Positive results: cache for 5 minutes
- Negative results (NXDOMAIN): cache for 1 hour
- Timeout results: cache for 15 minutes with backoff

### Option 3: Pre-filter Invalid URLs

Before doing DNS lookups, filter out:
- `.onion` addresses (can't resolve via regular DNS)
- `127.0.0.1` / localhost addresses
- Malformed URLs
- Known-dead relay patterns

### Option 4: Kubernetes Anti-Affinity (Workaround)

Add pod anti-affinity rules so discovery doesn't share nodes with Drive/Blossom. This doesn't fix the DNS load but isolates the impact.

```yaml
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app.kubernetes.io/name: cloistr-drive
          topologyKey: kubernetes.io/hostname
```

## Recommended Implementation

1. **Immediate:** Add URL pre-filtering to skip obviously invalid URLs
2. **Short-term:** Implement DNS result caching with negative caching
3. **Medium-term:** Add concurrency limiting for relay checks
4. **Optional:** Consider using a dedicated goroutine pool for health checks

## Metrics to Add

Consider adding Prometheus metrics:
- `discovery_dns_lookups_total{status="success|timeout|nxdomain"}`
- `discovery_relay_checks_total{result="healthy|unhealthy|unreachable"}`
- `discovery_relay_check_duration_seconds`

## Related Files

- Relay health check logic: (identify the file doing health checks)
- DNS lookup code: (identify where lookups happen)

## References

- CoreDNS error logs: `oc-atlantis logs -n openshift-dns -l dns.operator.openshift.io/daemonset-dns=default -c dns`
- Node with issues: `atlantis-d7b4z-worker-0-pm4vf`
