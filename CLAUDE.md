# CLAUDE.md - coldforge-discovery

**Nostr Discovery Protocol (NDP) implementation - relay discovery, content routing, and activity tracking**

**Status:** Live - Publishing to cloistr, full NDP protocol implemented, ready for production deployment

## Documentation

Full documentation: `~/claude/coldforge/services/discovery/CLAUDE.md`
NIP Draft: `~/claude/coldforge/research/nip-draft-discovery-protocol.md`
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
│   ├── relay/              # Relay monitoring via NIP-11
│   ├── inventory/          # Content routing index (Kind 30066)
│   ├── activity/           # Activity tracking (Kind 30067)
│   ├── discovery/          # Relay discovery sources (NIP-65, NIP-66, hosted lists, peers)
│   ├── publisher/          # Publishes Kind 30069 events to Nostr relays
│   ├── query/              # Handles Kind 30068 discovery queries
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
| `INVENTORY_TTL` | 12 | Hours before inventory expires |
| `ACTIVITY_TTL` | 15 | Minutes before activity expires |
| `PUBLISH_ENABLED` | false | Enable publishing Kind 30069 events |
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
| `GET /api/v1/relays` | List relays (filter by health, nips, location) |
| `GET /api/v1/pubkey/{pk}/relays` | Find relays with pubkey's content |
| `GET /api/v1/activity/streams` | List active streams |
| `GET /admin/dashboard` | Admin dashboard (requires auth) |
| `POST /admin/relays/submit` | Submit relay for discovery |
| `POST /admin/relays/whitelist` | Manage relay whitelist |
| `POST /admin/relays/blacklist` | Manage relay blacklist |

## NDP Event Kinds

| Kind | Name | Subscribe | Publish | Purpose |
|------|------|-----------|---------|---------|
| 30066 | Relay Inventory | Yes | - | Which pubkeys a relay has |
| 30067 | Activity Announcement | Yes | Yes (responses) | Real-time user activity |
| 30068 | Discovery Query | Yes | Yes (responses) | Request/response discovery |
| 30069 | Relay Directory Entry | - | Yes | Verified relay info + health |

## Completed

- [x] Wire up relay/inventory/activity goroutines to main.go
- [x] Add go.sum via `go mod tidy`
- [x] Write unit tests for cache layer (12 tests)
- [x] Write unit tests for API handlers (11 tests)
- [x] Test locally with Docker Compose
- [x] Fix critical issues from code review (event loop breaks, context management)
- [x] Configure Atlas role for Kubernetes deployment
- [x] Discovery sources: NIP-65 crawling, NIP-66 consumption, peer discovery, hosted lists
- [x] Admin interface with auth middleware and dashboard
- [x] Kind 30069 event publishing (publisher with per-relay connections)
- [x] Kind 30068 discovery query handler (request/response pattern)
- [x] NIP-42 auth for publishing to authenticated relays (tested with cloistr)
- [x] Test client CLI (`cmd/testclient`) with keygen, publish, query, listen commands
- [x] Local test environment (nostr-rs-relay + dragonfly in Docker Compose)
- [x] Integration testing against wss://relay.cloistr.xyz (NIP-42 auth working)
- [x] NIP draft updated with federation section

## Known Issues

- **Relay discovery dedup persists across restarts**: `discovery:seen` set in Dragonfly has no TTL. After container restart, NIP-65 discovered relays are treated as "already seen" and don't get forwarded to the relay monitor. Fix: load seen relays into monitor on startup.
- **`/api/v1/relays` returns empty without filters**: The handler requires `nips` or `location` query params to populate the relay list. Missing fallback to `GetAllRelayURLs()`.

## Next Steps

1. **Fix relay discovery feedback loop** - Load seen relays into monitor on startup, add TTL to seen set
2. **Fix `/api/v1/relays` endpoint** - Return all relays when no filters specified
3. **Deploy to Atlantis** - Atlas role is ready, run `atlas kube apply`
4. **Monitoring and health check strategy** - Evaluate options (Prometheus/Grafana vs alternatives)
5. Implement exponential backoff for relay reconnections
6. Add health check verification for background goroutines
7. Add TTL expiration tests
8. Expand test coverage (10 packages have no tests)
9. Consider HorizontalPodAutoscaler for automatic scaling

## See Also

- Service Documentation: `~/claude/coldforge/services/discovery/CLAUDE.md`
- NIP Draft: `~/claude/coldforge/research/nip-draft-discovery-protocol.md`
- Architecture: `~/claude/coldforge/research/architecture-discovery-cache-relay.md`
- Coldforge Overview: `~/claude/coldforge/CLAUDE.md`
