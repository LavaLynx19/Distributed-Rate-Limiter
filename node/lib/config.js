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
  },
  tokenBucket: {
    capacity: parseInt(process.env.TOKEN_BUCKET_CAPACITY, 10) || 10,
    refillRate: parseFloat(process.env.TOKEN_BUCKET_REFILL_RATE) || 1, // tokens/sec
  },
  leakyBucket: {
    capacity: parseInt(process.env.LEAKY_BUCKET_CAPACITY, 10) || 10,
    leakRate: parseFloat(process.env.LEAKY_BUCKET_LEAK_RATE) || 1, // requests drained/sec
  },
  server: {
    port: parseInt(process.env.PORT, 10) || 3000,
  },
};
