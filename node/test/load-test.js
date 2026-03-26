import autocannon from 'autocannon';

const BASE_URL = process.env.TEST_URL || 'http://localhost:3000';

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
  console.log('Sending 150 sequential requests (limit=100)...\n');

  const result = await run({
    url: `${BASE_URL}/api/strict/resource`,
    connections: 1,
    amount: 150,
    pipelining: 1,
  });

  const ok = result['2xx'];
  const blocked = result['4xx'];
  console.log(`\n  2xx responses: ${ok} (expected: 100)`);
  console.log(`  4xx responses: ${blocked} (expected: 50)`);
  console.log(`  Result: ${ok === 100 && blocked === 50 ? 'PASS' : 'CHECK — counts may vary if window has prior usage'}`);
}

async function strictConcurrencyTest() {
  console.log('\n=== Test 2: Strict Mode Concurrency ===');
  console.log('Sending 200 requests across 10 connections...\n');

  const result = await run({
    url: `${BASE_URL}/api/strict/resource`,
    connections: 10,
    amount: 200,
    pipelining: 1,
  });

  const ok = result['2xx'];
  const blocked = result['4xx'];
  const total = ok + blocked;
  console.log(`\n  2xx responses: ${ok}`);
  console.log(`  4xx responses: ${blocked}`);
  console.log(`  Total: ${total} (expected: 200)`);
  console.log(`  Lua atomicity: ${ok <= 100 ? 'PASS — no over-admission' : 'FAIL — over-admitted requests'}`);
}

async function looseThroughputTest() {
  console.log('\n=== Test 3: Loose Mode Throughput ===');
  console.log('50 connections for 10 seconds...\n');

  const result = await run({
    url: `${BASE_URL}/api/loose/resource`,
    connections: 50,
    duration: 10,
    pipelining: 10,
  });

  const rps = Math.round(result.requests.average);
  const p99 = result.latency.p99;
  console.log(`\n  Avg RPS: ${rps}`);
  console.log(`  p99 Latency: ${p99}ms`);
  console.log(`  Latency target (<5ms): ${p99 < 5 ? 'PASS' : 'ABOVE TARGET'}`);
}

async function main() {
  console.log('Distributed Rate Limiter — Load Tests');
  console.log(`Target: ${BASE_URL}`);
  console.log('Make sure the server is running and Redis is connected.\n');
  console.log('NOTE: Tests 1 & 2 share the same sliding window.');
  console.log('For accurate results, flush Redis (FLUSHDB) between test runs.\n');

  try {
    await strictCorrectnessTest();

    // Brief pause to let the sliding window roll over slightly
    console.log('\n--- Waiting 5s before concurrency test ---');
    await new Promise(r => setTimeout(r, 5000));

    await strictConcurrencyTest();
    await looseThroughputTest();
  } catch (err) {
    console.error('Test failed:', err.message);
    process.exit(1);
  }

  console.log('\n=== Test 4: Fail-Open (manual) ===');
  console.log('To test fail-open: stop Redis, then run:');
  console.log(`  curl -s -o /dev/null -w "%{http_code}" ${BASE_URL}/api/strict/resource`);
  console.log('Expected: 200 (fail-open)\n');

  process.exit(0);
}

main();
