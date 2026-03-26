import { checkRateLimit } from './sliding-window-counter.js';
import { extractIdentifier } from './identifier.js';
import { setRateLimitHeaders, setBlockedHeaders } from './headers.js';

export function strictRateLimiter() {
  return async (req, res, next) => {
    const identifier = extractIdentifier(req);
    const result = await checkRateLimit(identifier, 1);

    if (result.failedOpen) {
      console.warn('[Strict] Fail-open for:', identifier);
      return next();
    }

    if (result.allowed) {
      setRateLimitHeaders(res, { remaining: result.remaining, resetTtl: result.resetTtl });
      return next();
    }

    setBlockedHeaders(res, { resetTtl: result.resetTtl });
    return res.status(429).json({
      error: 'Too Many Requests',
      retryAfter: result.resetTtl,
    });
  };
}
