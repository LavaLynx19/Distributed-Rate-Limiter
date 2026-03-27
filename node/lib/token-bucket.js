import { redis, isRedisHealthy } from './redis-client.js';
import { config } from './config.js';

const TIMEOUT_MS = config.redis.commandTimeout;

function timeoutPromise(ms) {
  return new Promise((_, reject) => {
    setTimeout(() => reject(new Error('Token bucket check timed out')), ms);
  });
}

export async function checkTokenBucket(identifier) {
  if (!isRedisHealthy()) {
    return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
  }

  try {
    const result = await Promise.race([
      redis.tokenBucketCheck(identifier, config.tokenBucket.capacity, config.tokenBucket.refillRate),
      timeoutPromise(TIMEOUT_MS),
    ]);

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
