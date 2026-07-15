package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewDefaultRuntimeConfig(t *testing.T) {
	cfg := NewDefaultRuntimeConfig()

	if cfg.RequestLogEnabled != true {
		t.Errorf("RequestLogEnabled: got %v, want true", cfg.RequestLogEnabled)
	}
	if cfg.MaxConsecutiveFailures != 3 {
		t.Errorf("MaxConsecutiveFailures: got %d, want 3", cfg.MaxConsecutiveFailures)
	}
	if cfg.CacheFlushDirtyThreshold != 1000 {
		t.Errorf("CacheFlushDirtyThreshold: got %d, want 1000", cfg.CacheFlushDirtyThreshold)
	}
	if len(cfg.LatencyAuthorities) != 4 {
		t.Errorf("LatencyAuthorities: got %d items, want 4", len(cfg.LatencyAuthorities))
	}
}

func TestRuntimeConfig_JSONRoundTrip(t *testing.T) {
	original := NewDefaultRuntimeConfig()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded RuntimeConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Spot-check key fields after round-trip
	if decoded.MaxConsecutiveFailures != original.MaxConsecutiveFailures {
		t.Errorf("MaxConsecutiveFailures: got %d, want %d", decoded.MaxConsecutiveFailures, original.MaxConsecutiveFailures)
	}
}

func TestDuration_JSON(t *testing.T) {
	d := Duration(5 * time.Minute)

	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if string(data) != `"5m0s"` {
		t.Errorf("marshal: got %s, want %q", data, "5m0s")
	}

	var decoded Duration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if time.Duration(decoded) != 5*time.Minute {
		t.Errorf("unmarshal: got %v, want 5m", time.Duration(decoded))
	}
}

func TestDefaultRuntimeConfig_DeepCopyIsolation(t *testing.T) {
	// Verify that copyRuntimeConfig deep-copies ProxyCheckScoring
	// so that mutation of the copy does not affect the original.
	cfg := NewDefaultRuntimeConfig()
	// Simulate a canonical scoring policy being applied.
	policy := BalancedScoringPolicy()
	cfg.ProxyCheckScoring = &policy

	// Shallow copy (what copyRuntimeConfig used to do).
	shallow := *cfg
	shallow.ProxyCheckScoring = cfg.ProxyCheckScoring

	// Modify through shallow.
	*shallow.ProxyCheckScoring.Cloudflare.StatusScores.Clean = 50

	// Original must be unaffected for deep copy. But this is a shallow copy,
	// so it WILL affect the original (testing the problem, not the fix).
	// The fix (Clone) should prevent this.
	clone := cfg.ProxyCheckScoring.Clone()
	*clone.Cloudflare.StatusScores.Clean = 25

	if *cfg.ProxyCheckScoring.Cloudflare.StatusScores.Clean != 50 {
		t.Fatal("original clean should still be 50 (shallow copy affected it)")
	}
	// After Clone, original must NOT be changed by clone mutation.
	// Reset to verify isolation:
	*cfg.ProxyCheckScoring.Cloudflare.StatusScores.Clean = 100
	if *clone.Cloudflare.StatusScores.Clean != 25 {
		t.Fatal("clone clean should be 25 after mutation (clone is independent)")
	}
}

func TestDuration_JSONInvalid(t *testing.T) {
	var d Duration
	err := json.Unmarshal([]byte(`"not-a-duration"`), &d)
	if err == nil {
		t.Fatal("expected error for invalid duration string")
	}

	err = json.Unmarshal([]byte(`123`), &d)
	if err == nil {
		t.Fatal("expected error for non-string duration")
	}
}

func TestRuntimeConfig_JSONFieldNames(t *testing.T) {
	cfg := NewDefaultRuntimeConfig()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map error: %v", err)
	}

	// Check that JSON keys match the DESIGN.md GET /system/config response
	expectedKeys := []string{
		"request_log_enabled",
		"reverse_proxy_log_detail_enabled",
		"reverse_proxy_log_req_headers_max_bytes",
		"reverse_proxy_log_req_body_max_bytes",
		"reverse_proxy_log_resp_headers_max_bytes",
		"reverse_proxy_log_resp_body_max_bytes",
		"max_consecutive_failures",
		"max_latency_test_interval",
		"max_authority_latency_test_interval",
		"max_egress_test_interval",
		"latency_test_url",
		"latency_authorities",
		"p2c_latency_window",
		"latency_decay_window",
		"cache_flush_interval",
		"cache_flush_dirty_threshold",
	}

	for _, key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key: %q", key)
		}
	}
}
