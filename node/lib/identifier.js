import { redis, isRedisHealthy } from './redis-client.js';
import { config } from './config.js';
import { createResolver } from './registry.js';
import { withTimeout } from './with-timeout.js';

// Node's dual-stack sockets report IPv4 peers as IPv4-mapped IPv6
// ("::ffff:1.2.3.4") while proxies forward plain IPv4. Without normalizing,
// one client is two identities with two quotas depending on the path taken.
function normalizeIp(ip) {
  return ip?.startsWith('::ffff:') && ip.includes('.') ? ip.slice(7) : ip;
}

// The API key a request carries: X-API-Key, else an Authorization bearer
// token. Other auth schemes are ignored. It is only a claim until the
// registry confirms it.
export function presentedKey(req) {
  const apiKey = req.headers['x-api-key'];
  if (apiKey) return apiKey;
  const match = /^Bearer\s+(.+)$/i.exec(req.headers['authorization'] ?? '');
  return match ? match[1].trim() : '';
}

const resolve = createResolver({
  lookup: (hash) => {
    if (!isRedisHealthy()) return Promise.reject(new Error('redis unavailable'));
    return withTimeout(redis.resolveKey(hash), config.redis.commandTimeout, 'Key lookup');
  },
  plans: config.registry.plans,
  ttlMs: config.registry.cacheTtlMs,
  size: config.registry.cacheSize,
});

// Who a request is rate-limited as: a registered key's key or tenant identity
// at its plan limit; otherwise (no key, unknown or malformed key, failed
// lookup) the client IP at the anonymous limit, so rotating made-up keys
// gains nothing. ARCHITECTURE.md section 10.
export async function identify(req) {
  const key = presentedKey(req);
  if (key) {
    const identity = await resolve(key);
    if (identity) return identity;
  }
  return { id: `ip:${normalizeIp(req.ip)}`, limit: config.rateLimiter.maxLimit };
}
