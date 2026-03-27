import { Router } from 'express';
import { leakyBucketRateLimiter } from '../lib/leaky-bucket-middleware.js';

const router = Router();

router.get('/api/leaky-bucket/resource', leakyBucketRateLimiter(), (req, res) => {
  res.json({ message: 'Leaky bucket resource', timestamp: Date.now() });
});

export default router;
