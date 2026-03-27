import { config } from './config.js';

export function setRateLimitHeaders(res, { remaining, resetTtl, limit }) {
  const resetAt = Math.floor(Date.now() / 1000) + resetTtl;
  res.set('X-RateLimit-Limit', String(limit ?? config.rateLimiter.maxLimit));
  res.set('X-RateLimit-Remaining', String(remaining));
  res.set('X-RateLimit-Reset', String(resetAt));
}

export function setBlockedHeaders(res, { resetTtl, limit }) {
  const resetAt = Math.floor(Date.now() / 1000) + resetTtl;
  res.set('X-RateLimit-Limit', String(limit ?? config.rateLimiter.maxLimit));
  res.set('X-RateLimit-Remaining', '0');
  res.set('X-RateLimit-Reset', String(resetAt));
  res.set('Retry-After', String(resetTtl));
}
