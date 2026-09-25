package registry

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	goredis "github.com/redis/go-redis/v9"
)

// ErrNotFound is returned for an unknown tenant or key.
var ErrNotFound = errors.New("not found")

// Store writes the registry schema (ARCHITECTURE.md section 10). Only the
// provisioning CLI uses it; gateways read through resolve_key.lua.
type Store struct {
	rdb *goredis.Client
}

func NewStore(rdb *goredis.Client) *Store { return &Store{rdb: rdb} }

func tenantKey(id string) string     { return "tenant::" + id }
func tenantKeysKey(id string) string { return "tenant::" + id + "::keys" }
func apiKeyKey(hash string) string   { return "apikey::" + hash }

// SetTenant creates or updates a tenant record.
func (s *Store) SetTenant(ctx context.Context, t Tenant) error {
	if t.ID == "" || t.Plan == "" {
		return fmt.Errorf("tenant id and plan are required")
	}
	if t.Keys < 1 {
		return fmt.Errorf("keys must be at least 1")
	}
	if t.Mode != ModeIsolated && t.Mode != ModePooled {
		return fmt.Errorf("mode must be %q or %q", ModeIsolated, ModePooled)
	}
	err := s.rdb.HSet(ctx, tenantKey(t.ID), "plan", t.Plan, "keys", t.Keys, "mode", t.Mode).Err()
	if err != nil {
		return fmt.Errorf("saving tenant: %w", err)
	}
	return nil
}

// Tenant reads a tenant record.
func (s *Store) Tenant(ctx context.Context, id string) (Tenant, error) {
	fields, err := s.rdb.HMGet(ctx, tenantKey(id), "plan", "keys", "mode").Result()
	if err != nil {
		return Tenant{}, fmt.Errorf("reading tenant: %w", err)
	}
	plan, _ := fields[0].(string)
	if plan == "" {
		return Tenant{}, fmt.Errorf("tenant %q: %w", id, ErrNotFound)
	}
	keysStr, _ := fields[1].(string)
	keys, err := strconv.ParseInt(keysStr, 10, 64)
	if err != nil {
		keys = 1
	}
	mode, _ := fields[2].(string)
	return Tenant{ID: id, Plan: plan, Keys: keys, Mode: mode}, nil
}

// CreateKey issues a new key for a tenant and returns it. The raw key is not
// stored anywhere; the caller must hand it over now. An isolated tenant can't
// hold more keys than it has paid slots, since each key is its own quota.
func (s *Store) CreateKey(ctx context.Context, tenantID string) (string, error) {
	t, err := s.Tenant(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if t.Mode == ModeIsolated {
		held, err := s.rdb.SCard(ctx, tenantKeysKey(tenantID)).Result()
		if err != nil {
			return "", fmt.Errorf("counting keys: %w", err)
		}
		if held >= t.Keys {
			return "", fmt.Errorf("tenant %q already holds %d of %d paid keys", tenantID, held, t.Keys)
		}
	}

	key, err := GenerateKey()
	if err != nil {
		return "", err
	}
	hash := Hash(key)
	_, err = s.rdb.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		p.HSet(ctx, apiKeyKey(hash), "tenant", tenantID)
		p.SAdd(ctx, tenantKeysKey(tenantID), hash)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("saving key: %w", err)
	}
	return key, nil
}

// RevokeKey removes a key and returns the tenant it belonged to. Gateways
// stop honoring it within one cache TTL.
func (s *Store) RevokeKey(ctx context.Context, key string) (string, error) {
	hash := Hash(key)
	tenantID, err := s.rdb.HGet(ctx, apiKeyKey(hash), "tenant").Result()
	if errors.Is(err, goredis.Nil) {
		return "", fmt.Errorf("key: %w", ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("reading key: %w", err)
	}
	_, err = s.rdb.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		p.Del(ctx, apiKeyKey(hash))
		p.SRem(ctx, tenantKeysKey(tenantID), hash)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("revoking key: %w", err)
	}
	return tenantID, nil
}

// KeyHashes lists a tenant's key hashes (raw keys are never stored).
func (s *Store) KeyHashes(ctx context.Context, tenantID string) ([]string, error) {
	hashes, err := s.rdb.SMembers(ctx, tenantKeysKey(tenantID)).Result()
	if err != nil {
		return nil, fmt.Errorf("listing keys: %w", err)
	}
	return hashes, nil
}
