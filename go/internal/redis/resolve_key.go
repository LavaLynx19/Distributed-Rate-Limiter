package redis

import (
	"context"
	"errors"
	"fmt"
)

var errUnavailable = errors.New("redis unavailable")

// ResolveKey fetches the tenant record for a key hash under the same latency
// budget as a rate-limit check. It returns nil for an unregistered key and an
// error when the lookup itself failed, so the caller can tell "not
// registered" (cacheable) from "couldn't ask" (not cacheable).
func (c *Client) ResolveKey(ctx context.Context, keyHash string) ([]string, error) {
	if !c.IsHealthy() {
		return nil, errUnavailable
	}

	runCtx, cancel := context.WithTimeout(ctx, c.cfg.Redis.CommandTimeout)
	defer cancel()

	raw, err := c.resolveKey.Run(runCtx, c.rdb, []string{keyHash}).StringSlice()
	if err != nil {
		return nil, fmt.Errorf("resolving key: %w", err)
	}
	return raw, nil
}
