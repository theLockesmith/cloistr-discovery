# CLAUDE.md - coldforge-discovery

**Nostr Discovery Protocol (NDP) implementation - relay discovery, content routing, and activity tracking**

**Status:** Functional - Core services wired up, tested locally with Docker Compose

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
├── cmd/discovery/          # Main entry point
├── internal/
│   ├── api/                # HTTP API handlers
│   ├── cache/              # Dragonfly/Redis caching
│   ├── config/             # Configuration loading
│   ├── relay/              # Relay monitoring (Kind 30069)
│   ├── inventory/          # Content routing index (Kind 30066)
│   └── activity/           # Activity tracking (Kind 30067)
├── configs/                # Configuration templates
├── tests/                  # Integration tests
├── Dockerfile              # Multi-stage build
└── docker-compose.yml      # Local development
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
| `PUBLISH_RELAY` | relay.cloistr.xyz | Relay for publishing events |

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check |
| `GET /metrics` | Prometheus metrics |
| `GET /api/v1/relays` | List relays (filter by health, nips, location) |
| `GET /api/v1/pubkey/{pk}/relays` | Find relays with pubkey's content |
| `GET /api/v1/activity/streams` | List active streams |

## NDP Event Kinds

| Kind | Name | Purpose |
|------|------|---------|
| 30066 | Relay Inventory | Which pubkeys a relay has |
| 30067 | Activity Announcement | Real-time user activity |
| 30068 | Discovery Query | Request discovery info |
| 30069 | Relay Directory Entry | Verified relay info + health |

## Completed

- [x] Wire up relay/inventory/activity goroutines to main.go
- [x] Add go.sum via `go mod tidy`
- [x] Write unit tests for cache layer (12 tests)
- [x] Write unit tests for API handlers (11 tests)
- [x] Test locally with Docker Compose
- [x] Fix critical issues from code review (event loop breaks, context management)

## Next Steps

1. Deploy Dragonfly to Atlantis
2. Create Atlas role for Kubernetes deployment
3. Integration test with live relays
4. Add health check verification for background services
5. Implement exponential backoff for relay reconnections
6. Add TTL expiration tests

## See Also

- Service Documentation: `~/claude/coldforge/services/discovery/CLAUDE.md`
- NIP Draft: `~/claude/coldforge/research/nip-draft-discovery-protocol.md`
- Architecture: `~/claude/coldforge/research/architecture-discovery-cache-relay.md`
- Coldforge Overview: `~/claude/coldforge/CLAUDE.md`
