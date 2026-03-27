import { Router } from 'express';
import { tokenBucketRateLimiter } from '../lib/token-bucket-middleware.js';

const router = Router();

router.get('/api/token-bucket/resource', tokenBucketRateLimiter(), (req, res) => {
  res.json({ message: 'Token bucket resource', timestamp: Date.now() });
});

export default router;
