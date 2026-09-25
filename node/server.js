import dotenv from 'dotenv';
dotenv.config();
import express from 'express';
import { config } from './lib/config.js';
import { redis } from './lib/redis-client.js';
import { startFlushLoop, stopFlushLoop, drain } from './lib/heap-buffer.js';
import sampleApi from './routes/sample-api.js';
import looseApi from './routes/loose-api.js';
import tokenBucketApi from './routes/token-bucket-api.js';
import leakyBucketApi from './routes/leaky-bucket-api.js';

const app = express();

app.set('trust proxy', config.server.trustProxy);

app.use(sampleApi);
app.use(looseApi);
app.use(tokenBucketApi);
app.use(leakyBucketApi);

async function start() {
  try {
    await redis.connect();
  } catch (err) {
    console.warn('[Server] Redis unavailable at startup — running in fail-open mode:', err.message);
  }

  startFlushLoop();

  const server = app.listen(config.server.port, () => {
    console.log(`[Server] Listening on port ${config.server.port}`);
  });

  // Stop taking requests first, then write the loose-mode counts still held
  // in memory, and only then drop the Redis connection.
  function shutdown() {
    console.log('[Server] Shutting down gracefully...');
    server.close(async () => {
      stopFlushLoop();
      try {
        await drain();
        await redis.quit();
        process.exit(0);
      } catch (err) {
        console.error('[Server] Shutdown error:', err.message);
        process.exit(1);
      }
    });
  }

  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);
}

start();
