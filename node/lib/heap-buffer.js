import { checkRateLimit } from './sliding-window-counter.js';
import { config } from './config.js';

const buffer = new Map(); // identifier -> { count, lastFlush, blocked }
let flushInterval = null;

export function incrementLocal(identifier) {
  let entry = buffer.get(identifier);
  if (!entry) {
    entry = { count: 0, lastFlush: Date.now(), blocked: false };
    buffer.set(identifier, entry);
  }
  entry.count += 1;

  // Trigger immediate flush if batch threshold reached
  if (entry.count >= config.looseMode.batchThreshold) {
    flushIdentifier(identifier, entry);
  }

  return { localCount: entry.count, blocked: entry.blocked };
}

async function flushIdentifier(identifier, entry) {
  const countToFlush = entry.count;
  if (countToFlush === 0) return;

  // Subtract before async call to preserve increments arriving during flush
  entry.count -= countToFlush;
  entry.lastFlush = Date.now();

  const result = await checkRateLimit(identifier, countToFlush);
  entry.blocked = !result.allowed && !result.failedOpen;
}

async function flushAll() {
  const now = Date.now();
  const staleThreshold = config.rateLimiter.windowDurationSec * 1000;

  for (const [identifier, entry] of buffer) {
    // Prune stale entries
    if (entry.count === 0 && (now - entry.lastFlush) > staleThreshold) {
      buffer.delete(identifier);
      continue;
    }

    if (entry.count > 0) {
      flushIdentifier(identifier, entry);
    }
  }
}

export function startFlushLoop() {
  if (flushInterval) return;
  flushInterval = setInterval(flushAll, config.looseMode.flushIntervalMs);
  // Allow process to exit even if interval is active
  flushInterval.unref();
}

export function stopFlushLoop() {
  if (flushInterval) {
    clearInterval(flushInterval);
    flushInterval = null;
  }
}

export function getBufferStats() {
  return {
    identifiers: buffer.size,
    entries: [...buffer].map(([id, e]) => ({
      identifier: id,
      pendingCount: e.count,
      blocked: e.blocked,
    })),
  };
}
