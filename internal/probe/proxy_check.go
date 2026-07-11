package probe

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Resinat/Resin/internal/node"
)

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
	// through Cloudflare. When true and CloudflareDetection is enabled in
	// ProxyCheckOptions, the service response body is inspected for
	// Cloudflare challenge/block pages.
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

	// CloudflareDetection enables inspecting the service response body for
	// Cloudflare challenge or block pages (e.g., JS challenge, CAPTCHA).
	// This check is only performed when the profile's CloudflareFlag is true.
	// Unlike egress probes (which use cloudflare.com/cdn-cgi/trace), this
	// detects whether the target service itself challenged the request.
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
}

// DefaultOptions returns ProxyCheckOptions with sensible defaults:
// service reachability and Cloudflare detection enabled, single round.
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
	CloudflareChallenged bool

	// CloudflareChallengeType describes the type of challenge detected:
	// "js_challenge", "captcha_challenge", "block", or "".
	CloudflareChallengeType string

	// Error is non-empty if the round encountered a fatal error.
	Error string
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
	// succeeded while others failed for the same check).
	Unstable bool

	// ServiceReachable indicates the service was reachable in at least one round.
	ServiceReachable bool

	// APIReachable indicates the API was reachable in at least one round.
	APIReachable bool

	// CloudflareChallenged indicates at least one round detected a Cloudflare
	// challenge or block page in the service response body.
	CloudflareChallenged bool

	// CloudflareChallengeType describes the type of challenge detected
	// (from the first round that was challenged).
	CloudflareChallengeType string

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
func CheckProxy(fetcher Fetcher, hash node.Hash, profile TargetProfile, opts ProxyCheckOptions) (*ProxyScore, error) {
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

	roundResults := make([]ProxyRoundResult, 0, rounds)

	for i := 0; i < rounds; i++ {
		result := ProxyRoundResult{}

		if opts.ServiceReachability && profile.ServiceURL != "" {
			body, latency, err := fetcher(hash, profile.ServiceURL)
			if err != nil {
				result.Error = fmt.Sprintf("round %d: service: %v", i+1, err)
			} else {
				result.Latency = latency
				result.ServiceReachable = true

				// Verify response body contains at least one expected indicator.
				if len(profile.Indicators) > 0 {
					bodyLower := strings.ToLower(string(body))
					result.ServiceReachable = false
					for _, ind := range profile.Indicators {
						if strings.Contains(bodyLower, strings.ToLower(ind)) {
							result.ServiceReachable = true
							break
						}
					}
				}

				// Cloudflare challenge detection from service response body.
				if profile.CloudflareFlag && opts.CloudflareDetection && len(body) > 0 {
					challenged, cftype := detectCloudflareChallenge(body)
					result.CloudflareChallenged = challenged
					result.CloudflareChallengeType = cftype
				}
			}
		}

		if opts.APIReachability && profile.APIURL != "" {
			// Note: The Fetcher interface only returns body, latency, and error.
			// HTTP status codes (401, 403) are not accessible at this layer.
			// A nil error is treated as reachable; status-aware checks are a
			// future Phase 2 enhancement.
			_, _, err := fetcher(hash, profile.APIURL)
			if err == nil {
				result.APIReachable = true
			}
		}

		roundResults = append(roundResults, result)
	}

	score := aggregateScore(roundResults, opts)
	score.RoundResults = roundResults

	return &score, nil
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
	cfChallenged := make([]bool, len(results))

	for i, r := range results {
		serviceOK[i] = r.ServiceReachable
		apiOK[i] = r.APIReachable
		cfChallenged[i] = r.CloudflareChallenged
	}

	serviceAllPass := !opts.ServiceReachability || allTrue(serviceOK)
	apiAllPass := !opts.APIReachability || allTrue(apiOK)
	anyCFChallenged := anyTrue(cfChallenged)

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

	// CF challenge penalty.
	if anyCFChallenged {
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
	case (opts.ServiceReachability && serviceAllPass) || (opts.APIReachability && apiAllPass):
		score.Grade = "B"
	case anyTrue(serviceOK) || anyTrue(apiOK):
		if unstable {
			score.Grade = "D"
		} else {
			score.Grade = "C"
		}
	default:
		score.Grade = "F"
	}

	score.Unstable = unstable
	score.CloudflareChallenged = anyCFChallenged
	for _, r := range results {
		if r.CloudflareChallenged {
			score.CloudflareChallengeType = r.CloudflareChallengeType
			break
		}
	}

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
