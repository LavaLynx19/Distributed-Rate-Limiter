// Package config centralises environment-driven settings for the gateway.
// Field-for-field mirror of the Node implementation's lib/config.js so both
// gateways can be pointed at the same environment and behave identically.
package config

import (
	"os"
	"strconv"
	"time"
)

type Redis struct {
	Host string
	Port int
	// CommandTimeout is the per-request latency budget for a Redis round trip.
	// Exceeding it means fail-open, not failure — see ARCHITECTURE.md section 4.
	CommandTimeout time.Duration
}

type RateLimiter struct {
	MaxLimit        int64
	WindowSegments  int
	SegmentDuration time.Duration
	WindowDuration  time.Duration
}

type LooseMode struct {
	// BatchThreshold is the local increment count that triggers an immediate
	// write-back, independent of the periodic flush.
	BatchThreshold int64
	FlushInterval  time.Duration
	// RefreshInterval is how often an exhausted identifier with nothing
	// pending re-asks Redis for capacity, so it unblocks as segments expire.
	RefreshInterval time.Duration
}

type TokenBucket struct {
	Capacity   int64
	RefillRate float64 // tokens per second
}

type LeakyBucket struct {
	Capacity int64
	LeakRate float64 // requests drained per second
}

// Registry configures API-key resolution (ARCHITECTURE.md section 10).
type Registry struct {
	// PlanLimits is the raw PLAN_LIMITS value, e.g. "free=100,paid=1000".
	PlanLimits string
	CacheTTL   time.Duration
	CacheSize  int
}

type Server struct {
	Port int
	// TrustProxy is the raw TRUST_PROXY value: empty (use the socket address),
	// a hop count, or a comma-separated IP/CIDR list. Parsed by middleware.
	TrustProxy string
}

type Config struct {
	Redis       Redis
	RateLimiter RateLimiter
	LooseMode   LooseMode
	TokenBucket TokenBucket
	LeakyBucket LeakyBucket
	Server      Server

	Registry Registry

	// LuaDir points at the repo-root lua/ directory shared with the Node
	// implementation. Go has no equivalent of the Node loader's relative
	// require, and //go:embed cannot reach outside the module, so the path is
	// resolved at startup instead.
	LuaDir string
}

func Load() Config {
	maxLimit := envInt64("RATE_LIMIT_MAX", 100)

	return Config{
		Redis: Redis{
			Host:           envString("REDIS_HOST", "127.0.0.1"),
			Port:           envInt("REDIS_PORT", 6379),
			CommandTimeout: 5 * time.Millisecond,
		},
		RateLimiter: RateLimiter{
			MaxLimit:        maxLimit,
			WindowSegments:  5,
			SegmentDuration: 60 * time.Second,
			WindowDuration:  300 * time.Second,
		},
		LooseMode: LooseMode{
			BatchThreshold:  maxLimit / 2,
			FlushInterval:   500 * time.Millisecond,
			RefreshInterval: 5 * time.Second,
		},
		TokenBucket: TokenBucket{
			Capacity:   envInt64("TOKEN_BUCKET_CAPACITY", 10),
			RefillRate: envFloat("TOKEN_BUCKET_REFILL_RATE", 1),
		},
		LeakyBucket: LeakyBucket{
			Capacity: envInt64("LEAKY_BUCKET_CAPACITY", 10),
			LeakRate: envFloat("LEAKY_BUCKET_LEAK_RATE", 1),
		},
		Server: Server{
			Port:       envInt("PORT", 3000),
			TrustProxy: os.Getenv("TRUST_PROXY"),
		},
		Registry: Registry{
			PlanLimits: envString("PLAN_LIMITS", "free=100,paid=1000"),
			CacheTTL:   time.Duration(envInt("KEY_CACHE_TTL", 30)) * time.Second,
			CacheSize:  envInt("KEY_CACHE_SIZE", 10000),
		},
		LuaDir: envString("LUA_DIR", "../lua"),
	}
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v == 0 {
		return fallback
	}
	return v
}

func envInt64(key string, fallback int64) int64 {
	v, err := strconv.ParseInt(os.Getenv(key), 10, 64)
	if err != nil || v == 0 {
		return fallback
	}
	return v
}

func envFloat(key string, fallback float64) float64 {
	v, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil || v == 0 {
		return fallback
	}
	return v
}
