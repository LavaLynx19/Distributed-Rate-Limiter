import { checkTokenBucket } from './token-bucket.js';
import { extractIdentifier } from './identifier.js';
import { setRateLimitHeaders, setBlockedHeaders } from './headers.js';
import { config } from './config.js';

export function tokenBucketRateLimiter() {
  return async (req, res, next) => {
    const identifier = extractIdentifier(req);
    const result = await checkTokenBucket(identifier);

    if (result.failedOpen) {
      console.warn('[TokenBucket] Fail-open for:', identifier);
      return next();
    }

    if (result.allowed) {
      setRateLimitHeaders(res, {
        remaining: result.remaining,
        resetTtl: result.resetTtl,
        limit: config.tokenBucket.capacity,
      });
      return next();
    }

    setBlockedHeaders(res, {
      resetTtl: result.resetTtl,
      limit: config.tokenBucket.capacity,
    });
    return res.status(429).json({
      error: 'Too Many Requests',
      retryAfter: result.resetTtl,
    });
  };
}
