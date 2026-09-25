package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg := Load()

	if cfg.RateLimiter.MaxLimit != 100 {
		t.Errorf("MaxLimit = %d, want 100", cfg.RateLimiter.MaxLimit)
	}
	// The loose batch threshold is derived, not independently configured.
	if cfg.LooseMode.BatchThreshold != 50 {
		t.Errorf("BatchThreshold = %d, want 50", cfg.LooseMode.BatchThreshold)
	}
	if cfg.Redis.CommandTimeout != 5*time.Millisecond {
		t.Errorf("CommandTimeout = %v, want 5ms", cfg.Redis.CommandTimeout)
	}
	if cfg.RateLimiter.WindowDuration != 300*time.Second {
		t.Errorf("WindowDuration = %v, want 300s", cfg.RateLimiter.WindowDuration)
	}
	if cfg.LuaDir != "../lua" {
		t.Errorf("LuaDir = %q, want ../lua", cfg.LuaDir)
	}
}

func TestBatchThresholdTracksMaxLimit(t *testing.T) {
	t.Setenv("RATE_LIMIT_MAX", "200")

	cfg := Load()
	if cfg.LooseMode.BatchThreshold != 100 {
		t.Errorf("BatchThreshold = %d, want 100", cfg.LooseMode.BatchThreshold)
	}
}
