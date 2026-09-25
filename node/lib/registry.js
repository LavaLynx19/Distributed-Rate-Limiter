// API-key registry resolution (ARCHITECTURE.md section 10). Keys are
// registered in Redis by SHA-256 hash; raw keys are never stored.
import { createHash } from 'node:crypto';

const KEY_FORMAT = /^rlk_[0-9A-Za-z]{32}$/;

export const ModeIsolated = 'isolated';
export const ModePooled = 'pooled';

// Anything not in the generated format is unregistered — no lookup needed.
export function validFormat(key) {
  return KEY_FORMAT.test(key);
}

export function hashKey(key) {
  return createHash('sha256').update(key).digest('hex');
}

// PLAN_LIMITS, e.g. "free=100,paid=1000" -> Map { free => 100, paid => 1000 }
export function parsePlans(raw) {
  const plans = new Map();
  for (const item of raw.split(',').map((s) => s.trim()).filter(Boolean)) {
    const [name, value] = item.split('=').map((s) => s?.trim());
    const limit = Number(value);
    if (!name || !Number.isInteger(limit) || limit <= 0) {
      throw new Error(`PLAN_LIMITS: "${item}" is not name=positive-integer`);
    }
    plans.set(name, limit);
  }
  if (plans.size === 0) throw new Error('PLAN_LIMITS: no plans defined');
  return plans;
}

// Identity for a registered key, or null if the tenant's plan is unknown.
export function identityFor(plans, tenant, keyHash) {
  const base = plans.get(tenant.plan);
  if (base === undefined) return null;
  if (tenant.mode === ModeIsolated) {
    return { id: `key:${keyHash.slice(0, 16)}`, limit: base };
  }
  return { id: `tenant:${tenant.id}`, limit: base * Math.max(1, tenant.keys) };
}

// createResolver caches lookups (including "unregistered") for ttlMs, holds
// at most `size` entries (oldest evicted first), and shares one in-flight
// lookup between concurrent misses on the same key.
//
// lookup(hash) resolves to [tenant, plan, keys, mode], to [] when the key is
// unregistered, and rejects when the lookup itself failed.
export function createResolver({ lookup, plans, ttlMs, size }) {
  const cache = new Map(); // hash -> { identity, expires }; insertion-ordered
  const inflight = new Map(); // hash -> Promise<identity|null>

  async function fetchIdentity(hash) {
    const fields = await lookup(hash);
    if (!fields || fields.length === 0) return null;
    const [id, plan, keys, mode] = fields;
    const identity = identityFor(plans, { id, plan, keys: Number(keys) || 1, mode }, hash);
    if (!identity) console.warn(`[Registry] Tenant "${id}" has unknown plan "${plan}"; using anonymous limit`);
    return identity;
  }

  function store(hash, identity) {
    if (!cache.has(hash)) {
      while (cache.size >= size) cache.delete(cache.keys().next().value);
    }
    cache.set(hash, { identity, expires: Date.now() + ttlMs });
  }

  // Returns the identity for a presented key, or null if the caller should
  // fall back to IP identity (malformed, unregistered, unknown plan, or the
  // lookup failed — failures are not cached, so the next request retries).
  return async function resolve(key) {
    if (!validFormat(key)) return null;
    const hash = hashKey(key);

    const hit = cache.get(hash);
    if (hit && hit.expires > Date.now()) return hit.identity;

    let pending = inflight.get(hash);
    if (!pending) {
      pending = fetchIdentity(hash).finally(() => inflight.delete(hash));
      inflight.set(hash, pending);
    }

    try {
      const identity = await pending;
      store(hash, identity);
      return identity;
    } catch {
      return null;
    }
  };
}
