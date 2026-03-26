import { config } from './config.js';

export function setRateLimitHeaders(res, { remaining, resetTtl }) {
  const resetAt = Math.floor(Date.now() / 1000) + resetTtl;
  res.set('X-RateLimit-Limit', String(config.rateLimiter.maxLimit));
  res.set('X-RateLimit-Remaining', String(remaining));
  res.set('X-RateLimit-Reset', String(resetAt));
}

export function setBlockedHeaders(res, { resetTtl }) {
  const resetAt = Math.floor(Date.now() / 1000) + resetTtl;
  res.set('X-RateLimit-Limit', String(config.rateLimiter.maxLimit));
  res.set('X-RateLimit-Remaining', '0');
  res.set('X-RateLimit-Reset', String(resetAt));
  res.set('Retry-After', String(resetTtl));
}
