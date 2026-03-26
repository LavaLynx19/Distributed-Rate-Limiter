# Project Specific: Distributed Rate Limiter

## 1. Core Logic & Algorithms
- **Algorithm:** Sliding Window Counter (1-minute segments, 5-minute rolling window).
- **Atomicity:** Use Redis Lua scripts (`EVAL`) for all counter increments.
- **Clock Source:** Use `TIME` from Redis; never use local server time to avoid drift.

## 2. Critical Operating Modes
- **Strict Mode:** Synchronous Redis checks for billing/monetization.
- **Loose Mode:** Local Heap buffering (write-back every 50 reqs) for DDoS protection.

## 3. High-Performance Constraints
- **Latency Goal:** Interception logic must complete in <5ms.
- **Resilience:** Implement a **Fail-Open** strategy; if Redis is down, allow the request.

## 4. Key References
- Refer to `ARCHITECTURE.md` for the Mermaid sequence flows before refactoring Gateway middleware.
- Refer to `node/README.md` for Node.js implementation details, endpoints, and module walkthrough.
- The Lua script at `node/lua/sliding_window.lua` is the shared atomic core — both strict and loose modes use it.