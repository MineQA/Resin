package probe

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/topology"
)

// TestTriggerImmediateQualityProbe_Enqueues verifies that
// TriggerImmediateQualityProbe creates a queued quality task.
func TestTriggerImmediateQualityProbe_Enqueues(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"trigger-quality"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"trigger-quality"}`), "sub1")
	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}
	storeOutbound(entry)

	var called bool
	mgr := NewProbeManager(ProbeConfig{
		Pool:        pool,
		Concurrency: 1,
		Fetcher: func(_ node.Hash, url string) ([]byte, time.Duration, error) {
			called = true
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return true },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})
	mgr.Start()
	defer mgr.Stop()

	mgr.TriggerImmediateQualityProbe(hash)
	time.Sleep(50 * time.Millisecond)

	if !called {
		t.Fatal("expected quality probe fetcher to be called")
	}

	// Verify quality was stored.
	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality to be recorded")
	}
	if q.Profile != "generic" {
		t.Fatalf("quality profile = %q, want generic", q.Profile)
	}
	if q.ServiceReachable != true {
		t.Fatal("expected ServiceReachable true")
	}
}

// TestPerformQualityCheck_NoRecordResult verifies that performQualityCheck
// does not affect failure count, circuit breaker, or egress/latency state.
func TestPerformQualityCheck_NoRecordResult(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"quality-no-health"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"quality-no-health"}`), "sub1")
	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}
	storeOutbound(entry)

	// Close the startup circuit and then set failure count to verify the
	// quality check does not modify it.
	pool.RecordResult(hash, true)
	entry.FailureCount.Store(5)

	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, url string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return true },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})

	mgr.performQualityCheck(hash, entry, false)

	// FailureCount must remain unchanged.
	if entry.FailureCount.Load() != 5 {
		t.Fatalf("FailureCount changed to %d, want 5 (unchanged)", entry.FailureCount.Load())
	}

	// CircuitOpenSince must remain 0 (not circuit-open).
	if entry.CircuitOpenSince.Load() != 0 {
		t.Fatal("CircuitOpenSince should remain 0 after quality check")
	}

	// Egress/region must remain unchanged.
	if entry.GetEgressIP().IsValid() {
		t.Fatal("EgressIP should remain unchanged after quality check")
	}

	// Quality must be recorded.
	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality to be recorded")
	}
	if !q.ServiceReachable {
		t.Fatal("expected ServiceReachable true")
	}
}

// TestPerformQualityCheck_FailureStillRecords verifies that a failing quality
// check still writes back quality state with Grade F and LastError.
func TestPerformQualityCheck_FailureStillRecords(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"quality-fail-records"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"quality-fail-records"}`), "sub1")
	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}
	storeOutbound(entry)

	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, url string) ([]byte, time.Duration, error) {
			return nil, 0, errors.New("connection refused")
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return true },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})

	mgr.performQualityCheck(hash, entry, false)

	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality to be recorded even on failure")
	}
	if q.Grade != "F" {
		t.Fatalf("expected grade F on fetch failure, got %q", q.Grade)
	}
	if q.Score != 0 {
		t.Fatalf("expected score 0 on fetch failure, got %f", q.Score)
	}
	if q.LastError == "" {
		t.Fatal("expected LastError to be set on fetch failure")
	}
}

// TestScanQuality_SkipsDisabledNodes verifies that scanQuality does not
// enqueue quality probes for disabled nodes.
func TestScanQuality_SkipsDisabledNodes(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	sub := subscription.NewSubscription("sub1", "sub1", "url", false, false)
	subMgr.Register(sub)

	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"scan-quality-disabled"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"scan-quality-disabled"}`), "sub1")
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"tag"}})

	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}
	storeOutbound(entry)

	var calls int
	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			calls++
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return true },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})

	mgr.scanQuality()
	time.Sleep(30 * time.Millisecond)

	if calls != 0 {
		t.Fatalf("disabled node should be skipped by scanQuality, calls=%d", calls)
	}
}

// TestScanQuality_SkipsNilOutbound verifies that scanQuality skips nodes
// without an outbound.
func TestScanQuality_SkipsNilOutbound(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"scan-quality-no-outbound"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"scan-quality-no-outbound"}`), "sub1")

	var calls int
	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			calls++
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return true },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})

	mgr.scanQuality()
	time.Sleep(30 * time.Millisecond)

	if calls != 0 {
		t.Fatalf("node without outbound should be skipped by scanQuality, calls=%d", calls)
	}
}

// TestScanQuality_ReturnsWhenDisabled verifies that scanQuality is a no-op
// when quality config is nil or disabled.
func TestScanQuality_ReturnsWhenDisabled(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"scan-quality-disabled"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"scan-quality-disabled"}`), "sub1")
	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}
	storeOutbound(entry)

	var calls int
	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			calls++
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		// No QualityCfg — scan should return immediately.
	})

	mgr.scanQuality()
	time.Sleep(30 * time.Millisecond)

	if calls != 0 {
		t.Fatalf("disabled quality scan should not enqueue, calls=%d", calls)
	}

	// Also test with QualityCfg but Enabled=false
	mgr2 := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			calls++
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return false },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})

	mgr2.scanQuality()
	time.Sleep(30 * time.Millisecond)

	if calls != 0 {
		t.Fatalf("disabled quality scan should not enqueue, calls=%d", calls)
	}
}

// TestProxyScoreToNodeQuality_MapsCorrectly verifies the conversion helper.
func TestProxyScoreToNodeQuality_MapsCorrectly(t *testing.T) {
	score := &ProxyScore{
		Grade:                   "A",
		Score:                   95,
		Unstable:                false,
		ServiceReachable:        true,
		APIReachable:            true,
		CloudflareChallenged:    false,
		CloudflareChallengeType: "",
		AvgLatencyMs:            42.5,
	}

	nq := ProxyScoreToNodeQuality("openai", score, nil)
	if nq == nil {
		t.Fatal("expected non-nil NodeQuality")
	}
	if nq.Profile != "openai" {
		t.Fatalf("Profile = %q, want openai", nq.Profile)
	}
	if nq.Grade != "A" {
		t.Fatalf("Grade = %q, want A", nq.Grade)
	}
	if nq.Score != 95 {
		t.Fatalf("Score = %f, want 95", nq.Score)
	}
	if nq.Unstable {
		t.Fatal("Unstable should be false")
	}
	if !nq.ServiceReachable {
		t.Fatal("ServiceReachable should be true")
	}
	if !nq.APIReachable {
		t.Fatal("APIReachable should be true")
	}
	if nq.CloudflareChallenged {
		t.Fatal("CloudflareChallenged should be false")
	}
	if nq.AvgLatencyMs != 42.5 {
		t.Fatalf("AvgLatencyMs = %f, want 42.5", nq.AvgLatencyMs)
	}
	if nq.LastError != "" {
		t.Fatalf("LastError = %q, want empty", nq.LastError)
	}
}

// TestProxyScoreToNodeQuality_NilScore verifies converter handles nil score.
func TestProxyScoreToNodeQuality_NilScore(t *testing.T) {
	nq := ProxyScoreToNodeQuality("generic", nil, errors.New("network error"))
	if nq == nil {
		t.Fatal("expected non-nil NodeQuality")
	}
	if nq.Grade != "F" {
		t.Fatalf("Grade = %q, want F", nq.Grade)
	}
	if nq.Score != 0 {
		t.Fatalf("Score = %f, want 0", nq.Score)
	}
	if nq.LastError != "network error" {
		t.Fatalf("LastError = %q, want 'network error'", nq.LastError)
	}
}

// TestProxyScoreToNodeQuality_NilScoreNoError verifies nil score without error.
func TestProxyScoreToNodeQuality_NilScoreNoError(t *testing.T) {
	nq := ProxyScoreToNodeQuality("generic", nil, nil)
	if nq == nil {
		t.Fatal("expected non-nil NodeQuality")
	}
	if nq.Grade != "F" {
		t.Fatalf("Grade = %q, want F", nq.Grade)
	}
	if nq.LastError != "proxy check failed" {
		t.Fatalf("LastError = %q, want 'proxy check failed'", nq.LastError)
	}
}

// TestProxyScoreToNodeQuality_CloudflareStatusDerivation verifies that
// the aggregate CloudflareStatus is stored and challenged/compatibility
// fields are derived correctly.
func TestProxyScoreToNodeQuality_CloudflareStatusDerivation(t *testing.T) {
	tests := []struct {
		name              string
		cfStatus          CloudflareStatus
		wantStatus        string
		wantChallenged    bool
		wantChallengeType string
	}{
		{name: "clean", cfStatus: CFStatusClean, wantStatus: "clean", wantChallenged: false, wantChallengeType: ""},
		{name: "not_detected", cfStatus: CFStatusNotDetected, wantStatus: "not_detected", wantChallenged: false, wantChallengeType: ""},
		{name: "js_challenge", cfStatus: CFStatusJSChallenge, wantStatus: "js_challenge", wantChallenged: true, wantChallengeType: "js_challenge"},
		{name: "captcha_challenge", cfStatus: CFStatusCaptchaChallenge, wantStatus: "captcha_challenge", wantChallenged: true, wantChallengeType: "captcha_challenge"},
		{name: "block", cfStatus: CFStatusBlock, wantStatus: "block", wantChallenged: true, wantChallengeType: "block"},
		{name: "challenge", cfStatus: CFStatusChallenge, wantStatus: "challenge", wantChallenged: true, wantChallengeType: "challenge"},
		{name: "ng", cfStatus: CFStatusNG, wantStatus: "ng", wantChallenged: false, wantChallengeType: ""},
		{name: "empty_legacy", cfStatus: CFStatusEmpty, wantStatus: "", wantChallenged: false, wantChallengeType: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := &ProxyScore{
				Grade:            "A",
				Score:            100,
				ServiceReachable: true,
				APIReachable:     true,
				CloudflareStatus: tt.cfStatus,
			}
			nq := ProxyScoreToNodeQuality("generic", score, nil)
			if nq.CloudflareStatus != tt.wantStatus {
				t.Fatalf("CloudflareStatus = %q, want %q", nq.CloudflareStatus, tt.wantStatus)
			}
			if nq.CloudflareChallenged != tt.wantChallenged {
				t.Fatalf("CloudflareChallenged = %v, want %v", nq.CloudflareChallenged, tt.wantChallenged)
			}
			if nq.CloudflareChallengeType != tt.wantChallengeType {
				t.Fatalf("CloudflareChallengeType = %q, want %q", nq.CloudflareChallengeType, tt.wantChallengeType)
			}
		})
	}
}

// TestProxyScoreToNodeQuality_ScoringBreakdownSerialization verifies that
// when ScoringBreakdown is present, it is serialized into compact JSON and
// ScoringPolicyVersion is set; nil breakdown leaves version 0 and empty string.
func TestProxyScoreToNodeQuality_ScoringBreakdownSerialization(t *testing.T) {
	// --- With ScoringBreakdown ---
	sr := &ScoringResult{
		Version:          1,
		Score:            85,
		Grade:            "B",
		GradeFromScore:   "B",
		FinalGrade:       "B",
		EffectiveWeights: map[string]int{"service": 60, "latency": 40},
		SubScores: map[string]*SubScoreEntry{
			"service": {Value: float64Ptr(100)},
			"latency": {Value: float64Ptr(62.5)},
		},
		UnavailableDims: []string{},
		AppliedCaps:     []CapApplication{},
		TerminalReason:  "",
	}
	score := &ProxyScore{
		Grade:            "B",
		Score:            85,
		ServiceReachable: true,
		CloudflareStatus: CFStatusClean,
		ScoringBreakdown: sr,
	}
	nq := ProxyScoreToNodeQuality("generic", score, nil)
	if nq.ScoringPolicyVersion != 1 {
		t.Fatalf("ScoringPolicyVersion = %d, want 1", nq.ScoringPolicyVersion)
	}
	if nq.ScoreBreakdown == "" {
		t.Fatal("ScoreBreakdown should not be empty when ScoringBreakdown is present")
	}
	// Compact JSON should contain key fields but NOT redundant Grade or round data.
	for _, key := range []string{"version", "effective_weights", "sub_scores", "grade_from_score", "final_grade"} {
		if !strings.Contains(nq.ScoreBreakdown, key) {
			t.Fatalf("ScoreBreakdown missing key %q: %s", key, nq.ScoreBreakdown)
		}
	}
	// Should NOT contain "round_results" or "grade" (the redundant top-level field).
	if strings.Contains(nq.ScoreBreakdown, "round_results") {
		t.Fatal("ScoreBreakdown should NOT contain round_results")
	}
	// Verify it's valid JSON
	if !json.Valid([]byte(nq.ScoreBreakdown)) {
		t.Fatalf("ScoreBreakdown is not valid JSON: %s", nq.ScoreBreakdown)
	}

	// --- Nil ScoringBreakdown (legacy) ---
	score2 := &ProxyScore{
		Grade:            "A",
		Score:            100,
		ServiceReachable: true,
		CloudflareStatus: CFStatusClean,
	}
	nq2 := ProxyScoreToNodeQuality("generic", score2, nil)
	if nq2.ScoringPolicyVersion != 0 {
		t.Fatalf("legacy ScoringPolicyVersion = %d, want 0", nq2.ScoringPolicyVersion)
	}
	if nq2.ScoreBreakdown != "" {
		t.Fatalf("legacy ScoreBreakdown = %q, want empty", nq2.ScoreBreakdown)
	}
}

func float64Ptr(v float64) *float64 { return &v }

// TestTriggerImmediateQualityProbeForce_ExecutesWhenPeriodicDisabled verifies
// that a forced quality probe executes when periodic quality is disabled but
// TriggerOnNewNode is enabled.
func TestTriggerImmediateQualityProbeForce_ExecutesWhenPeriodicDisabled(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"force-quality"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"force-quality"}`), "sub1")
	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}
	storeOutbound(entry)

	var called bool
	mgr := NewProbeManager(ProbeConfig{
		Pool:        pool,
		Concurrency: 1,
		Fetcher: func(_ node.Hash, url string) ([]byte, time.Duration, error) {
			called = true
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return false }, // periodic disabled
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
			TriggerOnNewNode: func() bool { return true }, // trigger enabled
		},
	})
	mgr.Start()
	defer mgr.Stop()

	mgr.TriggerImmediateQualityProbeForce(hash)
	time.Sleep(50 * time.Millisecond)

	if !called {
		t.Fatal("expected forced quality probe fetcher to be called when periodic disabled but trigger enabled")
	}

	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality to be recorded")
	}
	if !q.ServiceReachable {
		t.Fatal("expected ServiceReachable true")
	}
}

// TestTriggerImmediateQualityProbeForce_NoopWhenTriggerDisabled verifies that
// a forced quality probe is a no-op when TriggerOnNewNode is disabled, even
// when periodic quality is enabled.
func TestTriggerImmediateQualityProbeForce_NoopWhenTriggerDisabled(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"force-noop"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"force-noop"}`), "sub1")
	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}
	storeOutbound(entry)

	var called bool
	mgr := NewProbeManager(ProbeConfig{
		Pool:        pool,
		Concurrency: 1,
		Fetcher: func(_ node.Hash, url string) ([]byte, time.Duration, error) {
			called = true
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return true }, // periodic enabled
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
			TriggerOnNewNode: func() bool { return false }, // trigger disabled
		},
	})
	mgr.Start()
	defer mgr.Stop()

	mgr.TriggerImmediateQualityProbeForce(hash)
	time.Sleep(50 * time.Millisecond)

	if called {
		t.Fatal("forced quality probe should NOT execute when TriggerOnNewNode is disabled")
	}
}

// TestScanQuality_NoopWhenDisabled verifies that scanQuality does not enqueue
// tasks when ProxyCheckEnabled is false, even though forced probes work.
func TestScanQuality_NoopWhenDisabled(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"scan-disabled"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"scan-disabled"}`), "sub1")
	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}
	storeOutbound(entry)

	var calls int
	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			calls++
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return false },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})

	mgr.scanQuality()
	time.Sleep(30 * time.Millisecond)

	if calls != 0 {
		t.Fatalf("scanQuality should not enqueue when enabled=false, calls=%d", calls)
	}
}

// ---------------------------------------------------------------------------
// Quality sweep tests
// ---------------------------------------------------------------------------

// TestQualitySweep_SkipsDisabledAndNilOutbound verifies that the sweep skips
// disabled nodes and nil outbound, only checking eligible nodes.
func TestQualitySweep_SkipsDisabledAndNilOutbound(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	sub := subscription.NewSubscription("sweep-sub", "sweep-sub", "url", true, false)
	subMgr.Register(sub)

	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	// Eligible node.
	hash1 := node.HashFromRawOptions([]byte(`{"type":"sweep-ok"}`))
	pool.AddNodeFromSub(hash1, []byte(`{"type":"sweep-ok"}`), sub.ID)
	sub.ManagedNodes().StoreNode(hash1, subscription.ManagedNode{Tags: []string{"eligible"}})
	entry1, _ := pool.GetEntry(hash1)
	storeOutbound(entry1)

	// Disabled node.
	hash2 := node.HashFromRawOptions([]byte(`{"type":"sweep-disabled"}`))
	pool.AddNodeFromSub(hash2, []byte(`{"type":"sweep-disabled"}`), sub.ID)
	// Not stored in sub's managed nodes → disabled.
	entry2, _ := pool.GetEntry(hash2)
	storeOutbound(entry2)

	// Nil outbound node.
	hash3 := node.HashFromRawOptions([]byte(`{"type":"sweep-no-ob"}`))
	pool.AddNodeFromSub(hash3, []byte(`{"type":"sweep-no-ob"}`), sub.ID)
	sub.ManagedNodes().StoreNode(hash3, subscription.ManagedNode{Tags: []string{"no-ob"}})
	entry3, _ := pool.GetEntry(hash3)
	_ = entry3 // No outbound set — entry3 is eligible by subscription but nil outbound.

	callCount := 0
	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			callCount++
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return false }, // background disabled — sweep ignores this
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})

	mgr.performQualitySweep()

	if callCount != 1 {
		t.Fatalf("expected 1 quality check (eligible node only), got %d", callCount)
	}

	// Only hash1 should have quality recorded.
	if q := entry1.GetQuality(); q == nil {
		t.Fatal("expected quality recorded for eligible node")
	}
	if q := entry2.GetQuality(); q != nil {
		t.Fatal("disabled node should not have quality recorded")
	}
	if q := entry3.GetQuality(); q != nil {
		t.Fatal("nil-outbound node should not have quality recorded")
	}
}

// TestQualitySweep_UsesRuntimeConfig verifies that the sweep uses the current
// runtime profile and options, not gate-controlled defaults.
func TestQualitySweep_UsesRuntimeConfig(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"sweep-runtime"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"sweep-runtime"}`), "sub1")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)

	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return false },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "openai" },
			Opts: func() ProxyCheckOptions {
				return ProxyCheckOptions{
					ServiceReachability: true,
					CloudflareDetection: true,
					Rounds:              1,
				}
			},
		},
	})

	mgr.performQualitySweep()

	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality recorded")
	}
	if q.Profile != "openai" {
		t.Fatalf("profile = %q, want openai (from runtime config)", q.Profile)
	}
}

// TestQualitySweep_RespectsStopCh verifies that the sweep stops processing
// nodes when stopCh is closed.
func TestQualitySweep_RespectsStopCh(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	// Add two eligible nodes.
	hashes := make([]node.Hash, 2)
	for i := range hashes {
		raw := []byte(`{"type":"sweep-stop-` + string(rune('0'+i)) + `"}`)
		h := node.HashFromRawOptions(raw)
		pool.AddNodeFromSub(h, raw, "sub1")
		entry, _ := pool.GetEntry(h)
		storeOutbound(entry)
		hashes[i] = h
	}

	callCount := 0
	mgr := NewProbeManager(ProbeConfig{
		Pool:        pool,
		Concurrency: 1,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			callCount++
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return true },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})

	// Close stopCh before sweep — should process 0 nodes.
	mgr.Stop()

	mgr.performQualitySweep()

	if callCount != 0 {
		t.Fatalf("expected 0 checks after stop, got %d", callCount)
	}
}

// TestQualitySweep_NoRecordResult verifies that the sweep does not call
// RecordResult and does not affect failure count or circuit breaker.
func TestQualitySweep_NoRecordResult(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"sweep-no-record"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"sweep-no-record"}`), "sub1")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)

	// Close startup circuit and set failure count.
	pool.RecordResult(hash, true)
	entry.FailureCount.Store(5)

	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return true },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})

	mgr.performQualitySweep()

	if entry.FailureCount.Load() != 5 {
		t.Fatalf("FailureCount changed to %d, want 5 (unchanged)", entry.FailureCount.Load())
	}
	if entry.CircuitOpenSince.Load() != 0 {
		t.Fatal("CircuitOpenSince should remain 0 after quality sweep")
	}

	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality recorded")
	}
}

// TestQualitySweep_EnqueuesViaTriggerAll verifies that TriggerAllQualityProbes
// enqueues a sweep task that eventually executes via the worker.
func TestQualitySweep_EnqueuesViaTriggerAll(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"sweep-enqueue"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"sweep-enqueue"}`), "sub1")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)

	var (
		startedOnce sync.Once
		started     = make(chan struct{})
	)
	mgr := NewProbeManager(ProbeConfig{
		Pool:        pool,
		Concurrency: 1,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		OnProbeEvent: func(kind string) {
			if kind == "quality" {
				startedOnce.Do(func() { close(started) })
			}
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return false }, // background disabled
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})
	mgr.Start()
	defer mgr.Stop()

	count, coalesced, rejected := mgr.TriggerAllQualityProbes()
	if rejected {
		t.Fatal("unexpected queue rejection")
	}
	if count != 1 {
		t.Fatalf("candidateCount = %d, want 1", count)
	}
	if coalesced {
		t.Fatal("expected first trigger not to be coalesced")
	}

	// Wait for sweep to start (quality event).
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sweep to start")
	}

	// Wait for quality writeback to complete.
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(time.Second)
	for entry.GetQuality() == nil {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for quality to be recorded")
		case <-ticker.C:
		}
	}

	// Verify quality was recorded via sweep.
	if q := entry.GetQuality(); q == nil {
		t.Fatal("expected quality recorded via sweep")
	}
}

// TestQualitySweep_Coalescing verifies that a second trigger while a sweep
// is running is coalesced, and the second round eventually executes (no lost
// updates) without concurrent execution.
//
// Rounds produce distinguishable quality content: round 1 returns a transport
// error (Grade F), round 2 returns success (Grade A). After releasing round 2,
// a ticker polls for Grade != "F" to confirm round 2 writeback completed.
//
// Gating strategy (sync.Once avoids double-close/panic when CheckProxy makes
// multiple fetcher calls per round):
//   - Round 1 first fetch: firstFetchOnce signals + blocks on firstGate.
//     Subsequent fetcher calls in round 1 skip the gate and return error.
//   - Round 2 first fetch: secondFetchOnce signals + blocks on secondGate.
//     When blocked, round 1 has fully completed (writeback + finishTask)
//     because the sweep task is sequential. Subsequent calls return success.
//   - Cleanup uses Once-closes so failure paths don't deadlock mgr.Stop().
func TestQualitySweep_Coalescing(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"sweep-coalesce"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"sweep-coalesce"}`), "sub1")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)

	var (
		qualityCount atomic.Int32
		activeCount  atomic.Int32
		maxActive    atomic.Int32

		// Round 1 gating.
		firstFetchOnce sync.Once
		firstGate      = make(chan struct{}) // closed to release round 1
		firstRunning   = make(chan struct{}) // closed when round 1 first fetch enters

		// Round 2 gating.
		secondFetchOnce sync.Once
		secondGate      = make(chan struct{}) // closed to release round 2
		secondReady     = make(chan struct{}) // closed when round 2 first fetch enters

		// Cleanup: close gates under Once so failure paths unblock worker.
		closeFirstGateOnce  sync.Once
		closeSecondGateOnce sync.Once

		errRound1 = errors.New("round 1 transport error")
	)
	mgr := NewProbeManager(ProbeConfig{
		Pool:        pool,
		Concurrency: 1,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			cur := activeCount.Add(1)
			for {
				prev := maxActive.Load()
				if cur > prev {
					if maxActive.CompareAndSwap(prev, cur) {
						break
					}
				} else {
					break
				}
			}
			defer activeCount.Add(-1)

			// Gate by sweep round: qualityCount is already incremented by
			// onProbeEvent("quality") which fires before each node's fetcher call.
			switch qualityCount.Load() {
			case 1:
				// Round 1 first fetch: signal test and block.
				firstFetchOnce.Do(func() {
					close(firstRunning)
					<-firstGate
				})
				// Return error so round 1 writes Grade F quality.
				return nil, 0, errRound1
			case 2:
				// Round 2 first fetch: signal test and block.
				secondFetchOnce.Do(func() {
					close(secondReady)
					<-secondGate
				})
				// Fall through to success — round 2 writes Grade A.
			}
			return []byte("ok"), 10 * time.Millisecond, nil
		},
		OnProbeEvent: func(kind string) {
			if kind == "quality" {
				qualityCount.Add(1)
			}
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return false },
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "generic" },
			Opts: func() ProxyCheckOptions {
				return DefaultOptions()
			},
		},
	})
	mgr.Start()
	// Cleanup: close gates under Once so Stop does not deadlock if the
	// test fails before releasing a gate.
	defer func() {
		closeFirstGateOnce.Do(func() { close(firstGate) })
		closeSecondGateOnce.Do(func() { close(secondGate) })
		mgr.Stop()
	}()

	// First trigger — should be queued.
	count1, coalesced1, rejected1 := mgr.TriggerAllQualityProbes()
	if rejected1 || count1 != 1 || coalesced1 {
		t.Fatalf("first trigger: count=%d coalesced=%v rejected=%v", count1, coalesced1, rejected1)
	}

	// Wait for round 1 first fetch to enter.
	select {
	case <-firstRunning:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sweep to start")
	}

	// Second trigger — must coalesce with the running sweep.
	count2, coalesced2, rejected2 := mgr.TriggerAllQualityProbes()
	if rejected2 {
		t.Fatal("second trigger should not be rejected")
	}
	if count2 != 1 {
		t.Fatalf("second trigger count = %d, want 1", count2)
	}
	if !coalesced2 {
		t.Fatal("second trigger should be coalesced (sweep running)")
	}

	// Release round 1 — fetcher returns error, sweep writes Grade F quality,
	// finishTask sees dirty flag and requeues.
	closeFirstGateOnce.Do(func() { close(firstGate) })

	// Wait for round 2 first fetch to enter (secondReady).
	// By now round 1 has fully completed (writeback + finishTask) because
	// the sweep runs sequentially on a single worker.
	select {
	case <-secondReady:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second sweep round to reach fetcher")
	}

	// Record round 1 quality (must be Grade F from the error).
	firstQuality := entry.GetQuality()
	if firstQuality == nil {
		t.Fatal("first round quality should be written before second round starts")
	}
	if firstQuality.Grade != "F" {
		t.Fatalf("first round grade = %q, want F (from transport error)", firstQuality.Grade)
	}

	// Release round 2 — fetcher returns success, sweep writes Grade A quality.
	closeSecondGateOnce.Do(func() { close(secondGate) })

	// Wait for quality to change from Grade F to Grade A (round 2 writeback).
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(time.Second)
	for {
		q := entry.GetQuality()
		if q != nil && q.Grade != "F" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for second round quality writeback (Grade never changed from F)")
		case <-ticker.C:
		}
	}

	// Exactly 2 quality events (sweep rounds, not fetcher calls).
	if c := qualityCount.Load(); c != 2 {
		t.Fatalf("expected 2 quality sweep events, got %d", c)
	}

	// No concurrent execution: maxActive must be 1 because the sweep task
	// processes nodes sequentially and Concurrency=1 allows only one task
	// at a time.
	if maxActive.Load() > 1 {
		t.Fatalf("expected no concurrent execution, maxActive=%d", maxActive.Load())
	}
}

// TestProbeQualitySync_UsesRuntimeConfig verifies that ProbeQualitySync uses
// current runtime config and writes back quality.
func TestProbeQualitySync_UsesRuntimeConfig(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"probe-quality-sync"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"probe-quality-sync"}`), "sub1")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)

	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte(`{"data": [{"id": "openai"}]}`), 10 * time.Millisecond, nil
		},
		QualityCfg: &QualityProbeConfig{
			Enabled:  func() bool { return false }, // background disabled — ProbeQualitySync ignores this
			Interval: func() time.Duration { return 30 * time.Minute },
			Profile:  func() string { return "openai" },
			Opts: func() ProxyCheckOptions {
				return ProxyCheckOptions{
					ServiceReachability: true,
					Rounds:              1,
				}
			},
		},
	})

	score, err := mgr.ProbeQualitySync(hash)
	if err != nil {
		t.Fatalf("ProbeQualitySync: %v", err)
	}
	if score == nil {
		t.Fatal("expected non-nil score")
	}

	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality recorded")
	}
	if q.Profile != "openai" {
		t.Fatalf("profile = %q, want openai", q.Profile)
	}
	if !q.ServiceReachable {
		t.Fatal("expected ServiceReachable true")
	}
}

// TestProbeQualitySync_NoRecordResult verifies that ProbeQualitySync does not
// affect failure count or circuit breaker.
func TestProbeQualitySync_NoRecordResult(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"probe-quality-no-record"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"probe-quality-no-record"}`), "sub1")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)

	pool.RecordResult(hash, true)
	entry.FailureCount.Store(5)

	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})

	_, err := mgr.ProbeQualitySync(hash)
	if err != nil {
		t.Fatalf("ProbeQualitySync: %v", err)
	}

	if entry.FailureCount.Load() != 5 {
		t.Fatalf("FailureCount changed to %d, want 5", entry.FailureCount.Load())
	}
	if entry.CircuitOpenSince.Load() != 0 {
		t.Fatal("CircuitOpenSince should remain 0")
	}
}

// TestProbeQualitySync_FailureStillRecords verifies that ProbeQualitySync
// writes back quality even when CheckProxy records a transport error. The
// transport error is captured in ProxyRoundResult.Error, which CheckProxy
// aggregates into score.Grade=F and score.LastError — the top-level
// ProbeQualitySync call returns (score, nil) with the error in the quality.
func TestProbeQualitySync_FailureStillRecords(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"probe-quality-fail"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"probe-quality-fail"}`), "sub1")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)

	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return nil, 0, errors.New("connection refused")
		},
	})

	score, err := mgr.ProbeQualitySync(hash)
	if err != nil {
		t.Fatalf("ProbeQualitySync returned unexpected error: %v", err)
	}
	if score == nil {
		t.Fatal("expected non-nil score even on transport failure")
	}

	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality recorded even on failure")
	}
	if q.Grade != "F" {
		t.Fatalf("grade = %q, want F", q.Grade)
	}
	if q.LastError == "" {
		t.Fatal("expected LastError on failure")
	}
}

// TestProbeQualitySync_UnavailableWhenStopped verifies error when stopped.
func TestProbeQualitySync_UnavailableWhenStopped(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	hash := node.HashFromRawOptions([]byte(`{"type":"probe-quality-stop"}`))
	pool.AddNodeFromSub(hash, []byte(`{"type":"probe-quality-stop"}`), "sub1")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)

	mgr := NewProbeManager(ProbeConfig{
		Pool: pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})
	mgr.Stop()

	_, err := mgr.ProbeQualitySync(hash)
	if err == nil {
		t.Fatal("expected error after stop")
	}
}
