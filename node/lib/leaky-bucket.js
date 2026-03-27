import { redis, isRedisHealthy } from './redis-client.js';
import { config } from './config.js';

const TIMEOUT_MS = config.redis.commandTimeout;

function timeoutPromise(ms) {
  return new Promise((_, reject) => {
    setTimeout(() => reject(new Error('Leaky bucket check timed out')), ms);
  });
}

export async function checkLeakyBucket(identifier) {
  if (!isRedisHealthy()) {
    return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
  }

  try {
    const result = await Promise.race([
      redis.leakyBucketCheck(identifier, config.leakyBucket.capacity, config.leakyBucket.leakRate),
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
    console.error('[LeakyBucket] Fail-open:', err.message);
    return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
  }
}
