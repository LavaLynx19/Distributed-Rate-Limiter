import { checkLeakyBucket } from './leaky-bucket.js';
import { extractIdentifier } from './identifier.js';
import { setRateLimitHeaders, setBlockedHeaders } from './headers.js';
import { config } from './config.js';

export function leakyBucketRateLimiter() {
  return async (req, res, next) => {
    const identifier = extractIdentifier(req);
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
