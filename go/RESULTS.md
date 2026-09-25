# Go Gateway — Results

The Go implementation of the distributed rate limiter: `net/http` + `go-redis`,
sharing the repo-root [`lua/`](../lua) scripts verbatim with the Node gateway.
Both gateways enforce the same algorithms, so the differences below come from
the runtime.

**Native environment:** macOS 26.6.2, Apple M4 Pro (14 cores), Go 1.26.2, Node v25.9.0, Redis 8.6.2
**Docker environment:** Docker Desktop 29.4 Linux VM (14 vCPUs, 8 GB), same images and versions
**Date:** 2026-09-25. All numbers are from the code after the write-back fix
(ARCHITECTURE.md §6); earlier revisions of this file are superseded.

---

## Summary

| | Strict (Go / Node) | Loose (Go / Node) |
|---|---|---|
| Native macOS, autocannon | **1.22×** (54.0k vs 44.1k RPS) | **2.94×** (214.9k vs 73.1k), lower bound: client-bound |
| Docker, CPU-pinned, autocannon | **2.15×** (101.1k vs 47.0k) | **4.15×** (287.3k vs 69.2k), lower bound: client-bound |
| Docker, CPU-pinned, wrk | **2.24×** (96.1k vs 42.9k) | **4.43×** (223.7k vs 50.5k): Go at its 4-vCPU budget |

What limits each gateway:

| | Strict | Loose |
|---|---|---|
| **Node** | Its single event-loop core (~100%) | Its single event-loop core (~100%) |
| **Go** | **Redis** on Linux (86–93% of its pinned core); on native macOS nothing is pinned, and the ceiling is most likely macOS loopback/syscall cost | Its own CPU budget under wrk (385% of 400%); the load generator under autocannon |

---

## Revision history

1. **2026-09-23 — first revision.** It drew three conclusions that turned out to be wrong:
   - "Lua atomicity: PASS" came from a test that couldn't fail.
   - The loose-mode "divergence" was a Node bug.
   - "Strict is Redis-bound" wasn't supported natively (see [Bottlenecks](#bottlenecks)).
2. **2026-09-24 — bug-fix pass on both gateways** (see [`../node/RESULTS.md`](../node/RESULTS.md#bugs-fixed-in-this-revision)).
3. **2026-09-25 — Docker and two more fixes.**
   - **Write-back across instances:** a loose batch that crossed the limit was dropped although its requests had been served, and blocked identifiers stayed blocked for up to 300 s.
   - **One client, two identities:** Node saw the same client as both IPv4 and IPv4-mapped IPv6.
   - Both are fixed in both gateways, and the re-measured numbers are within noise of the pre-fix Docker baseline ([Fix cost](#fix-cost)).

---

## Methodology

- **Native:** both gateways are driven by the same autocannon harness
  ([`node/test/load-test.js`](../node/test/load-test.js), parameterized by
  `TEST_URL`) on the same Mac and local Redis, one gateway at a time.
- **Docker:** [`bench/docker-bench.sh`](../bench/docker-bench.sh) uses the `bench`
  compose profile (ARCHITECTURE.md §9).
  - Redis, the gateway and the load generator are pinned to separate vCPUs
    (`0`, `1-4`, `5-13`) on an isolated network, with no proxy in the path.
  - Each configuration is measured with autocannon (continuity with the
    native runs) and wrk (multi-threaded, to remove the client ceiling).
  - CPU is sampled mid-run with `docker stats`.
- **Throughput runs** use `RATE_LIMIT_MAX=100000000`, so every request is
  admitted and does the full amount of work. There are 3 interleaved runs per
  configuration, and the median is reported.
  - autocannon: 50 connections (loose pipelined ×10).
  - wrk: 8 threads and 200 connections.

---

## Correctness — Go vs Node (native)

| Test | Algorithm | Requests | Expected | Node | Go | Result |
|---|---|---|---|---|---|---|
| 1. Strict correctness | Sliding Window | 150 sequential | 100 × 2xx, 50 × 4xx | 100 / 50 | 100 / 50 | **PASS** |
| 2. Strict concurrency | Sliding Window | 200 × 10 conn, fresh window | exactly 100 × 2xx | 100 / 100 | 100 / 100 | **PASS** |
| 3. Loose local guard | Sliding Window | 10s at full load | ≤ 100 × 2xx | 100 of 797,609 | 100 of 2,049,912 | **PASS** |
| 4. Token bucket correctness | Token Bucket | 15 sequential | 10 × 2xx, 5 × 4xx | 10 / 5 | 10 / 5 | **PASS** |
| 5. Token bucket refill | Token Bucket | 5 after 5s | 5 × 2xx | 5 | 5 | **PASS** |
| 6. Leaky bucket correctness | Leaky Bucket | 15 sequential | 10 × 2xx, 5 × 4xx | 10 / 5 | 10 / 5 | **PASS** |
| 7. Leaky bucket drain | Leaky Bucket | 5 after 5s | 5 × 2xx | 5 | 5 | **PASS** |

## Correctness — distributed (Docker default stack)

Both gateways share one Redis behind nginx (ARCHITECTURE.md §9).

| Check | Result |
|---|---|
| One strict quota across runtimes: 105 requests, one key, round-robin | **Exactly 100 admitted** (54 by Node, 46 by Go) |
| Loose write-back across runtimes: 300-request burst, one key | 132–139 admitted over 3 trials, **all recorded in Redis** (before the fix: 153 admitted, only 77 recorded). The overshoot stays within the documented bound of one batch per instance. |
| Another instance already used 90 of 100 | 41 admitted before the first sync corrected the allowance (bound: one batch of 50); Redis total = 90 + 41 |
| Unblock after capacity frees | Both gateways admit again within ~4 s (before the fix: stuck up to 300 s) |
| `X-Forwarded-For` spoofing on every port (3000, 3001, 8080–8082) | 100 admitted, 5 rejected on each |
| One client via a direct port *and* via nginx | 100 admitted in total, not 200 (before the fix, Node saw two identities) |

Checks run natively against both gateways:
- **Shutdown drain:** 6/6 trials had 0 in Redis before SIGTERM and 30 after.
- **Fail-open with Redis absent:** every request allowed.
- **Reconnect with no restart:** ~1 s on both gateways.
- **`go test -race`:** clean over 5 repeated runs.

**Known limitation, both gateways:** API keys aren't validated, so rotating
keys yields a fresh quota. A key registry is the next planned change (PLAN.md).

---

## Throughput — native (macOS)

| Mode | Gateway | Runs (RPS) | **Median** | p50 | p97.5 | p99 | Avg | Max |
|---|---|---|---|---|---|---|---|---|
| Strict | Node | 44,097 / 42,922 / 49,564 | **44,097** | 1 ms | 1 ms | 1 ms | 0.75 ms | 20 ms |
| Strict | **Go** | 54,003 / 53,267 / 54,451 | **54,003** | 0 ms | 1 ms | 1 ms | 0.36 ms | 12 ms |
| Loose (pipelined ×10) | Node | 73,097 / 73,121 / 76,128 | **73,121** | 7 ms | 10 ms | 11 ms | 6.32 ms | 94 ms |
| Loose (pipelined ×10) | **Go** | 215,040 / 214,912 / 210,240 | **214,912** | 2 ms | 4 ms | 4 ms | 2.08 ms | 14 ms |

Node's strict runs spread 15% between repetitions; everything else was
within 3%. There were no errors, timeouts or non-2xx responses in any run.

### Strict mode vs. connection count (native)

| Connections | Node RPS | Node p99 | Go RPS | Go p99 |
|---|---|---|---|---|
| 25 | 41,453 | 1 ms | 57,084 | 0 ms |
| 50 | 45,860 | 1 ms | 53,948 | 1 ms |
| 100 | 48,700 | 3 ms | 54,140 | 2 ms |
| 200 | 42,649 | 8 ms | 53,414 | 5 ms |

---

## Throughput — Docker (Linux, CPU-pinned)

| Tool | Mode | Gateway | Runs (RPS) | **Median** | p50 | p99 | Max | Gateway CPU | Redis CPU | Load-gen CPU |
|---|---|---|---|---|---|---|---|---|---|---|
| autocannon | strict | Node | 44,601 / 47,008 / 48,132 | **47,008** | 0 ms | 2 ms | 25 ms | 100% | 45% | 43% |
| autocannon | strict | **Go** | 98,720 / 101,065 / 101,254 | **101,065** | 0 ms | 1 ms | 36 ms | 198% | 86% | 83% |
| autocannon | loose | Node | 68,307 / 69,160 / 69,484 | **69,160** | 8 ms | 10 ms | 33 ms | 101% | 1% | 54% |
| autocannon | loose | **Go** | 265,965 / 287,334 / 289,152 | **287,334** | 1 ms | 5 ms | 36 ms | 231% | 1% | **100%** |
| wrk | strict | Node | 42,927 / 42,831 / 44,224 | **42,927** | 4.44 ms | 7.42 ms | 215 ms | 102% | 40% | 33% |
| wrk | strict | **Go** | 96,019 / 96,669 / 96,102 | **96,102** | 1.91 ms | 4.03 ms | 11 ms | 304% | **93%** | 120% |
| wrk | loose | Node | 52,945 / 50,504 / 50,111 | **50,504** | 3.84 ms | 5.70 ms | 252 ms | 101% | 1% | 53% |
| wrk | loose | **Go** | 225,190 / 223,688 / 222,562 | **223,688** | 0.84 ms | 3.77 ms | 8.8 ms | **385%** | 1% | 235% |

CPU is % of one vCPU. The gateway's budget is 400% (4 vCPUs) and Redis's is
100% (1 vCPU). There were no errors, timeouts or non-2xx responses in any run.

### Fix cost

The write-back fix changed the shared Lua script and the loose hot path in
both gateways. Docker medians, before vs. after:

| Tool | Mode | Node before → after | Go before → after |
|---|---|---|---|
| autocannon | strict | 47,719 → 47,008 (−1.5%) | 101,197 → 101,065 (−0.1%) |
| autocannon | loose | 69,873 → 69,160 (−1.0%) | 278,982 → 287,334 (+3.0%) |
| wrk | strict | 42,205 → 42,927 (+1.7%) | 97,053 → 96,102 (−1.0%) |
| wrk | loose | 48,658 → 50,504 (+3.8%) | 214,819 → 223,688 (+4.1%) |

All within ±4% and in both directions, so there's no measurable cost.

---

## Bottlenecks

Native CPU samples (% of one core; 14 available):

| Run | Gateway | Redis | Load generator | Saturated |
|---|---|---|---|---|
| Node strict | ~99% | ~48% | ~69% | **Node's event loop** |
| Node loose | ~100% | ~0% | ~57% | **Node's event loop** |
| Go strict | ~454–467% | ~71–75% | ~69% | nothing |
| Go loose | ~689% | ~0% | **~100%** | **the load generator** |

Across both environments:

- **Node is bound by its single thread in every configuration, native and
  Docker.** Its throughput barely moves between environments (strict 44k
  native vs 43–47k Docker) because the environment was never the limit.
- **Go strict is Redis-bound on Linux.** In Docker, Redis's pinned core runs at
  86–93% while the Go gateway uses half to three quarters of its budget.
  Natively, nothing is pinned and Go stops at ~54k. The same binary doubles
  on Linux, so the native ceiling is most likely macOS's loopback and syscall
  cost per round-trip rather than anything in Go. This is inferred, not
  profiled.
- **Go loose is limited by whatever runs out first.** autocannon runs out
  first (100% CPU) in both environments, so the autocannon loose figures are
  lower bounds. wrk has headroom (235% of 900%), and the Go gateway then uses
  385% of its 400% budget. **224k RPS is Go's real loose-mode ceiling on 4
  vCPUs.**
- autocannon's multi-worker mode (`-w 4`) was tried natively and produced
  *lower* numbers (Go loose 153k vs 210k), because the workers competed with
  the gateway for cores.

---

## Observations

1. **Correctness is identical** on every test, native and distributed. The two
   runtimes share one quota exactly in strict mode, and within the documented
   one-batch bound in loose mode.
2. **Strict: Go is 1.2× faster natively and 2.2× in Docker.** On Linux, Go drives
   Redis close to its single-core limit. Node's single event loop saturates
   first at ~45k in both environments.
3. **Loose: Go is 2.9× to 4.4× faster** depending on environment and client,
   and it stays within the <5ms p99 budget (ARCHITECTURE.md §4) everywhere.
   Node's loose p99 is 10–11 ms under pipelined load.
4. **Go's tail latency is much tighter:** max 8.8–36 ms vs 25–252 ms for Node.
   Node's worst tails appear under wrk's 200 connections.
5. **Scaling Go strict further means scaling Redis:** sharding by identifier, or
   Redis 8's I/O threads. On Linux, the gateway is no longer the bottleneck.

---

## Reproducing

```bash
# Native correctness (either gateway on :3000)
cd go && LUA_DIR=../lua go run ./cmd/server        # or: cd node && npm start
cd node && TEST_URL=http://localhost:3000 node test/load-test.js

# Native admit-path throughput (repeat 3×, alternating gateways)
redis-cli FLUSHALL
RATE_LIMIT_MAX=100000000 LUA_DIR=../lua go run ./cmd/server
node/node_modules/.bin/autocannon -c 50 -p 1  -d 10 http://localhost:3000/api/strict/resource
node/node_modules/.bin/autocannon -c 50 -p 10 -d 10 http://localhost:3000/api/loose/resource

# Docker, CPU-pinned (stop the default stack first)
docker compose stop && bench/docker-bench.sh     # summary in bench/results/<timestamp>/summary.md
```

---

## Caveats

- **Native runs share one host,** so the gateway, load generator and Redis
  compete for cores. Docker pins them apart, but the pinned vCPUs are still
  scheduled by the hypervisor onto the host's mixed performance and
  efficiency cores.
- **Only compare within one session.** Node's strict throughput moved ~10%
  between sessions on this machine.
- **Pipelined loose runs** (autocannon ×10) raise p50/p99 for both gateways
  compared with single-request concurrency.
- **The loose batch threshold isn't exercised.** With the limit at 1e8, it is
  never reached, so write-back is driven only by the 500 ms tick. This affects
  both gateways equally.
