package registry

import (
	"fmt"
	"strconv"
	"strings"
)

// Quota modes stored on a tenant record.
const (
	ModeIsolated = "isolated" // each key has its own quota
	ModePooled   = "pooled"   // all of a tenant's keys share one quota
)

// Identity is who a request is rate-limited as, and its sliding-window limit.
type Identity struct {
	ID    string
	Limit int64
}

// Tenant is a registry record, as returned by resolve_key.lua.
type Tenant struct {
	ID   string
	Plan string
	Keys int64
	Mode string
}

// Plans maps plan names to their base sliding-window limit.
type Plans map[string]int64

// ParsePlans reads PLAN_LIMITS, e.g. "free=100,paid=1000".
func ParsePlans(raw string) (Plans, error) {
	plans := Plans{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, value, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("PLAN_LIMITS: %q is not name=limit", item)
		}
		limit, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || limit <= 0 {
			return nil, fmt.Errorf("PLAN_LIMITS: limit for %q must be a positive integer", name)
		}
		plans[strings.TrimSpace(name)] = limit
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("PLAN_LIMITS: no plans defined")
	}
	return plans, nil
}

// identityFor derives the rate-limit identity for a registered key. ok is
// false when the tenant's plan is unknown, in which case the caller falls
// back to anonymous (IP) limiting.
func (p Plans) identityFor(t Tenant, keyHash string) (id Identity, ok bool) {
	base, known := p[t.Plan]
	if !known {
		return Identity{}, false
	}
	if t.Mode == ModeIsolated {
		return Identity{ID: "key:" + keyHash[:16], Limit: base}, true
	}
	slots := t.Keys
	if slots < 1 {
		slots = 1
	}
	return Identity{ID: "tenant:" + t.ID, Limit: base * slots}, true
}
