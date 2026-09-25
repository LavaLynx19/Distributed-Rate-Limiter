import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validFormat, hashKey, parsePlans, identityFor, createResolver, ModeIsolated, ModePooled } from '../../lib/registry.js';

const KEY = `rlk_${'a'.repeat(32)}`;
const OTHER = `rlk_${'b'.repeat(32)}`;
const plans = parsePlans('free=100,paid=1000');

test('key format', () => {
  assert.equal(validFormat(KEY), true);
  for (const bad of ['', 'rlk_short', `xyz_${'a'.repeat(32)}`, `rlk_${'a'.repeat(31)}!`, `rlk_${'a'.repeat(33)}`]) {
    assert.equal(validFormat(bad), false, bad);
  }
});

test('hash is SHA-256 (FIPS 180-2 test vector)', () => {
  assert.equal(hashKey('abc'), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
});

test('parsePlans accepts tiers and rejects garbage', () => {
  assert.deepEqual([...parsePlans(' free=100 , paid=1000 ')], [['free', 100], ['paid', 1000]]);
  for (const bad of ['', 'free', 'free=0', 'free=-5', 'free=abc']) {
    assert.throws(() => parsePlans(bad), undefined, bad);
  }
});

test('identityFor: isolated, pooled, free, unknown plan', () => {
  const hash = hashKey(KEY);
  assert.deepEqual(identityFor(plans, { id: 'acme', plan: 'paid', keys: 10, mode: ModeIsolated }, hash), { id: `key:${hash.slice(0, 16)}`, limit: 1000 });
  assert.deepEqual(identityFor(plans, { id: 'acme', plan: 'paid', keys: 10, mode: ModePooled }, hash), { id: 'tenant:acme', limit: 10000 });
  assert.deepEqual(identityFor(plans, { id: 'hobby', plan: 'free', keys: 0, mode: ModePooled }, hash), { id: 'tenant:hobby', limit: 100 });
  assert.equal(identityFor(plans, { id: 'x', plan: 'gold', keys: 1, mode: ModePooled }, hash), null);
});

function lookupWith(records, { fail = false, delayMs = 0 } = {}) {
  const lookup = async (hash) => {
    lookup.calls += 1;
    if (delayMs) await new Promise((r) => setTimeout(r, delayMs));
    if (lookup.fail) throw new Error('redis down');
    return records.get(hash) ?? [];
  };
  lookup.calls = 0;
  lookup.fail = fail;
  return lookup;
}

test('caches registered and unregistered results', async () => {
  const lookup = lookupWith(new Map([[hashKey(KEY), ['acme', 'paid', '3', ModePooled]]]));
  const resolve = createResolver({ lookup, plans, ttlMs: 60_000, size: 100 });
  for (let i = 0; i < 3; i++) assert.deepEqual(await resolve(KEY), { id: 'tenant:acme', limit: 3000 });
  for (let i = 0; i < 3; i++) assert.equal(await resolve(OTHER), null);
  assert.equal(lookup.calls, 2);
});

test('malformed keys never reach Redis', async () => {
  const lookup = lookupWith(new Map());
  const resolve = createResolver({ lookup, plans, ttlMs: 60_000, size: 100 });
  assert.equal(await resolve('made-up-key'), null);
  assert.equal(lookup.calls, 0);
});

test('failed lookups are not cached', async () => {
  const lookup = lookupWith(new Map([[hashKey(KEY), ['acme', 'free', '1', ModePooled]]]), { fail: true });
  const resolve = createResolver({ lookup, plans, ttlMs: 60_000, size: 100 });
  assert.equal(await resolve(KEY), null);
  lookup.fail = false;
  assert.deepEqual(await resolve(KEY), { id: 'tenant:acme', limit: 100 });
});

test('entries expire after the TTL (revocation)', async () => {
  const records = new Map([[hashKey(KEY), ['acme', 'free', '1', ModePooled]]]);
  const resolve = createResolver({ lookup: lookupWith(records), plans, ttlMs: 20, size: 100 });
  await resolve(KEY);
  records.delete(hashKey(KEY));
  assert.notEqual(await resolve(KEY), null, 'still cached within the TTL');
  await new Promise((r) => setTimeout(r, 30));
  assert.equal(await resolve(KEY), null, 'revoked key honored after the TTL');
});

test('cache size is bounded', async () => {
  const lookup = lookupWith(new Map());
  const resolve = createResolver({ lookup, plans, ttlMs: 60_000, size: 10 });
  for (let i = 0; i < 50; i++) await resolve(`rlk_${String(i).padStart(32, '0')}`);
  lookup.calls = 0;
  await resolve(`rlk_${String(0).padStart(32, '0')}`); // oldest: evicted
  await resolve(`rlk_${String(49).padStart(32, '0')}`); // newest: cached
  assert.equal(lookup.calls, 1);
});

test('concurrent misses on one key share a single lookup', async () => {
  const lookup = lookupWith(new Map([[hashKey(KEY), ['acme', 'free', '1', ModePooled]]]), { delayMs: 20 });
  const resolve = createResolver({ lookup, plans, ttlMs: 60_000, size: 100 });
  const results = await Promise.all(Array.from({ length: 50 }, () => resolve(KEY)));
  assert.ok(results.every((r) => r?.id === 'tenant:acme'));
  assert.equal(lookup.calls, 1);
});
