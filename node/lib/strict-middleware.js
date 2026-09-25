import { checkRateLimit } from './sliding-window-counter.js';
import { identify } from './identifier.js';
import { setRateLimitHeaders, setBlockedHeaders } from './headers.js';

export function strictRateLimiter() {
  return async (req, res, next) => {
    const { id, limit } = await identify(req);
    const result = await checkRateLimit(id, limit, 1);

    if (result.failedOpen) {
      console.warn('[Strict] Fail-open for:', id);
      return next();
    }

    if (result.allowed) {
      setRateLimitHeaders(res, { remaining: result.remaining, resetTtl: result.resetTtl, limit });
      return next();
    }

    setBlockedHeaders(res, { resetTtl: result.resetTtl, limit });
    return res.status(429).json({
      error: 'Too Many Requests',
      retryAfter: result.resetTtl,
    });
  };
}
