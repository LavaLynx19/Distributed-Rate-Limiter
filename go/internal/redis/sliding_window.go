package redis

import "context"

// CheckSlidingWindow consumes batchCount of the identity's limit from the 5-minute rolling
// window, rejecting without recording if they don't fit. Strict mode uses it
// with a batch of 1: the request has not been served yet.
func (c *Client) CheckSlidingWindow(ctx context.Context, identifier string, limit, batchCount int64) Decision {
	return c.run(ctx, "RateLimiter", c.slidingWindow, identifier, limit, batchCount, "check")
}

// WriteBack records requests a loose-mode buffer has already served, even if
// they push the window over the limit, and reports the cluster-wide capacity
// left. A batch of 0 only refreshes that figure.
func (c *Client) WriteBack(ctx context.Context, identifier string, limit, batchCount int64) Decision {
	return c.run(ctx, "WriteBack", c.slidingWindow, identifier, limit, batchCount, "writeback")
}
