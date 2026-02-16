# CLAUDE.md - coldforge-discovery

**Nostr Discovery Protocol (NDP) implementation - Kind 30072 Relay Directory Entry**

**NDP Focus:** Kind 30072 only (Relay Directory Entry). Other kinds have been deferred/dropped.

**Status:** Deployed - Live on Atlantis at `https://discover.cloistr.xyz`

## Documentation

- [README.md](README.md) - Project overview
- [DEPLOYMENT.md](DEPLOYMENT.md) - Full deployment guide
- [deploy/QUICK-START.md](deploy/QUICK-START.md) - Quick deployment reference
- [deploy/ATLAS-ROLE-SUMMARY.md](deploy/ATLAS-ROLE-SUMMARY.md) - Atlas role details

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
- [x] Docker Compose local development environment
- [x] Deploy to Atlantis (Atlas role, Harbor image, Cloudflare Tunnel, Dragonfly auth, NetworkPolicy)
- [x] Production verified: 900+ relays tracked, 400+ online, publishing to 2 relays
- [x] **NDP stripped to Kind 30072 only** - removed inventory, activity, query, annotation packages
- [x] Updated API: removed `/api/v1/pubkey/` and `/api/v1/activity/` endpoints
- [x] Relay monitor is now the primary data source (NIP-11 metadata + health checks)
- [x] Prometheus/Grafana deployed in cluster (monitoring infrastructure ready)
- [x] Service operational metrics (publisher, NIP-65, NIP-66, cache, health checks)
- [x] Relay network aggregate metrics (by NIP, country, content policy, moderation, software, latency)
- [x] Exponential backoff for NIP-66 relay reconnections
- [x] Health check verification for background goroutines (internal/health package)
- [x] TTL expiration tests (8 tests in cache package)
- [x] Comprehensive test suite (357 test runs across 11 packages: api, admin, cache, config, discovery, health, metrics, publisher, relay, backoff)

## Production Deployment

- **URL:** `https://discover.cloistr.xyz`
- **Cluster:** Atlantis (OpenShift)
- **Namespace:** `coldforge-discovery`
- **Image:** Built and pushed by CI/CD pipeline
- **Cache:** Dragonfly cluster at `dragonfly.dragonfly.svc.cluster.local:6379` (authenticated)
- **Tunnel:** Cloudflare Tunnel via `cloistr-tunnel` role
- **Atlas role:** `~/Atlas/roles/kube/coldforge-discovery/`
- **Secrets:** Ansible Vault (`vars/vault.yml`) - NOSTR_PRIVATE_KEY, ADMIN_API_KEY, dragonfly_password
- **Public key:** `532aceee51a63b3a7a242aca4e0b79f57352046b8743d0ea1833d135d2034ce6`

Deploy: `atlas kube apply coldforge-discovery --kube-context atlantis`

## Next Steps

1. Improve UI filtering (add NIP filter dropdowns, search, sorting)
2. Add relay submission form to public UI
3. Grafana dashboard for relay network analytics

## Scaling Considerations (HPA)

**Current architecture does NOT support horizontal scaling** without code changes.

Background workers that would run on ALL replicas:
- **Relay Monitor** - Duplicate health checks to same relays
- **NIP-65 Crawler** - Duplicate crawls, wasted resources
- **NIP-66 Consumer** - Multiple subscriptions, duplicate events
- **Publisher** - **Critical:** Would publish duplicate Kind 30072 events

**Requirements for safe HPA:**
1. **Leader election** - Only one replica runs background workers (use kubernetes-client or Redis lock)
2. **Work distribution** - Partition relay URLs across replicas using consistent hashing
3. **Deduplication** - Use Redis SETNX for distributed locking on publish operations

**Recommendation:** Keep single replica until usage justifies the complexity. Current deployment handles 900+ relays with minimal resources (100m CPU, 128Mi memory). Scale vertically first if needed.

## Related Links

- **Live Instance:** https://discover.cloistr.xyz
- **Atlas Role:** `~/Atlas/roles/kube/coldforge-discovery/`
- **CI/CD:** GitLab pipeline builds and pushes images on merge to main
