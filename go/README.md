# Go Implementation

Go-based API Gateway rate limiter using `net/http` and `go-redis` for Redis communication. See the [root README](../README.md) for algorithm and architecture details.

> **Status:** Phase 3 — Not yet started. See [PLAN.md](../PLAN.md) for the full roadmap.

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

## Planned Project Structure

```
cmd/
  server/
    main.go                    # Entry point, graceful shutdown
internal/
  config/
    config.go                  # Central config (env vars + defaults)
  redis/
    client.go                  # go-redis connection + Lua script loading
    sliding_window.go          # Lua execution wrapper, fail-open timeout
    token_bucket.go            # Token bucket Lua execution wrapper
    leaky_bucket.go            # Leaky bucket Lua execution wrapper
  middleware/
    identifier.go              # Extracts user ID from request (API key / IP)
    headers.go                 # Sets X-RateLimit-* and Retry-After headers
    strict.go                  # HTTP middleware — sync Redis per request
    loose.go                   # HTTP middleware — local map, async Redis
    token_bucket.go            # HTTP middleware — token bucket strict mode
    leaky_bucket.go            # HTTP middleware — leaky bucket strict mode
  buffer/
    heap.go                    # Local map buffer + flush goroutine
  routes/
    strict.go                  # Strict-mode demo endpoints
    loose.go                   # Loose-mode demo endpoints
    token_bucket.go            # Token bucket demo endpoints
    leaky_bucket.go            # Leaky bucket demo endpoints
    health.go                  # Health check + stats endpoints
lua/
  sliding_window.lua           # Atomic Lua script (shared with Node implementation)
  token_bucket.lua             # Token bucket Lua script (shared with Node implementation)
  leaky_bucket.lua             # Leaky bucket Lua script (shared with Node implementation)
```

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
- **Concurrency:** Loose mode uses a `sync.RWMutex`-guarded map with a background flush goroutine (replaces Node's `setInterval` pattern).
- **Fail-open:** Redis calls use `context.WithTimeout` (5ms budget) — if Redis is down or slow, requests pass through.
- **Idiomatic Go:** Uses `net/http` middleware chaining, struct-based dependency injection, and goroutines for background work.

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/redis/go-redis/v9` | Redis client with Lua `EVALSHA`/`EVAL` support, timeouts, reconnect |
| Standard library (`net/http`, `sync`, `context`) | HTTP server, concurrency, cancellation |

## Testing

```bash
# Unit tests
go test ./...

# Load test (once implemented)
go test -run TestLoad -v ./test/
```
