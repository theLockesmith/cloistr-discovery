# CLAUDE.md - coldforge-discovery

**Nostr Discovery Protocol (NDP) implementation - Kind 30072 Relay Directory Entry**

**NDP Focus:** Kind 30072 only (Relay Directory Entry). Other kinds have been deferred/dropped.

**Status:** Deployed - Live on Atlantis at `https://discover.cloistr.xyz`

## REQUIRED READING (Before ANY Action)

**Claude MUST read this file at the start of every session:**
- `~/claude/coldforge/cloistr/CLAUDE.md` - Cloistr project rules (contains further required reading)

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
| `GET /api/v1/relays` | List relays (filter by health, nips, location, etc.; supports `limit`/`offset` pagination) |
| `GET /api/v1/relay/?url={url}` | Single relay metadata (full NIP-11 info, health, policies) |
| `GET /api/v1/relays/recommend` | Relay recommendations (scored by health, latency, NIPs, region) |
| `GET /api/v1/relays/compare` | Side-by-side relay comparison (NIPs, latency, features, policies) |
| `GET /api/v1/relay/reviews` | Relay reviews with WoT-weighted ratings (Kind 30078) |
| `GET /api/v1/relay-prefs/{pubkey}` | User's relay preferences (cloistr-relays or NIP-65 fallback, 5min cache) |
| `GET /api/v1/users/{pubkey}/relays` | User's NIP-65 relay list with health enrichment (live fetch, 5min cache) |
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
- [x] Grafana dashboard for relay network analytics (`deploy/grafana/dashboard.json`)
- [x] Security: JSON body size limits on admin endpoints (DoS prevention)
- [x] Security: Input validation for relay URLs and pubkeys
- [x] Scaling: Batch cache retrieval for relay entries (pipelined Redis)
- [x] Scaling: Pagination support on `/api/v1/relays` (limit/offset)
- [x] Cleanup: Removed dead `publishEvent()` code
- [x] NIP-65 user relay list endpoint (`GET /api/v1/users/{pubkey}/relays`) with health enrichment
- [x] Single relay metadata endpoint (`GET /api/v1/relay/?url={url}`) with full NIP-11 data
- [x] Relay preferences endpoint (`GET /api/v1/relay-prefs/{pubkey}`) for cloistr-common library
- [x] Relay recommendations endpoint (`GET /api/v1/relays/recommend`) with scoring by health, latency, NIPs, region
- [x] Relay comparison endpoint (`GET /api/v1/relays/compare`) with feature summaries and NIP coverage
- [x] WoT-enhanced recommendations (`pubkey` param) with NIP-02 follows and network presence scoring
- [x] Relay reviews endpoint (`GET /api/v1/relay/reviews`) with Kind 30078 events and WoT weighting

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

**Frontend (external project):**
1. Improve UI filtering (add NIP filter dropdowns, search, sorting)
2. Add relay submission form to public UI
3. Integrate with user relay list endpoint for personalized relay recommendations

**Backend:**
1. ✅ **Relay recommendations endpoint** (`GET /api/v1/relays/recommend`) - COMPLETE
   - Scores relays by: health, latency, NIP support, region match
   - Input: `nips` (comma-separated), `region`, `exclude_auth`, `exclude_payment`, `limit`
   - Output: ranked list with score breakdown and reasons

2. ✅ **Relay comparison endpoint** (`GET /api/v1/relays/compare`) - COMPLETE
   - Side-by-side comparison of 2-10 relays
   - Input: `urls` (comma-separated relay URLs)
   - Output: full relay data, feature summary, common NIPs, NIP coverage matrix, fastest relay

3. ✅ **WoT-enhanced relay recommendations** - COMPLETE
   - Added `pubkey` param to `/api/v1/relays/recommend` for WoT-based scoring
   - Fetches NIP-02 follows, queries their NIP-65 relay lists
   - Scores relays by network presence (how many follows use them)
   - Network bonuses: +5pts/follow, +30pts if >10% use relay, +50pts if >25%

4. ✅ **Trusted relay reviews/ratings** (Kind 30078) - COMPLETE
   - `GET /api/v1/relay/reviews?url={url}&pubkey={pubkey}`
   - Fetches Kind 30078 events with `relay-review:{url}` d-tag
   - Returns ratings, comments, average rating
   - WoT weighting: marks reviews from followed users, computes WoT-weighted average

**Relay Preferences Integration:** (See `~/claude/coldforge/cloistr/architecture/relay-preferences.md`)
- ✅ Phase 1: `cloistr-common` library created
- ✅ Phase 2: Discovery API endpoint (`/api/v1/relay-prefs/{pubkey}`)
- ✅ Phase 3: Integrate relay prefs into Cloistr Go services
  - cloistr-drive: Frontend JS integration (relayprefs.js)
  - cloistr-calendar: Backend Go with relayprefs.Client
  - cloistr-chat: Backend Go with inline RelayPrefs (Go 1.21 compat)
  - cloistr-documents: Backend Go with relayprefs.Client
  - Atlas configs updated for calendar/documents
- Phase 4: Build unified relay settings UI component (JS API ready, needs UI modal)

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
