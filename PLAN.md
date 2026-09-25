# Building the Distributed Rate Limiter

## Phase 1: Planning & Setup
- [x] Define the architecture (API Gateway, Redis, Sliding Window Counter algorithm)
- [x] Set up project directories for Node.js and Go

## Phase 2: Node.js Implementation
- [x] Initialize Express server
- [x] Connect to Redis instance
- [x] Implement Sliding Window Counter logic
- [x] Implement Distributed Sliding Window logic via Redis (Lua scripts)
- [x] Implement strict middleware to use the sliding window rate limiter
- [x] Write a sample API to test the sliding window rate limiter
- [x] Add another middleware for loose sliding window rate limiting
- [x] Write another sample API to test the loose sliding window rate limiter
- [x] Write load tests to verify rate limiting
- [x] Implement Token Bucket logic
- [x] Implement Distributed Token Bucket logic via Redis (Lua scripts)
- [x] Implement strict middleware to use the token bucket rate limiter
- [x] Write a sample API to test the token bucket rate limiter
- [x] Implement Leaky Bucket logic
- [x] Implement Distributed Leaky Bucket logic via Redis (Lua scripts)
- [x] Implement strict middleware to use the leaky bucket rate limiter
- [x] Write a sample API to test the leaky bucket rate limiter

## Phase 3: Go Implementation
- [x] Initialize Go HTTP server (e.g. standard library or net/http)
- [x] Connect to Redis using a Go client
- [x] Translate Sliding Window Counter logic to Go
- [x] Translate Distributed Sliding Window logic via Redis (Lua scripts)
- [x] Translate Token Bucket logic to Go
- [x] Translate Distributed Token Bucket logic via Redis (Lua scripts)
- [x] Translate Leaky Bucket logic to Go
- [x] Translate Distributed Leaky Bucket logic via Redis (Lua scripts)
- [x] Benchmark and compare performance against Node.js

## Phase 3b: Correctness Fixes (both gateways)
- [x] B1 Loose-mode local guard reachable (cumulative counter survives flushes) — Node
- [x] B2 Redis reconnect never gives up — Node
- [x] B3 Load test isolates identifiers; atomicity test requires exactly the limit
- [x] B4 `TRUST_PROXY` config, X-Forwarded-For ignored by default — Node + Go
- [x] B5 Rejected loose-mode requests not charged to quota — Node + Go
- [x] B6 Shutdown drains pending loose-mode counts — Node + Go
- [x] B7 Fail-open flush restores its batch — Node + Go
- [x] B8 Strict-path timeout timer cleared — Node
- [x] Re-benchmark both gateways and rewrite both RESULTS.md files
- [>] Lua Retry-After precision (oldest-segment TTL) and loose-mode fixed 60s Retry-After → defer until: Phase 4 final performance review
- [>] API-key rotation bypass (identifiers are unvalidated) → defer until: a key-registry design is added to ARCHITECTURE.md

## Phase 4: Finalization
- [x] Design deployment + benchmark topology (ARCHITECTURE.md §9, Decision Log)
- [x] Set up Docker to run both gateways side-by-side
  - [x] `.dockerignore` (keeps secrets, node_modules and .git out of every image)
  - [x] `go/Dockerfile` (multi-stage, distroless static, non-root)
  - [x] `node/Dockerfile` (node:25-slim, production dependencies only)
  - [x] `nginx/nginx.conf` (:8080 round-robin, :8081 Node, :8082 Go, `X-Served-By`)
  - [x] `docker-compose.yml`: default stack (shared Redis, nginx at 172.28.0.10, `TRUST_PROXY` = nginx IP)
  - [x] `docker-compose.yml`: `bench` profile (isolated Redis, cpusets, loadgen image with autocannon + wrk)
  - [x] `bench/docker-bench.sh` + `bench/summarize.py` (one gateway at a time, 3 interleaved reps, CPU sampling)
- [x] Verify the default stack: parity through nginx, one quota across both gateways, spoofing blocked on direct ports
- [x] Fix: one client counted as two identities (IPv4-mapped IPv6 via direct port vs plain IPv4 via nginx) — Node + Go
- [x] Fix: loose write-back across instances (ARCHITECTURE.md §6 write-back protocol) — Lua `writeback` mode, allowance synced to Redis, 5 s refresh — Node + Go
- [x] Re-run all benchmarks on the fixed code (native correctness + throughput + scaling + CPU, Docker bench)
- [x] Final performance review and documentation (RESULTS files, root README usage)

## Phase 5: Deferred Work & Environment Gaps
- [x] Precise Reset / Retry-After from segment exit times (ARCHITECTURE.md §5) — Lua + loose mode in Node and Go
- [x] API-key registry: hashed keys → tenants with plan tiers, pooled or isolated quotas, cached per gateway; unknown keys fall back to IP (ARCHITECTURE.md §10)
  - [x] Load-test harness provisions registered keys per test (unregistered keys now share the IP identity)
  - [x] Go loose buffer: allowance + pending packed into one atomic word, so concurrent admits are exact (fixes a rare off-by-one found by the registry work)
  - [x] Design recorded in ARCHITECTURE.md (§10 + Decision Log)
  - [x] Go: identity resolver + bounded TTL cache; per-identity limits through strict and loose paths
  - [x] Node: same
  - [x] `go/cmd/keyctl` provisioning CLI (tenant add, key add / revoke, list)
  - [x] Verify: rotation bypass closed, pooled vs isolated quotas, plan limits, revocation within cache TTL, Redis-down fallback
- [x] Install golangci-lint and make `golangci-lint run` clean
- [x] Node unit tests with the built-in `node:test` (no new dependency): 24 tests; buffer split into injectable `lib/buffer-core.js`; mutation-checked
- [ ] Final re-benchmark: registry lookups and Reset calculation change both hot paths
