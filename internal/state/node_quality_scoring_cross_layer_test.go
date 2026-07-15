package state

import (
	"encoding/json"
	"testing"

	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/probe"
)

// float64Ptr is a helper for creating *float64 literals in test fixtures.
func float64Ptr(v float64) *float64 { return &v }

// TestNodeQuality_ScoredProxyScoreRoundTrip verifies the end-to-end path:
//
//	scored ProxyScore → ProxyScoreToNodeQuality → CacheRepo persist → CacheRepo load
//
// This is the conversion→persistence half of the Phase 3B2 cross-layer
// verification. The API-projection half lives in the service package test
// (TestNodeQualitySummary_ScoredProxyScoreProjection).
//
// Assertions:
//   - Canonical CloudflareStatus string is stored faithfully
//   - CloudflareChallenged / CloudflareChallengeType are correctly derived
//     from challenge statuses only (js_challenge, captcha_challenge, block,
//     challenge → true; clean, not_detected, ng → false)
//   - ScoringPolicyVersion is set from ScoringBreakdown.Version (1) or 0
//     for legacy/nil breakdown
//   - ScoreBreakdown is valid non-double-encoded compact JSON containing
//     expected fields and NOT containing redundant top-level "grade" or
//     "round_results"
//   - Legacy nil breakdown produces empty ScoreBreakdown and version 0
func TestNodeQuality_ScoredProxyScoreRoundTrip(t *testing.T) {
	repo := newTestCacheRepo(t)

	t.Run("clean_status_with_breakdown", func(t *testing.T) {
		score := &probe.ProxyScore{
			Grade:            "B",
			Score:            72.5,
			Unstable:         false,
			ServiceReachable: true,
			APIReachable:     true,
			CloudflareStatus: probe.CFStatusClean,
			AvgLatencyMs:     150.0,
			ScoringBreakdown: &probe.ScoringResult{
				Version:          1,
				Score:            72.5,
				Grade:            "B",
				GradeFromScore:   "B",
				FinalGrade:       "B",
				EffectiveWeights: map[string]int{"service": 40, "api": 25, "cf": 20, "latency": 15, "stability": 0},
				SubScores: map[string]*probe.SubScoreEntry{
					"service": {Value: float64Ptr(100)},
					"api":     {Value: float64Ptr(80)},
					"cf":      {Value: float64Ptr(100)},
					"latency": {Value: float64Ptr(100)},
				},
				UnavailableDims: []string{"stability"},
				AppliedCaps:     nil,
				TerminalReason:  "",
			},
		}
		nq := probe.ProxyScoreToNodeQuality("generic", score, nil)
		nq.NodeHash = "scored-hash"
		nq.LastCheckedNs = 1000

		if err := repo.BulkUpsertNodeQuality([]model.NodeQuality{*nq}); err != nil {
			t.Fatal(err)
		}

		loaded, err := repo.LoadAllNodeQuality()
		if err != nil {
			t.Fatal(err)
		}
		var e *model.NodeQuality
		for i := range loaded {
			if loaded[i].NodeHash == "scored-hash" {
				e = &loaded[i]
				break
			}
		}
		if e == nil {
			t.Fatal("scored-hash not found in loaded quality")
		}

		// Canonical CF status.
		if e.CloudflareStatus != "clean" {
			t.Fatalf("CloudflareStatus = %q, want clean", e.CloudflareStatus)
		}
		// CloudflareChallenged must be false for clean.
		if e.CloudflareChallenged {
			t.Fatal("CloudflareChallenged should be false for clean status")
		}
		if e.CloudflareChallengeType != "" {
			t.Fatalf("CloudflareChallengeType = %q, want empty", e.CloudflareChallengeType)
		}
		// Scoring policy version.
		if e.ScoringPolicyVersion != 1 {
			t.Fatalf("ScoringPolicyVersion = %d, want 1", e.ScoringPolicyVersion)
		}
		// ScoreBreakdown must be non-empty and valid JSON.
		if e.ScoreBreakdown == "" {
			t.Fatal("ScoreBreakdown should not be empty")
		}
		assertValidNonDoubleEncodedBreakdown(t, e.ScoreBreakdown)
		// Verify key fields.
		var decoded map[string]any
		if err := json.Unmarshal([]byte(e.ScoreBreakdown), &decoded); err != nil {
			t.Fatalf("ScoreBreakdown unmarshal: %v", err)
		}
		if decoded["version"] != float64(1) {
			t.Fatalf("version = %v, want 1", decoded["version"])
		}
		if decoded["final_grade"] != "B" {
			t.Fatalf("final_grade = %v, want B", decoded["final_grade"])
		}
		// Must NOT contain redundant fields.
		if _, ok := decoded["grade"]; ok {
			t.Fatal("ScoreBreakdown must NOT contain redundant top-level 'grade'")
		}
		if _, ok := decoded["round_results"]; ok {
			t.Fatal("ScoreBreakdown must NOT contain 'round_results'")
		}
	})

	t.Run("block_challenge_derivation", func(t *testing.T) {
		score := &probe.ProxyScore{
			Grade:            "D",
			Score:            20,
			Unstable:         false,
			ServiceReachable: true,
			APIReachable:     false,
			CloudflareStatus: probe.CFStatusBlock,
			AvgLatencyMs:     300.0,
			ScoringBreakdown: &probe.ScoringResult{
				Version:          1,
				Score:            20,
				Grade:            "D",
				GradeFromScore:   "D",
				FinalGrade:       "F", // CF block caps at F
				EffectiveWeights: map[string]int{"service": 40, "cf": 60},
				SubScores: map[string]*probe.SubScoreEntry{
					"service": {Value: float64Ptr(100)},
					"cf":      {Value: float64Ptr(0)},
				},
				UnavailableDims: []string{},
				AppliedCaps: []probe.CapApplication{
					{Dimension: "cf", Reason: "cf_status_cap", Cap: "F"},
				},
				TerminalReason: "",
			},
		}
		nq := probe.ProxyScoreToNodeQuality("generic", score, nil)
		nq.NodeHash = "block-hash"
		nq.LastCheckedNs = 2000

		if err := repo.BulkUpsertNodeQuality([]model.NodeQuality{*nq}); err != nil {
			t.Fatal(err)
		}

		loaded, err := repo.LoadAllNodeQuality()
		if err != nil {
			t.Fatal(err)
		}
		var e *model.NodeQuality
		for i := range loaded {
			if loaded[i].NodeHash == "block-hash" {
				e = &loaded[i]
				break
			}
		}
		if e == nil {
			t.Fatal("block-hash not found in loaded quality")
		}

		// Canonical CF status.
		if e.CloudflareStatus != "block" {
			t.Fatalf("CloudflareStatus = %q, want block", e.CloudflareStatus)
		}
		// CloudflareChallenged must be true for block.
		if !e.CloudflareChallenged {
			t.Fatal("CloudflareChallenged should be true for block status")
		}
		if e.CloudflareChallengeType != "block" {
			t.Fatalf("CloudflareChallengeType = %q, want block", e.CloudflareChallengeType)
		}
		// Scoring policy version.
		if e.ScoringPolicyVersion != 1 {
			t.Fatalf("ScoringPolicyVersion = %d, want 1", e.ScoringPolicyVersion)
		}
		assertValidNonDoubleEncodedBreakdown(t, e.ScoreBreakdown)
	})

	t.Run("legacy_nil_breakdown", func(t *testing.T) {
		score := &probe.ProxyScore{
			Grade:            "A",
			Score:            100,
			ServiceReachable: true,
			CloudflareStatus: probe.CFStatusClean,
		}
		nq := probe.ProxyScoreToNodeQuality("generic", score, nil)
		nq.NodeHash = "legacy-hash"
		nq.LastCheckedNs = 3000

		if err := repo.BulkUpsertNodeQuality([]model.NodeQuality{*nq}); err != nil {
			t.Fatal(err)
		}

		loaded, err := repo.LoadAllNodeQuality()
		if err != nil {
			t.Fatal(err)
		}
		var e *model.NodeQuality
		for i := range loaded {
			if loaded[i].NodeHash == "legacy-hash" {
				e = &loaded[i]
				break
			}
		}
		if e == nil {
			t.Fatal("legacy-hash not found in loaded quality")
		}

		// Canonical CF status still stored.
		if e.CloudflareStatus != "clean" {
			t.Fatalf("CloudflareStatus = %q, want clean", e.CloudflareStatus)
		}
		// Scoring policy version must be 0 for legacy.
		if e.ScoringPolicyVersion != 0 {
			t.Fatalf("ScoringPolicyVersion = %d, want 0", e.ScoringPolicyVersion)
		}
		if e.ScoreBreakdown != "" {
			t.Fatalf("ScoreBreakdown = %q, want empty for legacy", e.ScoreBreakdown)
		}
	})
}

// assertValidNonDoubleEncodedBreakdown verifies that the compact breakdown
// string is valid JSON (not double-encoded) and matches expected structure.
func assertValidNonDoubleEncodedBreakdown(t *testing.T, raw string) {
	t.Helper()
	if !json.Valid([]byte(raw)) {
		t.Fatalf("ScoreBreakdown is not valid JSON: %s", raw)
	}
	// Verify it is NOT double-encoded: a double-encoded JSON string would
	// unmarshal as a string, not a map.
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("ScoreBreakdown unmarshal: %v", err)
	}
	// It must be a JSON object with keys, not a string.
	m, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("ScoreBreakdown is double-encoded (decoded as %T, want map[string]any)", decoded)
	}
	// Must have the expected compact fields.
	requiredKeys := []string{"version", "effective_weights", "sub_scores", "grade_from_score", "final_grade"}
	for _, k := range requiredKeys {
		if _, exists := m[k]; !exists {
			t.Fatalf("ScoreBreakdown missing required key %q", k)
		}
	}
}
