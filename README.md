# Distributed Rate Limiter

A production-grade API Gateway rate limiter with implementations in **Node.js** and **Go**, backed by **Redis Lua scripts** for atomic, distributed enforcement.

## Overview

APIs need protection from abuse, runaway clients, and DDoS floods — while still enforcing accurate usage quotas for billing. This project implements a **distributed rate limiter** designed to sit at the API Gateway layer, intercepting every request before it reaches backend services.

**Why this exists:**
- **Usage enforcement** — Monetized APIs need exact request counting that can't be gamed by rotating across load-balanced servers
- **Abuse protection** — High-traffic APIs need sub-millisecond rate checks that don't bottleneck on a central store
- **One system, two modes** — Rather than choosing between accuracy and speed, this limiter offers both as configurable enforcement modes (Strict and Loose)

**What makes it different:**
- A single **Redis Lua script** handles all rate-limit logic atomically — no race conditions, no distributed locks
- **Fail-open resilience** — if Redis goes down, traffic flows through instead of failing closed
- Uses **Redis server time** exclusively to prevent clock drift across gateway instances
- **Zero garbage collection** — Redis TTLs auto-expire old segments with no background cleanup

## Tech Stack

| Layer | Technology | Role |
|---|---|---|
| Gateway (Node.js) | Express + ioredis | HTTP server, Redis client with Lua EVALSHA |
| Gateway (Go) | net/http + go-redis | HTTP server, Redis client (planned) |
| State Store | Redis | Central counter store, Lua script execution |
| Atomic Logic | Redis Lua Scripts | Single-script check-and-increment |
| Load Testing | autocannon | HTTP benchmarking with programmatic API |

## How It Works

### The Algorithm: Sliding Window Counter

Instead of a simple fixed window (vulnerable to boundary burst exploits) or a sliding window log (O(n) memory per user), this uses a **sliding window counter** — a hybrid that divides time into 1-minute segments and sums the last 5 segments to enforce a rolling 5-minute window.

```
Time:  [min 1] [min 2] [min 3] [min 4] [min 5]   <- 5 segments
Counts:  12      8       25      30      15        <- per-segment request counts
                                          |
                              total = 90 / 100 limit
```

Every incoming request triggers a single Redis Lua script that:
1. Gets the authoritative time from Redis (`TIME` command — prevents clock drift across servers)
2. Computes the current 1-minute segment: `floor(unix_time / 60) * 60`
3. Builds 5 Redis keys (current segment + 4 prior)
4. Fetches all 5 counts atomically via `MGET`
5. Sums them to get total rolling usage
6. If `(total + batch_count) > limit` → returns `BLOCKED`
7. Otherwise increments the current segment via `INCRBY` and returns `ALLOWED` with remaining count

The entire check-and-increment is **atomic** — no race conditions, even under massive concurrency.

### Redis Key Schema

```
rate_limit::{identifier}::{segment_timestamp}
```

| Route Type | Identifier | Example Key |
|---|---|---|
| Authenticated | API key or Bearer token | `rate_limit::ak_live_xyz::1710000300` |
| Open | Client IP address | `rate_limit::192.168.1.1::1710000300` |

- **Value:** Integer (request count for that 1-minute segment)
- **TTL:** 300 seconds — Redis auto-expires old segments, no cleanup jobs needed

### Two Enforcement Modes

#### Strict Mode (Billing / Monetization)
Every request synchronously calls Redis before proceeding. Guarantees **100% accurate** counting — no client can exploit load balancer routing to steal extra API calls.

```
Client → Gateway → [sync] Redis Lua → Allowed/Blocked → Backend
```

#### Loose Mode (DDoS Protection)
Requests are counted **locally in server memory**. The buffer flushes to Redis in batches. This means:
- **Zero synchronous Redis calls** in the hot path
- Sub-millisecond latency overhead
- ~95-99% accuracy (minor transient lag across servers)

```
Client → Gateway → [sync] Local Heap → Allowed/Blocked
                          ↓ [async batch]
                        Redis (write-back)
```

### Fail-Open Resilience

If Redis is down or responds slower than 5ms, the gateway **allows the request through** and logs a warning. Availability takes priority over strict enforcement.

### Response Headers

On every rate-limited response:

```
X-RateLimit-Limit: 100          # Max requests per window
X-RateLimit-Remaining: 73       # Requests left before throttling
X-RateLimit-Reset: 1710000600   # Unix timestamp when window resets
```

When blocked (HTTP 429):

```
X-RateLimit-Remaining: 0
Retry-After: 45                 # Seconds until capacity frees up
```

## Performance Targets

| Metric | Strict Mode | Loose Mode |
|---|---|---|
| **p99 Latency** | < 5ms (bound by Redis RTT) | Sub-millisecond (local map lookup) |
| **Counting Accuracy** | 100% exact | ~95-99% (transient lag across servers) |
| **Throughput** | Thousands RPS per gateway | Millions RPS per gateway |
| **Concurrency Safety** | Zero over-admission (Lua atomicity) | Eventual consistency via batch sync |
| **Fail-Open Timeout** | 5ms — allows request if Redis is slow/down | N/A — no Redis in hot path |

**Infrastructure SLOs:**

| Target | Value | Mechanism |
|---|---|---|
| Redis key auto-expiry | 300s TTL per segment | `SETEX` in Lua script — zero GC overhead |
| Clock drift prevention | 0ms | All time derived from Redis `TIME` command |
| Batch flush interval (Loose) | Every 500ms or 50 requests | Whichever threshold is hit first |
| Memory cleanup (Loose) | 5-minute idle prune | Stale heap entries removed each flush cycle |

## Observability

### Rate-Limit Headers

Every rate-limited response includes standard headers for client-side awareness:

| Header | When | Description |
|---|---|---|
| `X-RateLimit-Limit` | All responses | Max requests allowed per 5-minute window |
| `X-RateLimit-Remaining` | All responses | Requests left before throttling |
| `X-RateLimit-Reset` | All responses | Unix timestamp when the oldest segment expires |
| `Retry-After` | 429 only | Seconds until capacity frees up |

### Diagnostic Endpoints

| Endpoint | What it tells you |
|---|---|
| `GET /api/open/health` | Redis connection status — confirms whether rate limiting is active or in fail-open mode |
| `GET /api/open/stats` | Per-identifier heap buffer counts — shows local request tallies and flush state (loose mode) |

## Project Status

| Phase | Scope | Status |
|---|---|---|
| 1. Architecture | Algorithm design, Redis schema, resilience strategy | Complete |
| 2. Node.js | Express server, strict/loose middleware, Lua script, load tests | Complete |
| 3. Go | net/http server, go-redis client, benchmark vs Node.js | Planned |
| 4. Finalization | Docker side-by-side setup, final performance review | Planned |

See [PLAN.md](./PLAN.md) for the detailed task breakdown.

## Quick Start

### Prerequisites
- **Redis** running on localhost:6379

### Node.js

```bash
cd node && npm install && npm start
# Server starts on port 3000
```

### Try It Out

**Strict mode** — single request with full response headers:
```bash
curl -i http://localhost:3000/api/strict/resource
```

**Loose mode** — single request:
```bash
curl -i http://localhost:3000/api/loose/resource
```

**Authenticated request** — rate-limit by API key instead of IP:
```bash
curl -i -H "x-api-key: user_123" http://localhost:3000/api/strict/resource
```

**Trigger a 429** — exceed the limit (default 100):
```bash
for i in $(seq 1 105); do
  curl -s -o /dev/null -w "%{http_code} " http://localhost:3000/api/strict/resource
done
# Output: 100x "200" then 5x "429"
```

**Loose mode under burst:**
```bash
for i in $(seq 1 200); do
  curl -s -o /dev/null -w "%{http_code} " http://localhost:3000/api/loose/resource
done
```

**Health check** — verify Redis connection:
```bash
curl http://localhost:3000/api/open/health
```

**Heap buffer stats** — inspect loose mode internals:
```bash
curl http://localhost:3000/api/open/stats
```

## Implementations

| Directory | Stack | Status |
|---|---|---|
| [`node/`](./node/) | Express + ioredis | Complete |
| [`go/`](./go/) | net/http + go-redis | Planned |

Both implementations share the same Lua script, Redis schema, and enforcement logic. See each directory's README for setup and usage.

## Architecture

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full design specification including Mermaid sequence diagrams, algorithm rationale, and resilience strategies.
