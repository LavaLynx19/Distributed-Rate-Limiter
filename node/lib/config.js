// TRUST_PROXY follows Express's 'trust proxy' values: unset means use the socket
// address (X-Forwarded-For is client-controlled and would let anyone mint a
// fresh identifier per request); a number trusts that many hops; any other
// string is a subnet list or alias such as 'loopback'.
function parseTrustProxy(value) {
  if (!value) return false;
  const hops = Number(value);
  return Number.isInteger(hops) ? hops : value;
}

import { parsePlans } from './registry.js';

export const config = {
  redis: {
    host: process.env.REDIS_HOST || '127.0.0.1',
    port: parseInt(process.env.REDIS_PORT, 10) || 6379,
    commandTimeout: 5, // ms — fail-open if exceeded
  },
  rateLimiter: {
    maxLimit: parseInt(process.env.RATE_LIMIT_MAX, 10) || 100,
    windowSegments: 5,
    segmentDurationSec: 60,
    windowDurationSec: 300, // 5 * 60
  },
  looseMode: {
    batchThreshold: parseInt(process.env.RATE_LIMIT_MAX, 10) / 2 || 50, // flush after N local increments
    flushIntervalMs: 500, // flush every 500ms
    // How often an exhausted identifier with nothing pending re-asks Redis
    // for capacity, so it unblocks as window segments expire.
    refreshIntervalMs: 5000,
  },
  tokenBucket: {
    capacity: parseInt(process.env.TOKEN_BUCKET_CAPACITY, 10) || 10,
    refillRate: parseFloat(process.env.TOKEN_BUCKET_REFILL_RATE) || 1, // tokens/sec
  },
  leakyBucket: {
    capacity: parseInt(process.env.LEAKY_BUCKET_CAPACITY, 10) || 10,
    leakRate: parseFloat(process.env.LEAKY_BUCKET_LEAK_RATE) || 1, // requests drained/sec
  },
  // API-key registry (ARCHITECTURE.md section 10). Invalid PLAN_LIMITS fails
  // startup rather than silently limiting every tenant anonymously.
  registry: {
    plans: parsePlans(process.env.PLAN_LIMITS || 'free=100,paid=1000'),
    cacheTtlMs: (parseInt(process.env.KEY_CACHE_TTL, 10) || 30) * 1000,
    cacheSize: parseInt(process.env.KEY_CACHE_SIZE, 10) || 10000,
  },
  server: {
    port: parseInt(process.env.PORT, 10) || 3000,
    trustProxy: parseTrustProxy(process.env.TRUST_PROXY),
  },
};
