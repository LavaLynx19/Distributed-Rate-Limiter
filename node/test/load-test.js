import autocannon from 'autocannon';
import { parsePlans } from '../lib/registry.js';
import { provisionKeys, cleanupKeys } from './provision.js';

const BASE_URL = process.env.TEST_URL || 'http://localhost:3000';

// Every test runs as its own registered tenant, on the 'free' plan, so no
// test inherits another's usage. Unregistered keys would all collapse onto
// the client IP (ARCHITECTURE.md section 10). A per-run nonce lets the suite
// be re-run without flushing Redis. Keep PLAN_LIMITS in step with the gateway.
const PLAN = 'free';
const MAX_LIMIT = parsePlans(process.env.PLAN_LIMITS || 'free=100,paid=1000').get(PLAN);
const RUN_ID = Date.now().toString(36);
const TENANT_PREFIX = `load-test-${RUN_ID}`;
const TESTS = ['strict-correctness', 'strict-concurrency', 'loose-throughput', 'token-bucket', 'leaky-bucket'];
let keys = {};

function identity(test) {
  return { 'x-api-key': keys[test] };
}

function run(opts) {
  return new Promise((resolve, reject) => {
    const instance = autocannon(opts, (err, result) => {
      if (err) return reject(err);
      resolve(result);
    });
    autocannon.track(instance, { renderProgressBar: true });
  });
}

async function strictCorrectnessTest() {
  console.log('\n=== Test 1: Strict Mode Correctness ===');
  console.log(`Sending 150 sequential requests (limit=${MAX_LIMIT})...\n`);

  const result = await run({
    url: `${BASE_URL}/api/strict/resource`,
    connections: 1,
    amount: 150,
    pipelining: 1,
    headers: identity('strict-correctness'),
  });

  const ok = result['2xx'];
  const blocked = result['4xx'];
  console.log(`\n  2xx responses: ${ok} (expected: ${MAX_LIMIT})`);
  console.log(`  4xx responses: ${blocked} (expected: ${150 - MAX_LIMIT})`);
  console.log(`  Result: ${ok === MAX_LIMIT && blocked === 150 - MAX_LIMIT ? 'PASS' : 'FAIL'}`);
}

async function strictConcurrencyTest() {
  console.log('\n=== Test 2: Strict Mode Concurrency ===');
  console.log('Sending 200 requests across 10 connections...\n');

  // Fresh identifier: exactly MAX_LIMIT must be admitted. Fewer means lost
  // updates, more means over-admission — either is an atomicity failure.
  const result = await run({
    url: `${BASE_URL}/api/strict/resource`,
    connections: 10,
    amount: 200,
    pipelining: 1,
    headers: identity('strict-concurrency'),
  });

  const ok = result['2xx'];
  const blocked = result['4xx'];
  const total = ok + blocked;
  console.log(`\n  2xx responses: ${ok} (expected: ${MAX_LIMIT})`);
  console.log(`  4xx responses: ${blocked} (expected: ${200 - MAX_LIMIT})`);
  console.log(`  Total: ${total} (expected: 200)`);
  console.log(`  Lua atomicity: ${ok === MAX_LIMIT && total === 200 ? 'PASS — exactly the limit admitted under concurrency' : 'FAIL'}`);
}

async function looseThroughputTest() {
  console.log('\n=== Test 3: Loose Mode Throughput ===');
  console.log('50 connections for 10 seconds...\n');

  const result = await run({
    url: `${BASE_URL}/api/loose/resource`,
    connections: 50,
    duration: 10,
    pipelining: 10,
    headers: identity('loose-throughput'),
  });

  const rps = Math.round(result.requests.average);
  const p99 = result.latency.p99;
  const admitted = result['2xx'];
  console.log(`\n  Avg RPS: ${rps}`);
  console.log(`  p99 Latency: ${p99}ms`);
  console.log(`  Latency target (<5ms): ${p99 < 5 ? 'PASS' : 'ABOVE TARGET'}`);
  console.log(`  Admitted: ${admitted} of ${admitted + result['4xx']} (limit: ${MAX_LIMIT})`);
  console.log(`  Local guard: ${admitted <= MAX_LIMIT ? 'PASS — no over-admission' : 'FAIL — over-admitted requests'}`);
}

async function tokenBucketCorrectnessTest() {
  console.log('\n=== Test 4: Token Bucket Correctness ===');
  console.log('Sending 15 sequential requests (capacity=10, refill=1/sec)...\n');

  const result = await run({
    url: `${BASE_URL}/api/token-bucket/resource`,
    connections: 1,
    amount: 15,
    pipelining: 1,
    headers: identity('token-bucket'),
  });

  const ok = result['2xx'];
  const blocked = result['4xx'];
  console.log(`\n  2xx responses: ${ok} (expected: 10)`);
  console.log(`  4xx responses: ${blocked} (expected: 5)`);
  console.log(`  Result: ${ok === 10 && blocked === 5 ? 'PASS' : 'CHECK — counts may vary based on refill timing'}`);
}

async function tokenBucketRefillTest() {
  console.log('\n=== Test 5: Token Bucket Refill ===');
  console.log('Waiting 5s for tokens to refill, then sending 5 requests...\n');

  await new Promise(r => setTimeout(r, 5000));

  const result = await run({
    url: `${BASE_URL}/api/token-bucket/resource`,
    connections: 1,
    amount: 5,
    pipelining: 1,
    headers: identity('token-bucket'),
  });

  const ok = result['2xx'];
  console.log(`\n  2xx responses: ${ok} (expected: 5 — tokens refilled)`);
  console.log(`  Result: ${ok === 5 ? 'PASS' : 'CHECK — refill may still be catching up'}`);
}

async function leakyBucketCorrectnessTest() {
  console.log('\n=== Test 6: Leaky Bucket Correctness ===');
  console.log('Sending 15 sequential requests (capacity=10, leak=1/sec)...\n');

  const result = await run({
    url: `${BASE_URL}/api/leaky-bucket/resource`,
    connections: 1,
    amount: 15,
    pipelining: 1,
    headers: identity('leaky-bucket'),
  });

  const ok = result['2xx'];
  const blocked = result['4xx'];
  console.log(`\n  2xx responses: ${ok} (expected: 10)`);
  console.log(`  4xx responses: ${blocked} (expected: 5)`);
  console.log(`  Result: ${ok === 10 && blocked === 5 ? 'PASS' : 'CHECK — counts may vary based on drain timing'}`);
}

async function leakyBucketDrainTest() {
  console.log('\n=== Test 7: Leaky Bucket Drain ===');
  console.log('Waiting 5s for bucket to drain, then sending 5 requests...\n');

  await new Promise(r => setTimeout(r, 5000));

  const result = await run({
    url: `${BASE_URL}/api/leaky-bucket/resource`,
    connections: 1,
    amount: 5,
    pipelining: 1,
    headers: identity('leaky-bucket'),
  });

  const ok = result['2xx'];
  console.log(`\n  2xx responses: ${ok} (expected: 5 — bucket drained)`);
  console.log(`  Result: ${ok === 5 ? 'PASS' : 'CHECK — drain may still be catching up'}`);
}

async function main() {
  console.log('Distributed Rate Limiter — Load Tests');
  console.log(`Target: ${BASE_URL}`);
  console.log('Make sure the server is running and Redis is connected.\n');
  console.log(`Run ID: ${RUN_ID} — each test runs as its own registered tenant (plan '${PLAN}', limit ${MAX_LIMIT}).\n`);

  try {
    keys = await provisionKeys(TENANT_PREFIX, TESTS, PLAN);
    await strictCorrectnessTest();
    await strictConcurrencyTest();
    await looseThroughputTest();
    await tokenBucketCorrectnessTest();
    await tokenBucketRefillTest();
    await leakyBucketCorrectnessTest();
    await leakyBucketDrainTest();
  } catch (err) {
    console.error('Test failed:', err.message);
    process.exit(1);
  } finally {
    await cleanupKeys(TENANT_PREFIX, keys).catch((err) => console.warn('Cleanup failed:', err.message));
  }

  console.log('\n=== Test 8: Fail-Open (manual) ===');
  console.log('To test fail-open: stop Redis, then run:');
  console.log(`  curl -s -o /dev/null -w "%{http_code}" ${BASE_URL}/api/strict/resource`);
  console.log(`  curl -s -o /dev/null -w "%{http_code}" ${BASE_URL}/api/token-bucket/resource`);
  console.log(`  curl -s -o /dev/null -w "%{http_code}" ${BASE_URL}/api/leaky-bucket/resource`);
  console.log('Expected: 200 (fail-open) for all endpoints\n');

  process.exit(0);
}

main();
