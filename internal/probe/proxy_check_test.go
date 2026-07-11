package probe

import (
	"errors"
	"testing"
	"time"

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
				200*time.Millisecond, nil
		case "https://api.openai.com/v1/models":
			return []byte(`{"data":[]}`), 80*time.Millisecond, nil
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
	// CloudflareFlag=true, but opts.CloudflareDetection=false
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://chatgpt.com":
			return []byte(`<html><head><title>Just a moment...</title>
				<script src="/cf-browser-verification"></script></html>`),
				200*time.Millisecond, nil
		case "https://api.openai.com/v1/models":
			return []byte(`{"data":[]}`), 80*time.Millisecond, nil
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
	if score.CloudflareChallenged {
		t.Fatal("expected no CF challenged when CloudflareDetection is disabled")
	}
	// Grade should be A (all enabled checks pass, CF detection disabled)
	if score.Grade != "A" {
		t.Fatalf("expected A (CF detection disabled), got %s", score.Grade)
	}
}

func TestCheckProxy_CFChallengeGenericProfile(t *testing.T) {
	// Generic profile has CloudflareFlag=false, so body CF check should not run
	// even when opts.CloudflareDetection=true.
	fetcher := func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		switch url {
		case "https://www.gstatic.com/generate_204":
			return []byte(`<html><head><title>Just a moment...</title><footer>Cloudflare</footer></html>`),
				200*time.Millisecond, nil
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
	if score.CloudflareChallenged {
		t.Fatal("expected no CF challenged on generic profile (CloudflareFlag=false)")
	}
	if !score.ServiceReachable {
		t.Fatal("service should be reachable")
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
