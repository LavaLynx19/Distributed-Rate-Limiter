// Command keyctl provisions the API-key registry (ARCHITECTURE.md section 10).
// It writes to Redis directly, so the gateways expose no admin surface.
//
//	keyctl tenant set <id> [-plan free] [-keys 1] [-mode pooled]
//	keyctl tenant show <id>
//	keyctl key create <tenant>
//	keyctl key revoke <key>
//	keyctl key list <tenant>
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/manan/distributed-rate-limiter/go/internal/config"
	"github.com/manan/distributed-rate-limiter/go/internal/registry"
)

const usage = `usage:
  keyctl tenant set <id> [-plan free] [-keys 1] [-mode pooled|isolated]
  keyctl tenant show <id>
  keyctl key create <tenant>
  keyctl key revoke <key>
  keyctl key list <tenant>

Redis is taken from REDIS_HOST / REDIS_PORT, as for the gateway.`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("missing arguments\n%s", usage)
	}

	cfg := config.Load()
	rdb := goredis.NewClient(&goredis.Options{Addr: fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)})
	defer func() {
		if err := rdb.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "closing redis:", err)
		}
	}()
	store := registry.NewStore(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch object, verb, rest := args[0], args[1], args[2:]; object + " " + verb {
	case "tenant set":
		return tenantSet(ctx, store, rest)

	case "tenant show":
		t, err := store.Tenant(ctx, rest[0])
		if err != nil {
			return err
		}
		fmt.Printf("tenant=%s plan=%s keys=%d mode=%s\n", t.ID, t.Plan, t.Keys, t.Mode)
		return nil

	case "key create":
		key, err := store.CreateKey(ctx, rest[0])
		if err != nil {
			return err
		}
		fmt.Println(key)
		fmt.Fprintln(os.Stderr, "Store this key now: only its hash is kept, so it cannot be shown again.")
		return nil

	case "key revoke":
		tenant, err := store.RevokeKey(ctx, rest[0])
		if err != nil {
			return err
		}
		fmt.Printf("revoked key of tenant %s (gateways stop honoring it within %s)\n", tenant, cfg.Registry.CacheTTL)
		return nil

	case "key list":
		hashes, err := store.KeyHashes(ctx, rest[0])
		if err != nil {
			return err
		}
		for _, h := range hashes {
			fmt.Printf("sha256:%s…\n", h[:16])
		}
		return nil
	}
	return fmt.Errorf("unknown command %q\n%s", args[0]+" "+args[1], usage)
}

func tenantSet(ctx context.Context, store *registry.Store, args []string) error {
	fs := flag.NewFlagSet("tenant set", flag.ContinueOnError)
	plan := fs.String("plan", "free", "plan name, as defined in PLAN_LIMITS")
	keys := fs.Int64("keys", 1, "paid key slots: the pooled multiplier, and the isolated key cap")
	mode := fs.String("mode", registry.ModePooled, "pooled (shared quota) or isolated (quota per key)")

	id := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	t := registry.Tenant{ID: id, Plan: *plan, Keys: *keys, Mode: *mode}
	if err := store.SetTenant(ctx, t); err != nil {
		return err
	}
	fmt.Printf("tenant=%s plan=%s keys=%d mode=%s\n", t.ID, t.Plan, t.Keys, t.Mode)
	return nil
}
