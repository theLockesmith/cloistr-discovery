# Discovery Roadmap

**Nostr relay discovery and health monitoring service**

**Live at:** `discover.cloistr.xyz`

---

## Phase 0: Foundation (Complete)

| Feature | Status |
|---------|--------|
| Relay health monitoring (online/degraded/offline) | ✓ |
| NIP support detection | ✓ |
| Latency monitoring | ✓ |
| NIP-65 discovery (user relay lists) | ✓ |
| NIP-66 consumer (other monitors) | ✓ |
| Peer relay discovery | ✓ |
| REST API with filtering | ✓ |
| Relay recommendations | ✓ |
| User relay preferences endpoint | ✓ |
| NIP-46 suitability scoring | ✓ |
| Relay comparison endpoint | ✓ |
| WoT integration | ✓ |
| Kind 30072 event publishing | ✓ |
| Admin dashboard | ✓ |
| 357 tests | ✓ |

---

## Phase 1: NIP-66 Publishing (Complete)

Become a relay monitor by publishing NIP-66 events.

### Kind 10166: Monitor Announcement

Declare our monitoring capabilities:

| Tag | Value | Notes |
|-----|-------|-------|
| `frequency` | TBD (e.g., 3600) | Seconds between 30166 publications |
| `c` | open, read, nip11 | Check types we perform |
| `timeout` | e.g., 5000 | Timeout in ms |

### Kind 30166: Relay Status Events

Publish relay health as addressable events:

| Tag | Source | Notes |
|-----|--------|-------|
| `d` | relay URL | Required - identifies the relay |
| `rtt-open` | latency_ms | Connection latency |
| `N` | supported_nips | One tag per NIP |
| `R` | auth_required, payment_required | Requirements |
| `n` | detect from URL | Network type (clearnet/tor/i2p) |
| Content | NIP-11 JSON | Optional relay info document |

### Implementation Tasks

- [x] Add monitor pubkey configuration (reuses existing publisher key)
- [x] Create NIP66Publisher component (`internal/publisher/nip66_publisher.go`)
- [x] Implement kind 10166 announcement (publish on startup, refresh every 24h)
- [x] Implement kind 30166 publishing (batch publish on configurable interval)
- [x] Add metrics for NIP-66 events published
- [x] Tests (6 new tests in `nip66_publisher_test.go`)

### Configuration

Enable with environment variables:

```bash
NIP66_PUBLISH_ENABLED=true
NIP66_PUBLISH_INTERVAL=3600  # seconds (default: 1 hour)
```

Requires `NOSTR_PRIVATE_KEY` and `PUBLISH_ENABLED=true` (shares key with kind 30072 publisher).

**Note:** The discovery service pubkey must be added to the relay's `ALLOWED_PUBKEYS` to bypass WoT PoW requirements. See `cloistr-config/base/relay/configmap-relay.yaml`.

---

## Phase 2: Enhanced Monitoring (Complete)

### Historical Health Trends

| Task | Description | Status |
|------|-------------|--------|
| Store health check history | Redis sorted sets, 30-day retention | ✓ |
| Calculate uptime percentages | Rolling 24h, 7d, 30d | ✓ |
| API endpoint for history | `/api/v1/relay/history/?url=` | ✓ |
| UI visualization | Uptime graph/sparkline | Pending (UI) |

### Relay Operator Verification

| Task | Description | Status |
|------|-------------|--------|
| Link relays to operator pubkeys | Via NIP-11 `pubkey` field | ✓ |
| Operator relays endpoint | `/api/v1/operators/{pubkey}/relays` | ✓ |
| Verify operator signatures | Optional signed relay claims | Not planned |
| Display operator info in UI | Profile, other relays they run | Pending (UI) |

### nostr-watch Federation

| Task | Description | Status |
|------|-------------|--------|
| Consume from nostr-watch monitors | Already doing via NIP-66 consumer | ✓ |
| Extract health data from NIP-66 | Store external monitor latency reports | ✓ |
| Coordinate with nostr-watch | Avoid duplicate monitoring | Not planned |
| Cross-reference health data | Multiple monitor consensus | Not planned |

---

## Status: Feature Complete

Backend is feature complete as of 2026-03-25. The service:

- Discovers relays from multiple sources (NIP-65, NIP-66, peers, hosted lists)
- Monitors health, latency, and NIP support
- Publishes status via NIP-66 (kind 30166) and NDP (kind 30072)
- Provides comprehensive query APIs with filtering, recommendations, and WoT integration
- Tracks 30-day historical uptime data

Remaining UI work (uptime graphs, operator profiles) belongs in a frontend project.

---

## Not In Scope

| Feature | Belongs In |
|---------|------------|
| NIP-0A contact CRDTs | cloistr-relay |
| Files & Storage | cloistr-drive, cloistr-blossom |
| Unified Platform schema | cloistr-platform |

---

**Last Updated:** 2026-03-25
