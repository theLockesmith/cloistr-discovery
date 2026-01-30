# coldforge-discovery

Nostr Discovery Protocol (NDP) implementation for Coldforge.

## Overview

coldforge-discovery implements the Nostr Discovery Protocol (NDP), providing three key capabilities:

1. **Relay Discovery** (Kind 30069) - Monitor and catalog Nostr relays
2. **Content Routing** (Kind 30066) - Index which relays have which pubkeys' content
3. **Activity Discovery** (Kind 30067) - Track real-time user activities

## Why NDP?

The Nostr ecosystem lacks standardized discovery mechanisms:

- **Users don't know which relays to use** - There's no way to find healthy, well-maintained relays
- **Clients waste resources** - Without routing info, clients must query many relays to find content
- **No real-time activity** - No standard way to announce "I'm streaming" or "I'm online"

NDP solves these problems with a federated discovery layer that preserves Nostr's decentralized principles.

## Quick Start

```bash
# Clone the repository
git clone git@gitlab-coldforge:coldforge/coldforge-discovery.git
cd coldforge-discovery

# Run with Docker Compose (includes Dragonfly)
docker compose up -d

# Check health
curl http://localhost:8080/health

# Query relays
curl http://localhost:8080/api/v1/relays?health=online

# Find where a pubkey's content is
curl http://localhost:8080/api/v1/pubkey/<hex-pubkey>/relays

# List active streams
curl http://localhost:8080/api/v1/activity/streams
```

## Architecture

```
                    ┌─────────────────────────────────────┐
                    │         coldforge-discovery         │
                    │                                     │
                    │  ┌─────────┐  ┌─────────┐  ┌─────┐ │
                    │  │ Relay   │  │Inventory│  │Activ│ │
                    │  │ Monitor │  │ Indexer │  │Track│ │
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

## Event Kinds

| Kind | Name | Description |
|------|------|-------------|
| 30066 | Relay Inventory | Published by relays to announce which pubkeys they have content for |
| 30067 | Activity Announcement | Published by users to announce real-time activities |
| 30068 | Discovery Query | Used by clients to request discovery information |
| 30069 | Relay Directory Entry | Published by discovery services with verified relay info |

## Configuration

See [CLAUDE.md](CLAUDE.md) for full configuration options.

## Development

```bash
# Build
make build

# Test
make test

# Run locally
make run

# Lint
make lint
```

## License

AGPL-3.0

## Links

- [Coldforge](https://coldforge.xyz)
- [NDP Draft](research/nip-draft-discovery-protocol.md)
