package probe

import (
	"encoding/json"
	"errors"
	"strings"
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
		Grade:             "B",
		Score:             85,
		ServiceReachable:  true,
		CloudflareStatus:  CFStatusClean,
		ScoringBreakdown:  sr,
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
