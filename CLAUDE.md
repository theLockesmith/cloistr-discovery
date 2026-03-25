# CLAUDE.md - cloistr-discovery

**Nostr Discovery Protocol - Kind 30072 Relay Directory (Go)**

**Status:** Production | **Domain:** discover.cloistr.xyz

## Required Reading

| Document | Purpose |
|----------|---------|
| `~/claude/coldforge/cloistr/CLAUDE.md` | Cloistr project rules |
| [ROADMAP.md](ROADMAP.md) | Discovery-specific roadmap |
| [DEPLOYMENT.md](DEPLOYMENT.md) | Deployment guide |

## Autonomous Work Mode

**Work autonomously. Do NOT stop to ask what to do next.**

- Keep working until task complete or genuine blocker
- Make reasonable decisions - don't ask permission on obvious choices
- If tests fail, fix them. Use reviewer agent. Keep going.

## Agent Usage

| When | Agent |
|------|-------|
| Starting work / need context | `explore` |
| After significant code changes | `reviewer` |
| Writing/running tests | `test-writer` / `tester` |
| Security-sensitive code | `security` |

## Quick Commands

```bash
make docker-run    # Run locally
make test          # Run tests
make build         # Build binary
make logs          # View logs
```

## Project Structure

```
cmd/
  discovery/        Entry point
  testclient/       CLI testing tool
internal/
  api/              HTTP handlers
  cache/            Dragonfly/Redis
  discovery/        NIP-65, NIP-66, peer sources
  publisher/        Kind 30072 event publishing
  admin/            Admin dashboard
```

## Key Features

| Feature | Status |
|---------|--------|
| Kind 30072 relay directory | Done |
| NIP-11 relay health checks | Done |
| NIP-65/66 discovery | Done |
| Relay preferences API | Done |
| Admin dashboard | Done |
| 357 tests | Done |

## Core Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/relays` | List relays (paginated) |
| GET | `/api/v1/relay/?url=` | Single relay metadata |
| GET | `/api/v1/relay/history/?url=` | Relay uptime history (24h/7d/30d) |
| GET | `/api/v1/relay-prefs/{pubkey}` | User relay preferences |
| GET | `/api/v1/users/{pubkey}/relays` | User's NIP-65 list |
| GET | `/api/v1/operators/{pubkey}/relays` | Relays operated by pubkey |
| GET | `/api/v1/relays/recommend` | Relay recommendations |

## Container Registry (CRITICAL)

**GitLab builds images. Harbor is NOT involved.**

| Registry | Purpose |
|----------|---------|
| `registry.coldforge.xyz` | CI/CD builds push here |
| `oci.coldforge.xyz` (Harbor) | External images only |

## Deployment

```bash
atlas kube apply coldforge-discovery --kube-context atlantis
```

- **Namespace:** coldforge-discovery
- **Cache:** Dragonfly cluster (authenticated)
- **Tunnel:** Cloudflare via cloistr-tunnel

## Scaling Note

Single replica recommended - background workers (monitor, crawler, publisher) don't support HPA without leader election.

## See Also

- [NIP-66](https://github.com/nostr-protocol/nips/blob/master/66.md)
- Atlas Role: `~/Atlas/roles/kube/coldforge-discovery/`

---

**Last Updated:** 2026-03-25
