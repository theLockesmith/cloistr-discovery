# coldforge-discovery

Nostr Relay Discovery Service - Kind 30072 Relay Directory Entry publisher.

## Overview

coldforge-discovery monitors Nostr relays and publishes verified relay information as Kind 30072 (Relay Directory Entry) events. It aggregates data from multiple discovery sources:

- **NIP-11 Relay Information** - Direct metadata fetches from relays
- **NIP-65 Relay Lists** - Crawls user relay lists for discovery
- **NIP-66 Relay Monitor Events** - Consumes relay health data from monitors
- **Peer Discovery** - Learns relays from trusted discovery peers

## Live Instance

**Production:** https://discover.cloistr.xyz

```bash
# Query healthy relays
curl https://discover.cloistr.xyz/api/v1/relays?health=online

# Get relay details
curl https://discover.cloistr.xyz/api/v1/relay/wss%3A%2F%2Frelay.damus.io

# Health check
curl https://discover.cloistr.xyz/health
```

## Quick Start

```bash
# Clone the repository
git clone git@gitlab-coldforge:coldforge/coldforge-discovery.git
cd coldforge-discovery

# Run with Docker Compose (includes Dragonfly cache)
docker compose up -d

# Check health
curl http://localhost:8080/health

# Query relays
curl http://localhost:8080/api/v1/relays?health=online
```

## Architecture

```
                    ┌─────────────────────────────────────┐
                    │         coldforge-discovery         │
                    │                                     │
                    │  ┌─────────┐  ┌─────────┐  ┌─────┐ │
                    │  │  Relay  │  │Discovery│  │     │ │
                    │  │ Monitor │  │Coordinatr│ │Pubsh│ │
                    │  └────┬────┘  └────┬────┘  └──┬──┘ │
                    │       │            │          │     │
                    │       └────────────┼──────────┘     │
                    │                    │                │
                    │              ┌─────▼─────┐          │
                    │              │ Dragonfly │          │
                    │              │  (Cache)  │          │
                    │              └───────────┘          │
                    └─────────────────────────────────────┘
```

**Components:**
- **Relay Monitor** - Health checks, NIP-11 metadata fetches, latency tracking
- **Discovery Coordinator** - Aggregates relay URLs from NIP-65, NIP-66, peers
- **Publisher** - Creates and publishes Kind 30072 events to Nostr relays

## Event Kind

| Kind | Name | Description |
|------|------|-------------|
| 30072 | Relay Directory Entry | Verified relay info with health status, NIPs, location |

Published events include:
- Relay URL and health status (online/degraded/offline)
- Supported NIPs
- Geographic location (country)
- Content policies and moderation stance
- Software type and version
- Latency metrics

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check with worker status |
| `GET /metrics` | Prometheus metrics |
| `GET /api/v1/relays` | List relays with filters |
| `GET /api/v1/relay/{url}` | Get specific relay details |
| `GET /admin/dashboard` | Admin dashboard (auth required) |

### Query Parameters for /api/v1/relays

- `health` - Filter by status: online, degraded, offline
- `nips` - Filter by supported NIPs (comma-separated)
- `country` - Filter by country code
- `limit` - Maximum results (default: 100)
- `offset` - Pagination offset

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DISCOVERY_PORT` | 8080 | HTTP server port |
| `LOG_LEVEL` | info | debug, info, warn, error |
| `CACHE_URL` | redis://localhost:6379 | Dragonfly/Redis URL |
| `SEED_RELAYS` | (list) | Initial relays to monitor |
| `RELAY_CHECK_INTERVAL` | 300 | Seconds between health checks |
| `NIP11_TIMEOUT` | 10 | NIP-11 fetch timeout seconds |
| `PUBLISH_ENABLED` | false | Enable Kind 30072 publishing |
| `PUBLISH_RELAYS` | (list) | Relays to publish events to |
| `PUBLISH_INTERVAL` | 10 | Minutes between publish cycles |
| `NOSTR_PRIVATE_KEY` | - | Hex or nsec key for signing |
| `NIP65_CRAWL_ENABLED` | true | Enable NIP-65 discovery |
| `NIP66_ENABLED` | true | Consume NIP-66 events |
| `ADMIN_ENABLED` | true | Enable admin dashboard |
| `ADMIN_API_KEY` | - | API key for admin endpoints |

## Development

```bash
# Build
make build

# Test (357 tests across 11 packages)
make test

# Run locally
make run

# Lint
make lint

# Docker build
make docker-build
```

## Deployment

See [DEPLOYMENT.md](DEPLOYMENT.md) for Kubernetes deployment guide.

**Quick deploy via Atlas:**
```bash
atlas kube apply coldforge-discovery --kube-context atlantis
```

## Test Coverage

Comprehensive test suite covering:
- API handlers and routing
- Cache operations with TTL verification
- Relay monitor health checks
- Discovery coordinator deduplication
- Publisher event creation
- Admin authentication and handlers
- Health registry for background workers

## License

AGPL-3.0

## Links

- [Coldforge](https://coldforge.xyz)
- [Live Instance](https://discover.cloistr.xyz)
- [CLAUDE.md](CLAUDE.md) - Full documentation
