import { admitLocal } from './heap-buffer.js';
import { identify } from './identifier.js';
import { setRateLimitHeaders, setBlockedHeaders } from './headers.js';
import { config } from './config.js';

export function looseRateLimiter() {
  return async (req, res, next) => {
    // A cache hit resolves without a network call; a miss costs one lookup
    // per key per cache TTL (ARCHITECTURE.md section 10).
    const { id, limit } = await identify(req);
    const { admitted, remaining, retryAfter } = admitLocal(id, limit);

    // Rejected when this instance's allowance is spent. Each write-back resets
    // it to the cluster-wide remaining capacity, so no network call is needed
    // to say no.
    if (!admitted) {
      setBlockedHeaders(res, { resetTtl: retryAfter, limit });
      return res.status(429).json({ error: 'Too Many Requests', retryAfter });
    }

    setRateLimitHeaders(res, { remaining, resetTtl: config.rateLimiter.segmentDurationSec, limit });
    return next();
  };
}
