# Distributed Rate Limiter — Project Standards

Algorithm: Sliding Window Counter (1-min segments, 5-min rolling window). Atomicity via Redis Lua scripts (EVAL). Clock source: Redis TIME — never local server time.

Modes: Strict (synchronous Redis for billing/monetization). Loose (local heap buffering, write-back every 50 reqs for DDoS protection).

Constraints: Interception logic <5ms latency. Fail-Open if Redis is down — allow the request.

Key refs: ARCHITECTURE.md for Mermaid sequence flows. node/README.md for Node.js implementation details. lua/sliding_window.lua is the shared atomic core — both modes and both language implementations use it. go/README.md for Go implementation details.
