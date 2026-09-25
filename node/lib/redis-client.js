import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import Redis from 'ioredis';
import { config } from './config.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const luaScript = readFileSync(join(__dirname, '..', '..', 'lua', 'sliding_window.lua'), 'utf-8');
const tokenBucketLua = readFileSync(join(__dirname, '..', '..', 'lua', 'token_bucket.lua'), 'utf-8');
const leakyBucketLua = readFileSync(join(__dirname, '..', '..', 'lua', 'leaky_bucket.lua'), 'utf-8');
const resolveKeyLua = readFileSync(join(__dirname, '..', '..', 'lua', 'resolve_key.lua'), 'utf-8');

const redis = new Redis({
  host: config.redis.host,
  port: config.redis.port,
  commandTimeout: config.redis.commandTimeout,
  maxRetriesPerRequest: 1,
  // Never give up: returning null here closes the client for good, which
  // would leave the gateway failing open until a restart. isRedisHealthy()
  // gates requests while a reconnect is pending.
  retryStrategy(times) {
    return Math.min(times * 200, 2000); // linear backoff, capped at 2s
  },
  lazyConnect: true,
});

// Register Lua scripts as custom commands — ioredis handles EVALSHA/EVAL fallback
redis.defineCommand('rateLimitCheck', {
  numberOfKeys: 1,
  lua: luaScript,
});

redis.defineCommand('tokenBucketCheck', {
  numberOfKeys: 1,
  lua: tokenBucketLua,
});

redis.defineCommand('leakyBucketCheck', {
  numberOfKeys: 1,
  lua: leakyBucketLua,
});

redis.defineCommand('resolveKey', {
  numberOfKeys: 1,
  lua: resolveKeyLua,
});

redis.on('error', (err) => {
  console.error('[Redis] Connection error:', err.message);
});

redis.on('connect', () => {
  console.log('[Redis] Connected successfully');
});

export function isRedisHealthy() {
  return redis.status === 'ready';
}

export { redis };
