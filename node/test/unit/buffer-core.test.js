import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createBuffer } from '../../lib/buffer-core.js';

// A tiny stand-in for the Lua script's writeback mode: every batch is
// recorded and the reply carries the cluster-wide remaining capacity.
function fakeRedis(limitTotal = 0) {
  const redis = {
    total: limitTotal,
    batches: [],
    calls: 0,
    failOpen: false,
    async writeBack(_id, limit, batch) {
      redis.calls += 1;
      if (redis.failOpen) return { allowed: true, remaining: -1, resetTtl: 0, failedOpen: true };
      if (batch > 0) redis.batches.push(batch);
      redis.total += batch;
      const remaining = limit - redis.total;
      return remaining > 0
        ? { allowed: true, remaining, resetTtl: 60, failedOpen: false }
        : { allowed: false, remaining: 0, resetTtl: 60, failedOpen: false };
    },
  };
  return redis;
}

function setup({ redis = fakeRedis(), start = 1_000_000 } = {}) {
  const clock = { t: start };
  const buf = createBuffer({
    writeBack: redis.writeBack,
    settings: { windowMs: 300_000, batchThreshold: 50, refreshIntervalMs: 5000, flushIntervalMs: 500 },
    now: () => clock.t,
  });
  return { buf, redis, clock };
}

function admitN(buf, id, n, limit = 100) {
  let admitted = 0;
  for (let i = 0; i < n; i++) if (buf.admit(id, limit).admitted) admitted += 1;
  return admitted;
}

const settle = () => new Promise((resolve) => setImmediate(resolve));

test('admits exactly the limit and never charges rejected requests', async () => {
  const { buf, redis } = setup();
  assert.equal(admitN(buf, 'flooder', 150), 100);
  await buf.drain();
  assert.equal(redis.total, 100);
});

test('a threshold flush records one batch and syncs the allowance', async () => {
  const { buf, redis } = setup();
  admitN(buf, 'user', 50);
  await settle();
  assert.deepEqual(redis.batches, [50]);
  assert.equal(buf.stats().entries[0].allowance, 50);
});

test('learns other instances usage at the first sync and records every served request', async () => {
  const { buf, redis } = setup({ redis: fakeRedis(90) });
  let admitted = admitN(buf, 'shared', 50); // unknown usage: one batch before the sync
  await settle();
  admitted += admitN(buf, 'shared', 50); // after the sync: nothing left
  await buf.drain();
  assert.equal(admitted, 50);
  assert.equal(redis.total, 140);
});

test('an exhausted identifier refreshes after the interval and unblocks', async () => {
  const { buf, redis, clock } = setup();
  admitN(buf, 'user', 100);
  await buf.drain();
  assert.equal(buf.admit('user', 100).admitted, false);

  const callsBefore = redis.calls;
  await Promise.all(buf.flushAll());
  assert.equal(redis.calls, callsBefore, 'refreshed before the interval');

  redis.total = 30; // segments expired in Redis
  clock.t += 5000;
  await Promise.all(buf.flushAll());
  assert.equal(buf.admit('user', 100).admitted, true);
  assert.equal(redis.batches.length, 2, 'a refresh must not record anything');
});

test('a fail-open flush keeps its batch for the next attempt', async () => {
  const redis = fakeRedis();
  redis.failOpen = true;
  const { buf } = setup({ redis });
  admitN(buf, 'user', 50);
  await settle();
  assert.equal(buf.stats().entries[0].pendingCount, 50);
});

test('with Redis down for a whole window the allowance resets (fail-open)', () => {
  const redis = fakeRedis();
  redis.failOpen = true;
  const { buf, clock } = setup({ redis });
  assert.equal(admitN(buf, 'user', 150), 100, 'the local guard holds without Redis');
  clock.t += 300_001;
  assert.equal(buf.admit('user', 100).admitted, true);
});

test('drain writes counts below the batch threshold', async () => {
  const { buf, redis } = setup();
  admitN(buf, 'user', 30);
  await buf.drain();
  assert.deepEqual(redis.batches, [30]);
});

test('a plan change shifts the allowance by the difference', () => {
  const { buf } = setup();
  admitN(buf, 'tenant:acme', 30);
  assert.deepEqual(buf.admit('tenant:acme', 1000), { admitted: true, remaining: 969 });
  assert.deepEqual(buf.admit('tenant:acme', 100), { admitted: true, remaining: 68 });
});

test('the batch threshold scales down for small limits', async () => {
  const { buf, redis } = setup();
  for (let i = 0; i < 5; i++) buf.admit('small', 10);
  await settle();
  assert.deepEqual(redis.batches, [5]);
});

test('Retry-After is 1s before a BLOCKED reply, then the precise wait', async () => {
  const { buf, clock } = setup();
  admitN(buf, 'user', 100); // second batch still pending
  assert.deepEqual(buf.admit('user', 100), { admitted: false, remaining: 0, retryAfter: 1 });

  await buf.drain(); // second batch: total 100 -> BLOCKED, resetTtl 60
  clock.t += 10_000;
  assert.equal(buf.admit('user', 100).retryAfter, 50);
});

test('admits made while a write-back is in flight stay deducted', async () => {
  const { buf } = setup();
  admitN(buf, 'user', 60); // flush of 50 starts at the 50th; 10 more admitted meanwhile
  await settle(); // Redis replies: remaining 50, which doesn't include those 10
  const entry = buf.stats().entries[0];
  assert.equal(entry.pendingCount, 10);
  assert.equal(entry.allowance, 40);
});
