# Go Gateway — Results

The Go implementation of the distributed rate limiter: `net/http` + `go-redis`,
sharing the repo-root [`lua/`](../lua) scripts verbatim with the Node gateway.
Both gateways enforce the same algorithms, so the differences below come from
the runtime.

**Native environment:** macOS 26.6.2, Apple M4 Pro (14 cores), Go 1.26.2, Node v25.9.0, Redis 8.6.2
**Docker environment:** Docker Desktop 29.4 Linux VM (14 vCPUs, 8 GB), same images and versions
**Date:** 2026-09-25, final run. All numbers are from the committed code:
- the write-back protocol (ARCHITECTURE.md §6);
- exact Reset/Retry-After (§5);
- the API-key registry (§10);
- Go's packed atomic counters.

Earlier revisions of this file are superseded.

---

## Summary

| | Strict (Go / Node) | Loose (Go / Node) |
|---|---|---|
| Native macOS, autocannon | **1.17×** (53.2k vs 45.4k RPS) | **2.76×** (217.5k vs 78.7k), lower bound: client-bound |
| Docker, CPU-pinned, autocannon | **2.06×** (100.0k vs 48.5k) | **4.05×** (283.6k vs 70.1k), lower bound: client-bound |
| Docker, CPU-pinned, wrk | **2.17×** (95.5k vs 44.0k) | **4.49×** (225.5k vs 50.2k): Go at its 4-vCPU budget |
| Docker, wrk, **registered API key** | **2.41×** (95.3k vs 39.6k) | **4.82×** (219.3k vs 45.5k) |

What limits each gateway:

| | Strict | Loose |
|---|---|---|
| **Node** | Its single event-loop core (~100%) | Its single event-loop core (~100%) |
| **Go** | **Redis** on Linux (87–95% of its pinned core); natively nothing is pinned, and the ceiling is most likely macOS loopback/syscall cost | Its own CPU budget under wrk (388% of 400%); the load generator under autocannon |

**Cost of a registered API key** (a SHA-256 hash plus a cached registry lookup
per request): about 0% for Go, and **about 10% for Node**, whose single event
loop absorbs the extra hash and async step on every request.

---

## Revision history

1. **2026-09-23 — first revision.** It drew three conclusions that turned out to be wrong:
   - "Lua atomicity: PASS" came from a test that couldn't fail.
   - The loose-mode "divergence" was a Node bug.
   - "Strict is Redis-bound" wasn't supported natively.
2. **2026-09-24 — bug-fix pass on both gateways** (see [`../node/RESULTS.md`](../node/RESULTS.md#bugs-fixed-in-this-revision)).
3. **2026-09-25 — Docker stack, write-back fix, one identity per client.** No measurable cost ([Fix cost](#fix-cost)).
4. **2026-09-25 — this revision.**
   - Exact Reset and Retry-After.
   - The API-key registry.
   - Go's allowance and pending counters packed into one atomic word, fixing a rare off-by-one under concurrent admits.
   - Anonymous-path throughput is within ±3% of revision 3.

---

## Methodology

- **Native:** both gateways are driven by the same autocannon harness
  ([`node/test/load-test.js`](../node/test/load-test.js), parameterized by
  `TEST_URL`) on the same Mac and local Redis, one gateway at a time.
  - The harness provisions a registered tenant per test, so tests are isolated.
- **Docker:** [`bench/docker-bench.sh`](../bench/docker-bench.sh) uses the `bench`
  compose profile (ARCHITECTURE.md §9).
  - Redis, the gateway and the load generator are pinned to separate vCPUs
    (`0`, `1-4`, `5-13`) on an isolated network, with no proxy in the path.
  - Each configuration is measured with autocannon (continuity with the
    native runs) and wrk (multi-threaded, to remove the client ceiling).
  - Each is run twice: **anonymous** (IP identity) and **keyed** (a key
    provisioned with `keyctl`, served from the gateway's registry cache).
  - CPU is sampled mid-run with `docker stats`.
- **Throughput runs** set every limit to 1e8, so every request is admitted and
  does the full amount of work. There are 3 interleaved runs per
  configuration, and the median is reported.
  - autocannon: 50 connections (loose pipelined ×10).
  - wrk: 8 threads and 200 connections.

---

## Correctness — Go vs Node (native)

| Test | Algorithm | Requests | Expected | Node | Go | Result |
|---|---|---|---|---|---|---|
| 1. Strict correctness | Sliding Window | 150 sequential | 100 × 2xx, 50 × 4xx | 100 / 50 | 100 / 50 | **PASS** |
| 2. Strict concurrency | Sliding Window | 200 × 10 conn, fresh window | exactly 100 × 2xx | 100 / 100 | 100 / 100 | **PASS** |
| 3. Loose local guard | Sliding Window | 10s at full load | ≤ 100 × 2xx | 100 of 774,743 | 100 of 2,109,752 | **PASS** |
| 4. Token bucket correctness | Token Bucket | 15 sequential | 10 × 2xx, 5 × 4xx | 10 / 5 | 10 / 5 | **PASS** |
| 5. Token bucket refill | Token Bucket | 5 after 5s | 5 × 2xx | 5 | 5 | **PASS** |
| 6. Leaky bucket correctness | Leaky Bucket | 15 sequential | 10 × 2xx, 5 × 4xx | 10 / 5 | 10 / 5 | **PASS** |
| 7. Leaky bucket drain | Leaky Bucket | 5 after 5s | 5 × 2xx | 5 | 5 | **PASS** |

## Correctness — distributed and registry

Checks in the Docker default stack (both gateways, one shared Redis, nginx in
front; ARCHITECTURE.md §9) and natively against both gateways:

| Check | Result |
|---|---|
| One strict quota across runtimes: 105 requests, one registered key, round-robin | **Exactly 100 admitted** (57 by Node, 43 by Go), all under one `tenant:` identity |
| Loose write-back across runtimes: 300-request burst | 132–139 admitted over 3 trials, **all recorded in Redis** (before the fix: 153 admitted, only 77 recorded). The overshoot stays within the documented bound of one batch per instance. |
| Unblock after capacity frees | ~4 s on both (before the fix: stuck up to 300 s) |
| Key rotation: 105 requests, a fresh made-up key each | 100 admitted, 5 rejected: limited by IP, not 105 fresh quotas |
| Pooled paid tenant, 2 slots × plan 20 | Limit 40, shared across both keys |
| Isolated paid tenant | 20 per key, independent quotas; a key beyond the paid slots is refused by `keyctl` |
| Revocation | Honored within the cache TTL; the key falls back to IP identity |
| Retry-After, full window with segments of known age | Exact: 81 s and 77 s against expected 81 and 77 (the old code said 60) |
| `X-Forwarded-For` spoofing on every port | 100 admitted, 5 rejected on each |
| One client via a direct port *and* via nginx | 100 admitted in total, not 200 |

Other checks:
- **Shutdown drain:** 6/6 trials per gateway had 0 in Redis before SIGTERM and 30 after.
- **Fail-open and reconnect:** both work with no gateway restart.
- **`go test -race`:** clean, and `golangci-lint` reports 0 issues.
- **Node `npm test`:** 24 tests, mutation-checked.

---

## Throughput — native (macOS, anonymous)

| Mode | Gateway | Runs (RPS) | **Median** | p50 | p97.5 | p99 | Avg | Max |
|---|---|---|---|---|---|---|---|---|
| Strict | Node | 45,430 / 47,897 / 43,344 | **45,430** | 1 ms | 1 ms | 1 ms | 0.89 ms | 31 ms |
| Strict | **Go** | 55,699 / 53,156 / 53,046 | **53,156** | 0 ms | 1 ms | 1 ms | 0.38 ms | 12 ms |
| Loose (pipelined ×10) | Node | 78,729 / 79,119 / 76,576 | **78,729** | 5 ms | 9 ms | 9 ms | 5.92 ms | 158 ms |
| Loose (pipelined ×10) | **Go** | 216,166 / 217,523 / 217,574 | **217,523** | 2 ms | 4 ms | 4 ms | 2.03 ms | 13 ms |

There were no errors, timeouts or non-2xx responses in any run.

### Strict mode vs. connection count (native)

| Connections | Node RPS | Node p99 | Go RPS | Go p99 |
|---|---|---|---|---|
| 25 | 44,906 | 1 ms | 55,809 | 0 ms |
| 50 | 47,777 | 1 ms | 52,627 | 1 ms |
| 100 | 47,801 | 3 ms | 53,695 | 3 ms |
| 200 | 47,341 | 7 ms | 53,434 | 5 ms |

---

## Throughput — Docker (Linux, CPU-pinned)

| Identity | Tool | Mode | Gateway | Runs (RPS) | **Median** | p50 | p99 | Max | Gateway CPU | Redis CPU | Load-gen CPU |
|---|---|---|---|---|---|---|---|---|---|---|---|
| anon | autocannon | strict | Node | 48,353 / 48,538 / 48,615 | **48,538** | 0 ms | 1 ms | 35 ms | 100% | 45% | 42% |
| anon | autocannon | strict | **Go** | 100,291 / 99,965 / 99,511 | **99,965** | 0 ms | 1 ms | 56 ms | 203% | 88% | 85% |
| anon | autocannon | loose | Node | 70,317 / 70,065 / 69,645 | **70,065** | 8 ms | 9 ms | 41 ms | 101% | 0% | 75% |
| anon | autocannon | loose | **Go** | 266,362 / 285,978 / 283,622 | **283,622** | 1 ms | 6 ms | 31 ms | 242% | 1% | **99%** |
| anon | wrk | strict | Node | 43,540 / 44,226 / 43,976 | **43,976** | 4.42 ms | 6.96 ms | 230 ms | 101% | 41% | 35% |
| anon | wrk | strict | **Go** | 94,989 / 95,453 / 95,723 | **95,453** | 1.94 ms | 4.12 ms | 12 ms | 293% | **93%** | 121% |
| anon | wrk | loose | Node | 49,956 / 51,553 / 50,191 | **50,191** | 3.84 ms | 7.14 ms | 299 ms | 101% | 1% | 52% |
| anon | wrk | loose | **Go** | 225,542 / 225,149 / 226,038 | **225,542** | 0.82 ms | 4.38 ms | 12 ms | **388%** | 0% | 239% |
| keyed | autocannon | strict | Node | 43,897 / 43,107 / 43,449 | **43,449** | 1 ms | 2 ms | 30 ms | 101% | 44% | 38% |
| keyed | autocannon | strict | **Go** | 100,678 / 98,895 / 99,738 | **99,738** | 0 ms | 1 ms | 25 ms | 201% | 87% | 84% |
| keyed | autocannon | loose | Node | 63,804 / 61,679 / 64,089 | **63,804** | 8 ms | 12 ms | 29 ms | 101% | 0% | 57% |
| keyed | autocannon | loose | **Go** | 285,242 / 286,746 / 262,938 | **285,242** | 1 ms | 6 ms | 46 ms | 242% | 1% | **98%** |
| keyed | wrk | strict | Node | 39,232 / 40,236 / 39,609 | **39,609** | 4.78 ms | 7.87 ms | 193 ms | 102% | 41% | 32% |
| keyed | wrk | strict | **Go** | 95,698 / 94,434 / 95,266 | **95,266** | 1.94 ms | 4.14 ms | 9.6 ms | 302% | **95%** | 121% |
| keyed | wrk | loose | Node | 45,799 / 45,482 / 45,057 | **45,482** | 4.26 ms | 6.73 ms | 285 ms | 101% | 1% | 49% |
| keyed | wrk | loose | **Go** | 219,280 / 217,564 / 219,272 | **219,272** | 0.86 ms | 4.16 ms | 11 ms | **388%** | 0% | 234% |

- **CPU budgets:** CPU is % of one vCPU. The gateway's budget is 400% (4 vCPUs), and Redis's is 100% (1 vCPU).
- **Errors:** none, except 14 wrk client socket timeouts (at wrk's 2 s default) in one of the three Go keyed wrk loose runs. That's 14 of 2.2 million requests, with no non-2xx responses. It didn't recur in the other runs, so it looks like a transient VM scheduling stall rather than a gateway fault.

### Cost of a registered API key

| Tool | Mode | Node keyed / anon | Go keyed / anon |
|---|---|---|---|
| autocannon | strict | 0.90× | 1.00× |
| autocannon | loose | 0.91× | 1.01× |
| wrk | strict | 0.90× | 1.00× |
| wrk | loose | 0.91× | 0.97× |

Every keyed request hashes the key (SHA-256) and hits the resolver's cache.
- **Go:** the cost disappears in the noise. The work runs in parallel across goroutines, and in strict mode Redis is the bottleneck anyway.
- **Node:** the cost lands on its single saturated event loop, as a hash plus an extra `await`, so it shows up as a steady ~10% loss.
- **Possible optimization (not implemented):** cache by raw key to skip the per-request hash, at the cost of holding raw keys in memory.

### Fix cost

Anonymous Docker medians. Before the write-back fix → after it (revision 3) → final (revision 4):

| Tool | Mode | Node | Go |
|---|---|---|---|
| autocannon | strict | 47,719 → 47,008 → 48,538 | 101,197 → 101,065 → 99,965 |
| autocannon | loose | 69,873 → 69,160 → 70,065 | 278,982 → 287,334 → 283,622 |
| wrk | strict | 42,205 → 42,927 → 43,976 | 97,053 → 96,102 → 95,453 |
| wrk | loose | 48,658 → 50,504 → 50,191 | 214,819 → 223,688 → 225,542 |

Every step is within ±4%, in both directions: none of the correctness work had
a measurable cost on the anonymous path.

---

## Bottlenecks

Native CPU samples (% of one core; 14 available):

| Run | Gateway | Redis | Load generator | Saturated |
|---|---|---|---|---|
| Node strict | ~98–99% | ~51% | ~69% | **Node's event loop** |
| Node loose | ~100% | ~0% | ~60% | **Node's event loop** |
| Go strict | ~434–468% | ~71–77% | ~74% | nothing |
| Go loose | ~576% | ~0% | **~99%** | **the load generator** |

- **Node is bound by its single thread in every configuration, native and Docker.**
  Its throughput barely moves between environments because the environment
  was never the limit.
- **Go strict is Redis-bound on Linux.** In Docker, Redis's pinned core runs at
  87–95% while the Go gateway uses half to three quarters of its budget.
  Natively, nothing is pinned and Go stops at ~53k. The same binary nearly
  doubles on Linux, so the native ceiling is most likely macOS's loopback and
  syscall cost per round-trip. This is inferred, not profiled.
- **Go loose is limited by whatever runs out first.** autocannon runs out first
  (98–99% CPU) in both environments, so its figures are lower bounds. Under
  wrk, the Go gateway uses 388% of its 400% budget. **~225k RPS is Go's real
  loose-mode ceiling on 4 vCPUs.**
- autocannon's multi-worker mode (`-w 4`) produced *lower* numbers natively
  (Go loose 144k vs 222k), because the workers compete with the gateway for cores.

---

## Observations

1. **Correctness is identical** on every test, native, distributed and registry.
2. **Strict: Go is 1.2× faster natively and 2.1–2.4× in Docker.** On Linux, Go
   drives Redis to its single-core limit. Node's event loop saturates first at
   ~44–48k in both environments.
3. **Loose: Go is 2.8× to 4.8× faster** depending on environment, client and
   identity, and it stays within the <5ms p99 budget everywhere except
   autocannon's pipelined runs (p99 6 ms, client-bound). Node's loose p99 is
   9–12 ms under pipelined load.
4. **Go's tail latency is much tighter:** max 9.6–56 ms vs 29–299 ms for Node.
5. **Authentication widens the gap.** Registered keys cost Node ~10% and Go
   nothing measurable, so for authenticated traffic Go leads by 2.3–2.4×
   (strict) and 4.5–4.8× (loose) in Docker.
6. **Scaling Go strict further means scaling Redis:** sharding by identity, or
   Redis 8's I/O threads. On Linux, the gateway is no longer the bottleneck.

---

## Reproducing

```bash
# Native correctness (either gateway on :3000; the harness provisions its own keys)
cd go && LUA_DIR=../lua go run ./cmd/server        # or: cd node && npm start
cd node && TEST_URL=http://localhost:3000 node test/load-test.js

# Native admit-path throughput (repeat 3×, alternating gateways)
redis-cli FLUSHALL
RATE_LIMIT_MAX=100000000 LUA_DIR=../lua go run ./cmd/server
node/node_modules/.bin/autocannon -c 50 -p 1  -d 10 http://localhost:3000/api/strict/resource
node/node_modules/.bin/autocannon -c 50 -p 10 -d 10 http://localhost:3000/api/loose/resource

# Docker, CPU-pinned, anonymous + keyed (stop the default stack first)
docker compose stop && bench/docker-bench.sh     # summary in bench/results/<timestamp>/summary.md
```

---

## Caveats

- **Native runs share one host,** so the gateway, load generator and Redis
  compete for cores. Docker pins them apart, but the pinned vCPUs are still
  scheduled by the hypervisor onto the host's mixed performance and
  efficiency cores.
- **Only compare within one session.** Node's strict throughput moves ~10%
  between sessions on this machine.
- **Pipelined loose runs** (autocannon ×10) raise p50/p99 for both gateways
  compared with single-request concurrency.
- **The loose batch threshold isn't exercised.** With the limit at 1e8, it is
  never reached, so write-back is driven only by the 500 ms tick. This affects
  both gateways equally.
- **Keyed runs use one hot key,** so every request after the first is a cache hit.
  Cache-miss cost (one Redis lookup per key per 30 s) isn't measured here.
