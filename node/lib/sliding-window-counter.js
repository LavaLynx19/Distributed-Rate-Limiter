import { redis, isRedisHealthy } from './redis-client.js';
import { config } from './config.js';
import { withTimeout } from './with-timeout.js';

const TIMEOUT_MS = config.redis.commandTimeout;

function failOpen() {
  return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
}

async function runSlidingWindow(identifier, limit, batchCount, mode, label) {
  if (!isRedisHealthy()) return failOpen();

  try {
    const result = await withTimeout(
      redis.rateLimitCheck(identifier, limit, batchCount, mode),
      TIMEOUT_MS,
      label,
    );

    const [status, remaining, ttl] = result;
    return {
      allowed: status === 'ALLOWED',
      remaining: Number(remaining),
      resetTtl: Number(ttl),
      failedOpen: false,
    };
  } catch (err) {
    console.error(`[${label}] Fail-open:`, err.message);
    return failOpen();
  }
}

// Strict mode: the request hasn't been served yet, so a batch that doesn't fit
// is rejected without being recorded.
export function checkRateLimit(identifier, limit, batchCount = 1) {
  return runSlidingWindow(identifier, limit, batchCount, 'check', 'RateLimiter');
}

// Loose mode: records requests the local buffer already served, even past the
// limit, and reports the cluster-wide capacity left. A batch of 0 only
// refreshes that figure.
export function writeBack(identifier, limit, batchCount) {
  return runSlidingWindow(identifier, limit, batchCount, 'writeback', 'WriteBack');
}
