import { redis, isRedisHealthy } from './redis-client.js';
import { config } from './config.js';

const TIMEOUT_MS = config.redis.commandTimeout;

function timeoutPromise(ms) {
  return new Promise((_, reject) => {
    setTimeout(() => reject(new Error('Rate limit check timed out')), ms);
  });
}

export async function checkRateLimit(identifier, batchCount = 1) {
  if (!isRedisHealthy()) {
    return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
  }

  try {
    const result = await Promise.race([
      redis.rateLimitCheck(identifier, config.rateLimiter.maxLimit, batchCount),
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
    console.error('[RateLimiter] Fail-open:', err.message);
    return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
  }
}
