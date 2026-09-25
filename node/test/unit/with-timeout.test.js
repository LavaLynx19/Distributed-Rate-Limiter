import { test } from 'node:test';
import assert from 'node:assert/strict';
import { withTimeout } from '../../lib/with-timeout.js';

test('resolves when the promise wins', async () => {
  assert.equal(await withTimeout(Promise.resolve('ok'), 50, 'op'), 'ok');
});

test('rejects with the label when the budget is exceeded', async () => {
  await assert.rejects(withTimeout(new Promise(() => {}), 5, 'Key lookup'), /Key lookup timed out/);
});

test('clears its timer, so nothing keeps the event loop alive', async () => {
  // Without clearTimeout this 60s timer would hold the process open and the
  // test runner would report the test as still running.
  await withTimeout(Promise.resolve(), 60_000, 'op');
  assert.ok(true);
});
