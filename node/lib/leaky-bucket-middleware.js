import { checkLeakyBucket } from './leaky-bucket.js';
import { identify } from './identifier.js';
import { setRateLimitHeaders, setBlockedHeaders } from './headers.js';
import { config } from './config.js';

export function leakyBucketRateLimiter() {
  return async (req, res, next) => {
    // Same identity as the sliding window; the bucket keeps its global
    // capacity, which protects backend capacity rather than a billing plan.
    const { id: identifier } = await identify(req);
    const result = await checkLeakyBucket(identifier);

    if (result.failedOpen) {
      console.warn('[LeakyBucket] Fail-open for:', identifier);
      return next();
    }

    if (result.allowed) {
      setRateLimitHeaders(res, {
        remaining: result.remaining,
        resetTtl: result.resetTtl,
        limit: config.leakyBucket.capacity,
      });
      return next();
    }

    setBlockedHeaders(res, {
      resetTtl: result.resetTtl,
      limit: config.leakyBucket.capacity,
    });
    return res.status(429).json({
      error: 'Too Many Requests',
      retryAfter: result.resetTtl,
    });
  };
}
