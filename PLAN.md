# Building the Distributed Rate Limiter

## Phase 1: Planning & Setup
- [x] Define the architecture (API Gateway, Redis, Sliding Window Counter algorithm)
- [x] Set up project directories for Node.js and Go

## Phase 2: Node.js Implementation
- [x] Initialize Express server
- [x] Connect to Redis instance
- [x] Implement Sliding Window Counter logic
- [x] Implement Distributed Sliding Window logic via Redis (Lua scripts)
- [x] Implement strict middleware to use the rate limiter
- [x] Write a sample API to test the rate limiter
- [x] Add another middleware for loose rate limiting
- [x] Write another sample API to test the loose rate limiter
- [x] Write load tests to verify rate limiting

## Phase 3: Go Implementation
- [ ] Initialize Go HTTP server (e.g. standard library or net/http)
- [ ] Connect to Redis using a Go client
- [ ] Translate Sliding Window Counter logic to Go
- [ ] Translate Distributed Lua script execution
- [ ] Benchmark and compare performance against Node.js

## Phase 4: Finalization
- [ ] Set up Docker to run both gateways side-by-side
- [ ] Final performance review