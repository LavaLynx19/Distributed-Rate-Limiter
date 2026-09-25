// Package redis wraps the go-redis client and the three atomic Lua scripts
// that hold all rate-limiting logic. Every decision is made inside Redis so
// that concurrent gateway instances cannot over-admit.
package redis

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/manan/distributed-rate-limiter/go/internal/config"
)

// scriptFiles are loaded from the repo-root lua/ directory, shared verbatim
// with the Node implementation.
var scriptFiles = []string{"sliding_window.lua", "token_bucket.lua", "leaky_bucket.lua", "resolve_key.lua"}

type Client struct {
	rdb *goredis.Client
	cfg config.Config

	slidingWindow *goredis.Script
	tokenBucket   *goredis.Script
	leakyBucket   *goredis.Script
	resolveKey    *goredis.Script

	// healthy mirrors ioredis's `status === 'ready'`, which go-redis does not
	// expose. A background pinger owns it; request paths only read it.
	healthy atomic.Bool
}

// New loads the Lua scripts and builds a client. A missing or unreadable
// script is a startup error, not a fail-open case: fail-open covers Redis
// being unreachable, not a misconfigured binary.
func New(cfg config.Config) (*Client, error) {
	sources := make(map[string]string, len(scriptFiles))
	for _, name := range scriptFiles {
		path := filepath.Join(cfg.LuaDir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("loading lua script %s: %w", path, err)
		}
		sources[name] = string(src)
	}

	rdb := goredis.NewClient(&goredis.Options{
		Addr:        fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		MaxRetries:  1,
		DialTimeout: time.Second,
		// Deliberately no global ReadTimeout: the 5ms budget is applied
		// per call via context, so connect-time PINGs are not starved.
	})

	return &Client{
		rdb:           rdb,
		cfg:           cfg,
		slidingWindow: goredis.NewScript(sources["sliding_window.lua"]),
		tokenBucket:   goredis.NewScript(sources["token_bucket.lua"]),
		leakyBucket:   goredis.NewScript(sources["leaky_bucket.lua"]),
		resolveKey:    goredis.NewScript(sources["resolve_key.lua"]),
	}, nil
}

// Connect performs the initial reachability check. Redis being down is not
// fatal — the gateway runs fail-open until the pinger sees it recover.
func (c *Client) Connect(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	if err := c.rdb.Ping(pingCtx).Err(); err != nil {
		c.healthy.Store(false)
		return err
	}

	c.healthy.Store(true)
	log.Println("[Redis] Connected successfully")
	return nil
}

// StartHealthProbe keeps the healthy flag current so that request paths can
// skip Redis entirely while it is down, instead of paying a timeout each time.
func (c *Client) StartHealthProbe(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
				err := c.rdb.Ping(pingCtx).Err()
				cancel()

				if was := c.healthy.Swap(err == nil); was != (err == nil) {
					if err != nil {
						log.Printf("[Redis] Connection lost: %v — failing open", err)
					} else {
						log.Println("[Redis] Connection recovered")
					}
				}
			}
		}
	}()
}

func (c *Client) IsHealthy() bool { return c.healthy.Load() }

func (c *Client) Close() error { return c.rdb.Close() }
