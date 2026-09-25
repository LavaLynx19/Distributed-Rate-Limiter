import { redis, isRedisHealthy } from './redis-client.js';
import { config } from './config.js';
import { withTimeout } from './with-timeout.js';

const TIMEOUT_MS = config.redis.commandTimeout;

export async function checkLeakyBucket(identifier) {
  if (!isRedisHealthy()) {
    return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
  }

  try {
    const result = await withTimeout(
      redis.leakyBucketCheck(identifier, config.leakyBucket.capacity, config.leakyBucket.leakRate),
      TIMEOUT_MS,
      'Leaky bucket check',
    );

    const [status, remaining, ttl] = result;
    return {
      allowed: status === 'ALLOWED',
      remaining: Number(remaining),
      resetTtl: Number(ttl),
      failedOpen: false,
    };
  } catch (err) {
    console.error('[LeakyBucket] Fail-open:', err.message);
    return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
  }
}
