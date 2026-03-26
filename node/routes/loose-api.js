import { Router } from 'express';
import { looseRateLimiter } from '../lib/loose-middleware.js';

const router = Router();

// Loose rate-limited endpoint
router.get('/api/loose/resource', looseRateLimiter(), (req, res) => {
  res.json({ message: 'Loose-mode resource', timestamp: Date.now() });
});

// Burst traffic simulation endpoint
router.get('/api/loose/burst', looseRateLimiter(), (req, res) => {
  res.json({ message: 'Loose-mode burst endpoint', timestamp: Date.now() });
});

export default router;
