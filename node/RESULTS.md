# Distributed Rate Limiter — Results (Node)

A distributed rate-limiting API gateway built with Express + Redis, implementing three algorithms (Sliding Window Counter, Token Bucket, Leaky Bucket) across two enforcement modes (Strict and Loose). All rate-limit logic runs atomically inside Redis via the shared Lua scripts in [`../lua`](../lua).

**Test environment:** macOS 26.6.2, Apple M4 Pro (14 cores), Node v25.9.0, Redis 8.6.2 local, autocannon; Docker runs on Docker Desktop 29.4 (Linux VM, 14 vCPUs)
**Date:** 2026-09-25. Supersedes the 2026-03-28 results, which were measured with the bugs listed below.

---

## Bugs fixed in this revision

An audit prompted by the Go port's benchmark found these. Each was verified end to end after the fix.

| # | Bug | Impact before | Verified after |
|---|---|---|---|
| B1 | Loose mode zeroed its only counter on every flush, so the local `> maxLimit` guard could never fire | Loose mode admitted **71,622** requests against a limit of 100 in 10s | Admits exactly **100** |
| B2 | `retryStrategy` returned `null` after 3 attempts, which closes the ioredis client for good | Any Redis outage longer than ~1.2s, or Redis down at boot, left rate limiting **off** until a restart | Reconnects within ~1s of Redis returning, from both scenarios, with no restart |
| B3 | Load test 2 ("atomicity") reused test 1's already-exhausted window and asserted `ok <= 100` | Admitted 0, so the check could never fail and **atomicity was never actually tested** | Fresh identifier; exactly 100 of 200 concurrent requests admitted |
| B4 | `trust proxy: true` let any client choose its identity via `X-Forwarded-For` | 105 requests with random XFF headers: **all 105 admitted** | Off by default (`TRUST_PROXY`); 100 admitted, 5 rejected |
| B5 | Loose mode counted rejected requests and flushed them to Redis | A blocked client kept burning its own future quota | Only admitted requests are counted |
| B6 | Shutdown stopped the flush loop without writing counts still pending | Up to one batch per identifier lost on every deploy | 6/6 trials: 0 in Redis before SIGTERM, 30 after |
| B7 | A flush that failed open dropped its batch | Requests served during a Redis blip were never recorded | The batch goes back into `pending` and is retried |
| B8 | Each strict request created a 5ms timer that was never cleared | Timer churn on the hot path | Timer cleared; strict throughput **+13%** in an interleaved A/B (43.0k → 48.7k RPS, 3 pairs) |
| B9 | A client was two identities: `::ffff:a.b.c.d` on a direct connection and `a.b.c.d` through a proxy (found in the Docker stack) | Alternating between the direct port and nginx gave **2× the quota** | IPv4-mapped addresses normalized; 100 admitted in total |
| B10 | Loose write-backs were all-or-nothing, and a blocked identifier never re-checked Redis (found in the Docker stack) | Two instances sharing a quota: 153 admitted but **only 77 recorded**; blocked up to 300 s after capacity freed | `writeback` Lua mode records every served request; allowance synced to Redis; unblocks in ~4 s (ARCHITECTURE.md §6) |

B4, B5, B6, B7, B9 and B10 were also present in the Go port and are fixed there too.

**Known limitation, not yet fixed:** `X-API-Key` and `Authorization` values are trusted as identifiers without validation, so a client that rotates keys gets a fresh quota each time. A key registry is the next planned change (PLAN.md).

---

## Algorithm Comparison

| Property | Sliding Window | Token Bucket | Leaky Bucket |
|---|---|---|---|
| **Redis keys** | 5 string keys per user | 1 hash per user | 1 hash per user |
| **Burst handling** | Allows up to limit in window | Allows burst up to capacity | Smooths output rate |
| **Recovery** | Segments expire over 5 min | Continuous refill (tokens/sec) | Continuous drain (reqs/sec) |
| **Best for** | API quotas, billing | Bursty workloads | Steady-rate enforcement |
| **Clock source** | Redis TIME | Redis TIME (sub-second) | Redis TIME (sub-second) |

---

## Correctness Tests

`npm run test:load`, with each test on its own identifier.

| Test | Algorithm | Requests | Expected | Actual | Result |
|---|---|---|---|---|---|
| 1. Strict correctness | Sliding Window | 150 sequential | 100 × 2xx, 50 × 4xx | 100 / 50 | **PASS** |
| 2. Strict concurrency | Sliding Window | 200 × 10 conn, fresh window | exactly 100 × 2xx | 100 / 100 | **PASS** |
| 3. Loose local guard | Sliding Window | 797,609 over 10s | ≤ 100 × 2xx | 100 | **PASS** |
| 4. Token bucket correctness | Token Bucket | 15 sequential | 10 × 2xx, 5 × 4xx | 10 / 5 | **PASS** |
| 5. Token bucket refill | Token Bucket | 5 after 5s | 5 × 2xx | 5 | **PASS** |
| 6. Leaky bucket correctness | Leaky Bucket | 15 sequential | 10 × 2xx, 5 × 4xx | 10 / 5 | **PASS** |
| 7. Leaky bucket drain | Leaky Bucket | 5 after 5s | 5 × 2xx | 5 | **PASS** |

Also verified by hand: fail-open with Redis absent (all requests allowed), and reconnect after a 5s Redis outage (enforcement resumed at exactly 100/5).

## Latency (from the test suite)

| Test | p50 | p97.5 | p99 | Avg | Max |
|---|---|---|---|---|---|
| Strict (sequential) | 0 ms | 1 ms | 4 ms | 0.10 ms | 6 ms |
| Strict (10 conn) | 0 ms | 3 ms | 3 ms | 0.44 ms | 4 ms |
| Loose (50 conn, pipelined ×10) | 8 ms | 9 ms | 10 ms | 6.52 ms | 118 ms |
| Token bucket | 0 ms | 7 ms | 7 ms | 0.47 ms | 7 ms |
| Leaky bucket | 0 ms | 3 ms | 3 ms | 0.27 ms | 3 ms |

## Throughput — admit path

`RATE_LIMIT_MAX=100000000`, so every request is admitted and does the full amount of work. 3 interleaved runs, median reported. There were no errors, timeouts or non-2xx responses in any run.

**Native (macOS), autocannon, 50 connections:**

| Mode | Runs (RPS) | Median RPS | p99 | Gateway CPU |
|---|---|---|---|---|
| Strict | 44,097 / 42,922 / 49,564 | **44,097** | 1 ms | ~99% of one core |
| Loose (pipelined ×10) | 73,097 / 73,121 / 76,128 | **73,121** | 11 ms | ~100% of one core |

**Docker (Linux), Redis, gateway and load generator pinned to separate vCPUs:**

| Tool | Mode | Median RPS | p99 | Gateway CPU | Redis CPU |
|---|---|---|---|---|---|
| autocannon | Strict | **47,008** | 2 ms | 100% | 45% |
| autocannon | Loose | **69,160** | 10 ms | 101% | 1% |
| wrk (200 conn) | Strict | **42,927** | 7.4 ms | 102% | 40% |
| wrk (200 conn) | Loose | **50,504** | 5.7 ms | 101% | 1% |

---

## Observations

1. **Node is bound by its single event loop everywhere.** The gateway sits at ~100% of one core in every configuration, native and Docker, while Redis stays at ~40–48%. That's why its throughput barely changes between environments.
2. **Strict latency is well inside the 5ms budget** at up to 10 concurrent connections (p99 3–4 ms). At 200 connections (wrk) p99 reaches 7.4 ms, with rare stalls up to ~250 ms.
3. **Loose mode misses the <5ms p99 target under pipelined load** (p99 10–11 ms). The in-process decision is sub-microsecond; the tail comes from a saturated event loop queueing requests.
4. **Atomicity and the local guard are now actually demonstrated.** 200 concurrent requests on a fresh window admitted exactly 100. Loose mode admits exactly 100 on one instance, and records every served request when sharing a quota with the Go gateway.
5. **Scaling Node means more processes.** A cluster of workers, or more instances behind the proxy, now share one quota correctly, which is what the B10 fix guarantees.

For the Go comparison and the full Docker analysis, see [`../go/RESULTS.md`](../go/RESULTS.md).

---

## Screenshots

<!-- Add screenshot: Terminal output of `npm run test:load` showing all PASS results -->

<!-- Add screenshot: Redis MONITOR output during strict mode test showing Lua EVALSHA calls -->

<!-- Add screenshot: `GET /api/open/stats` response showing heap buffer state during loose mode test -->
