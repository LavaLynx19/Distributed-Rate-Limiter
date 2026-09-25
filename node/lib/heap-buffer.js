// The gateway's loose-mode buffer: lib/buffer-core.js wired to the real
// Redis write-back and configuration.
import { writeBack } from './sliding-window-counter.js';
import { config } from './config.js';
import { createBuffer } from './buffer-core.js';

const buffer = createBuffer({
  writeBack,
  settings: {
    windowMs: config.rateLimiter.windowDurationSec * 1000,
    batchThreshold: config.looseMode.batchThreshold,
    refreshIntervalMs: config.looseMode.refreshIntervalMs,
    flushIntervalMs: config.looseMode.flushIntervalMs,
  },
});

export const admitLocal = buffer.admit;
export const startFlushLoop = buffer.startFlushLoop;
export const stopFlushLoop = buffer.stopFlushLoop;
export const drain = buffer.drain;
export const getBufferStats = buffer.stats;
