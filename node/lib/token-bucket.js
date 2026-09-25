import { redis, isRedisHealthy } from './redis-client.js';
import { config } from './config.js';
import { withTimeout } from './with-timeout.js';

const TIMEOUT_MS = config.redis.commandTimeout;

export async function checkTokenBucket(identifier) {
  if (!isRedisHealthy()) {
    return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
  }

  try {
    const result = await withTimeout(
      redis.tokenBucketCheck(identifier, config.tokenBucket.capacity, config.tokenBucket.refillRate),
      TIMEOUT_MS,
      'Token bucket check',
    );

    const [status, remaining, ttl] = result;
    return {
      allowed: status === 'ALLOWED',
      remaining: Number(remaining),
      resetTtl: Number(ttl),
      failedOpen: false,
    };
  } catch (err) {
    console.error('[TokenBucket] Fail-open:', err.message);
    return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
  }
}
