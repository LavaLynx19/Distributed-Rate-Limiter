import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import Redis from 'ioredis';
import { config } from './config.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const luaScript = readFileSync(join(__dirname, '..', 'lua', 'sliding_window.lua'), 'utf-8');
const tokenBucketLua = readFileSync(join(__dirname, '..', 'lua', 'token_bucket.lua'), 'utf-8');
const leakyBucketLua = readFileSync(join(__dirname, '..', 'lua', 'leaky_bucket.lua'), 'utf-8');

const redis = new Redis({
  host: config.redis.host,
  port: config.redis.port,
  commandTimeout: config.redis.commandTimeout,
  maxRetriesPerRequest: 1,
  retryStrategy(times) {
    if (times > 3) return null; // stop retrying after 3 attempts
    return Math.min(times * 200, 2000); // exponential backoff: 200, 400, 800ms
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
