package probe

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
)

// ---------------------------------------------------------------------------
// Profile lookup
// ---------------------------------------------------------------------------

func TestLookupProfile_Default(t *testing.T) {
	p := LookupProfile("")
	if p.Name != "generic" {
		t.Fatalf("expected generic, got %s", p.Name)
	}
}

func TestLookupProfile_Known(t *testing.T) {
	tests := []struct {
		name       string
		wantURL    string
		wantAPIURL string
		wantCF     bool
	}{
		{"generic", "https://www.gstatic.com/generate_204", "", false},
		{"openai", "https://chatgpt.com", "https://api.openai.com/v1/models", true},
		{"grok", "https://grok.com", "https://api.x.ai/v1/models", true},
		{"gemini", "https://gemini.google.com", "https://generativelanguage.googleapis.com/v1/models", false},
		{"claude", "https://claude.ai", "https://api.anthropic.com/v1/models", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := LookupProfile(tt.name)
			if p.ServiceURL != tt.wantURL {
				t.Errorf("ServiceURL = %q, want %q", p.ServiceURL, tt.wantURL)
			}
			if p.APIURL != tt.wantAPIURL {
				t.Errorf("APIURL = %q, want %q", p.APIURL, tt.wantAPIURL)
			}
			if p.CloudflareFlag != tt.wantCF {
				t.Errorf("CloudflareFlag = %v, want %v", p.CloudflareFlag, tt.wantCF)
			}
		})
	}
}

func TestLookupProfile_Unknown_FallsBackToGeneric(t *testing.T) {
	p := LookupProfile("nonexistent")
	if p.Name != "generic" {
		t.Fatalf("expected generic fallback, got %s", p.Name)
	}
}

func TestLookupProfile_CaseInsensitive(t *testing.T) {
	p := LookupProfile("OpenAI")
	if p.Name != "openai" {
		t.Fatalf("expected openai, got %s", p.Name)
	}
}

func TestProfiles_AllPresent(t *testing.T) {
	ps := Profiles()
	for _, name := range []string{"generic", "openai", "grok", "gemini", "claude"} {
		if _, ok := ps[name]; !ok {
			t.Errorf("expected profile %q in Profiles()", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Profile specific checks
// ---------------------------------------------------------------------------

func TestProfile_GrokCloudflareFlagTrue(t *testing.T) {
	p := LookupProfile("grok")
	if !p.CloudflareFlag {
		t.Error("ProfileGrok should have CloudflareFlag = true")
	}
}

func TestProfile_ClaudeAPIURL(t *testing.T) {
	p := LookupProfile("claude")
	want := "https://api.anthropic.com/v1/models"
	if p.APIURL != want {
		t.Errorf("ProfileClaude APIURL = %q, want %q", p.APIURL, want)
	}
}

// ---------------------------------------------------------------------------
// Options default / normalisation
// ---------------------------------------------------------------------------

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if !opts.ServiceReachability {
		t.Error("ServiceReachability should be true")
	}
	if !opts.CloudflareDetection {
		t.Error("CloudflareDetection should be true")
	}
	if opts.APIReachability {
		t.Error("APIReachability should be false by default")
	}
	if opts.ProtocolDiscovery {
		t.Error("ProtocolDiscovery should be false by default")
	}
	if opts.IPInfoEnrichment {
		t.Error("IPInfoEnrichment should be false by default")
	}
	if opts.Rounds != 1 {
		t.Errorf("Rounds should be 1, got %d", opts.Rounds)
	}
}

func TestOptions_NormaliseRounds(t *testing.T) {
	var calls int
	fetcher := func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
		calls++
		return []byte("ok"), 10 * time.Millisecond, nil
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		Rounds:              5,
		MultiRound:          false,
	}
	_, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 fetcher call when MultiRound=false, got %d", calls)
	}
}

func TestOptions_ZeroRoundsDefaultsToOne(t *testing.T) {
	var calls int
	fetcher := func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
		calls++
		return []byte("ok"), 10 * time.Millisecond, nil
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		Rounds:              0,
	}
	_, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 fetcher call when Rounds=0, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// Cloudflare challenge detection (unit tests)
// ---------------------------------------------------------------------------

func TestDetectCloudflareChallenge_JSChallenge(t *testing.T) {
	// CF JS challenge with cf-browser-verification widget.
	body := []byte(`<!DOCTYPE html><html><head><title>Just a moment...</title>
		<script src="/cf-browser-verification"></script></html>`)
	challenged, ctype := detectCloudflareChallenge(body)
	if !challenged {
		t.Fatal("expected CF challenge detected")
	}
	if ctype != "js_challenge" {
		t.Fatalf("expected js_challenge, got %q", ctype)
	}
}

func TestDetectCloudflareChallenge_JSChallengeJustAMoment(t *testing.T) {
	// CF JS challenge with "Just a moment..." title + Cloudflare.
	body := []byte(`<html><head><title>Just a moment...</title>
		<span>Checking your browser</span>
		<footer>Cloudflare</footer></html>`)
	challenged, ctype := detectCloudflareChallenge(body)
	if !challenged {
		t.Fatal("expected CF challenge detected")
	}
	if ctype != "js_challenge" {
		t.Fatalf("expected js_challenge, got %q", ctype)
	}
}

func TestDetectCloudflareChallenge_CaptchaChallenge(t *testing.T) {
	// CF CAPTCHA challenge (cf-chl-widget).
	body := []byte(`<html><body>
		<div id="cf-chl-widget"></div>
		<input type="hidden" name="__cf_chl_f_tk" value="abc123"/>
		</body></html>`)
	challenged, ctype := detectCloudflareChallenge(body)
	if !challenged {
		t.Fatal("expected CF challenge detected")
	}
	if ctype != "captcha_challenge" {
		t.Fatalf("expected captcha_challenge, got %q", ctype)
	}
}

func TestDetectCloudflareChallenge_BlockPage(t *testing.T) {
	// CF block / WAF page.
	body := []byte(`<html><head><title>Attention Required! | Cloudflare</title>
		<p>This website is using a security service to protect itself.</p></html>`)
	challenged, ctype := detectCloudflareChallenge(body)
	if !challenged {
		t.Fatal("expected CF challenged (block)")
	}
	if ctype != "block" {
		t.Fatalf("expected block, got %q", ctype)
	}
}

func TestDetectCloudflareChallenge_ErrorCode1020(t *testing.T) {
	body := []byte(`<html><head><title>Error 1020</title>
		<p>Error code 1020</p>
		<p>Access denied</p></html>`)
	challenged, ctype := detectCloudflareChallenge(body)
	if !challenged {
		t.Fatal("expected CF challenged (error 1020)")
	}
	if ctype != "block" {
		t.Fatalf("expected block, got %q", ctype)
	}
}

func TestDetectCloudflareChallenge_NoMatch(t *testing.T) {
	body := []byte(`<html><head><title>Welcome</title><body>Normal page content</body></html>`)
	challenged, _ := detectCloudflareChallenge(body)
	if challenged {
		t.Fatal("expected no CF challenge on normal page")
	}
}

func TestDetectCloudflareChallenge_EmptyBody(t *testing.T) {
	challenged, _ := detectCloudflareChallenge([]byte{})
	if challenged {
		t.Fatal("expected no CF challenge on empty body")
	}
}

func TestDetectCloudflareChallenge_JustCloudflareNotEnough(t *testing.T) {
	// "cloudflare" alone should not trigger; needs "just a moment" or other markers.
	body := []byte(`Powered by Cloudflare`)
	challenged, _ := detectCloudflareChallenge(body)
	if challenged {
		t.Fatal("expected no CF challenge when only 'cloudflare' text is present")
	}
}

// ---------------------------------------------------------------------------
// Scoring
// ---------------------------------------------------------------------------

func TestAggregateScore_EmptyResults(t *testing.T) {
	score := aggregateScore(nil, DefaultOptions())
	if score.Grade != "F" || score.Score != 0 {
		t.Fatalf("expected F/0, got %s/%.0f", score.Grade, score.Score)
	}
}

func TestAggregateScore_AllPass(t *testing.T) {
	results := []ProxyRoundResult{
		{ServiceReachable: true, APIReachable: true, Latency: 50 * time.Millisecond},
	}
	opts := ProxyCheckOptions{ServiceReachability: true, APIReachability: true}
	score := aggregateScore(results, opts)
	// 50+25 = 75 base, no latency bonus, not unstable → 75
	if score.Score != 75 {
		t.Fatalf("expected score 75, got %.0f", score.Score)
	}
	if score.Grade != "A" {
		t.Fatalf("expected A, got %s", score.Grade)
	}
}

func TestAggregateScore_ServiceOnly(t *testing.T) {
	results := []ProxyRoundResult{
		{ServiceReachable: true, APIReachable: false, Latency: 300 * time.Millisecond},
	}
	// DefaultOptions has ServiceReachability=true, CF detection enabled.
	opts := DefaultOptions()
	score := aggregateScore(results, opts)
	// 50/1 = 50 base. All required checks pass (service=OK, API not enabled,
	// no CF challenged). Grade A.
	if score.Score != 50 {
		t.Fatalf("expected score 50, got %.0f", score.Score)
	}
	if score.Grade != "A" {
		t.Fatalf("expected A, got %s", score.Grade)
	}
}

func TestAggregateScore_AllFail(t *testing.T) {
	results := []ProxyRoundResult{
		{ServiceReachable: false, APIReachable: false},
	}
	opts := DefaultOptions()
	score := aggregateScore(results, opts)
	if score.Score != 0 {
		t.Fatalf("expected score 0, got %.0f", score.Score)
	}
	if score.Grade != "F" {
		t.Fatalf("expected F, got %s", score.Grade)
	}
}

func TestAggregateScore_ServiceAllPassApiFail(t *testing.T) {
	// Service passes all rounds, API fails → B (service all-round pass).
	results := []ProxyRoundResult{
		{ServiceReachable: true, APIReachable: false},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
	}
	score := aggregateScore(results, opts)
	// (50+0)/1 = 50, no CF → score 50
	if score.Score != 50 {
		t.Fatalf("expected score 50, got %.0f", score.Score)
	}
	if score.Grade != "B" {
		t.Fatalf("expected B (service all-round pass), got %s", score.Grade)
	}
}

func TestAggregateScore_ApiOnly(t *testing.T) {
	// API passes all rounds, service fails → B (API all-round pass).
	results := []ProxyRoundResult{
		{ServiceReachable: false, APIReachable: true},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
	}
	score := aggregateScore(results, opts)
	// (0+25)/1 = 25, no CF
	if score.Score != 25 {
		t.Fatalf("expected score 25, got %.0f", score.Score)
	}
	if score.Grade != "B" {
		t.Fatalf("expected B (API all-round pass), got %s", score.Grade)
	}
}

func TestAggregateScore_MultiRoundConsistency(t *testing.T) {
	results := []ProxyRoundResult{
		{ServiceReachable: true, APIReachable: true, Latency: 100 * time.Millisecond},
		{ServiceReachable: true, APIReachable: true, Latency: 120 * time.Millisecond},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		MultiRound:          true,
		Rounds:              2,
	}
	score := aggregateScore(results, opts)
	// (50+25+50+25)/2 = 75 base, +10 stability bonus = 85
	if score.Score != 85 {
		t.Fatalf("expected score 85, got %.0f", score.Score)
	}
	if score.Grade != "A" {
		t.Fatalf("expected A, got %s", score.Grade)
	}
	if score.Unstable {
		t.Fatal("expected stable (consistent results)")
	}
}

func TestAggregateScore_PartialMultiRoundUnstable(t *testing.T) {
	// Multi-round with one round failing → unstable → D
	results := []ProxyRoundResult{
		{ServiceReachable: true, APIReachable: true, Latency: 100 * time.Millisecond},
		{ServiceReachable: false, APIReachable: true, Latency: 120 * time.Millisecond},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		MultiRound:          true,
		Rounds:              2,
	}
	score := aggregateScore(results, opts)
	if !score.Unstable {
		t.Fatal("expected unstable (inconsistent results)")
	}
	if score.Grade != "D" {
		t.Fatalf("expected D for unstable, got %s", score.Grade)
	}
	// (50+25+0+25)/2 = 50, no stability bonus, no CF penalty
	if score.Score != 50 {
		t.Fatalf("expected score 50, got %.0f", score.Score)
	}
}

func TestAggregateScore_MultiRoundAllFailThenPass(t *testing.T) {
	// Some reachable → basic connectivity, but unstable → D
	results := []ProxyRoundResult{
		{ServiceReachable: false, APIReachable: false},
		{ServiceReachable: true, APIReachable: false},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		MultiRound:          true,
		Rounds:              2,
	}
	score := aggregateScore(results, opts)
	if !score.Unstable {
		t.Fatal("expected unstable")
	}
	if score.Grade != "D" {
		t.Fatalf("expected D, got %s", score.Grade)
	}
	if score.Score != 25 {
		t.Fatalf("expected score 25, got %.0f", score.Score)
	}
}

func TestAggregateScore_CFChallengedGradeD(t *testing.T) {
	// Service reachable but CF challenged → grade D
	results := []ProxyRoundResult{
		{ServiceReachable: true, APIReachable: true, CloudflareChallenged: true, CloudflareChallengeType: "js_challenge"},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: true,
	}
	score := aggregateScore(results, opts)
	if score.Grade != "D" {
		t.Fatalf("expected D for CF challenged, got %s", score.Grade)
	}
	if !score.CloudflareChallenged {
		t.Fatal("expected CloudflareChallenged = true")
	}
	if score.CloudflareChallengeType != "js_challenge" {
		t.Fatalf("expected challenge type js_challenge, got %q", score.CloudflareChallengeType)
	}
	if score.CloudflareStatus != "js_challenge" {
		t.Fatalf("expected CloudflareStatus js_challenge (via legacy fallback), got %q", score.CloudflareStatus)
	}
	// Score: (50+25)/1 = 75 - 20 (CF penalty) = 55
	if score.Score != 55 {
		t.Fatalf("expected score 55, got %.0f", score.Score)
	}
}

func TestAggregateScore_CFDisabledNoPenalty(t *testing.T) {
	// CF detection not enabled → no penalty even if result has challenged
	results := []ProxyRoundResult{
		{ServiceReachable: true, APIReachable: true, CloudflareChallenged: true},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: false,
	}
	score := aggregateScore(results, opts)
	// CF is challenged but detection not enabled → no penalty for grade
	// Grade: serviceAllPass=true, apiAllPass=true, CF detection OFF → allPassedAllRounds=true → A
	if score.Grade != "A" {
		t.Fatalf("expected A (CF detection disabled), got %s", score.Grade)
	}
}

// ---------------------------------------------------------------------------
// Full CheckProxy integration
// ---------------------------------------------------------------------------

func TestCheckProxy_Success(t *testing.T) {
	var callCount int
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		callCount++
		switch url {
		case "https://www.gstatic.com/generate_204":
			return []byte{}, 50 * time.Millisecond, nil
		default:
			return nil, 0, errors.New("unexpected URL: " + url)
		}
	}

	score, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, DefaultOptions())
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if !score.ServiceReachable {
		t.Fatal("expected service reachable")
	}
	if score.CloudflareChallenged {
		t.Fatal("expected no CF challenged (generic profile has no CloudflareFlag)")
	}
	// Grade: service only, all pass → A
	if score.Grade != "A" {
		t.Fatalf("expected grade A for service-only all-pass, got %s (score=%.0f)", score.Grade, score.Score)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 fetcher call (no separate CF endpoint), got %d", callCount)
	}
}

func TestCheckProxy_ServiceFailure(t *testing.T) {
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://www.gstatic.com/generate_204":
			return nil, 0, errors.New("timeout")
		default:
			return nil, 0, errors.New("unexpected URL")
		}
	}

	score, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, DefaultOptions())
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if score.ServiceReachable {
		t.Fatal("expected service NOT reachable")
	}
	if score.Grade != "F" {
		t.Fatalf("expected grade F, got %s (score=%.0f)", score.Grade, score.Score)
	}
	// Only 1 call (service), no separate CF trace call.
	if len(score.RoundResults) != 1 {
		t.Fatalf("expected 1 round result")
	}
}

func TestCheckProxy_APIAndServiceWithIndicators(t *testing.T) {
	var callCount int
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		callCount++
		switch url {
		case "https://chatgpt.com":
			return []byte("<html>Welcome to ChatGPT by OpenAI</html>"), 100 * time.Millisecond, nil
		case "https://api.openai.com/v1/models":
			return []byte(`{"data":[{"id":"gpt-4"}]}`), 80 * time.Millisecond, nil
		default:
			return nil, 0, errors.New("unexpected URL: " + url)
		}
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: true,
		Rounds:              1,
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileOpenAI, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if !score.ServiceReachable {
		t.Fatal("expected service reachable (indicator matched)")
	}
	if !score.APIReachable {
		t.Fatal("expected API reachable")
	}
	if score.CloudflareChallenged {
		t.Fatal("expected no CF challenged (body is normal page)")
	}
	if score.Grade != "A" {
		t.Fatalf("expected A grade, got %s (score=%.0f)", score.Grade, score.Score)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 fetcher calls (service + API, no separate CF trace), got %d", callCount)
	}
}

func TestCheckProxy_APIFailure(t *testing.T) {
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://chatgpt.com":
			return []byte("<html>Welcome to ChatGPT by OpenAI</html>"), 100 * time.Millisecond, nil
		case "https://api.openai.com/v1/models":
			return nil, 0, errors.New("API unreachable")
		default:
			return nil, 0, errors.New("unexpected URL")
		}
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: true,
		Rounds:              1,
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileOpenAI, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if !score.ServiceReachable {
		t.Fatal("expected service reachable")
	}
	if score.APIReachable {
		t.Fatal("expected API NOT reachable")
	}
	// Service all-round pass → B
	if score.Grade != "B" {
		t.Fatalf("expected B grade (service all-round pass), got %s (score=%.0f)", score.Grade, score.Score)
	}
}

func TestCheckProxy_IndicatorNotMatched(t *testing.T) {
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://chatgpt.com":
			return []byte("<html>Some unrelated page</html>"), 50 * time.Millisecond, nil
		case "https://api.openai.com/v1/models":
			return []byte(`{"data":[]}`), 40 * time.Millisecond, nil
		default:
			return nil, 0, errors.New("unexpected URL")
		}
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: true,
		Rounds:              1,
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileOpenAI, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if score.ServiceReachable {
		t.Fatal("expected service NOT reachable when indicators don't match")
	}
	if !score.APIReachable {
		t.Fatal("API check is independent and should still succeed")
	}
	// API all-round pass → B
	if score.Grade != "B" {
		t.Fatalf("expected B (API all-round pass), got %s (score=%.0f)", score.Grade, score.Score)
	}
}

func TestCheckProxy_NilFetcher(t *testing.T) {
	_, err := CheckProxy(nil, node.Zero, ProfileGeneric, DefaultOptions())
	if err == nil {
		t.Fatal("expected error for nil fetcher")
	}
}

func TestCheckProxy_RoundResultsPopulated(t *testing.T) {
	fetcher := func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
		return []byte("ok"), 25 * time.Millisecond, nil
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, DefaultOptions())
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if len(score.RoundResults) != 1 {
		t.Fatalf("expected 1 round result, got %d", len(score.RoundResults))
	}
	if !score.RoundResults[0].ServiceReachable {
		t.Fatal("round result should have service reachable")
	}
}

func TestCheckProxy_MultiRound(t *testing.T) {
	var callCount int
	fetcher := func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
		callCount++
		return []byte("ok"), 30 * time.Millisecond, nil
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		CloudflareDetection: false,
		MultiRound:          true,
		Rounds:              3,
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if callCount != 3 {
		t.Fatalf("expected 3 fetcher calls for 3 rounds, got %d", callCount)
	}
	if len(score.RoundResults) != 3 {
		t.Fatalf("expected 3 round results, got %d", len(score.RoundResults))
	}
}

// ---------------------------------------------------------------------------
// CF challenge detection via CheckProxy (integration)
// ---------------------------------------------------------------------------

func TestCheckProxy_CFChallengeDetectedInBody(t *testing.T) {
	// OpenAI profile has CloudflareFlag=true. Body contains CF JS challenge.
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://chatgpt.com":
			return []byte(`<html><head><title>Just a moment...</title>
				<script src="/cf-browser-verification"></script><footer>Cloudflare</footer></html>`),
				200 * time.Millisecond, nil
		case "https://api.openai.com/v1/models":
			return []byte(`{"data":[]}`), 80 * time.Millisecond, nil
		default:
			return nil, 0, errors.New("unexpected URL")
		}
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: true,
		Rounds:              1,
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileOpenAI, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	// CF body doesn't contain OpenAI indicators, so ServiceReachable=false
	// even though the HTTP request succeeded.
	if score.ServiceReachable {
		t.Fatal("expected service NOT reachable (CF body doesn't match indicators)")
	}
	if !score.CloudflareChallenged {
		t.Fatal("expected CloudflareChallenged = true (body has CF challenge)")
	}
	if score.CloudflareChallengeType != "js_challenge" {
		t.Fatalf("expected challenge type js_challenge, got %q", score.CloudflareChallengeType)
	}
	// Grade should be D (CF challenged)
	if score.Grade != "D" {
		t.Fatalf("expected D for CF challenged, got %s", score.Grade)
	}
	// Only 2 calls: service + API, no separate CF trace endpoint
}

func TestCheckProxy_CFChallengeDetectionDisabled(t *testing.T) {
	// CloudflareFlag=true, opts.CloudflareDetection=false.
	// Phase 3A: observation runs unconditionally even when detection is
	// disabled. The CloudflareDetection flag gates only the score/grade
	// compatibility effect, not the observation itself.
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://chatgpt.com":
			return []byte(`<html><head><title>Just a moment...</title>
				<script src="/cf-browser-verification"></script><div>OpenAI ChatGPT</div></html>`),
				200 * time.Millisecond, nil
		case "https://api.openai.com/v1/models":
			return []byte(`{"data":[]}`), 80 * time.Millisecond, nil
		default:
			return nil, 0, errors.New("unexpected URL")
		}
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: false,
		Rounds:              1,
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileOpenAI, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	// Observation is unconditional — challenge must be detected.
	if !score.CloudflareChallenged {
		t.Fatal("expected CloudflareChallenged = true (observation is unconditional)")
	}
	if score.CloudflareChallengeType != "js_challenge" {
		t.Fatalf("expected challenge type js_challenge, got %q", score.CloudflareChallengeType)
	}
	if score.CloudflareStatus == "" {
		t.Fatal("expected CloudflareStatus to be set (observation ran)")
	}
	// Grade: CloudflareDetection=false → no grade penalty despite detection.
	if score.Grade != "A" {
		t.Fatalf("expected A (CF detection disabled gates grade, not observation), got %s", score.Grade)
	}
}

func TestCheckProxy_CFChallengeGenericProfile(t *testing.T) {
	// Generic profile has CloudflareFlag=false, but Phase 3A observation is
	// unconditional. Body CF challenge markers are detected regardless.
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://www.gstatic.com/generate_204":
			return []byte(`<html><head><title>Just a moment...</title><footer>Cloudflare</footer></html>`),
				200 * time.Millisecond, nil
		default:
			return nil, 0, errors.New("unexpected URL")
		}
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		CloudflareDetection: true,
		Rounds:              1,
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	// Observation is unconditional — challenge must be detected even on
	// generic profile (CloudflareFlag is a no-op in Phase 3A).
	if !score.CloudflareChallenged {
		t.Fatal("expected CloudflareChallenged = true (observation is unconditional)")
	}
	if score.CloudflareStatus == "" {
		t.Fatal("expected CloudflareStatus to be set (observation ran)")
	}
	if score.Grade != "D" {
		t.Fatalf("expected D for CF challenged on generic profile (observation unconditional), got %s", score.Grade)
	}
	if !score.ServiceReachable {
		t.Fatal("service should be reachable (generic has no indicators)")
	}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func TestNormalizeProxyString_StandardOnly(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  https://example.com:8080  ", "example.com:8080"},
		{"http://1.2.3.4:3128", "1.2.3.4:3128"},
		{"socks5://proxy.example.com:1080", "proxy.example.com:1080"},
		{"socks5h://proxy.example.com:1080", "proxy.example.com:1080"},
		{"socks4://10.0.0.1:4145", "10.0.0.1:4145"},
		{"ss://YWVzLTEyOC1nY206dGVzdA@example.com:8388", "ss://YWVzLTEyOC1nY206dGVzdA@example.com:8388"},
		{"vmess://uuid@example.com:443", "vmess://uuid@example.com:443"},
		{"trojan://password@example.com:443", "trojan://password@example.com:443"},
		{"already-clean:8080", "already-clean:8080"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeProxyString(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeProxyString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeProxyString_SSNotStripped(t *testing.T) {
	// ss:// must be preserved as-is — it carries cipher & auth info.
	got := NormalizeProxyString("ss://YWVzLTEyOC1nY206dGVzdA@example.com:8388")
	want := "ss://YWVzLTEyOC1nY206dGVzdA@example.com:8388"
	if got != want {
		t.Errorf("expected ss:// preserved, got %q", got)
	}
}

func TestNormalizeProxyString_VmessNotStripped(t *testing.T) {
	got := NormalizeProxyString("vmess://uuid@example.com:443")
	want := "vmess://uuid@example.com:443"
	if got != want {
		t.Errorf("expected vmess:// preserved, got %q", got)
	}
}

func TestNormalizeProxyString_TrojanNotStripped(t *testing.T) {
	got := NormalizeProxyString("trojan://password@example.com:443")
	want := "trojan://password@example.com:443"
	if got != want {
		t.Errorf("expected trojan:// preserved, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// CloudflareStatus classification (header-first + body fallback)
// ---------------------------------------------------------------------------

func makeResp(body []byte, statusCode int, headers map[string]string) FetchResponse {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return FetchResponse{
		Body:       body,
		StatusCode: statusCode,
		Header:     h,
	}
}

func TestClassifyCloudflareStatus_CFMitigatedChallenge(t *testing.T) {
	// cf-mitigated: challenge is authoritative.
	resp := makeResp([]byte("<html>challenge page</html>"), 503, map[string]string{
		"cf-mitigated": "challenge",
		"Server":       "cloudflare",
		"CF-Ray":       "abc123",
	})
	status := classifyCloudflareStatus(resp)
	if status != CFStatusChallenge {
		t.Fatalf("expected generic challenge when header has no subtype markers, got %q", status)
	}
}

func TestClassifyCloudflareStatus_CFMitigatedCaptcha(t *testing.T) {
	resp := makeResp([]byte(`<div class="cf-chl-widget"></div>`), 403, map[string]string{
		"cf-mitigated": "challenge",
		"Content-Type": "text/html; captcha",
	})
	status := classifyCloudflareStatus(resp)
	if status != CFStatusCaptchaChallenge {
		t.Fatalf("expected captcha_challenge (403 + captcha content-type), got %q", status)
	}
}

func TestClassifyCloudflareStatus_ServerCloudflareClean(t *testing.T) {
	resp := makeResp([]byte(`{"ok":true}`), 200, map[string]string{
		"Server": "cloudflare",
		"CF-Ray": "xyz789",
	})
	status := classifyCloudflareStatus(resp)
	if status != CFStatusClean {
		t.Fatalf("expected clean (CF headers, no challenge), got %q", status)
	}
}

func TestClassifyCloudflareStatus_NoCFEvidenceNotDetected(t *testing.T) {
	resp := makeResp([]byte("normal page content"), 200, map[string]string{
		"Server": "nginx",
	})
	status := classifyCloudflareStatus(resp)
	if status != CFStatusNotDetected {
		t.Fatalf("expected not_detected (no CF evidence), got %q", status)
	}
}

func TestClassifyCloudflareStatus_LegacyAdapterUnchecked(t *testing.T) {
	// Metadata-poor: StatusCode=0, Header=nil, no body challenge.
	// Body inspection found no known challenge, but without headers/status the
	// adapter cannot claim clean or not_detected.
	resp := makeResp([]byte("some body"), 0, nil)
	resp.Header = nil
	status := classifyCloudflareStatus(resp)
	if status != CFStatusEmpty {
		t.Fatalf("expected empty/unchecked for metadata-poor response, got %q", status)
	}
}

func TestClassifyCloudflareStatus_BodyFallbackJSChallenge(t *testing.T) {
	// Headers available but no CF evidence; body has JS challenge markers.
	resp := makeResp([]byte(`<html><script src="/cf-browser-verification"></script></html>`), 200, map[string]string{
		"Server": "nginx",
	})
	status := classifyCloudflareStatus(resp)
	if status != CFStatusJSChallenge {
		t.Fatalf("expected js_challenge via body fallback, got %q", status)
	}
}

func TestClassifyCloudflareStatus_BodyFallbackBlock(t *testing.T) {
	resp := makeResp([]byte(`<html>Attention Required! Cloudflare Error 1020</html>`), 200, nil)
	status := classifyCloudflareStatus(resp)
	if status != CFStatusBlock {
		t.Fatalf("expected block via body fallback, got %q", status)
	}
}

func TestClassifyCloudflareStatus_CFRayOnlyClean(t *testing.T) {
	resp := makeResp([]byte("content"), 200, map[string]string{
		"CF-Ray": "abc-def-123",
	})
	status := classifyCloudflareStatus(resp)
	if status != CFStatusClean {
		t.Fatalf("expected clean (CF-Ray present, no challenge), got %q", status)
	}
}

func TestClassifyCloudflareStatus_TransportFailureNG(t *testing.T) {
	// Only set via CheckProxy, not classifyCloudflareStatus.
	// classifyCloudflareStatus is not called on transport errors.
}

// ---------------------------------------------------------------------------
// mostSevereCFStatus
// ---------------------------------------------------------------------------

func TestMostSevereCFStatus_BlockWins(t *testing.T) {
	got := mostSevereCFStatus(CFStatusNotDetected, CFStatusBlock, CFStatusClean, CFStatusEmpty)
	if got != CFStatusBlock {
		t.Fatalf("expected block (most severe), got %q", got)
	}
}

func TestMostSevereCFStatus_CaptchaBeatsJS(t *testing.T) {
	got := mostSevereCFStatus(CFStatusJSChallenge, CFStatusCaptchaChallenge, CFStatusClean)
	if got != CFStatusCaptchaChallenge {
		t.Fatalf("expected captcha_challenge, got %q", got)
	}
}

func TestMostSevereCFStatus_NGBeatsClean(t *testing.T) {
	got := mostSevereCFStatus(CFStatusClean, CFStatusNotDetected, CFStatusNG)
	if got != CFStatusNG {
		t.Fatalf("expected ng (more severe than clean), got %q", got)
	}
}

func TestMostSevereCFStatus_EmptyIsLeastSevere(t *testing.T) {
	got := mostSevereCFStatus(CFStatusEmpty, CFStatusEmpty)
	if got != CFStatusEmpty {
		t.Fatalf("expected empty, got %q", got)
	}

	got2 := mostSevereCFStatus(CFStatusEmpty, CFStatusNotDetected)
	if got2 != CFStatusNotDetected {
		t.Fatalf("expected not_detected > empty, got %q", got2)
	}
}

func TestMostSevereCFStatus_AllEmptyReturnsEmpty(t *testing.T) {
	got := mostSevereCFStatus()
	if got != CFStatusEmpty {
		t.Fatalf("expected empty for no inputs, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// isChallengeStatus
// ---------------------------------------------------------------------------

func TestIsChallengeStatus(t *testing.T) {
	tests := []struct {
		status CloudflareStatus
		want   bool
	}{
		{CFStatusBlock, true},
		{CFStatusCaptchaChallenge, true},
		{CFStatusJSChallenge, true},
		{CFStatusChallenge, true},
		{CFStatusClean, false},
		{CFStatusNotDetected, false},
		{CFStatusNG, false},
		{CFStatusEmpty, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := isChallengeStatus(tt.status)
			if got != tt.want {
				t.Errorf("isChallengeStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// aggregateScore with CloudflareStatus
// ---------------------------------------------------------------------------

func TestAggregateScore_AggregateCFStatus(t *testing.T) {
	results := []ProxyRoundResult{
		{ServiceReachable: true, APIReachable: true, CloudflareStatus: CFStatusNotDetected},
		{ServiceReachable: true, APIReachable: true, CloudflareStatus: CFStatusJSChallenge},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: true,
		MultiRound:          true,
		Rounds:              2,
	}
	score := aggregateScore(results, opts)
	if score.CloudflareStatus != CFStatusJSChallenge {
		t.Fatalf("expected aggregate js_challenge, got %q", score.CloudflareStatus)
	}
	if !score.CloudflareChallenged {
		t.Fatal("expected CloudflareChallenged = true")
	}
	if score.CloudflareChallengeType != "js_challenge" {
		t.Fatalf("expected challenge type js_challenge, got %q", score.CloudflareChallengeType)
	}
}

func TestAggregateScore_CFStatusNotDetectedNoPenalty(t *testing.T) {
	results := []ProxyRoundResult{
		{ServiceReachable: true, CloudflareStatus: CFStatusNotDetected},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		CloudflareDetection: true,
	}
	score := aggregateScore(results, opts)
	if score.CloudflareChallenged {
		t.Fatal("expected no challenge for not_detected")
	}
	if score.CloudflareChallengeType != "" {
		t.Fatalf("expected empty challenge type, got %q", score.CloudflareChallengeType)
	}
	// 50 points, no CF penalty
	if score.Score != 50 {
		t.Fatalf("expected score 50 (no CF penalty), got %.0f", score.Score)
	}
	// allPassedAllRounds: serviceOk=true, API not enabled (allPass=nil), CF detection on but no challenge → true → A
	if score.Grade != "A" {
		t.Fatalf("expected A for clean round, got %s", score.Grade)
	}
}

func TestAggregateScore_CFStatusNGNoPenalty(t *testing.T) {
	results := []ProxyRoundResult{
		{ServiceReachable: false, CloudflareStatus: CFStatusNG, Error: "transport failure"},
	}
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		CloudflareDetection: true,
	}
	score := aggregateScore(results, opts)
	if score.CloudflareChallenged {
		t.Fatal("expected no challenge for ng")
	}
	if score.CloudflareStatus != CFStatusNG {
		t.Fatalf("expected NG status, got %q", score.CloudflareStatus)
	}
	// No service points, no CF penalty → score 0
	if score.Score != 0 {
		t.Fatalf("expected score 0, got %.0f", score.Score)
	}
	if score.Grade != "F" {
		t.Fatalf("expected F for failed round, got %s", score.Grade)
	}
}

// ---------------------------------------------------------------------------
// CheckProxyWithResponse: CF-only request when ServiceReachability disabled
// ---------------------------------------------------------------------------

func TestCheckProxyWithResponse_CFOnlyRequest(t *testing.T) {
	// Service evaluation disabled, but CF observation should still run.
	var callCount int
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		callCount++
		return []byte("ok"), 10 * time.Millisecond, nil
	}

	opts := ProxyCheckOptions{
		ServiceReachability: false, // service criterion disabled
		CloudflareDetection: true,
		Rounds:              1,
	}

	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileGeneric, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	// One request should be made (for CF observation).
	if callCount != 1 {
		t.Fatalf("expected 1 request (CF-only), got %d", callCount)
	}
	// ServiceReachable must be false because the criterion is disabled.
	if score.ServiceReachable {
		t.Fatal("expected ServiceReachable = false when service evaluation disabled")
	}
	// The request ran, but this test uses the legacy metadata-poor fetcher, so
	// a non-challenge body remains empty/unchecked rather than guessed clean.
	if score.CloudflareStatus != CFStatusEmpty {
		t.Fatalf("expected empty/unchecked metadata-poor status, got %q", score.CloudflareStatus)
	}
	// But CloudflareChallenged should be false for the body "ok"
	if score.CloudflareChallenged {
		t.Fatal("expected no CF challenge for body 'ok'")
	}
}

func TestCheckProxyWithResponse_SharedServiceAndCFRequest(t *testing.T) {
	// When both service evaluation and CF observation are needed, only
	// ONE request should be made per round (shared). The API check is
	// a separate request when APIReachability is enabled.
	var callCount int
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		callCount++
		return []byte("Welcome to ChatGPT"), 50 * time.Millisecond, nil
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: true,
		Rounds:              1,
	}

	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileOpenAI, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	// Exactly 1 request for service (shared with CF). API check adds 1.
	if callCount != 2 {
		t.Fatalf("expected 2 calls (1 service+CF shared + 1 API), got %d", callCount)
	}
	if !score.ServiceReachable {
		t.Fatal("expected service reachable")
	}
	// Shared request used a legacy metadata-poor fetcher. It proves request
	// reuse, but cannot classify a non-challenge response as clean/not_detected.
	if score.CloudflareStatus != CFStatusEmpty {
		t.Fatalf("expected empty/unchecked metadata-poor status, got %q", score.CloudflareStatus)
	}
}

// ---------------------------------------------------------------------------
// All profiles observed regardless of CloudflareFlag
// ---------------------------------------------------------------------------

func TestCheckProxyWithResponse_GenericProfileCFStillRuns(t *testing.T) {
	// Generic profile has CloudflareFlag=false, but CF observation should
	// still run because observation is now unconditional.
	var callCount int
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		callCount++
		return []byte("ok"), 10 * time.Millisecond, nil
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		CloudflareDetection: true,
		Rounds:              1,
	}

	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileGeneric, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	if callCount < 1 {
		t.Fatal("expected at least 1 request")
	}
	if score.CloudflareStatus != CFStatusEmpty {
		t.Fatalf("expected empty/unchecked metadata-poor status, got %q", score.CloudflareStatus)
	}
	if score.CloudflareChallenged {
		t.Fatal("expected no CF challenge for generic profile body 'ok'")
	}
}

func TestCheckProxyWithResponse_OpenaiProfileCFRuns(t *testing.T) {
	// OpenAI has CloudflareFlag=true; CF observation runs as usual.
	var callCount int
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		callCount++
		return []byte("Welcome to ChatGPT by OpenAI"), 50 * time.Millisecond, nil
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		APIReachability:     true,
		CloudflareDetection: true,
		Rounds:              1,
	}

	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileOpenAI, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls (1 service+CF + 1 API), got %d", callCount)
	}
	if score.CloudflareStatus != CFStatusEmpty {
		t.Fatalf("expected empty/unchecked metadata-poor status, got %q", score.CloudflareStatus)
	}
}

// ---------------------------------------------------------------------------
// Multi-round severity aggregation integration
// ---------------------------------------------------------------------------

func TestCheckProxyWithResponse_MultiRoundSeverityAggregation(t *testing.T) {
	round := 0
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		round++
		switch round {
		case 1:
			// Round 1: not_detected
			return []byte("normal content"), 50 * time.Millisecond, nil
		case 2:
			// Round 2: CF challenge via body
			return []byte(`<html><script src="/cf-browser-verification"></script></html>`), 60 * time.Millisecond, nil
		case 3:
			// Round 3: block
			return []byte(`<html>Attention Required! Cloudflare</html>`), 70 * time.Millisecond, nil
		default:
			return nil, 0, fmt.Errorf("unexpected round %d", round)
		}
	}

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		CloudflareDetection: true,
		MultiRound:          true,
		Rounds:              3,
	}

	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileGeneric, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	// Most severe is block.
	if score.CloudflareStatus != CFStatusBlock {
		t.Fatalf("expected block as most severe aggregate, got %q", score.CloudflareStatus)
	}
	if !score.CloudflareChallenged {
		t.Fatal("expected challenged = true (block is challenge)")
	}
	if score.CloudflareChallengeType != "block" {
		t.Fatalf("expected challenge type block, got %q", score.CloudflareChallengeType)
	}
	if len(score.RoundResults) != 3 {
		t.Fatalf("expected 3 round results, got %d", len(score.RoundResults))
	}
}

// ---------------------------------------------------------------------------
// DirectResponseFetcher metadata
// ---------------------------------------------------------------------------

func TestDirectResponseFetcher_Metadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	fetcher := DirectResponseFetcher(func() time.Duration { return time.Second })
	resp, err := fetcher(node.Zero, srv.URL)
	if err != nil {
		t.Fatalf("DirectResponseFetcher failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if resp.Header == nil {
		t.Fatal("Header is nil")
	}
	if resp.Header.Get("X-Custom") != "value" {
		t.Errorf("X-Custom = %q, want 'value'", resp.Header.Get("X-Custom"))
	}
	if string(resp.Body) != "hello" {
		t.Errorf("Body = %q, want 'hello'", string(resp.Body))
	}
	if resp.FinalURL == "" {
		t.Error("FinalURL should not be empty")
	}
	if resp.Latency <= 0 {
		t.Errorf("Latency should be > 0, got %v", resp.Latency)
	}
}

// ---------------------------------------------------------------------------
// LegacyResponseFetcher adapter
// ---------------------------------------------------------------------------

func TestLegacyResponseFetcher_MetadataPoor(t *testing.T) {
	plainFetcher := Fetcher(func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		return []byte("body-only"), 25 * time.Millisecond, nil
	})

	adapter := LegacyResponseFetcher(plainFetcher)
	resp, err := adapter(node.Zero, "http://example.com")
	if err != nil {
		t.Fatalf("LegacyResponseFetcher failed: %v", err)
	}
	if resp.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0 (metadata-poor)", resp.StatusCode)
	}
	if resp.Header != nil {
		t.Error("Header should be nil (metadata-poor)")
	}
	if resp.FinalURL != "" {
		t.Errorf("FinalURL = %q, want empty", resp.FinalURL)
	}
	if string(resp.Body) != "body-only" {
		t.Errorf("Body = %q, want 'body-only'", string(resp.Body))
	}
	if resp.Latency != 25*time.Millisecond {
		t.Errorf("Latency = %v, want 25ms", resp.Latency)
	}
}

func TestLegacyResponseFetcher_PropagatesConnectionRefused(t *testing.T) {
	plainFetcher := Fetcher(func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		return nil, 0, fmt.Errorf("connection refused")
	})

	adapter := LegacyResponseFetcher(plainFetcher)
	_, err := adapter(node.Zero, "http://example.com")
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

// ---------------------------------------------------------------------------
// ScoringBreakdown and Unstable in new scoring path (Phase 3B1)
// ---------------------------------------------------------------------------

func TestCheckProxyWithResponse_ScoringBreakdownPopulated(t *testing.T) {
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		return []byte("ok"), 50 * time.Millisecond, nil
	}
	policy := config.BalancedScoringPolicy()
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		ScoringPolicy:       &policy,
		Rounds:              1,
	}
	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileGeneric, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	if score.ScoringBreakdown == nil {
		t.Fatal("ScoringBreakdown should be non-nil when ScoringPolicy is set")
	}
	if score.ScoringBreakdown.Score != score.Score {
		t.Fatalf("ScoringBreakdown.Score=%.0f != ProxyScore.Score=%.0f", score.ScoringBreakdown.Score, score.Score)
	}
	if score.ScoringBreakdown.FinalGrade != score.Grade {
		t.Fatalf("ScoringBreakdown.FinalGrade=%s != ProxyScore.Grade=%s", score.ScoringBreakdown.FinalGrade, score.Grade)
	}
}

func TestCheckProxyWithResponse_LegacyNoBreakdown(t *testing.T) {
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		return []byte("ok"), 50 * time.Millisecond, nil
	}
	// No ScoringPolicy set — legacy path.
	opts := DefaultOptions()
	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileGeneric, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	if score.ScoringBreakdown != nil {
		t.Fatal("ScoringBreakdown should be nil for legacy scoring path")
	}
}

func TestCheckProxyWithResponse_UnstableNewScoring(t *testing.T) {
	policy := config.BalancedScoringPolicy()
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		ScoringPolicy:       &policy,
		MultiRound:          true,
		Rounds:              2,
	}

	// Inconsistent results: round 1 passes, round 2 doesn't.
	round1 := true
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		if round1 {
			round1 = false
			return []byte("ok"), 50 * time.Millisecond, nil
		}
		return nil, 0, fmt.Errorf("timeout")
	}

	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileGeneric, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	if !score.Unstable {
		t.Fatal("expected Unstable=true for inconsistent multi-round with new scoring")
	}
}

func TestCheckProxyWithResponse_StableNewScoring(t *testing.T) {
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		return []byte("ok"), 50 * time.Millisecond, nil
	}
	policy := config.BalancedScoringPolicy()
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		ScoringPolicy:       &policy,
		MultiRound:          true,
		Rounds:              2,
	}

	// Consistent results: both pass.
	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileGeneric, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	if score.Unstable {
		t.Fatal("expected Unstable=false for consistent multi-round with new scoring")
	}
}

func TestCheckProxyWithResponse_UnstableSingleRoundFalse(t *testing.T) {
	policy := config.BalancedScoringPolicy()
	opts := ProxyCheckOptions{
		ServiceReachability: true,
		ScoringPolicy:       &policy,
		Rounds:              1,
	}
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		return []byte("ok"), 50 * time.Millisecond, nil
	}
	score, err := CheckProxyWithResponse(fetcher, node.Zero, ProfileGeneric, opts, nil)
	if err != nil {
		t.Fatalf("CheckProxyWithResponse: %v", err)
	}
	if score.Unstable {
		t.Fatal("Unstable should be false for single round")
	}
}

// ---------------------------------------------------------------------------
// Custom CF target semantics (Phase 3B1)
// ---------------------------------------------------------------------------

func TestCheckProxy_CustomCFTarget_AuthoritativeOverride(t *testing.T) {
	// Custom CF target differs from ServiceURL. Service request returns clean,
	// custom target returns block. Custom target is authoritative → block wins.
	var customCalled bool
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://www.gstatic.com/generate_204":
			return []byte("ok"), 50 * time.Millisecond, nil
		case "https://custom-cf.example.com/check":
			customCalled = true
			return []byte(`<html>Attention Required! Cloudflare Error 1020</html>`), 100 * time.Millisecond, nil
		default:
			return nil, 0, fmt.Errorf("unexpected URL: %s", url)
		}
	}

	policy := config.BalancedScoringPolicy()
	policy.Cloudflare.TargetURL = "https://custom-cf.example.com/check"

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		ScoringPolicy:       &policy,
		Rounds:              1,
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if !customCalled {
		t.Fatal("custom CF target should have been called")
	}
	// Custom target returned block → CloudflareStatus must be block, not clean.
	if score.CloudflareStatus != "block" {
		t.Fatalf("expected block from authoritative custom target, got %q", score.CloudflareStatus)
	}
}

func TestCheckProxy_CustomCFTarget_EqualURLReusesServiceResponse(t *testing.T) {
	// Custom CF target equals normalized ServiceURL → no separate request.
	var callCount int
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		callCount++
		return []byte("ok"), 50 * time.Millisecond, nil
	}

	policy := config.BalancedScoringPolicy()
	// Same URL as ProfileGeneric.ServiceURL
	policy.Cloudflare.TargetURL = "https://www.gstatic.com/generate_204"

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		ScoringPolicy:       &policy,
		Rounds:              1,
	}
	_, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	// Only 1 request (service, reused for CF).
	if callCount != 1 {
		t.Fatalf("expected 1 request (reused), got %d", callCount)
	}
}

func TestCheckProxy_CustomCFTarget_DistinctSecondRequest(t *testing.T) {
	// Custom CF target differs, service evaluation is enabled → two requests:
	// service + custom CF.
	var urlsCalled []string
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		urlsCalled = append(urlsCalled, url)
		return []byte("ok"), 50 * time.Millisecond, nil
	}

	policy := config.BalancedScoringPolicy()
	policy.Cloudflare.TargetURL = "https://custom-cf.example.com/check"

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		ScoringPolicy:       &policy,
		Rounds:              1,
	}
	_, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if len(urlsCalled) != 2 {
		t.Fatalf("expected 2 requests (service + custom CF), got %d: %v", len(urlsCalled), urlsCalled)
	}
}

func TestCheckProxy_CustomCFTarget_ServiceDisabledCustomTargetOnly(t *testing.T) {
	// Service evaluation disabled, custom CF target set → only custom CF request,
	// no ServiceURL request.
	var urlsCalled []string
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		urlsCalled = append(urlsCalled, url)
		return []byte("ok"), 50 * time.Millisecond, nil
	}

	policy := config.BalancedScoringPolicy()
	policy.Cloudflare.TargetURL = "https://custom-cf.example.com/check"

	opts := ProxyCheckOptions{
		ServiceReachability: false, // disabled
		ScoringPolicy:       &policy,
		Rounds:              1,
	}
	_, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if len(urlsCalled) != 1 {
		t.Fatalf("expected 1 request (custom CF only), got %d: %v", len(urlsCalled), urlsCalled)
	}
	if urlsCalled[0] != "https://custom-cf.example.com/check" {
		t.Fatalf("expected custom CF URL, got %s", urlsCalled[0])
	}
}

func TestCheckProxy_CustomCFTarget_TransportFailureNG(t *testing.T) {
	// Custom CF target transport failure → aggregate CF status is ng,
	// even if service request succeeded.
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://www.gstatic.com/generate_204":
			return []byte("ok"), 50 * time.Millisecond, nil
		case "https://custom-cf.example.com/check":
			return nil, 0, fmt.Errorf("connection refused")
		default:
			return nil, 0, fmt.Errorf("unexpected URL: %s", url)
		}
	}

	policy := config.BalancedScoringPolicy()
	policy.Cloudflare.TargetURL = "https://custom-cf.example.com/check"

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		ScoringPolicy:       &policy,
		Rounds:              1,
	}
	score, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err != nil {
		t.Fatalf("CheckProxy: %v", err)
	}
	if score.CloudflareStatus != "ng" {
		t.Fatalf("expected ng from custom CF transport failure (authoritative), got %q", score.CloudflareStatus)
	}
}

func TestCheckProxy_CustomCFTarget_MalformedURLReturnsError(t *testing.T) {
	// When a malformed custom target URL fails NormalizeURLForComparison,
	// CheckProxyWithResponse must return a descriptive error before any fetch.
	// This test bypasses service-level validation and calls the probe directly.
	var fetcherCalled bool
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		fetcherCalled = true
		return []byte("ok"), 50 * time.Millisecond, nil
	}

	policy := config.BalancedScoringPolicy()
	// Malformed URL with invalid host — NormalizeURLForComparison will fail.
	policy.Cloudflare.TargetURL = "https://[invalid::host::]"

	opts := ProxyCheckOptions{
		ServiceReachability: true,
		ScoringPolicy:       &policy,
		Rounds:              1,
	}
	_, err := CheckProxy(fetcher, node.Zero, ProfileGeneric, opts)
	if err == nil {
		t.Fatal("expected error for malformed custom CF target URL")
	}
	if !strings.Contains(err.Error(), "invalid custom CF target URL") {
		t.Fatalf("error should mention invalid custom CF target URL, got: %v", err)
	}
	if fetcherCalled {
		t.Fatal("fetcher should not be called when custom target URL is malformed")
	}
}
