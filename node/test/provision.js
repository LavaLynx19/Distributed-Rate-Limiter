// Provisions throwaway registry tenants and keys for the load-test harness.
// Since the API-key registry (ARCHITECTURE.md section 10), unregistered keys
// fall back to IP identity, so tests need real keys to stay isolated from
// each other. Writes the same schema as go/cmd/keyctl.
import { randomInt } from 'node:crypto';
import Redis from 'ioredis';
import { hashKey } from '../lib/registry.js';

const BASE62 = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';

function generateKey() {
  let body = '';
  for (let i = 0; i < 32; i++) body += BASE62[randomInt(BASE62.length)];
  return `rlk_${body}`;
}

function connect() {
  return new Redis({
    host: process.env.REDIS_HOST || '127.0.0.1',
    port: parseInt(process.env.REDIS_PORT, 10) || 6379,
    lazyConnect: true,
  });
}

// Creates one pooled tenant with one key per name; returns name -> key.
export async function provisionKeys(prefix, names, plan) {
  const redis = connect();
  await redis.connect();
  try {
    const keys = {};
    const tx = redis.multi();
    for (const name of names) {
      const tenant = `${prefix}-${name}`;
      const key = generateKey();
      const hash = hashKey(key);
      tx.hset(`tenant::${tenant}`, 'plan', plan, 'keys', 1, 'mode', 'pooled');
      tx.hset(`apikey::${hash}`, 'tenant', tenant);
      tx.sadd(`tenant::${tenant}::keys`, hash);
      keys[name] = key;
    }
    await tx.exec();
    return keys;
  } finally {
    redis.disconnect();
  }
}

// Removes everything provisionKeys created for this prefix.
export async function cleanupKeys(prefix, keys) {
  const redis = connect();
  await redis.connect();
  try {
    const tx = redis.multi();
    for (const [name, key] of Object.entries(keys)) {
      const tenant = `${prefix}-${name}`;
      tx.del(`apikey::${hashKey(key)}`, `tenant::${tenant}`, `tenant::${tenant}::keys`);
    }
    await tx.exec();
  } finally {
    redis.disconnect();
  }
}
