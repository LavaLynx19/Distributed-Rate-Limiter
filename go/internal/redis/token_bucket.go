package redis

import "context"

// CheckTokenBucket consumes one token, refilling first based on elapsed time.
func (c *Client) CheckTokenBucket(ctx context.Context, identifier string) Decision {
	return c.run(ctx, "TokenBucket", c.tokenBucket, identifier,
		c.cfg.TokenBucket.Capacity, c.cfg.TokenBucket.RefillRate)
}
