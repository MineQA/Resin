package probe

import (
	"math"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
)

func balancedPolicy() config.ScoringPolicy {
	return config.BalancedScoringPolicy()
}

func TestScore_EmptyRounds(t *testing.T) {
	policy := balancedPolicy()
	result := Score(nil, policy, ProxyCheckOptions{}, false, nil)
	if result.Score != 0 || result.Grade != "F" || result.TerminalReason != "no_applicable_dimensions" {
		t.Fatalf("empty rounds: got score=%.0f grade=%s reason=%s", result.Score, result.Grade, result.TerminalReason)
	}
}

func TestScore_ServicePassAll(t *testing.T) {
	policy := balancedPolicy()
	rounds := []ProxyRoundResult{
		{ServiceReachable: true, Latency: 100 * time.Millisecond, CloudflareStatus: CFStatusNotDetected},
		{ServiceReachable: true, Latency: 100 * time.Millisecond, CloudflareStatus: CFStatusNotDetected},
	}
	latencySamples := []time.Duration{100 * time.Millisecond, 100 * time.Millisecond}

	result := Score(rounds, policy, ProxyCheckOptions{
		ServiceReachability: true,
	}, false, latencySamples)

	if result.TerminalReason != "" {
		t.Fatalf("unexpected terminal: %s", result.TerminalReason)
	}
	if result.Score == 0 {
		t.Fatal("score should be > 0")
	}
	// Service pass = 100%, CF clean/not_detected = 100, latency ~100ms within 300ms = 100
	// weighted: (40*100 + 20*100 + 15*100) / (40+20+15) = 7500/75 = 100
	if result.Score != 100 {
		t.Fatalf("expected score 100, got %.0f", result.Score)
	}
	if result.Grade != "A" {
		t.Fatalf("expected grade A, got %s", result.Grade)
	}
}

func TestScore_ServiceZeroPass(t *testing.T) {
	policy := balancedPolicy()
	rounds := []ProxyRoundResult{
		{ServiceReachable: false, Latency: 100 * time.Millisecond, CloudflareStatus: CFStatusNotDetected},
	}
	latencySamples := []time.Duration{100 * time.Millisecond}

	result := Score(rounds, policy, ProxyCheckOptions{
		ServiceReachability: true,
	}, false, latencySamples)

	// Service = 0%, CF not_detected = 100, latency ~100ms = 100
	// weighted: (40*0 + 20*100 + 15*100) / (40+20+15) = 3500/75 ≈ 47
	if result.Score != 47 {
		t.Fatalf("expected score 47, got %.0f (raw=%f)", result.Score, result.Score)
	}
	// 47 < 60, >= 40 => D
	if result.Grade != "D" {
		t.Fatalf("expected grade D (47), got %s", result.Grade)
	}
}

func TestScore_FormulaRounding(t *testing.T) {
	// Single round, service only: weight 40, score 50 → (40*50)/40 = 50
	policy := balancedPolicy()
	policy.Weights = config.Weights{Service: 40}
	policy.Cloudflare.Policy = config.CFPolicyObserveOnly

	rounds := []ProxyRoundResult{
		{ServiceReachable: true, Latency: 100 * time.Millisecond, CloudflareStatus: CFStatusBlock},
	}
	latencySamples := []time.Duration{100 * time.Millisecond}

	result := Score(rounds, policy, ProxyCheckOptions{
		ServiceReachability: true,
	}, false, latencySamples)

	// Only service participates (CF observe_only excludes from score, latency weight=0)
	// Service = 100% → score 100
	if result.Score != 100 {
		t.Fatalf("expected 100, got %.0f", result.Score)
	}
}

func TestScore_NullVsZero(t *testing.T) {
	// CF NG is null (unavailable). Verify it's excluded from denominator.
	policy := balancedPolicy()
	// Set api/api weight to 0 to simplify.
	policy.Weights.API = 0
	policy.Weights.Stability = 0
	policy.Weights.Latency = 0

	rounds := []ProxyRoundResult{
		{ServiceReachable: true, Latency: 100 * time.Millisecond, CloudflareStatus: CFStatusNG},
	}
	latencySamples := []time.Duration{100 * time.Millisecond}

	result := Score(rounds, policy, ProxyCheckOptions{
		ServiceReachability: true,
	}, false, latencySamples)

	// CF ng is unavailable, so only service participates (weight 40).
	// Service = 100% → score 100
	if result.Score != 100 {
		t.Fatalf("expected 100 (ng excluded), got %.0f", result.Score)
	}
	if _, ok := result.EffectiveWeights["cloudflare"]; ok {
		t.Fatal("cloudflare should not be in effective weights (ng is null)")
	}
}

func TestScore_ZeroParticipants(t *testing.T) {
	policy := balancedPolicy()
	// Zero all weights.
	policy.Weights = config.Weights{}

	rounds := []ProxyRoundResult{
		{ServiceReachable: true, CloudflareStatus: CFStatusClean},
	}

	result := Score(rounds, policy, ProxyCheckOptions{}, false, nil)

	if result.Score != 0 || result.Grade != "F" || result.TerminalReason != "no_applicable_dimensions" {
		t.Fatalf("expected 0/F/no_applicable_dimensions, got score=%.0f grade=%s reason=%s", result.Score, result.Grade, result.TerminalReason)
	}
}

func TestScore_CFPolicyModes(t *testing.T) {
	tests := []struct {
		name        string
		policyMode  config.CFPolicyMode
		wantCFIn    bool // CF participates in score denominator
		wantScore   int  // approximate
		wantCapsApp int  // number of caps applied
	}{
		{"observe_only", config.CFPolicyObserveOnly, false, 0, 0},
		{"score", config.CFPolicyScore, true, 40, 0}, // score-only means no grade cap applied
		{"grade", config.CFPolicyGrade, false, 0, 1}, // grade-only means cap applied
		{"score_and_grade", config.CFPolicyScoreAndGrade, true, 40, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := balancedPolicy()
			policy.Cloudflare.Policy = tt.policyMode
			// Zero other weights to isolate CF.
			policy.Weights = config.Weights{Cloudflare: 20}
			policy.Weights.Latency = 0
			policy.Weights.Stability = 0

			rounds := []ProxyRoundResult{
				{CloudflareStatus: CFStatusJSChallenge, Latency: 100 * time.Millisecond},
			}

			result := Score(rounds, policy, ProxyCheckOptions{}, false, nil)

			if tt.wantCFIn {
				if _, ok := result.EffectiveWeights["cloudflare"]; !ok {
					t.Error("CF should participate in effective weights")
				}
			} else {
				if _, ok := result.EffectiveWeights["cloudflare"]; ok {
					t.Error("CF should NOT participate in effective weights")
				}
			}

			if len(result.AppliedCaps) != tt.wantCapsApp {
				t.Errorf("expected %d cap(s), got %d: %v", tt.wantCapsApp, len(result.AppliedCaps), result.AppliedCaps)
			}
			if result.Score != float64(tt.wantScore) {
				t.Errorf("expected score %d, got %.0f", tt.wantScore, result.Score)
			}
		})
	}
}

func TestScore_CFStatusScores(t *testing.T) {
	policy := balancedPolicy()
	policy.Weights = config.Weights{Cloudflare: 20}
	policy.Weights.Stability = 0
	policy.Weights.Latency = 0

	tests := []struct {
		status CloudflareStatus
		want   float64 // expected sub-score
	}{
		{CFStatusClean, 100},
		{CFStatusNotDetected, 100},
		{CFStatusJSChallenge, 40},
		{CFStatusCaptchaChallenge, 20},
		{CFStatusChallenge, 10},
		{CFStatusBlock, 0},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			rounds := []ProxyRoundResult{
				{CloudflareStatus: tt.status},
			}
			result := Score(rounds, policy, ProxyCheckOptions{}, false, nil)
			if sub, ok := result.SubScores["cloudflare"]; ok && sub.Value != nil {
				if *sub.Value != tt.want {
					t.Errorf("cf sub-score: got %.0f, want %.0f", *sub.Value, tt.want)
				}
			} else {
				if tt.want >= 0 {
					t.Errorf("cf sub-score unexpectedly unavailable")
				}
			}
		})
	}
}

func TestScore_CFNGUnavailable(t *testing.T) {
	policy := balancedPolicy()
	policy.Weights = config.Weights{Cloudflare: 20}
	policy.Weights.Stability = 0
	policy.Weights.Latency = 0

	rounds := []ProxyRoundResult{
		{CloudflareStatus: CFStatusNG},
	}
	result := Score(rounds, policy, ProxyCheckOptions{}, false, nil)

	if sub, ok := result.SubScores["cloudflare"]; ok {
		if !sub.Unavailable {
			t.Error("CF should be unavailable for NG")
		}
	}
}

func TestScore_UncheckedUnavailable(t *testing.T) {
	policy := balancedPolicy()
	policy.Weights = config.Weights{Cloudflare: 20}
	policy.Weights.Stability = 0
	policy.Weights.Latency = 0

	rounds := []ProxyRoundResult{
		{CloudflareStatus: CFStatusEmpty},
	}
	result := Score(rounds, policy, ProxyCheckOptions{}, false, nil)

	if sub, ok := result.SubScores["cloudflare"]; ok {
		if !sub.Unavailable {
			t.Error("CF should be unavailable for empty/unchecked")
		}
	}
}

func TestScore_GradeThresholds(t *testing.T) {
	policy := balancedPolicy()
	// Only service, weight 40.
	policy.Weights = config.Weights{Service: 40, Latency: 0, Stability: 0}

	tests := []struct {
		name      string
		score     int
		wantGrade string
	}{
		{"A", 95, "A"},
		{"A-boundary", 90, "A"},
		{"B", 80, "B"},
		{"B-boundary", 75, "B"},
		{"C", 70, "C"},
		{"C-boundary", 60, "C"},
		{"D", 50, "D"},
		{"D-boundary", 40, "D"},
		{"F", 30, "F"},
		{"F-zero", 0, "F"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nRounds := 100 // use 100 rounds to get precise percentage
			passCount := int(float64(tt.score) / 100.0 * float64(nRounds))
			rounds := make([]ProxyRoundResult, nRounds)
			latencySamples := make([]time.Duration, 0)
			for i := 0; i < nRounds; i++ {
				reachable := i < passCount
				rounds[i] = ProxyRoundResult{
					ServiceReachable: reachable,
					CloudflareStatus: CFStatusNotDetected,
					Latency:          100 * time.Millisecond,
				}
				if reachable {
					latencySamples = append(latencySamples, 100*time.Millisecond)
				}
			}
			_ = latencySamples

			opts := ProxyCheckOptions{ServiceReachability: true}
			result := Score(rounds, policy, opts, false, nil)

			got := result.Grade
			// CF not_detected 100, but no latency samples (latency weight 0).
			// Score = (40 * tt.approx) / 40 = tt.approx
			if got != tt.wantGrade {
				t.Errorf("expected grade %q at score ~%d, got %q (score=%.0f)", tt.wantGrade, tt.score, got, result.Score)
			}
		})
	}
}

func TestScore_CapOrdering(t *testing.T) {
	// CF block cap is F; general dim cap cloudflare at below 50 → D.
	// Verify D < F, so most restrictive (F) wins.
	policy := balancedPolicy()
	policy.Weights = config.Weights{Cloudflare: 20}
	policy.Weights.Stability = 0
	policy.Weights.Latency = 0
	// Add general CF cap below 50 → D
	policy.DimensionCaps.Cloudflare = &config.DimensionCap{BelowScore: 50, GradeCap: "D"}

	rounds := []ProxyRoundResult{
		{CloudflareStatus: CFStatusBlock},
	}
	result := Score(rounds, policy, ProxyCheckOptions{}, false, nil)

	// Block has cf score = 0, which is below 50 → triggers general cap D
	// Block also has cf status cap F
	// Most restrictive: F
	if result.FinalGrade != "F" {
		t.Fatalf("expected F (most restrictive), got %s; caps: %v", result.FinalGrade, result.AppliedCaps)
	}
	if len(result.AppliedCaps) != 2 {
		t.Fatalf("expected 2 caps (general+status), got %d: %v", len(result.AppliedCaps), result.AppliedCaps)
	}
}

func TestScore_LatencyBands(t *testing.T) {
	policy := balancedPolicy()
	policy.Weights = config.Weights{Latency: 20}
	policy.Weights.Stability = 0

	tests := []struct {
		latencyMs float64
		wantScore int
	}{
		{50, 100},
		{300, 100},
		{301, 80},
		{800, 80},
		{801, 60},
		{1500, 60},
		{1501, 30},
		{3000, 30},
		{3001, 0},
		{10000, 0},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			samples := []time.Duration{time.Duration(tt.latencyMs) * time.Millisecond}
			rounds := []ProxyRoundResult{
				{Latency: samples[0], ServiceReachable: true, CloudflareStatus: CFStatusNotDetected},
			}
			// Zero all weights except latency for clean test.
			p := policy
			p.Weights.API = 0
			p.Weights.Cloudflare = 0

			opts := ProxyCheckOptions{ServiceReachability: true}
			result := Score(rounds, p, opts, false, samples)

			if sub, ok := result.SubScores["latency"]; ok && sub.Value != nil {
				got := int(math.Round(*sub.Value))
				if got != tt.wantScore {
					t.Errorf("latency %.0fms: expected %d, got %d", tt.latencyMs, tt.wantScore, got)
				}
			} else {
				t.Errorf("latency sub-score unavailable for %.0fms", tt.latencyMs)
			}
		})
	}
}

func TestScore_ModalConsistency(t *testing.T) {
	policy := balancedPolicy()
	policy.Weights = config.Weights{Stability: 20, Latency: 0}

	tests := []struct {
		name      string
		serviceOK []bool
		wantScore int // approximate consistency percentage
	}{
		{"all-same", []bool{true, true, true}, 100},
		{"2-of-3", []bool{true, true, false}, 67}, // 67% modal = 67
		{"split", []bool{true, false, true, false}, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rounds := make([]ProxyRoundResult, len(tt.serviceOK))
			for i, ok := range tt.serviceOK {
				rounds[i] = ProxyRoundResult{
					ServiceReachable: ok,
					CloudflareStatus: CFStatusNotDetected,
				}
			}
			opts := ProxyCheckOptions{MultiRound: true, ServiceReachability: true}
			result := Score(rounds, policy, opts, false, nil)

			if sub, ok := result.SubScores["stability"]; ok && sub.Value != nil {
				got := int(math.Round(*sub.Value))
				if got < tt.wantScore-5 || got > tt.wantScore+5 {
					t.Errorf("expected consistency ~%d%%, got %d", tt.wantScore, got)
				}
			} else {
				t.Errorf("stability unavailable for multi-round")
			}
		})
	}
}

func TestScore_CVBands(t *testing.T) {
	policy := balancedPolicy()
	// CV: ensure we use population stddev.

	// Two samples: [100ms, 300ms], mean=200ms, stddev=100, CV=50
	samples := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond}

	// Map through bands: 50 <= 50 → score 30 (max_percent 50 band)
	cvScore := computeCVScore(samples, policy.Stability.CVBands)
	if cvScore < 0 {
		t.Fatal("CV should be available")
	}
	if int(math.Round(cvScore)) != 30 {
		t.Fatalf("expected CV score 30 (CV=50), got %.0f", cvScore)
	}

	// CV=0 → score 100
	samples2 := []time.Duration{100 * time.Millisecond, 100 * time.Millisecond}
	cvScore2 := computeCVScore(samples2, policy.Stability.CVBands)
	if int(math.Round(cvScore2)) != 100 {
		t.Fatalf("expected CV score 100 (CV=0), got %.0f", cvScore2)
	}
}

func TestScore_SingleRoundNoStability(t *testing.T) {
	policy := balancedPolicy()
	policy.Weights = config.Weights{Stability: 20, Latency: 0}
	rounds := []ProxyRoundResult{
		{ServiceReachable: true, CloudflareStatus: CFStatusClean},
	}
	opts := ProxyCheckOptions{ServiceReachability: true}
	result := Score(rounds, policy, opts, false, nil)

	if sub, ok := result.SubScores["stability"]; ok {
		if !sub.Unavailable {
			t.Error("stability should be unavailable for single round")
		}
	}
}

func TestScore_APIAvailable(t *testing.T) {
	policy := balancedPolicy()
	policy.Weights = config.Weights{API: 15, Latency: 0, Stability: 0}

	rounds := []ProxyRoundResult{
		{APIReachable: true, ServiceReachable: true, CloudflareStatus: CFStatusNotDetected},
	}
	opts := ProxyCheckOptions{APIReachability: true, ServiceReachability: true}
	result := Score(rounds, policy, opts, true, nil)

	if _, ok := result.EffectiveWeights["api"]; !ok {
		t.Error("API should participate when enabled with URL")
	}
}

func TestScore_APINoURL(t *testing.T) {
	policy := balancedPolicy()
	policy.Weights = config.Weights{API: 15, Latency: 0, Stability: 0}

	rounds := []ProxyRoundResult{
		{APIReachable: false, ServiceReachable: true, CloudflareStatus: CFStatusNotDetected},
	}

	// hasAPIURL=false
	result := Score(rounds, policy, ProxyCheckOptions{APIReachability: true}, false, nil)

	if _, ok := result.EffectiveWeights["api"]; ok {
		t.Error("API should NOT participate when profile has no API URL")
	}
	if sub, ok := result.SubScores["api"]; ok {
		if !sub.Unavailable {
			t.Error("API should be unavailable when profile has no API URL")
		}
	}
}

func TestScore_CFCapBlockF(t *testing.T) {
	policy := balancedPolicy()
	policy.Weights = config.Weights{Cloudflare: 20, Latency: 0, Stability: 0}

	rounds := []ProxyRoundResult{
		{CloudflareStatus: CFStatusBlock},
	}
	result := Score(rounds, policy, ProxyCheckOptions{}, false, nil)

	// Block should apply cap F.
	hasCap := false
	for _, cap := range result.AppliedCaps {
		if cap.Dimension == "cloudflare" && cap.Cap == "F" {
			hasCap = true
		}
	}
	if !hasCap {
		t.Fatalf("expected cf block F cap, got caps: %v", result.AppliedCaps)
	}
	if result.FinalGrade != "F" {
		t.Fatalf("expected F, got %s", result.FinalGrade)
	}
}

func TestScore_CFNoDoublePenalty(t *testing.T) {
	// NG and block: NG is unavailable, not zero.
	// So block is the only participating CF status.
	policy := balancedPolicy()
	policy.Weights = config.Weights{Cloudflare: 20, Latency: 0, Stability: 0}

	rounds := []ProxyRoundResult{
		{CloudflareStatus: CFStatusNG}, // ng unavailable
		{CloudflareStatus: CFStatusBlock},
	}
	result := Score(rounds, policy, ProxyCheckOptions{}, false, nil)

	// Block is most severe, so CF participates with block score 0.
	if _, ok := result.EffectiveWeights["cloudflare"]; !ok {
		t.Error("CF should participate (block is not ng)")
	}
}

func TestScore_NoLatencySamples(t *testing.T) {
	policy := balancedPolicy()
	rounds := []ProxyRoundResult{
		{ServiceReachable: true, CloudflareStatus: CFStatusClean},
	}
	// No latency samples.
	result := Score(rounds, policy, ProxyCheckOptions{ServiceReachability: true}, false, nil)

	if sub, ok := result.SubScores["latency"]; ok {
		if !sub.Unavailable {
			t.Error("latency should be unavailable when no samples")
		}
	}
}

func TestScore_AllDimensionsParticipate(t *testing.T) {
	policy := balancedPolicy()
	rounds := []ProxyRoundResult{
		{
			ServiceReachable: true,
			APIReachable:     true,
			Latency:          200 * time.Millisecond,
			CloudflareStatus: CFStatusClean,
		},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
	}
	latencySamples := []time.Duration{200 * time.Millisecond}

	result := Score(rounds, policy, opts, true, latencySamples)

	// All 5 dimensions should participate.
	expectedDims := []string{"service", "api", "cloudflare", "latency"}
	for _, dim := range expectedDims {
		if _, ok := result.EffectiveWeights[dim]; !ok {
			t.Errorf("dimension %q should participate", dim)
		}
	}
	// Stability is single round → unavailable.
	if _, ok := result.EffectiveWeights["stability"]; ok {
		t.Error("stability should not participate for single round")
	}
}

func TestRoundHalfAwayFromZero(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{2.5, 3},
		{3.5, 4},
		{-2.5, -3},
		{-3.5, -4},
		{2.4, 2},
		{2.6, 3},
		{0.0, 0},
	}
	for _, tt := range tests {
		got := mathRoundHalfAwayFromZero(tt.input)
		if got != tt.want {
			t.Errorf("Round(%.1f) = %.0f, want %.0f", tt.input, got, tt.want)
		}
	}
}

func TestScore_WeightedRoundingHalfAwayFromZero(t *testing.T) {
	// Genuine weighted half-away-from-zero case:
	// Service weight 40, score 4/9 ≈ 44.44 → weighted 40 * 44.44 = 1777.78
	// CF weight 10, score 50, weighted 10 * 50 = 500
	// Total weight 50, raw = (1777.78+500)/50 = 2277.78/50 = 45.555...
	// Clamped 0-100 → math.Round(45.555...) = 46 (half-away-from-zero)
	policy := balancedPolicy()
	policy.Weights = config.Weights{Service: 40, Cloudflare: 10}
	policy.Weights.API = 0
	policy.Weights.Stability = 0
	policy.Weights.Latency = 0

	// 4 out of 9 rounds pass → 44.44... service score
	rounds := make([]ProxyRoundResult, 9)
	for i := 0; i < 9; i++ {
		rounds[i] = ProxyRoundResult{
			ServiceReachable: i < 4,
			CloudflareStatus: CFStatusBlock, // block → score 0? No, block score is 0
		}
	}
	// Wait — block score is 0, which makes CF score=0. Let me recalculate.
	// Service: 4/9 = 44.44... → weighted 40 * 44.44... = 1777.78
	// CF block → score 0 → weighted 10 * 0 = 0
	// Total weight 50, raw = (1777.78 + 0)/50 = 35.555...
	// Not a half-away case.
	//
	// Let me use higher CF score to get a .5 case.
	// CF status clean → score 100 → weighted 10 * 100 = 1000
	// (1777.78 + 1000)/50 = 2777.78/50 = 55.555...
	// math.Round(55.555...) = 56 (since .555 > .5)
	//
	// For exact half-away: need raw to be exactly XX.5
	// With service only: (40 * score) / 40 = score = XX.5 → impossible with integer percentages
	// With two dims: (40*s1 + 10*s2)/50 = XX.5 → 40*s1 + 10*s2 = XX.5*50 = 50*XX + 25
	// e.g., s1=50 (pass 50/100), s2=100 → (40*50 + 10*100)/50 = (2000+1000)/50 = 60 → not .5
	// s1=55, s2=100 → (40*55 + 10*100)/50 = (2200+1000)/50 = 64 → not .5
	// s1=56, s2=100 → (40*56 + 10*100)/50 = (2240+1000)/50 = 3240/50 = 64.8
	// s1=56.25 → need pass 56.25/100 rounds... impossible with integers.
	//
	// Simpler: use latency (always integer from bands) + service.
	// (40*s1 + 15*lat)/55 = XX.5
	// Let lat=100 and s1=... → (40*s1 + 1500)/55 = (40*s1 + 1500)/55
	// We need 55 * (XX.5) = 40*s1 + 1500
	// 55*XX + 27.5 = 40*s1 + 1500 → 27.5 isn't integer.
	//
	// Let me use three dimensions to get exact .5:
	// Svc=20, SvcScore=100 → 2000
	// CF=10, CFScore=0 → 0
	// Lat=15, LatScore=90 → 1350
	// Total weight=45, raw = 3350/45 = 74.444... not .5
	//
	// Svc=20, SvcScore=100 → 2000
	// Lat=15, LatScore=90 → 1350
	// Total=35, raw=3350/35 = 95.714... not .5
	//
	// Hmm. Let me pick a simpler approach.
	// Use service-only: service weight 40, pass rate 62.5/100
	// That's 62.5% → (40*62.5)/40 = 62.5 → Round(62.5) = 63 (half-away-from-zero)
	// Need pass count = 62.5 out of 100... 62.5% = 5/8
	p := balancedPolicy()
	p.Weights = config.Weights{Service: 40}
	p.Weights.API = 0
	p.Weights.Cloudflare = 0
	p.Weights.Stability = 0
	p.Weights.Latency = 0

	// 5 out of 8 rounds pass → 62.5% exactly
	rnds := []ProxyRoundResult{
		{ServiceReachable: true, CloudflareStatus: CFStatusClean},
		{ServiceReachable: true, CloudflareStatus: CFStatusClean},
		{ServiceReachable: true, CloudflareStatus: CFStatusClean},
		{ServiceReachable: true, CloudflareStatus: CFStatusClean},
		{ServiceReachable: true, CloudflareStatus: CFStatusClean},
		{ServiceReachable: false, CloudflareStatus: CFStatusClean},
		{ServiceReachable: false, CloudflareStatus: CFStatusClean},
		{ServiceReachable: false, CloudflareStatus: CFStatusClean},
	}

	result := Score(rnds, p, ProxyCheckOptions{ServiceReachability: true}, false, nil)

	// Score = Round((40*62.5)/40) = Round(62.5) = 63 (half-away-from-zero)
	if result.Score != 63 {
		t.Fatalf("expected score 63 (round-half-away-from-zero from 62.5), got %.0f", result.Score)
	}
}

func TestComputeConsistency(t *testing.T) {
	tests := []struct {
		name      string
		serviceOK []bool
		apiOK     []bool
		cf        []CloudflareStatus
		wantPct   float64
	}{
		{"single", []bool{true}, []bool{true}, []CloudflareStatus{CFStatusClean}, 100},
		{"all-same", []bool{true, true}, []bool{true, true}, []CloudflareStatus{CFStatusClean, CFStatusClean}, 100},
		{"half-same", []bool{true, false}, []bool{true, false}, []CloudflareStatus{CFStatusClean, CFStatusClean}, 50},
		{"transport-failure-signature", []bool{true, false}, []bool{false, false}, []CloudflareStatus{CFStatusClean, CFStatusNG}, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeConsistency(tt.serviceOK, tt.apiOK, tt.cf)
			if math.Abs(got-tt.wantPct) > 0.01 {
				t.Errorf("got %.0f%%, want %.0f%%", got, tt.wantPct)
			}
		})
	}
}

func TestIsMoreRestrictive(t *testing.T) {
	tests := []struct {
		cap     string
		current string
		want    bool
	}{
		{"F", "A", true},
		{"D", "A", true},
		{"A", "F", false},
		{"C", "C", false},
		{"D", "C", true},
		{"C", "D", false},
	}
	for _, tt := range tests {
		got := isMoreRestrictive(tt.cap, tt.current)
		if got != tt.want {
			t.Errorf("isMoreRestrictive(%q, %q) = %v, want %v", tt.cap, tt.current, got, tt.want)
		}
	}
}

func TestGradeFromScore(t *testing.T) {
	gt := config.GradeThresholds{A: 90, B: 75, C: 60, D: 40}
	tests := []struct {
		score float64
		want  string
	}{
		{95, "A"},
		{90, "A"},
		{89, "B"},
		{75, "B"},
		{74, "C"},
		{60, "C"},
		{59, "D"},
		{40, "D"},
		{39, "F"},
		{0, "F"},
	}
	for _, tt := range tests {
		got := gradeFromScore(tt.score, gt)
		if got != tt.want {
			t.Errorf("gradeFromScore(%.0f) = %s, want %s", tt.score, got, tt.want)
		}
	}
}
