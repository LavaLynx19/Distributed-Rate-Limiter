import { Router } from 'express';
import { strictRateLimiter } from '../lib/strict-middleware.js';
import { isRedisHealthy } from '../lib/redis-client.js';
import { getBufferStats } from '../lib/heap-buffer.js';

const router = Router();

// Strict rate-limited endpoint
router.get('/api/strict/resource', strictRateLimiter(), (req, res) => {
  res.json({ message: 'Strict-mode resource', timestamp: Date.now() });
});

// Health check (no rate limiting)
router.get('/api/open/health', (req, res) => {
  res.json({ status: 'ok', redis: isRedisHealthy() ? 'connected' : 'disconnected' });
});

// Buffer stats for debugging loose mode (no rate limiting)
router.get('/api/open/stats', (req, res) => {
  res.json(getBufferStats());
});

export default router;
