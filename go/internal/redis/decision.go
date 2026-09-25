package redis

import (
	"context"
	"fmt"
	"log"

	goredis "github.com/redis/go-redis/v9"
)

// Decision is the outcome of one rate-limit check, shared by all three
// algorithms. FailedOpen distinguishes "Redis said yes" from "Redis could not
// answer in time, so we allowed it anyway".
type Decision struct {
	Allowed    bool
	Remaining  int64
	ResetTTL   int64
	FailedOpen bool
}

// failOpen is the answer whenever Redis is unreachable, slow, or returns
// something unparseable. Availability beats strict enforcement.
func failOpen() Decision {
	return Decision{Allowed: true, Remaining: -1, ResetTTL: 0, FailedOpen: true}
}

// run executes a script under the 5ms latency budget and converts the Lua
// reply. Every failure mode collapses to fail-open.
func (c *Client) run(ctx context.Context, label string, script *goredis.Script, identifier string, args ...any) Decision {
	if !c.IsHealthy() {
		return failOpen()
	}

	runCtx, cancel := context.WithTimeout(ctx, c.cfg.Redis.CommandTimeout)
	defer cancel()

	raw, err := script.Run(runCtx, c.rdb, []string{identifier}, args...).Slice()
	if err != nil {
		log.Printf("[%s] Fail-open: %v", label, err)
		return failOpen()
	}

	decision, err := parseDecision(raw)
	if err != nil {
		log.Printf("[%s] Fail-open: %v", label, err)
		return failOpen()
	}

	return decision
}

// parseDecision converts the Lua reply {status, remaining, ttl}. Lua numbers
// returned in a table arrive as Redis integers; the scripts already apply
// floor/ceil so no precision is lost here.
func parseDecision(raw []any) (Decision, error) {
	if len(raw) != 3 {
		return Decision{}, fmt.Errorf("expected 3 reply elements, got %d", len(raw))
	}

	status, ok := raw[0].(string)
	if !ok {
		return Decision{}, fmt.Errorf("expected string status, got %T", raw[0])
	}

	remaining, ok := raw[1].(int64)
	if !ok {
		return Decision{}, fmt.Errorf("expected integer remaining, got %T", raw[1])
	}

	ttl, ok := raw[2].(int64)
	if !ok {
		return Decision{}, fmt.Errorf("expected integer ttl, got %T", raw[2])
	}

	return Decision{
		Allowed:   status == "ALLOWED",
		Remaining: remaining,
		ResetTTL:  ttl,
	}, nil
}
