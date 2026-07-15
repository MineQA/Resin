package probe

import (
	"fmt"
	"math"
	"time"

	"github.com/Resinat/Resin/internal/config"
)

// ---------------------------------------------------------------------------
// Scoring result type
// ---------------------------------------------------------------------------

// SubScoreEntry holds a single dimension's sub-score.
type SubScoreEntry struct {
	Value       *float64 `json:"value,omitempty"`
	Unavailable bool     `json:"unavailable,omitempty"`
}

// CapApplication records an applied grade cap and its reason.
type CapApplication struct {
	Dimension string `json:"dimension"`
	Reason    string `json:"reason"`
	Cap       string `json:"cap"`
}

// ScoringResult is the pure result of the scoring engine.
type ScoringResult struct {
	Version          int                       `json:"version"`
	Score            float64                   `json:"score"`
	Grade            string                    `json:"grade"`
	GradeFromScore   string                    `json:"grade_from_score"`
	FinalGrade       string                    `json:"final_grade"`
	EffectiveWeights map[string]int            `json:"effective_weights"`
	SubScores        map[string]*SubScoreEntry `json:"sub_scores"`
	UnavailableDims  []string                  `json:"unavailable_dimensions"`
	AppliedCaps      []CapApplication          `json:"applied_caps"`
	TerminalReason   string                    `json:"terminal_reason,omitempty"`
}

// ---------------------------------------------------------------------------
// Score — pure scoring function
// ---------------------------------------------------------------------------

// Score computes the aggregate score, grade, sub-scores, and applied caps from
// round observations using the given normalized scoring policy.
//
// Arguments:
//   - rounds: per-round ProxyRoundResult from the observation phase.
//   - policy: a validated, normalized ScoringPolicy.
//   - opts: the original ProxyCheckOptions used (determines which dimensions
//     were enabled/checked).
//   - hasAPIURL: whether the profile has a non-empty API URL.
//   - latencySamples: latency measurements from all successful distinct HTTP
//     requests (service, API, separate CF target) for the current check.
//
// The function is a pure calculation with no side effects.
func Score(rounds []ProxyRoundResult, policy config.ScoringPolicy, opts ProxyCheckOptions, hasAPIURL bool, latencySamples []time.Duration) ScoringResult {
	result := ScoringResult{
		Version:          policy.Version,
		SubScores:        make(map[string]*SubScoreEntry),
		EffectiveWeights: make(map[string]int),
		AppliedCaps:      make([]CapApplication, 0),
	}

	if len(rounds) == 0 {
		result.Score = 0
		result.Grade = "F"
		result.GradeFromScore = "F"
		result.FinalGrade = "F"
		result.TerminalReason = "no_applicable_dimensions"
		return result
	}

	// Collect round-level data.
	serviceOK := make([]bool, len(rounds))
	apiOK := make([]bool, len(rounds))
	cfStatuses := make([]CloudflareStatus, len(rounds))
	for i, r := range rounds {
		serviceOK[i] = r.ServiceReachable
		apiOK[i] = r.APIReachable
		cfStatuses[i] = r.CloudflareStatus
	}

	// Aggregate CF status.
	aggCF := mostSevereCFStatus(cfStatuses...)

	// -----------------------------------------------------------------------
	// Compute dimension sub-scores
	// -----------------------------------------------------------------------

	// --- Service sub-score ---
	if policy.Weights.Service > 0 && opts.ServiceReachability {
		passCount := 0
		for _, ok := range serviceOK {
			if ok {
				passCount++
			}
		}
		if passCount > 0 || len(rounds) > 0 {
			score := float64(passCount) / float64(len(rounds)) * 100
			result.SubScores["service"] = &SubScoreEntry{Value: &score}
			result.EffectiveWeights["service"] = policy.Weights.Service
		}
	}
	if _, ok := result.SubScores["service"]; !ok {
		result.SubScores["service"] = &SubScoreEntry{Unavailable: true}
		result.UnavailableDims = append(result.UnavailableDims, "service")
	}

	// --- API sub-score ---
	if policy.Weights.API > 0 && opts.APIReachability && hasAPIURL {
		passCount := 0
		for _, ok := range apiOK {
			if ok {
				passCount++
			}
		}
		score := float64(passCount) / float64(len(rounds)) * 100
		result.SubScores["api"] = &SubScoreEntry{Value: &score}
		result.EffectiveWeights["api"] = policy.Weights.API
	}
	if _, ok := result.SubScores["api"]; !ok {
		result.SubScores["api"] = &SubScoreEntry{Unavailable: true}
		result.UnavailableDims = append(result.UnavailableDims, "api")
	}

	// --- Cloudflare sub-score ---
	cfPolicy := policy.Cloudflare.Policy
	includeCFScore := cfPolicy == config.CFPolicyScore || cfPolicy == config.CFPolicyScoreAndGrade
	applyCFGradeCaps := cfPolicy == config.CFPolicyGrade || cfPolicy == config.CFPolicyScoreAndGrade

	if policy.Weights.Cloudflare > 0 && aggCF != "" && aggCF != CFStatusEmpty {
		cfScoreVal := cfStatusScore(aggCF, policy.Cloudflare.StatusScores)
		if cfScoreVal != nil && includeCFScore {
			score := float64(*cfScoreVal)
			result.SubScores["cloudflare"] = &SubScoreEntry{Value: &score}
			result.EffectiveWeights["cloudflare"] = policy.Weights.Cloudflare
		} else {
			result.SubScores["cloudflare"] = &SubScoreEntry{Unavailable: true}
			result.UnavailableDims = append(result.UnavailableDims, "cloudflare")
		}
	} else {
		result.SubScores["cloudflare"] = &SubScoreEntry{Unavailable: true}
		result.UnavailableDims = append(result.UnavailableDims, "cloudflare")
	}

	// --- Latency sub-score ---
	if policy.Weights.Latency > 0 && len(latencySamples) > 0 {
		// Arithmetic mean.
		var totalNs float64
		for _, s := range latencySamples {
			totalNs += float64(s)
		}
		meanMs := totalNs / float64(len(latencySamples)) / float64(time.Millisecond)

		ls := latencyScore(meanMs, policy.Latency.Bands)
		result.SubScores["latency"] = &SubScoreEntry{Value: &ls}
		result.EffectiveWeights["latency"] = policy.Weights.Latency
	}
	if _, ok := result.SubScores["latency"]; !ok {
		result.SubScores["latency"] = &SubScoreEntry{Unavailable: true}
		result.UnavailableDims = append(result.UnavailableDims, "latency")
	}

	// --- Stability sub-score ---
	if policy.Weights.Stability > 0 && opts.MultiRound && len(rounds) >= 2 {
		// Outcome consistency: modal signature.
		consistencyPct := computeConsistency(serviceOK, apiOK, cfStatuses)

		// CV from latency samples.
		cvScore := computeCVScore(latencySamples, policy.Stability.CVBands)

		var stabScore float64
		if cvScore < 0 {
			// CV unavailable (<2 samples or mean=0).
			stabScore = consistencyPct
		} else if consistencyPct < cvScore {
			stabScore = consistencyPct
		} else {
			stabScore = cvScore
		}

		result.SubScores["stability"] = &SubScoreEntry{Value: &stabScore}
		result.EffectiveWeights["stability"] = policy.Weights.Stability
	}
	if _, ok := result.SubScores["stability"]; !ok {
		result.SubScores["stability"] = &SubScoreEntry{Unavailable: true}
		result.UnavailableDims = append(result.UnavailableDims, "stability")
	}

	// -----------------------------------------------------------------------
	// Apply caps (before terminal check to record CF caps even when no dims
	// participate for grade/score_and_grade policy modes).
	// -----------------------------------------------------------------------

	// 1. General dimension caps (only when sub-scores exist).
	for _, dim := range []string{"service", "api", "cloudflare", "stability", "latency"} {
		dc := getDimCap(&policy, dim)
		if dc == nil {
			continue
		}
		entry := result.SubScores[dim]
		if entry == nil || entry.Value == nil || entry.Unavailable {
			continue
		}
		if *entry.Value < float64(dc.BelowScore) {
			result.AppliedCaps = append(result.AppliedCaps, CapApplication{
				Dimension: dim,
				Reason:    fmt.Sprintf("sub_score %.0f < %d", *entry.Value, dc.BelowScore),
				Cap:       dc.GradeCap,
			})
		}
	}

	// 2. CF status-specific caps (only for grade/score_and_grade).
	if applyCFGradeCaps && aggCF != "" {
		cfCap := cfGradeCap(aggCF, policy.Cloudflare.GradeCaps)
		if cfCap != "" {
			result.AppliedCaps = append(result.AppliedCaps, CapApplication{
				Dimension: "cloudflare",
				Reason:    fmt.Sprintf("cf_status %q cap", string(aggCF)),
				Cap:       cfCap,
			})
		}
	}

	// -----------------------------------------------------------------------
	// Weighted final score
	// -----------------------------------------------------------------------

	if len(result.EffectiveWeights) == 0 {
		result.Score = 0
		result.Grade = "F"
		result.GradeFromScore = "F"
		result.FinalGrade = "F"
		result.TerminalReason = "no_applicable_dimensions"
		// Still need most-restrictive cap applied to grade.
		applyMostRestrictiveCap(&result)
		return result
	}

	var weightedSum float64
	var totalWeight int
	for dim, weight := range result.EffectiveWeights {
		entry := result.SubScores[dim]
		if entry != nil && entry.Value != nil {
			weightedSum += float64(weight) * (*entry.Value)
			totalWeight += weight
		}
	}

	if totalWeight == 0 {
		result.Score = 0
		result.Grade = "F"
		result.GradeFromScore = "F"
		result.FinalGrade = "F"
		result.TerminalReason = "no_applicable_dimensions"
		applyMostRestrictiveCap(&result)
		return result
	}

	rawScore := weightedSum / float64(totalWeight)
	rawScore = mathRoundHalfAwayFromZero(rawScore)

	// Clamp 0–100.
	if rawScore < 0 {
		rawScore = 0
	}
	if rawScore > 100 {
		rawScore = 100
	}

	result.Score = rawScore

	// Grade from score thresholds (inclusive).
	result.GradeFromScore = gradeFromScore(rawScore, policy.GradeThresholds)
	result.Grade = result.GradeFromScore
	result.FinalGrade = result.GradeFromScore

	// Apply recorded caps to final grade.
	applyMostRestrictiveCap(&result)

	return result
}

// applyMostRestrictiveCap applies the most restrictive grade from all recorded
// caps to FinalGrade. It sets FinalGrade to the worst grade among all caps.
func applyMostRestrictiveCap(result *ScoringResult) {
	for _, cap := range result.AppliedCaps {
		if isMoreRestrictive(cap.Cap, result.FinalGrade) {
			result.FinalGrade = cap.Cap
		}
	}
}

// ---------------------------------------------------------------------------
// Sub-score helpers
// ---------------------------------------------------------------------------

// cfStatusScore returns the numeric score for a CF status, or nil if unavailable.
func cfStatusScore(status CloudflareStatus, ss config.CFStatusScores) *int {
	switch status {
	case CFStatusClean:
		return ss.Clean
	case CFStatusNotDetected:
		return ss.NotDetected
	case CFStatusJSChallenge:
		return ss.JSChallenge
	case CFStatusCaptchaChallenge:
		return ss.CaptchaChallenge
	case CFStatusChallenge:
		return ss.Challenge
	case CFStatusBlock:
		return ss.Block
	case CFStatusNG:
		return ss.NG
	case CFStatusEmpty:
		return ss.Unchecked
	default:
		return nil
	}
}

// cfGradeCap returns the grade cap for a CF status, or empty string if none.
func cfGradeCap(status CloudflareStatus, caps config.CFGradeCaps) string {
	switch status {
	case CFStatusJSChallenge:
		return caps.JSChallenge
	case CFStatusCaptchaChallenge:
		return caps.CaptchaChallenge
	case CFStatusChallenge:
		return caps.Challenge
	case CFStatusBlock:
		return caps.Block
	default:
		return ""
	}
}

// latencyScore maps a mean latency (ms) through ascending bands.
// Returns the score of the first band where mean_ms <= max_ms.
// The final open-ended band catches all higher values.
func latencyScore(meanMs float64, bands []config.LatencyBand) float64 {
	for _, b := range bands {
		if b.MaxMS == nil {
			return float64(b.Score)
		}
		if meanMs <= float64(*b.MaxMS) {
			return float64(b.Score)
		}
	}
	return 0
}

// computeConsistency calculates the percentage of rounds matching the modal
// outcome signature (service_ok, api_ok, per_round_cf_status).
// Returns 0..100.
func computeConsistency(serviceOK, apiOK []bool, cfStatuses []CloudflareStatus) float64 {
	if len(serviceOK) < 2 {
		return 100
	}

	type signature struct {
		s  bool
		a  bool
		cf CloudflareStatus
	}

	counts := make(map[signature]int)
	for i := range serviceOK {
		sig := signature{s: serviceOK[i], a: apiOK[i], cf: cfStatuses[i]}
		counts[sig]++
	}

	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}

	return float64(maxCount) / float64(len(serviceOK)) * 100
}

// computeCVScore calculates the CV-based score from latency samples.
// Returns -1 if CV is unavailable (<2 samples or mean=0).
// Otherwise returns a score from the CV bands (0..100).
func computeCVScore(samples []time.Duration, bands []config.CVBand) float64 {
	if len(samples) < 2 {
		return -1
	}

	var sum float64
	for _, s := range samples {
		sum += float64(s)
	}
	mean := sum / float64(len(samples))
	if mean == 0 {
		return -1
	}

	var varianceSum float64
	for _, s := range samples {
		diff := float64(s) - mean
		varianceSum += diff * diff
	}
	// Population standard deviation.
	stdDev := math.Sqrt(varianceSum / float64(len(samples)))
	cv := 100 * stdDev / mean

	// Map through ascending CV bands.
	for _, b := range bands {
		if b.MaxPercent == nil {
			return float64(b.Score)
		}
		if cv <= float64(*b.MaxPercent) {
			return float64(b.Score)
		}
	}

	return 0
}

// gradeFromScore maps a clamped score (0-100) to a letter grade using
// the configured thresholds (inclusive).
func gradeFromScore(score float64, gt config.GradeThresholds) string {
	s := int(score)
	switch {
	case s >= gt.A:
		return "A"
	case s >= gt.B:
		return "B"
	case s >= gt.C:
		return "C"
	case s >= gt.D:
		return "D"
	default:
		return "F"
	}
}

// isMoreRestrictive returns true if cap is a worse grade than current.
// Order: A < B < C < D < F (A is best, F is worst).
func isMoreRestrictive(cap, current string) bool {
	order := map[string]int{"A": 0, "B": 1, "C": 2, "D": 3, "F": 4}
	return order[cap] > order[current]
}

// getDimCap returns the DimensionCap for a named dimension, or nil.
func getDimCap(policy *config.ScoringPolicy, dim string) *config.DimensionCap {
	switch dim {
	case "service":
		return policy.DimensionCaps.Service
	case "api":
		return policy.DimensionCaps.API
	case "cloudflare":
		return policy.DimensionCaps.Cloudflare
	case "stability":
		return policy.DimensionCaps.Stability
	case "latency":
		return policy.DimensionCaps.Latency
	}
	return nil
}

// mathRoundHalfAwayFromZero delegates to math.Round (Go 1.25 implements
// round-half-away-from-zero).
func mathRoundHalfAwayFromZero(f float64) float64 {
	return math.Round(f)
}
