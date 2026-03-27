# Node.js Implementation

Express-based API Gateway with ioredis for Redis communication. See the [root README](../README.md) for algorithm and architecture details.

## Setup

### Prerequisites
- **Node.js** >= 18
- **Redis** running on localhost:6379 (or configure via env vars)

### Install & Run

```bash
npm install
npm start
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

You can also create a `.env` file — dotenv is loaded at startup.

## Project Structure

```
server.js                        # Express entry point, graceful shutdown
lua/
  sliding_window.lua             # Sliding window counter Lua script (shared across modes)
  token_bucket.lua               # Token bucket Lua script (strict mode only)
  leaky_bucket.lua               # Leaky bucket Lua script (strict mode only)
lib/
  config.js                      # Central config (env vars + defaults)
  redis-client.js                # ioredis connection + Lua defineCommand
  sliding-window-counter.js      # Sliding window: calls Lua, parses result, fail-open
  token-bucket.js                # Token bucket: calls Lua, parses result, fail-open
  leaky-bucket.js                # Leaky bucket: calls Lua, parses result, fail-open
  identifier.js                  # Extracts user ID from request (API key / IP)
  headers.js                     # Sets X-RateLimit-* and Retry-After headers
  strict-middleware.js            # Express middleware — sliding window strict mode
  token-bucket-middleware.js      # Express middleware — token bucket strict mode
  leaky-bucket-middleware.js      # Express middleware — leaky bucket strict mode
  heap-buffer.js                 # Local Map buffer + flush loop for loose mode
  loose-middleware.js             # Express middleware — sliding window loose mode
routes/
  sample-api.js                  # Strict-mode demo endpoints + health/stats
  loose-api.js                   # Loose-mode demo endpoints
  token-bucket-api.js            # Token bucket demo endpoints
  leaky-bucket-api.js            # Leaky bucket demo endpoints
test/
  load-test.js                   # autocannon-based load tests
```

## API Endpoints

### Rate-Limited

| Endpoint | Mode | Description |
|---|---|---|
| `GET /api/strict/resource` | Strict | Synchronous Redis check per request (sliding window) |
| `GET /api/loose/resource` | Loose | Local heap check, async Redis sync (sliding window) |
| `GET /api/loose/burst` | Loose | Burst traffic simulation endpoint (sliding window) |
| `GET /api/token-bucket/resource` | Strict | Token bucket — burst-tolerant rate limiting |
| `GET /api/leaky-bucket/resource` | Strict | Leaky bucket — steady-rate enforcement |

### Open (No Rate Limiting)

| Endpoint | Description |
|---|---|
| `GET /api/open/health` | Health check — shows Redis connection status |
| `GET /api/open/stats` | Heap buffer stats — shows local counts per identifier |

## Testing

### Manual — Strict Mode

```bash
# Send 105 requests — first 100 get 200, last 5 get 429
for i in $(seq 1 105); do
  curl -s -o /dev/null -w "%{http_code} " http://localhost:3000/api/strict/resource
done
```

### Manual — Loose Mode

```bash
# Send 200 rapid requests — 429s appear as local buffer fills
for i in $(seq 1 200); do
  curl -s -o /dev/null -w "%{http_code} " http://localhost:3000/api/loose/resource
done
```

### Manual — Token Bucket

```bash
# Send 12 requests (capacity=10) — first 10 get 200, last 2 get 429
for i in $(seq 1 12); do
  curl -s -o /dev/null -w "%{http_code} " http://localhost:3000/api/token-bucket/resource
done
# Wait 3 seconds for tokens to refill, then send 3 more — should get 200
sleep 3 && for i in $(seq 1 3); do
  curl -s -o /dev/null -w "%{http_code} " http://localhost:3000/api/token-bucket/resource
done
```

### Manual — Leaky Bucket

```bash
# Send 12 requests (capacity=10) — first 10 get 200, last 2 get 429
for i in $(seq 1 12); do
  curl -s -o /dev/null -w "%{http_code} " http://localhost:3000/api/leaky-bucket/resource
done
# Wait 3 seconds for bucket to drain, then send 3 more — should get 200
sleep 3 && for i in $(seq 1 3); do
  curl -s -o /dev/null -w "%{http_code} " http://localhost:3000/api/leaky-bucket/resource
done
```

### Manual — Fail-Open

Stop Redis, then send requests to any rate-limited endpoint — all should return 200.

### Load Tests

```bash
# Flush Redis first for clean results
redis-cli FLUSHDB

# Run the test suite (server must be running)
npm run test:load
```

The load test suite runs seven scenarios:
1. **Strict correctness** — 150 sequential requests, verifies exactly 100 pass (sliding window)
2. **Strict concurrency** — 200 requests across 10 connections, verifies Lua atomicity (sliding window)
3. **Loose throughput** — 50 connections for 10 seconds, measures RPS and p99 latency
4. **Token bucket correctness** — 15 sequential requests (capacity=10), verifies blocking
5. **Token bucket refill** — Waits for tokens to refill, verifies recovery
6. **Leaky bucket correctness** — 15 sequential requests (capacity=10), verifies blocking
7. **Leaky bucket drain** — Waits for bucket to drain, verifies recovery

## Module Walkthrough

### `lua/sliding_window.lua`
Sliding window counter Lua script. Executes atomically inside Redis. Serves both strict and loose modes — the `batch_count` argument (1 for strict, N for loose) is the only difference. Uses 5 String keys per user (one per 60s segment).

### `lua/token_bucket.lua`
Token bucket Lua script. Tracks tokens and last refill time in a Redis Hash per user. Refills tokens based on elapsed time, consumes 1 per request. Strict mode only.

### `lua/leaky_bucket.lua`
Leaky bucket Lua script. Tracks water level and last leak time in a Redis Hash per user. Drains at a constant rate, rejects on overflow. Strict mode only.

### `lib/redis-client.js`
Creates the ioredis client and registers the Lua script via `defineCommand`. This gives us automatic `EVALSHA`/`EVAL` fallback — the first call caches the script SHA in Redis, all subsequent calls use `EVALSHA` (no script re-transmission).

### `lib/sliding-window-counter.js`
Thin wrapper around the Lua call. Adds a `Promise.race` with a 5ms timeout as defense-in-depth on top of ioredis's `commandTimeout`. If anything goes wrong, returns `{ allowed: true, failedOpen: true }`.

### `lib/heap-buffer.js`
The local in-memory buffer for loose mode. Maintains a `Map<identifier, { count, lastFlush, blocked }>`. Key design details:
- **Race-safe flush:** Subtracts the flushed count (instead of resetting to 0) to preserve increments that arrive during the async flush
- **Memory management:** Prunes stale entries (count=0, idle > 5 minutes) during each flush cycle
- **Immediate flush:** Triggers a flush when any identifier hits `batchThreshold` (half of `RATE_LIMIT_MAX`)

### `lib/strict-middleware.js` / `lib/loose-middleware.js`
Express middleware factories. Strict makes a synchronous Redis call per request. Loose calls `incrementLocal()` (a synchronous Map operation — sub-microsecond) and lets the background flush loop handle Redis.

## Dependencies

| Package | Purpose |
|---|---|
| `express` ^4.x | HTTP server and middleware pipeline |
| `ioredis` ^5.x | Redis client with Lua `EVALSHA`/`EVAL` support, timeouts, reconnect |
| `autocannon` ^8.x (dev) | HTTP load testing with programmatic API |
