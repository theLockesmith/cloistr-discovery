# CLAUDE.md - coldforge-discovery

**Nostr Discovery Protocol (NDP) implementation - Kind 30072 Relay Directory Entry**

**NDP Focus:** Kind 30072 only (Relay Directory Entry). Other kinds have been deferred/dropped.

**Status:** Deployed - Live on Atlantis at `https://discovery.cloistr.xyz`

## Documentation

Full documentation: `~/claude/coldforge/services/discovery/CLAUDE.md`
NIP Draft (minimal): `~/claude/coldforge/research/nip-draft-ndp-minimal.md`
NIP Draft (full): `~/claude/coldforge/research/nip-draft-discovery-protocol.md`
Architecture: `~/claude/coldforge/research/architecture-discovery-cache-relay.md`
Coldforge overview: `~/claude/coldforge/CLAUDE.md`

## Autonomous Work Mode (CRITICAL)

**Work autonomously. Do NOT stop to ask what to do next.**

- Keep working until the task is complete or you hit a genuine blocker
- Use the "Next Steps" section in the service docs to know what to work on
- Make reasonable decisions - don't ask for permission on obvious choices
- Only stop to ask if there's a true ambiguity that affects architecture
- If tests fail, fix them. If code needs review, use the reviewer agent. Keep going.
- Update this CLAUDE.md and the service docs as you make progress

## Agent Usage (IMPORTANT)

**Use agents proactively. Do not wait for explicit instructions.**

| When... | Use agent... |
|---------|-------------|
| Starting new work or need context | `explore` |
| Need to research NIPs or protocols | `explore` |
| Writing or modifying code | `reviewer` after significant changes |
| Writing tests | `test-writer` |
| Running tests | `tester` |
| Investigating bugs | `debugger` |
| Updating documentation | `documenter` |
| Creating Dockerfiles | `docker` |
| Setting up Kubernetes deployment | `atlas-deploy` |
| Security-sensitive code (auth, crypto) | `security` |

## Workflow

1. **Before coding:** Use `explore` to read the service documentation and NIP draft
2. **While coding:** Write code, then use `reviewer` to check it
3. **Testing:** Use `test-writer` to create tests, `tester` to run them
4. **Before committing:** Use `security` for auth/crypto code
5. **Deployment:** Use `docker` for containers, `atlas-deploy` for Kubernetes

## Quick Commands

```bash
# Run locally with Docker
make docker-run

# Run tests
make test

# Build binary
make build

# View logs
make logs

# Stop services
make stop
```

## Project Structure

```
coldforge-discovery/
├── cmd/
│   ├── discovery/          # Main entry point
│   └── testclient/         # CLI tool for integration testing
├── internal/
│   ├── api/                # HTTP API handlers (+ tests)
│   ├── cache/              # Dragonfly/Redis caching (+ tests)
│   ├── config/             # Configuration loading
│   ├── relay/              # Relay monitoring via NIP-11 (fallback data source)
│   ├── discovery/          # Relay discovery sources (NIP-65, NIP-66, hosted lists, peers)
│   ├── publisher/          # Publishes Kind 30072 events to Nostr relays
│   └── admin/              # Admin dashboard + auth middleware
├── configs/                # Configuration templates (relay config)
├── Dockerfile              # Multi-stage build
└── docker-compose.yml      # Local dev (discovery + dragonfly + nostr-rs-relay)
```

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DISCOVERY_PORT` | 8080 | HTTP server port |
| `LOG_LEVEL` | info | debug, info, warn, error |
| `CACHE_URL` | redis://localhost:6379 | Dragonfly/Redis URL |
| `SEED_RELAYS` | damus,nos.lol,nostr.band | Comma-separated relay URLs |
| `RELAY_CHECK_INTERVAL` | 300 | Seconds between health checks |
| `NIP11_TIMEOUT` | 10 | NIP-11 fetch timeout seconds |
| `PUBLISH_ENABLED` | false | Enable publishing Kind 30072 events |
| `PUBLISH_RELAYS` | relay.cloistr.xyz | Comma-separated relays to publish to |
| `PUBLISH_INTERVAL` | 10 | Minutes between publish cycles |
| `NOSTR_PRIVATE_KEY` | - | Hex or nsec key for signing events |
| `NIP65_CRAWL_ENABLED` | true | Enable NIP-65 relay discovery |
| `NIP65_CRAWL_INTERVAL` | 30 | Minutes between NIP-65 crawls |
| `NIP66_ENABLED` | true | Consume NIP-66 relay monitor events |
| `PEER_DISCOVERY_ENABLED` | true | Discover relays from trusted peers |
| `ADMIN_ENABLED` | true | Enable admin dashboard |
| `ADMIN_API_KEY` | - | API key for admin endpoints |

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check |
| `GET /metrics` | Prometheus metrics |
| `GET /api/v1/relays` | List relays (filter by health, nips, location, moderation, etc.) |
| `GET /admin/dashboard` | Admin dashboard (requires auth) |
| `POST /admin/relays/submit` | Submit relay for discovery |
| `POST /admin/relays/whitelist` | Manage relay whitelist |
| `POST /admin/relays/blacklist` | Manage relay blacklist |

## NDP Event Kinds

**Current Proposal: Kind 30072 only.** Other kinds deferred/dropped from initial proposal.

| Kind | Name | Status | Purpose |
|------|------|--------|---------|
| 30072 | Relay Directory Entry | **ACTIVE** | Verified relay info + health (published) |
| 30069 | Relay Inventory | DEFERRED | Which pubkeys a relay has |
| 30070 | Activity Announcement | DEFERRED | Real-time user activity |
| 30071 | Discovery Query | DROPPED | Request/response discovery |
| 30073 | Relay Annotation | DROPPED | Community-curated topics/atmosphere |

## Completed

- [x] Core relay monitoring with NIP-11 fetches and health checks
- [x] Kind 30072 event publishing (publisher with per-relay connections)
- [x] NIP-42 auth for publishing to authenticated relays (tested with cloistr)
- [x] Discovery sources: NIP-65 crawling, NIP-66 consumption, peer discovery, hosted lists
- [x] Admin interface with auth middleware and dashboard
- [x] Unit tests for cache layer (12 tests) and API handlers (7 tests)
- [x] Docker Compose local development environment
- [x] Deploy to Atlantis (Atlas role, Harbor image, Cloudflare Tunnel, Dragonfly auth, NetworkPolicy)
- [x] Production verified: 900+ relays tracked, 400+ online, publishing to 2 relays
- [x] **NDP stripped to Kind 30072 only** - removed inventory, activity, query, annotation packages
- [x] Updated API: removed `/api/v1/pubkey/` and `/api/v1/activity/` endpoints
- [x] Relay monitor is now the primary data source (NIP-11 metadata + health checks)
- [x] Prometheus/Grafana deployed in cluster (monitoring infrastructure ready)

## Production Deployment

- **URL:** `https://discovery.cloistr.xyz`
- **Cluster:** Atlantis (OpenShift)
- **Namespace:** `coldforge-discovery`
- **Image:** `oci.coldforge.xyz/coldforge/coldforge-discovery:latest` (Harbor)
- **Cache:** Dragonfly cluster at `dragonfly.dragonfly.svc.cluster.local:6379` (authenticated)
- **Tunnel:** Cloudflare Tunnel via `cloistr-tunnel` role
- **Atlas role:** `~/Atlas/roles/kube/coldforge-discovery/`
- **Secrets:** Ansible Vault (`vars/vault.yml`) - NOSTR_PRIVATE_KEY, ADMIN_API_KEY, dragonfly_password
- **Public key:** `532aceee51a63b3a7a242aca4e0b79f57352046b8743d0ea1833d135d2034ce6`

Deploy: `atlas kube apply coldforge-discovery --kube-context atlantis`

## Next Steps

1. **Add service operational metrics** - publish success/failure, NIP-65 crawl stats, NIP-66 events consumed, cache operations, health check duration/success rate
2. **Add relay network aggregate metrics** - relays by NIP support, by country, by content policy, by health status, average response latency, NIP-11 fetch success rate
3. Implement exponential backoff for relay reconnections
4. Add health check verification for background goroutines
5. Add TTL expiration tests
6. Expand test coverage (10 packages have no tests)
7. Consider HorizontalPodAutoscaler for automatic scaling

## See Also

- Service Documentation: `~/claude/coldforge/services/discovery/CLAUDE.md`
- NIP Draft: `~/claude/coldforge/research/nip-draft-discovery-protocol.md`
- Architecture: `~/claude/coldforge/research/architecture-discovery-cache-relay.md`
- Coldforge Overview: `~/claude/coldforge/CLAUDE.md`
