# Go Implementation

Go-based API Gateway rate limiter using `net/http` and `go-redis` for Redis communication. See the [root README](../README.md) for algorithm and architecture details.

> **Status:** Phase 3 complete. See [RESULTS.md](RESULTS.md) for benchmarks against the Node gateway, and [PLAN.md](../PLAN.md) for the full roadmap.

## Setup

### Prerequisites
- **Go** >= 1.22
- **Redis** running on localhost:6379 (or configure via env vars)

### Build & Run

```bash
go mod tidy
go run ./cmd/server
```

The server starts on port 3000. If Redis is unavailable, it runs in fail-open mode.

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `REDIS_HOST` | `127.0.0.1` | Redis host address |
| `REDIS_PORT` | `6379` | Redis port |
| `RATE_LIMIT_MAX` | `100` | Max requests per 5-minute rolling window |
| `TOKEN_BUCKET_CAPACITY` | `10` | Token bucket max tokens (burst limit) |
| `TOKEN_BUCKET_REFILL_RATE` | `1` | Token bucket refill rate (tokens/sec) |
| `LEAKY_BUCKET_CAPACITY` | `10` | Leaky bucket max size (queue depth) |
| `LEAKY_BUCKET_LEAK_RATE` | `1` | Leaky bucket drain rate (requests/sec) |
| `PORT` | `3000` | Server listening port |
| `LUA_DIR` | `../lua` | Directory holding the shared Lua scripts, relative to the working directory |
| `TRUST_PROXY` | unset | X-Forwarded-For trust: unset uses the socket address; a hop count (e.g. `1`) or IP/CIDR list (e.g. `10.0.0.0/8`, `loopback`) trusts those proxies |
| `PLAN_LIMITS` | `free=100,paid=1000` | Sliding-window base limit per plan tier (ARCHITECTURE.md §10). Pooled tenants get base × key slots |
| `KEY_CACHE_TTL` | `30` | Seconds a key lookup (including "unregistered") is cached per gateway; also the revocation delay |
| `KEY_CACHE_SIZE` | `10000` | Maximum cached key lookups per gateway (oldest evicted first) |

## Project Structure

```
cmd/
  server/
    main.go                    # Entry point, graceful shutdown
  keyctl/
    main.go                    # Registry provisioning CLI (tenants, keys)
internal/
  config/
    config.go                  # Central config (env vars + defaults)
  redis/
    client.go                  # go-redis connection, script loading, health probe
    decision.go                # Shared Decision type, script runner, fail-open
    resolve_key.go             # Registry lookup (resolve_key.lua), 5ms budget
    sliding_window.go          # Lua execution wrapper, fail-open timeout
    token_bucket.go            # Token bucket Lua execution wrapper
    leaky_bucket.go            # Leaky bucket Lua execution wrapper
  registry/
    keys.go                    # Key format, generation, SHA-256 hashing
    identity.go                # Plan tiers, tenant → identity + limit
    resolver.go                # Per-gateway TTL cache, negative caching, miss dedupe
    store.go                   # Registry writes (used by keyctl only)
  middleware/
    limiter.go                 # Shared deps + the strict-mode middleware shape
    identifier.go              # Presented API key, trusted client IP
    headers.go                 # Sets X-RateLimit-* and Retry-After headers
    strict.go                  # HTTP middleware — sync Redis per request
    loose.go                   # HTTP middleware — local map, async Redis
    token_bucket.go            # HTTP middleware — token bucket strict mode
    leaky_bucket.go            # HTTP middleware — leaky bucket strict mode
  buffer/
    heap.go                    # Local map buffer + flush goroutine
  routes/
    routes.go                  # Mux registration + shared JSON helpers
    strict.go                  # Strict-mode demo endpoints
    loose.go                   # Loose-mode demo endpoints
    token_bucket.go            # Token bucket demo endpoints
    leaky_bucket.go            # Leaky bucket demo endpoints
    health.go                  # Health check + stats endpoints
```

The Lua scripts are **not** inside this module. They live in the repo-root
[`lua/`](../lua) directory, shared verbatim with the Node implementation so
both gateways enforce byte-identical logic. `//go:embed` cannot reach outside
the module directory, so they are read at startup from `LUA_DIR` instead — a
missing script is a fatal boot error, not a fail-open case.

## API Endpoints

### Rate-Limited

| Endpoint | Mode | Description |
|---|---|---|
| `GET /api/strict/resource` | Strict | Synchronous Redis check per request (sliding window) |
| `GET /api/loose/resource` | Loose | Local map check, async Redis sync (sliding window) |
| `GET /api/loose/burst` | Loose | Burst traffic simulation endpoint (sliding window) |
| `GET /api/token-bucket/resource` | Strict | Token bucket — burst-tolerant rate limiting |
| `GET /api/leaky-bucket/resource` | Strict | Leaky bucket — steady-rate enforcement |

### Open (No Rate Limiting)

| Endpoint | Description |
|---|---|
| `GET /api/open/health` | Health check — shows Redis connection status |
| `GET /api/open/stats` | Buffer stats — shows local counts per identifier |

## Implementation Notes

- **Lua scripts:** The same `sliding_window.lua`, `token_bucket.lua`, and `leaky_bucket.lua` atomic scripts used by the Node implementation will be reused. Sliding window strict and loose modes call their script with different `batch_count` values. Token bucket and leaky bucket are strict-only.
- **Concurrency:** Loose mode uses a `sync.RWMutex`-guarded map with a background flush goroutine (replaces Node's `setInterval` pattern). Per-entry counters are atomics, so the request path only ever takes a read lock.
- **Loose-mode write-back protocol:** each identifier has an *allowance*: how many more requests this instance may admit without asking Redis. Every flush records its batch in the Lua script's `writeback` mode (always recorded, since those requests were already served). It then resets the allowance to Redis's cluster-wide `remaining`, applied as an atomic delta so requests admitted during the call aren't lost. Exhausted identifiers refresh every 5 s, and rejected requests are never counted. See ARCHITECTURE.md §6. Node implements the same protocol.
- **Fail-open:** Redis calls use `context.WithTimeout` (5ms budget) — if Redis is down or slow, requests pass through.
- **Idiomatic Go:** Uses `net/http` middleware chaining, struct-based dependency injection, and goroutines for background work.

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/redis/go-redis/v9` | Redis client with Lua `EVALSHA`/`EVAL` support, timeouts, reconnect |
| Standard library (`net/http`, `sync`, `context`) | HTTP server, concurrency, cancellation |

## Testing

```bash
go vet ./...
golangci-lint run ./...      # default linter set; currently 0 issues
go test -race ./...
```

Tests cover pure logic only — identifier precedence, config defaults, Lua
reply parsing, and buffer behaviour under concurrency — so no Redis is needed.

For end-to-end and load testing, the Node harness drives either gateway (same
client, same scenarios, one gateway at a time on port 3000):

```bash
redis-cli FLUSHALL
LUA_DIR=../lua go run ./cmd/server
cd ../node && TEST_URL=http://localhost:3000 node test/load-test.js
```

Results and methodology: [RESULTS.md](RESULTS.md).
