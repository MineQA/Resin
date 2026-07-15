package probe

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
)

// ---------------------------------------------------------------------------
// Cloudflare status types
// ---------------------------------------------------------------------------

// CloudflareStatus is the canonical CF observation outcome for a single round
// or the aggregate across rounds.
type CloudflareStatus string

const (
	// CFStatusEmpty is the zero value — legacy record not yet refreshed or
	// metadata-poor adapter could not classify.
	CFStatusEmpty            CloudflareStatus = ""
	CFStatusClean            CloudflareStatus = "clean"
	CFStatusNotDetected      CloudflareStatus = "not_detected"
	CFStatusJSChallenge      CloudflareStatus = "js_challenge"
	CFStatusCaptchaChallenge CloudflareStatus = "captcha_challenge"
	CFStatusBlock            CloudflareStatus = "block"
	CFStatusChallenge        CloudflareStatus = "challenge"
	CFStatusNG               CloudflareStatus = "ng"
)

// cfStatusSeverity ranks statuses for multi-round aggregation.
// Lower index = more severe.
var cfStatusSeverity = map[CloudflareStatus]int{
	CFStatusBlock:            0,
	CFStatusCaptchaChallenge: 1,
	CFStatusJSChallenge:      2,
	CFStatusChallenge:        3,
	CFStatusNG:               4,
	CFStatusClean:            5,
	CFStatusNotDetected:      6,
	CFStatusEmpty:            7,
}

// mostSevereCFStatus returns the most severe status from the given set using
// the locked severity ordering: block > captcha_challenge > js_challenge >
// challenge > ng > clean > not_detected > empty/unchecked.
func mostSevereCFStatus(statuses ...CloudflareStatus) CloudflareStatus {
	most := CFStatusEmpty
	worst := -1
	for _, s := range statuses {
		if sev, ok := cfStatusSeverity[s]; ok && (worst < 0 || sev < worst) {
			worst = sev
			most = s
		}
	}
	return most
}

// isChallengeStatus returns true for statuses that represent a detected
// Cloudflare challenge (block, captcha, JS, or generic challenge).
func isChallengeStatus(s CloudflareStatus) bool {
	switch s {
	case CFStatusBlock, CFStatusCaptchaChallenge, CFStatusJSChallenge, CFStatusChallenge:
		return true
	}
	return false
}

// classifyCloudflareStatus performs header-first classification of a service
// response to determine the Cloudflare observation outcome.
//
// Classification order:
//  1. cf-mitigated: challenge (authoritative challenge signal)
//  2. Status code-based hints (503, 403)
//  3. Supporting header evidence (Server: cloudflare, CF-Ray)
//  4. Content-Type-based hint (text/html)
//  5. Body fallback patterns (same as detectCloudflareChallenge)
//
// When headers are unavailable (StatusCode=0, Header=nil), body-only
// classification is used. If the body contains no challenge markers and no
// CF evidence exists, the result is CFStatusEmpty for metadata-poor paths,
// or CFStatusClean when positive CF evidence is present in headers.
func classifyCloudflareStatus(resp FetchResponse) CloudflareStatus {
	// --- Phase 1: Header-first classification ---

	// 1a. Authoritative cf-mitigated: challenge header.
	if resp.Header != nil {
		if v := resp.Header.Get("cf-mitigated"); strings.EqualFold(v, "challenge") {
			// The header authoritatively proves a challenge, but status codes do
			// not identify its subtype. Prefer body markers; otherwise retain the
			// generic challenge status rather than guessing CAPTCHA/JS.
			if challenged, ctype := detectCloudflareChallenge(resp.Body); challenged {
				switch ctype {
				case "captcha_challenge":
					return CFStatusCaptchaChallenge
				case "js_challenge":
					return CFStatusJSChallenge
				case "block":
					return CFStatusBlock
				}
			}
			return CFStatusChallenge
		}
	}

	// 1b. Supporting header evidence.
	hasCFEvidence := false
	if resp.Header != nil {
		server := resp.Header.Get("Server")
		if strings.Contains(strings.ToLower(server), "cloudflare") {
			hasCFEvidence = true
		}
		if resp.Header.Get("CF-Ray") != "" {
			hasCFEvidence = true
		}
	}

	// 1c. Status code hints.
	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusForbidden {
		if hasCFEvidence {
			return CFStatusBlock
		}
	}

	// --- Phase 2: Body fallback classification ---
	// Check for known Cloudflare challenge/block body markers.
	if len(resp.Body) > 0 {
		challenged, ctype := detectCloudflareChallenge(resp.Body)
		if challenged {
			switch ctype {
			case "captcha_challenge":
				return CFStatusCaptchaChallenge
			case "js_challenge":
				return CFStatusJSChallenge
			case "block":
				return CFStatusBlock
			default:
				return CFStatusChallenge
			}
		}
	}

	// --- Phase 3: Evidence-aware result ---
	if hasCFEvidence {
		// Positive CF headers but no challenge → clean.
		return CFStatusClean
	}

	if resp.StatusCode == 0 && resp.Header == nil {
		// Metadata-poor path (legacy adapter): body inspection found no known
		// challenge, but without headers/status we cannot distinguish clean from
		// a non-Cloudflare response. Preserve empty/unchecked rather than guess.
		return CFStatusEmpty
	}

	// Full response with no CF evidence and no challenge → not_detected.
	return CFStatusNotDetected
}

// ---------------------------------------------------------------------------
// Target profiles
// ---------------------------------------------------------------------------

// TargetProfile describes a service target for proxy checking.
type TargetProfile struct {
	// Name is the profile identifier (e.g. "openai", "grok").
	Name string

	// ServiceURL is the primary service endpoint to check.
	ServiceURL string

	// APIURL is an optional API endpoint for additional reachability checks.
	// Leave empty if the service has no separate API endpoint.
	APIURL string

	// CloudflareFlag indicates that this service is known to be served
	// through Cloudflare.
	//
	// Deprecated: CF observation is now unconditional for every profile.
	// This field has no behavioral effect and is retained only for
	// compatibility metadata / UI hints until removed in a future cleanup.
	CloudflareFlag bool

	// Indicators are keywords expected in the service response body.
	// They help confirm that the proxy reached the real service.
	Indicators []string
}

// Predefined target profiles.
var (
	ProfileGeneric = TargetProfile{
		Name:       "generic",
		ServiceURL: "https://www.gstatic.com/generate_204",
	}
	ProfileOpenAI = TargetProfile{
		Name:           "openai",
		ServiceURL:     "https://chatgpt.com",
		APIURL:         "https://api.openai.com/v1/models",
		CloudflareFlag: true,
		Indicators:     []string{"openai", "chatgpt"},
	}
	ProfileGrok = TargetProfile{
		Name:           "grok",
		ServiceURL:     "https://grok.com",
		APIURL:         "https://api.x.ai/v1/models",
		CloudflareFlag: true,
	}
	ProfileGemini = TargetProfile{
		Name:       "gemini",
		ServiceURL: "https://gemini.google.com",
		APIURL:     "https://generativelanguage.googleapis.com/v1/models",
		Indicators: []string{"gemini"},
	}
	ProfileClaude = TargetProfile{
		Name:       "claude",
		ServiceURL: "https://claude.ai",
		APIURL:     "https://api.anthropic.com/v1/models",
		Indicators: []string{"claude", "anthropic"},
	}
)

// Profiles returns a map of all built-in target profiles keyed by name.
func Profiles() map[string]TargetProfile {
	return map[string]TargetProfile{
		"generic": ProfileGeneric,
		"openai":  ProfileOpenAI,
		"grok":    ProfileGrok,
		"gemini":  ProfileGemini,
		"claude":  ProfileClaude,
	}
}

// LookupProfile returns the target profile for the given name.
// If name is empty or unknown it returns the generic profile.
func LookupProfile(name string) TargetProfile {
	if name == "" {
		return ProfileGeneric
	}
	profiles := Profiles()
	if p, ok := profiles[strings.ToLower(name)]; ok {
		return p
	}
	return ProfileGeneric
}

// ---------------------------------------------------------------------------
// Check options
// ---------------------------------------------------------------------------

// ProxyCheckOptions controls which checks are performed during a proxy check.
type ProxyCheckOptions struct {
	// ServiceReachability enables checking the primary service URL.
	ServiceReachability bool `json:"service_reachability"`

	// APIReachability enables checking the API URL (if the profile has one).
	//
	// Note: The current Fetcher interface only returns body, latency, and
	// error. HTTP status codes (401, 403) are not accessible at this layer.
	// A nil error is treated as reachable. Status-aware checks are reserved
	// for Phase 2+ when a status-returning Fetcher variant may be introduced.
	APIReachability bool `json:"api_reachability"`

	// CloudflareDetection is a legacy scoring-input field only.
	// CF observation runs unconditionally for every profile and is NOT gated
	// by this field. When the new nested scoring policy is absent, this
	// legacy boolean is used to derive the CF policy mode (true → score_and_grade,
	// false → observe_only). This field will be removed in a future release.
	CloudflareDetection bool `json:"cloudflare_detection"`

	// ProtocolDiscovery is a placeholder for future protocol detection.
	ProtocolDiscovery bool `json:"protocol_discovery"`

	// MultiRound enables multiple rounds of checks for consistency.
	MultiRound bool `json:"multi_round"`

	// Rounds is the number of check rounds when MultiRound is enabled.
	// Default: 1
	Rounds int `json:"rounds"`

	// IPInfoEnrichment is a placeholder for future IP info enrichment.
	IPInfoEnrichment bool `json:"ip_info_enrichment"`

	// ScoringPolicy is the optional canonical scoring policy for this check.
	// When nil, the old flat-option scoring behavior is used (legacy compat).
	// When non-nil, the new weighted scoring engine is used.
	ScoringPolicy *ScoringPolicy `json:"-"`
}

// ScoringPolicy is an alias for config.ScoringPolicy to avoid importing config
// in consumer code that only needs the type. The canonical type lives in config.
// This alias is resolved through the package import.
type ScoringPolicy = config.ScoringPolicy

// DefaultOptions returns ProxyCheckOptions with sensible defaults:
// service reachability and Cloudflare detection enabled, single round.
// The ScoringPolicy is nil (legacy compat normalization applies at caller).
func DefaultOptions() ProxyCheckOptions {
	return ProxyCheckOptions{
		ServiceReachability: true,
		CloudflareDetection: true,
		Rounds:              1,
	}
}

// ---------------------------------------------------------------------------
// Result types
// ---------------------------------------------------------------------------

// ProxyRoundResult holds the results from a single probe round.
type ProxyRoundResult struct {
	// Latency is the round-trip latency for the service check.
	Latency time.Duration

	// ServiceReachable indicates whether the service URL was reachable.
	ServiceReachable bool

	// APIReachable indicates whether the API URL was reachable.
	APIReachable bool

	// CloudflareChallenged indicates the service response body matched a
	// known Cloudflare challenge or block page pattern.
	//
	// Deprecated: Use CloudflareStatus instead. This field is derived from
	// CloudflareStatus in aggregateScore for backward compatibility.
	CloudflareChallenged bool

	// CloudflareChallengeType describes the type of challenge detected:
	// "js_challenge", "captcha_challenge", "block", or "".
	//
	// Deprecated: Use CloudflareStatus instead. This field is derived from
	// CloudflareStatus in aggregateScore for backward compatibility.
	CloudflareChallengeType string

	// Error is non-empty if the round encountered a fatal error.
	Error string

	// CloudflareStatus is the canonical CF observation outcome for this round.
	// Set by the observation phase of CheckProxy; never gates on old flags.
	CloudflareStatus CloudflareStatus `json:"cloudflare_status"`
}

// ProxyScore holds the aggregated results of a proxy check.
type ProxyScore struct {
	// Grade is the overall letter grade: A, B, C, D, or F.
	//   A = all required checks (service + API + no CF block) pass all rounds
	//   B = service or API all-round pass
	//   C = basic connectivity (any check reached at least once)
	//   D = partial success / inconsistent rounds / CF challenged
	//   F = no success
	Grade string

	// Score is the numeric score from 0–100 (rough indicator).
	Score float64

	// Unstable indicates multi-round results were inconsistent (some rounds
	// succeeded while others failed for the same check in the legacy scoring
	// path). In the new scoring path, Unstable is true when modal consistency
	// is below 100% for multi-round checks.
	Unstable bool

	// ScoringBreakdown is the full scoring result from the new weighted
	// scoring engine (Phase 3B1). It is nil when the legacy scoring path
	// was used. This is an in-memory/API-visible field and is NOT persisted.
	ScoringBreakdown *ScoringResult `json:"scoring_breakdown,omitempty"`

	// ServiceReachable indicates the service was reachable in at least one round.
	ServiceReachable bool

	// APIReachable indicates the API was reachable in at least one round.
	APIReachable bool

	// CloudflareChallenged indicates at least one round detected a Cloudflare
	// challenge or block page in the service response body.
	//
	// Deprecated: Use CloudflareStatus instead. Derived from aggregate status
	// for backward compatibility with existing filters and persistence.
	CloudflareChallenged bool

	// CloudflareChallengeType describes the type of challenge detected
	// (from the first round that was challenged).
	//
	// Deprecated: Use CloudflareStatus instead. Derived from aggregate status
	// for backward compatibility.
	CloudflareChallengeType string

	// CloudflareStatus is the aggregate CF observation outcome across all
	// rounds, using the locked severity ordering.
	CloudflareStatus CloudflareStatus `json:"cloudflare_status"`

	// AvgLatencyMs is the average latency across all successful rounds.
	AvgLatencyMs float64

	// RoundResults contains individual round results.
	RoundResults []ProxyRoundResult
}

// ---------------------------------------------------------------------------
// Core check
// ---------------------------------------------------------------------------

// CheckProxy performs a proxy check for the given profile and options.
// It uses the provided Fetcher to make requests through the specified node hash.
//
// Phase 3A restructuring:
//   - CF observation runs unconditionally for every profile (not gated by
//     CloudflareDetection or CloudflareFlag).
//   - When service evaluation is enabled, the service request response is
//     reused for CF observation (one request per round for both purposes).
//   - When service evaluation is disabled but a ServiceURL is present, a
//     single request is made for CF observation only; ServiceReachable is
//     NOT set because that criterion was not selected.
//   - Header-first classification (cf-mitigated: challenge) takes precedence
//     over body fallback.
//   - Legacy CloudflareChallenged/CloudflareChallengeType fields are derived
//     from CloudflareStatus in aggregateScore.
//
// CheckProxy accepts both Fetcher (legacy) and ResponseFetcher (quality).
// When responseFetcher is non-nil it is preferred over fetcher for service
// and CF requests, providing full response metadata. When responseFetcher is
// nil, a legacy adapter wraps fetcher with limited metadata confidence.
func CheckProxy(fetcher Fetcher, hash node.Hash, profile TargetProfile, opts ProxyCheckOptions) (*ProxyScore, error) {
	return CheckProxyWithResponse(fetcher, hash, profile, opts, nil)
}

// CheckProxyWithResponse is the response-aware variant of CheckProxy.
//
// When respFetcher is non-nil, it is used for service/CF requests, providing
// status code, headers, and final URL. When nil, the plain Fetcher is used
// with limited metadata (body bytes only — status 0, header nil).
//
// This split preserves the existing CheckProxy signature for backward
// compatibility while allowing quality callers to provide response metadata.
//
// Phase 3B1: When opts.ScoringPolicy is non-nil, the new weighted scoring
// engine is used. The custom CF target_url from the scoring policy controls
// whether an additional request per round is made for CF observation. When
// ScoringPolicy is nil, the old aggregateScore logic applies (legacy compat).
func CheckProxyWithResponse(fetcher Fetcher, hash node.Hash, profile TargetProfile, opts ProxyCheckOptions, respFetcher ResponseFetcher) (*ProxyScore, error) {
	if fetcher == nil {
		return nil, fmt.Errorf("proxy check: fetcher is nil")
	}

	rounds := opts.Rounds
	if rounds <= 0 {
		rounds = 1
	}
	if !opts.MultiRound && rounds > 1 {
		rounds = 1
	}

	// Determine effective CF target URL from scoring policy (Phase 3B1).
	cfTarget := profile.ServiceURL
	hasCustomCFTarget := false
	if opts.ScoringPolicy != nil && opts.ScoringPolicy.Cloudflare.TargetURL != "" {
		normalizedCustom, err := config.NormalizeURLForComparison(opts.ScoringPolicy.Cloudflare.TargetURL)
		if err != nil {
			return nil, fmt.Errorf("proxy check: invalid custom CF target URL %q: %w",
				opts.ScoringPolicy.Cloudflare.TargetURL, err)
		}
		normalizedService, serr := config.NormalizeURLForComparison(profile.ServiceURL)
		if serr != nil {
			// Built-in ServiceURL should always normalize cleanly; if not,
			// treat as no match to avoid a hard failure.
			normalizedService = ""
		}
		if normalizedCustom != normalizedService {
			cfTarget = normalizedCustom
			hasCustomCFTarget = true
		}
	}

	// Determine if we need a service request.
	// Without a custom CF target, CF observation reuses the service request
	// response, so a service request is needed whenever ServiceURL is set.
	// With a distinct custom CF target, the service request is only needed
	// when service reachability is explicitly enabled as a criterion.
	needsServiceRequest := profile.ServiceURL != ""
	if hasCustomCFTarget {
		needsServiceRequest = opts.ServiceReachability && profile.ServiceURL != ""
	}

	roundResults := make([]ProxyRoundResult, 0, rounds)
	// Collect latency samples from all successful distinct requests.
	var latencySamples []time.Duration

	for i := 0; i < rounds; i++ {
		result := ProxyRoundResult{}

		// --- Observation phase: service request ---
		if needsServiceRequest {
			var fetchResp FetchResponse
			var fetchErr error
			var respFetcherUsed ResponseFetcher

			if respFetcher != nil {
				respFetcherUsed = respFetcher
			} else {
				respFetcherUsed = LegacyResponseFetcher(fetcher)
			}

			fetchResp, fetchErr = respFetcherUsed(hash, profile.ServiceURL)

			if fetchErr != nil {
				result.Error = fmt.Sprintf("round %d: service: %v", i+1, fetchErr)
				result.CloudflareStatus = CFStatusNG
				result.Latency = fetchResp.Latency
			} else {
				result.Latency = fetchResp.Latency
				latencySamples = append(latencySamples, fetchResp.Latency)

				// Service reachability: set only when criterion is enabled.
				if opts.ServiceReachability {
					result.ServiceReachable = true
					if len(profile.Indicators) > 0 {
						bodyLower := strings.ToLower(string(fetchResp.Body))
						result.ServiceReachable = false
						for _, ind := range profile.Indicators {
							if strings.Contains(bodyLower, strings.ToLower(ind)) {
								result.ServiceReachable = true
								break
							}
						}
					}
				}

				// CF observation from service response (unconditional).
				result.CloudflareStatus = classifyCloudflareStatus(fetchResp)
			}
		}

		// --- Custom CF target request (distinct from service URL) ---
		if hasCustomCFTarget {
			var respFetcherUsed ResponseFetcher
			if respFetcher != nil {
				respFetcherUsed = respFetcher
			} else {
				respFetcherUsed = LegacyResponseFetcher(fetcher)
			}

			cfResp, cfErr := respFetcherUsed(hash, cfTarget)
			if cfErr != nil {
				// Custom CF target is authoritative for CF observation.
				// On transport failure, aggregate CF status is ng regardless
				// of the service response result.
				result.CloudflareStatus = CFStatusNG
			} else {
				latencySamples = append(latencySamples, cfResp.Latency)
				// Custom CF target is authoritative — its observation replaces
				// any prior service-domain result.
				result.CloudflareStatus = classifyCloudflareStatus(cfResp)
				// Do NOT set ServiceReachable from CF-only request.
			}
		}

		// API check.
		if opts.APIReachability && profile.APIURL != "" {
			if respFetcher != nil {
				apiResp, apiErr := respFetcher(hash, profile.APIURL)
				if apiErr == nil {
					result.APIReachable = true
					latencySamples = append(latencySamples, apiResp.Latency)
				}
			} else {
				body, latency, apiErr := fetcher(hash, profile.APIURL)
				if apiErr == nil {
					result.APIReachable = true
					_ = body
					latencySamples = append(latencySamples, latency)
				}
			}
		}

		roundResults = append(roundResults, result)
	}

	// --- Scoring phase ---
	var score ProxyScore
	if opts.ScoringPolicy != nil {
		// New weighted scoring engine (Phase 3B1).
		hasAPIURL := profile.APIURL != ""
		sr := Score(roundResults, *opts.ScoringPolicy, opts, hasAPIURL, latencySamples)

		score.Grade = sr.FinalGrade
		score.Score = sr.Score
		score.ScoringBreakdown = &sr

		// Unstable is true when modal consistency < 100% for multi-round.
		if opts.MultiRound && len(roundResults) >= 2 {
			serviceOK := make([]bool, len(roundResults))
			apiOK := make([]bool, len(roundResults))
			cfStatuses := make([]CloudflareStatus, len(roundResults))
			for i, r := range roundResults {
				serviceOK[i] = r.ServiceReachable
				apiOK[i] = r.APIReachable
				cfStatuses[i] = r.CloudflareStatus
			}
			if computeConsistency(serviceOK, apiOK, cfStatuses) < 100 {
				score.Unstable = true
			}
		}

		// Derive legacy compatibility fields from aggregate CF status.
		aggCF := mostSevereCFStatus(collectCFStatuses(roundResults)...)
		score.CloudflareStatus = aggCF
		score.CloudflareChallenged = isChallengeStatus(aggCF)
		if score.CloudflareChallenged {
			score.CloudflareChallengeType = string(aggCF)
		}

		// Legacy aggregate reachability.
		for _, r := range roundResults {
			if r.ServiceReachable {
				score.ServiceReachable = true
			}
			if r.APIReachable {
				score.APIReachable = true
			}
		}

		// Round-trip latency.
		if len(latencySamples) > 0 {
			var total time.Duration
			for _, s := range latencySamples {
				total += s
			}
			score.AvgLatencyMs = float64(total/time.Duration(len(latencySamples))) / float64(time.Millisecond)
		}

		score.RoundResults = roundResults
	} else {
		// Legacy scoring path (Phase 3A compatibility).
		score = aggregateScore(roundResults, opts)
		score.RoundResults = roundResults
	}

	return &score, nil
}

// collectCFStatuses returns the CloudflareStatus values from round results.
func collectCFStatuses(results []ProxyRoundResult) []CloudflareStatus {
	out := make([]CloudflareStatus, len(results))
	for i, r := range results {
		if r.CloudflareStatus != "" {
			out[i] = r.CloudflareStatus
		} else if r.CloudflareChallenged {
			switch r.CloudflareChallengeType {
			case "js_challenge":
				out[i] = CFStatusJSChallenge
			case "captcha_challenge":
				out[i] = CFStatusCaptchaChallenge
			case "block":
				out[i] = CFStatusBlock
			default:
				out[i] = CFStatusChallenge
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Cloudflare challenge detection
// ---------------------------------------------------------------------------

// detectCloudflareChallenge checks whether the response body indicates a
// Cloudflare challenge or block page. It returns whether a challenge was
// detected along with the challenge type ("js_challenge", "captcha_challenge",
// "block", or "").
//
// This inspects the body only — no additional outbound request is made.
func detectCloudflareChallenge(body []byte) (challenged bool, challengeType string) {
	s := strings.ToLower(string(body))

	switch {
	case strings.Contains(s, "__cf_chl_f_tk") || strings.Contains(s, "cf-chl-widget"):
		// CAPTCHA challenge (or JS challenge with captcha fallback).
		// These DOM markers appear in CF challenge pages served to the client.
		return true, "captcha_challenge"
	case strings.Contains(s, "cf-browser-verification"):
		// JS challenge page — the most common Cloudflare challenge.
		return true, "js_challenge"
	case strings.Contains(s, "just a moment") && strings.Contains(s, "cloudflare"):
		// JS challenge page title + footer text.
		return true, "js_challenge"
	case strings.Contains(s, "attention required") && strings.Contains(s, "cloudflare"):
		// Cloudflare block page (e.g., when WAF blocks the request).
		return true, "block"
	case strings.Contains(s, "error code 1020"):
		// Cloudflare error 1020 — access denied by firewall rule.
		return true, "block"
	}
	return false, ""
}

// ---------------------------------------------------------------------------
// Scoring
// ---------------------------------------------------------------------------

// aggregateScore computes the numeric score and letter grade from round results.
//
// Phase 3A changes:
//   - CloudflareStatus is now the canonical CF observation field, populated
//     by the CheckProxy observation phase without gating on old flags.
//   - Legacy CloudflareChallenged/CloudflareChallengeType fields are derived
//     from the aggregate CloudflareStatus for backward compatibility.
//   - The CloudflareDetection boolean still controls the score penalty and
//     grade cap (temporary compatibility shim, removed in Phase 3B).
//   - Observation is independent from scoring: CF observation runs for all
//     profiles, while scoring impact is gated by CloudflareDetection.
func aggregateScore(results []ProxyRoundResult, opts ProxyCheckOptions) ProxyScore {
	var score ProxyScore

	if len(results) == 0 {
		score.Score = 0
		score.Grade = "F"
		return score
	}

	// Cross-round analysis.
	serviceOK := make([]bool, len(results))
	apiOK := make([]bool, len(results))
	cfStatuses := make([]CloudflareStatus, len(results))

	for i, r := range results {
		serviceOK[i] = r.ServiceReachable
		apiOK[i] = r.APIReachable
		// Phase 3A: prefer CloudflareStatus when set; fall back to legacy
		// CloudflareChallenged field for backward-compatible unit tests.
		if r.CloudflareStatus != "" {
			cfStatuses[i] = r.CloudflareStatus
		} else if r.CloudflareChallenged {
			switch r.CloudflareChallengeType {
			case "js_challenge":
				cfStatuses[i] = CFStatusJSChallenge
			case "captcha_challenge":
				cfStatuses[i] = CFStatusCaptchaChallenge
			case "block":
				cfStatuses[i] = CFStatusBlock
			default:
				cfStatuses[i] = CFStatusChallenge
			}
		}
	}

	serviceAllPass := !opts.ServiceReachability || allTrue(serviceOK)
	apiAllPass := !opts.APIReachability || allTrue(apiOK)

	// Aggregate CF status using locked severity ordering.
	aggCFStatus := mostSevereCFStatus(cfStatuses...)
	score.CloudflareStatus = aggCFStatus

	// Derive legacy compatibility fields from aggregate status.
	anyCFChallenged := isChallengeStatus(aggCFStatus)
	score.CloudflareChallenged = anyCFChallenged
	if anyCFChallenged {
		score.CloudflareChallengeType = string(aggCFStatus)
	} else {
		score.CloudflareChallengeType = ""
	}

	// Unstable: multi-round with inconsistent results for the same check.
	unstable := opts.MultiRound && len(results) > 1 &&
		(!allSame(serviceOK) || !allSame(apiOK))

	// Numeric score: simple weighted average without latency bonuses.
	var points float64
	var totalLatency time.Duration
	successCount := 0

	for _, r := range results {
		if r.ServiceReachable {
			points += 50
		}
		if r.APIReachable {
			points += 25
		}
		if r.ServiceReachable || r.APIReachable {
			successCount++
		}
		totalLatency += r.Latency
	}
	points /= float64(len(results))

	// Multi-round stability bonus (all rounds same result).
	if !unstable && opts.MultiRound && len(results) > 1 {
		points += 10
	}

	// CF challenge penalty — gated by CloudflareDetection (Phase 3A
	// compatibility shim; moved to policy in Phase 3B).
	if anyCFChallenged && opts.CloudflareDetection {
		points -= 20
	}

	// Clamp.
	if points < 0 {
		points = 0
	}
	if points > 100 {
		points = 100
	}
	score.Score = math.Round(points)

	// Average latency.
	if successCount > 0 {
		score.AvgLatencyMs = float64(totalLatency/time.Duration(successCount)) / float64(time.Millisecond)
	}

	// Letter grade from semantic rules.
	// CF challenged (when detection enabled) forces D — the proxy reached
	// Cloudflare but not the actual service content. This overrides A/B.
	allPassedAllRounds := serviceAllPass && apiAllPass && (!opts.CloudflareDetection || !anyCFChallenged)

	switch {
	case allPassedAllRounds:
		score.Grade = "A"
	case anyCFChallenged && opts.CloudflareDetection:
		score.Grade = "D"
	case unstable:
		// Inconsistent multi-round results override B (partial all-pass).
		score.Grade = "D"
	case (opts.ServiceReachability && serviceAllPass) || (opts.APIReachability && apiAllPass):
		score.Grade = "B"
	case anyTrue(serviceOK) || anyTrue(apiOK):
		score.Grade = "C"
	default:
		score.Grade = "F"
	}

	score.Unstable = unstable

	score.ServiceReachable = anyTrue(serviceOK)
	score.APIReachable = anyTrue(apiOK)

	return score
}

// ---------------------------------------------------------------------------
// Bool slice helpers
// ---------------------------------------------------------------------------

func allTrue(vals []bool) bool {
	for _, v := range vals {
		if !v {
			return false
		}
	}
	return true
}

func anyTrue(vals []bool) bool {
	for _, v := range vals {
		if v {
			return true
		}
	}
	return false
}

func allSame(vals []bool) bool {
	if len(vals) <= 1 {
		return true
	}
	first := vals[0]
	for _, v := range vals[1:] {
		if v != first {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// NormalizeProxyString performs basic normalisation on a proxy input string.
// It strips only standard proxy scheme prefixes (http, https, socks4, socks5).
// Non-standard prefixes (ss://, vmess://, trojan://) are preserved as-is
// since they carry essential metadata beyond transport addressing.
func NormalizeProxyString(proxy string) string {
	proxy = strings.TrimSpace(proxy)
	prefixes := []string{"https://", "http://", "socks5://", "socks5h://", "socks4://"}
	for _, p := range prefixes {
		proxy = strings.TrimPrefix(proxy, p)
	}
	return proxy
}
