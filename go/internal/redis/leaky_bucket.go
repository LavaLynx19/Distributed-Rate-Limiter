package redis

import "context"

// CheckLeakyBucket adds one unit of water, draining first based on elapsed time.
func (c *Client) CheckLeakyBucket(ctx context.Context, identifier string) Decision {
	return c.run(ctx, "LeakyBucket", c.leakyBucket, identifier,
		c.cfg.LeakyBucket.Capacity, c.cfg.LeakyBucket.LeakRate)
}
