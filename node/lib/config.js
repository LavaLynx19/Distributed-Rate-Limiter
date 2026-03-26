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
  server: {
    port: parseInt(process.env.PORT, 10) || 3000,
  },
};
