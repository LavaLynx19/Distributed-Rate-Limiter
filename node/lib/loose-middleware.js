import { incrementLocal } from './heap-buffer.js';
import { extractIdentifier } from './identifier.js';
import { setRateLimitHeaders, setBlockedHeaders } from './headers.js';
import { config } from './config.js';

export function looseRateLimiter() {
  return (req, res, next) => {
    const identifier = extractIdentifier(req);
    const { localCount, blocked } = incrementLocal(identifier);

    // Local DDoS protection — block immediately if a single server
    // sees more than maxLimit from one identifier (no network call)
    if (blocked || localCount > config.rateLimiter.maxLimit) {
      setBlockedHeaders(res, { resetTtl: config.rateLimiter.segmentDurationSec });
      return res.status(429).json({
        error: 'Too Many Requests',
        retryAfter: config.rateLimiter.segmentDurationSec,
      });
    }

    // Approximate remaining based on local count
    const remaining = Math.max(0, config.rateLimiter.maxLimit - localCount);
    setRateLimitHeaders(res, { remaining, resetTtl: config.rateLimiter.segmentDurationSec });
    return next();
  };
}
