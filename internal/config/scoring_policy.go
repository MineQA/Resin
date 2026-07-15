package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ---------------------------------------------------------------------------
// CF policy mode
// ---------------------------------------------------------------------------

// CFPolicyMode controls how the Cloudflare dimension affects score and grade.
type CFPolicyMode string

const (
	CFPolicyObserveOnly   CFPolicyMode = "observe_only"
	CFPolicyScore         CFPolicyMode = "score"
	CFPolicyGrade         CFPolicyMode = "grade"
	CFPolicyScoreAndGrade CFPolicyMode = "score_and_grade"
)

// ValidCFPolicyModes enumerates the four allowed policy values.
var ValidCFPolicyModes = map[CFPolicyMode]bool{
	CFPolicyObserveOnly:   true,
	CFPolicyScore:         true,
	CFPolicyGrade:         true,
	CFPolicyScoreAndGrade: true,
}

// ---------------------------------------------------------------------------
// CF sub-scores (nullable)
// ---------------------------------------------------------------------------

// CFStatusScores maps every canonical CF status to a nullable integer 0–100.
// A nil pointer means the status is unavailable (excluded from scoring).
type CFStatusScores struct {
	Clean            *int `json:"clean"`
	NotDetected      *int `json:"not_detected"`
	JSChallenge      *int `json:"js_challenge"`
	CaptchaChallenge *int `json:"captcha_challenge"`
	Challenge        *int `json:"challenge"`
	Block            *int `json:"block"`
	NG               *int `json:"ng"`
	Unchecked        *int `json:"unchecked"`
}

// CFGradeCaps maps challenge statuses to a letter grade cap.
// Only JSChallenge, CaptchaChallenge, Challenge, and Block have caps.
// Others (clean, not_detected, ng, unchecked) have no cap.
type CFGradeCaps struct {
	JSChallenge      string `json:"js_challenge"`
	CaptchaChallenge string `json:"captcha_challenge"`
	Challenge        string `json:"challenge"`
	Block            string `json:"block"`
}

// ---------------------------------------------------------------------------
// CF scoring config
// ---------------------------------------------------------------------------

// CFScoringConfig groups the Cloudflare scoring sub-policy.
type CFScoringConfig struct {
	Policy       CFPolicyMode   `json:"policy"`
	TargetURL    string         `json:"target_url"`
	StatusScores CFStatusScores `json:"status_scores"`
	GradeCaps    CFGradeCaps    `json:"grade_caps"`
}

// ---------------------------------------------------------------------------
// Latency bands
// ---------------------------------------------------------------------------

// LatencyBand maps a latency threshold (max_ms) to a score.
// MaxMS nil is the open upper band.
type LatencyBand struct {
	MaxMS *int `json:"max_ms"`
	Score int  `json:"score"`
}

// LatencyConfig holds the latency score bands.
type LatencyConfig struct {
	Bands []LatencyBand `json:"bands"`
}

// ---------------------------------------------------------------------------
// Stability CV bands
// ---------------------------------------------------------------------------

// CVBand maps a coefficient-of-variation threshold (max_percent) to a score.
// MaxPercent nil is the open upper band.
type CVBand struct {
	MaxPercent *int `json:"max_percent"`
	Score      int  `json:"score"`
}

// StabilityConfig holds the stability scoring configuration.
type StabilityConfig struct {
	CVBands []CVBand `json:"cv_bands"`
}

// ---------------------------------------------------------------------------
// Dimension caps
// ---------------------------------------------------------------------------

// DimensionCap defines an optional grade degradation rule.
// If a participating dimension sub-score is below BelowScore, apply GradeCap.
// A nil pointer in DimensionCaps means no rule is configured.
type DimensionCap struct {
	BelowScore int    `json:"below_score"`
	GradeCap   string `json:"grade_cap"`
}

// DimensionCaps holds optional per-dimension grade cap rules.
type DimensionCaps struct {
	Service    *DimensionCap `json:"service"`
	API        *DimensionCap `json:"api"`
	Cloudflare *DimensionCap `json:"cloudflare"`
	Stability  *DimensionCap `json:"stability"`
	Latency    *DimensionCap `json:"latency"`
}

// ---------------------------------------------------------------------------
// Weights
// ---------------------------------------------------------------------------

// Weights holds the five dimension weights, each 0–100.
type Weights struct {
	Service    int `json:"service"`
	API        int `json:"api"`
	Cloudflare int `json:"cloudflare"`
	Stability  int `json:"stability"`
	Latency    int `json:"latency"`
}

// ---------------------------------------------------------------------------
// Grade thresholds
// ---------------------------------------------------------------------------

// GradeThresholds defines the score boundaries for A/B/C/D.
// All must be integers 0–100 and A > B > C > D.
type GradeThresholds struct {
	A int `json:"A"`
	B int `json:"B"`
	C int `json:"C"`
	D int `json:"D"`
}

// ---------------------------------------------------------------------------
// ScoringPolicy — versioned nested config object
// ---------------------------------------------------------------------------

// ScoringPolicy is the complete versioned scoring policy.
// Version must be 1 when non-zero. A zero-value ScoringPolicy is invalid and
// should be replaced by a default/normalized policy before use.
type ScoringPolicy struct {
	Version         int             `json:"version"`
	Weights         Weights         `json:"weights"`
	GradeThresholds GradeThresholds `json:"grade_thresholds"`
	Cloudflare      CFScoringConfig `json:"cloudflare"`
	Latency         LatencyConfig   `json:"latency"`
	Stability       StabilityConfig `json:"stability"`
	DimensionCaps   DimensionCaps   `json:"dimension_caps"`
}

// ---------------------------------------------------------------------------
// Balanced preset
// ---------------------------------------------------------------------------

// intPtr is a helper to create *int literals.
func intPtr(v int) *int { return &v }

// BalancedScoringPolicy returns the locked balanced default preset.
func BalancedScoringPolicy() ScoringPolicy {
	return ScoringPolicy{
		Version: 1,
		Weights: Weights{
			Service:    40,
			API:        15,
			Cloudflare: 20,
			Stability:  10,
			Latency:    15,
		},
		GradeThresholds: GradeThresholds{
			A: 90,
			B: 75,
			C: 60,
			D: 40,
		},
		Cloudflare: CFScoringConfig{
			Policy:    CFPolicyScoreAndGrade,
			TargetURL: "",
			StatusScores: CFStatusScores{
				Clean:            intPtr(100),
				NotDetected:      intPtr(100),
				JSChallenge:      intPtr(40),
				CaptchaChallenge: intPtr(20),
				Challenge:        intPtr(10),
				Block:            intPtr(0),
				NG:               nil, // unavailable
				Unchecked:        nil, // unavailable
			},
			GradeCaps: CFGradeCaps{
				JSChallenge:      "D",
				CaptchaChallenge: "D",
				Challenge:        "D",
				Block:            "F",
			},
		},
		Latency: LatencyConfig{
			Bands: []LatencyBand{
				{MaxMS: intPtr(300), Score: 100},
				{MaxMS: intPtr(800), Score: 80},
				{MaxMS: intPtr(1500), Score: 60},
				{MaxMS: intPtr(3000), Score: 30},
				{MaxMS: nil, Score: 0},
			},
		},
		Stability: StabilityConfig{
			CVBands: []CVBand{
				{MaxPercent: intPtr(5), Score: 100},
				{MaxPercent: intPtr(15), Score: 80},
				{MaxPercent: intPtr(30), Score: 60},
				{MaxPercent: intPtr(50), Score: 30},
				{MaxPercent: nil, Score: 0},
			},
		},
		DimensionCaps: DimensionCaps{
			Service:    nil,
			API:        nil,
			Cloudflare: nil,
			Stability:  nil,
			Latency:    nil,
		},
	}
}

// ---------------------------------------------------------------------------
// Deep copy
// ---------------------------------------------------------------------------

// Clone returns a deep copy of the ScoringPolicy, ensuring all pointer and
// slice fields are independent of the original.
func (p *ScoringPolicy) Clone() *ScoringPolicy {
	if p == nil {
		return nil
	}
	out := *p

	// Deep copy CF status scores (contains *int pointers).
	ss := &out.Cloudflare.StatusScores
	origSS := &p.Cloudflare.StatusScores
	if origSS.Clean != nil {
		v := *origSS.Clean
		ss.Clean = &v
	}
	if origSS.NotDetected != nil {
		v := *origSS.NotDetected
		ss.NotDetected = &v
	}
	if origSS.JSChallenge != nil {
		v := *origSS.JSChallenge
		ss.JSChallenge = &v
	}
	if origSS.CaptchaChallenge != nil {
		v := *origSS.CaptchaChallenge
		ss.CaptchaChallenge = &v
	}
	if origSS.Challenge != nil {
		v := *origSS.Challenge
		ss.Challenge = &v
	}
	if origSS.Block != nil {
		v := *origSS.Block
		ss.Block = &v
	}
	if origSS.NG != nil {
		v := *origSS.NG
		ss.NG = &v
	}
	if origSS.Unchecked != nil {
		v := *origSS.Unchecked
		ss.Unchecked = &v
	}

	// Deep copy latency bands (contains *int MaxMS).
	if len(p.Latency.Bands) > 0 {
		out.Latency.Bands = make([]LatencyBand, len(p.Latency.Bands))
		for i, b := range p.Latency.Bands {
			cb := b
			if b.MaxMS != nil {
				v := *b.MaxMS
				cb.MaxMS = &v
			}
			out.Latency.Bands[i] = cb
		}
	}

	// Deep copy stability CV bands (contains *int MaxPercent).
	if len(p.Stability.CVBands) > 0 {
		out.Stability.CVBands = make([]CVBand, len(p.Stability.CVBands))
		for i, b := range p.Stability.CVBands {
			cb := b
			if b.MaxPercent != nil {
				v := *b.MaxPercent
				cb.MaxPercent = &v
			}
			out.Stability.CVBands[i] = cb
		}
	}

	// Deep copy dimension caps (contains *DimensionCap pointers).
	dc := &out.DimensionCaps
	origDC := &p.DimensionCaps
	if origDC.Service != nil {
		v := *origDC.Service
		dc.Service = &v
	}
	if origDC.API != nil {
		v := *origDC.API
		dc.API = &v
	}
	if origDC.Cloudflare != nil {
		v := *origDC.Cloudflare
		dc.Cloudflare = &v
	}
	if origDC.Stability != nil {
		v := *origDC.Stability
		dc.Stability = &v
	}
	if origDC.Latency != nil {
		v := *origDC.Latency
		dc.Latency = &v
	}

	return &out
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

var validGradeTokens = map[string]bool{
	"A": true, "B": true, "C": true, "D": true, "F": true,
}

// Validate checks the scoring policy contract and returns a descriptive error
// if any constraint is violated.
func (p *ScoringPolicy) Validate() error {
	if p == nil {
		return fmt.Errorf("scoring policy: is nil")
	}
	if p.Version != 1 {
		return fmt.Errorf("scoring policy: version must be 1, got %d", p.Version)
	}

	// Weights: all 0–100, at least one positive.
	w := &p.Weights
	if err := validateWeight("service", w.Service); err != nil {
		return err
	}
	if err := validateWeight("api", w.API); err != nil {
		return err
	}
	if err := validateWeight("cloudflare", w.Cloudflare); err != nil {
		return err
	}
	if err := validateWeight("stability", w.Stability); err != nil {
		return err
	}
	if err := validateWeight("latency", w.Latency); err != nil {
		return err
	}
	if w.Service <= 0 && w.API <= 0 && w.Cloudflare <= 0 && w.Stability <= 0 && w.Latency <= 0 {
		return fmt.Errorf("scoring policy: at least one weight must be positive")
	}

	// Grade thresholds: all present, 0–100, strict A > B > C > D.
	gt := &p.GradeThresholds
	if err := validateThreshold("A", gt.A, -1, 101); err != nil {
		return err
	}
	if err := validateThreshold("B", gt.B, -1, gt.A); err != nil {
		return err
	}
	if err := validateThreshold("C", gt.C, -1, gt.B); err != nil {
		return err
	}
	if err := validateThreshold("D", gt.D, -1, gt.C); err != nil {
		return err
	}

	// CF policy mode.
	cf := &p.Cloudflare
	if !ValidCFPolicyModes[cf.Policy] {
		return fmt.Errorf("scoring policy: invalid cloudflare policy %q", cf.Policy)
	}

	// CF status scores: all keys must be present, values either int 0–100 or nil
	// (only for ng and unchecked).
	ss := &cf.StatusScores
	if err := validateCFScore("clean", ss.Clean, false); err != nil {
		return err
	}
	if err := validateCFScore("not_detected", ss.NotDetected, false); err != nil {
		return err
	}
	if err := validateCFScore("js_challenge", ss.JSChallenge, false); err != nil {
		return err
	}
	if err := validateCFScore("captcha_challenge", ss.CaptchaChallenge, false); err != nil {
		return err
	}
	if err := validateCFScore("challenge", ss.Challenge, false); err != nil {
		return err
	}
	if err := validateCFScore("block", ss.Block, false); err != nil {
		return err
	}
	// ng and unchecked are permitted to be nil (unavailable).
	if err := validateCFScore("ng", ss.NG, true); err != nil {
		return err
	}
	if err := validateCFScore("unchecked", ss.Unchecked, true); err != nil {
		return err
	}

	// CF grade caps: non-empty when policy is grade or score_and_grade.
	if cf.Policy == CFPolicyGrade || cf.Policy == CFPolicyScoreAndGrade {
		caps := &cf.GradeCaps
		if err := validateGradeCap("js_challenge", caps.JSChallenge); err != nil {
			return err
		}
		if err := validateGradeCap("captcha_challenge", caps.CaptchaChallenge); err != nil {
			return err
		}
		if err := validateGradeCap("challenge", caps.Challenge); err != nil {
			return err
		}
		if err := validateGradeCap("block", caps.Block); err != nil {
			return err
		}
	}

	// Latency bands: non-empty, sorted by strictly increasing finite maxima,
	// scores 0–100, exactly one final open-ended nil-max band.
	if err := validateBands(p.Latency.Bands); err != nil {
		return fmt.Errorf("scoring policy: latency bands: %w", err)
	}

	// Stability CV bands: same rules.
	if err := validateCVBands(p.Stability.CVBands); err != nil {
		return fmt.Errorf("scoring policy: stability cv_bands: %w", err)
	}

	// Dimension caps: optional, validate when non-nil.
	if err := validateDimCap("service", p.DimensionCaps.Service); err != nil {
		return err
	}
	if err := validateDimCap("api", p.DimensionCaps.API); err != nil {
		return err
	}
	if err := validateDimCap("cloudflare", p.DimensionCaps.Cloudflare); err != nil {
		return err
	}
	if err := validateDimCap("stability", p.DimensionCaps.Stability); err != nil {
		return err
	}
	if err := validateDimCap("latency", p.DimensionCaps.Latency); err != nil {
		return err
	}

	return nil
}

func validateWeight(name string, v int) error {
	if v < 0 || v > 100 {
		return fmt.Errorf("scoring policy: weight %q must be 0–100, got %d", name, v)
	}
	return nil
}

func validateThreshold(name string, v, min, max int) error {
	if v < 0 || v > 100 {
		return fmt.Errorf("scoring policy: grade threshold %q must be 0–100, got %d", name, v)
	}
	if min >= 0 && v <= min {
		return fmt.Errorf("scoring policy: grade threshold %q must be > %d, got %d", name, min, v)
	}
	if max >= 0 && v >= max {
		return fmt.Errorf("scoring policy: grade threshold %q must be < %d, got %d", name, max, v)
	}
	return nil
}

func validateCFScore(name string, v *int, allowNil bool) error {
	if v == nil {
		if !allowNil {
			return fmt.Errorf("scoring policy: cloudflare status_score %q must not be null", name)
		}
		return nil
	}
	if *v < 0 || *v > 100 {
		return fmt.Errorf("scoring policy: cloudflare status_score %q must be 0–100 or null, got %d", name, *v)
	}
	return nil
}

func validateGradeCap(name, cap string) error {
	if cap == "" {
		return fmt.Errorf("scoring policy: cloudflare grade_cap %q must not be empty when policy is grade or score_and_grade", name)
	}
	if !validGradeTokens[cap] {
		return fmt.Errorf("scoring policy: cloudflare grade_cap %q must be A/B/C/D/F, got %q", name, cap)
	}
	return nil
}

func validateBands(bands []LatencyBand) error {
	if len(bands) == 0 {
		return fmt.Errorf("must have at least one band")
	}
	lastMax := -1
	hasOpenEnd := false
	for i, b := range bands {
		if hasOpenEnd {
			return fmt.Errorf("band[%d]: open-ended band must be the final band", i)
		}
		if b.Score < 0 || b.Score > 100 {
			return fmt.Errorf("band[%d] score must be 0–100, got %d", i, b.Score)
		}
		if b.MaxMS == nil {
			hasOpenEnd = true
			continue
		}
		if *b.MaxMS <= lastMax {
			return fmt.Errorf("band[%d] max_ms %d must be strictly greater than previous %d", i, *b.MaxMS, lastMax)
		}
		lastMax = *b.MaxMS
	}
	if !hasOpenEnd {
		return fmt.Errorf("must have exactly one final open-ended band with null max_ms")
	}
	return nil
}

func validateCVBands(bands []CVBand) error {
	if len(bands) == 0 {
		return fmt.Errorf("must have at least one band")
	}
	lastMax := -1
	hasOpenEnd := false
	for i, b := range bands {
		if hasOpenEnd {
			return fmt.Errorf("cv_band[%d]: open-ended band must be the final band", i)
		}
		if b.Score < 0 || b.Score > 100 {
			return fmt.Errorf("cv_band[%d] score must be 0–100, got %d", i, b.Score)
		}
		if b.MaxPercent == nil {
			hasOpenEnd = true
			continue
		}
		if *b.MaxPercent <= lastMax {
			return fmt.Errorf("cv_band[%d] max_percent %d must be strictly greater than previous %d", i, *b.MaxPercent, lastMax)
		}
		lastMax = *b.MaxPercent
	}
	if !hasOpenEnd {
		return fmt.Errorf("must have exactly one final open-ended band with null max_percent")
	}
	return nil
}

func validateDimCap(name string, dc *DimensionCap) error {
	if dc == nil {
		return nil
	}
	if dc.BelowScore < 0 || dc.BelowScore > 100 {
		return fmt.Errorf("scoring policy: dimension_cap %q below_score must be 0–100, got %d", name, dc.BelowScore)
	}
	if !validGradeTokens[dc.GradeCap] {
		return fmt.Errorf("scoring policy: dimension_cap %q grade_cap must be A/B/C/D/F, got %q", name, dc.GradeCap)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Legacy normalization
// ---------------------------------------------------------------------------

// NormalizeScoringPolicy derives a canonical ScoringPolicy from either the
// nested policy or legacy flat options. The contract:
//
//   - When nested is non-nil and version > 0, return a validated copy (canonical wins).
//   - When nested is nil or version < 1, build a compatibility policy from
//     the flat options that approximates current enabled dimensions.
//
// The flat opts correspond to the legacy RuntimeConfig flat proxy check fields.
// normalizeOpts is a helper that accepts the old boolean fields.
type NormalizeOpts struct {
	// Service enabled when ProxyCheckServiceReachability is true.
	ServiceReachability bool
	// API enabled when ProxyCheckAPIReachability is true.
	APIReachability bool
	// CloudflareDetection legacy boolean for CF scoring impact.
	CloudflareDetection bool
	// MultiRound enabled when ProxyCheckMultiRound is true.
	MultiRound bool
}

// NormalizeScoringPolicy produces a ScoringPolicy from either the canonical
// nested object or the legacy flat options.
func NormalizeScoringPolicy(nested *ScoringPolicy, opts NormalizeOpts) ScoringPolicy {
	if nested != nil && nested.Version >= 1 {
		return *nested.Clone()
	}

	// Start from balanced preset.
	policy := BalancedScoringPolicy()

	// Zero out weights for disabled dimensions.
	if !opts.ServiceReachability {
		policy.Weights.Service = 0
	}
	if !opts.APIReachability {
		policy.Weights.API = 0
	}
	if !opts.CloudflareDetection {
		policy.Weights.Cloudflare = 0
		policy.Cloudflare.Policy = CFPolicyObserveOnly
	}
	if !opts.MultiRound {
		policy.Weights.Stability = 0
	}
	// Latency is always measured.

	return policy
}

// ---------------------------------------------------------------------------
// Custom target URL validation (best-effort)
// ---------------------------------------------------------------------------

// ValidateCustomTargetURL performs best-effort URL shape validation for the
// custom CF target URL field.
//
// Rules:
//   - Empty is allowed (means use profile ServiceURL).
//   - Must be absolute HTTPS.
//   - Must not contain userinfo (credentials).
//   - Must not contain fragment.
//   - Must not use localhost hostnames.
//   - Must not use IP literals in loopback, private, link-local, multicast,
//     unspecified, documentation, or reserved ranges.
//
// This is URL-shape validation only. It does NOT prevent SSRF via DNS rebinding
// or public hostnames resolving to internal addresses at the remote proxy egress.
// See locked contract §10 for documented residual risk.
func ValidateCustomTargetURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if !u.IsAbs() {
		return fmt.Errorf("must be an absolute URL, got %q", rawURL)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("must use https scheme, got %q", u.Scheme)
	}
	if u.User != nil && u.User.String() != "" {
		return fmt.Errorf("must not contain userinfo (credentials)")
	}
	if u.Fragment != "" {
		return fmt.Errorf("must not contain fragment")
	}

	rawHost := strings.ToLower(u.Host)
	if rawHost == "" {
		return fmt.Errorf("must have a non-empty host")
	}
	// Strip port for host checks.
	host := rawHost
	if h, _, err := net.SplitHostPort(rawHost); err == nil {
		host = h
	}
	// Strip IPv6 brackets for IP parsing.
	host = strings.Trim(host, "[]")

	if host == "" {
		return fmt.Errorf("must have a non-empty hostname")
	}

	// Reject localhost names.
	if host == "localhost" || strings.HasPrefix(host, "localhost.") {
		return fmt.Errorf("must not use localhost hostname")
	}

	// Check if host is an IP literal.
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return fmt.Errorf("must not use loopback address")
		}
		if ip.IsPrivate() {
			return fmt.Errorf("must not use private address")
		}
		if ip.IsLinkLocalUnicast() {
			return fmt.Errorf("must not use link-local unicast address")
		}
		if ip.IsLinkLocalMulticast() {
			return fmt.Errorf("must not use link-local multicast address")
		}
		if ip.IsMulticast() {
			return fmt.Errorf("must not use multicast address")
		}
		if ip.IsUnspecified() {
			return fmt.Errorf("must not use unspecified address")
		}
		if isDocumentationIP(ip) {
			return fmt.Errorf("must not use documentation address (TEST-NET)")
		}
		if isReservedIP(ip) {
			return fmt.Errorf("must not use reserved address")
		}
	}

	return nil
}

// isDocumentationIP checks for TEST-NET ranges (192.0.2.0/24, 198.51.100.0/24,
// 203.0.113.0/24) and documentation IPv6 (2001:db8::/32).
func isDocumentationIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		// 192.0.2.0/24
		if ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2 {
			return true
		}
		// 198.51.100.0/24
		if ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100 {
			return true
		}
		// 203.0.113.0/24
		if ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113 {
			return true
		}
		return false
	}
	// 2001:db8::/32
	return len(ip) == net.IPv6len && ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8
}

// isReservedIP checks for IANA reserved ranges that should not be targeted.
func isReservedIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		// 240.0.0.0/4 (future use / reserved)
		if ip4[0] >= 240 {
			return true
		}
		return false
	}
	// IPv6 reserved ranges: 100::/64 (discard), fec0::/10 (site-local, deprecated)
	if len(ip) == net.IPv6len && ip[0] == 0x01 && ip[1] == 0x00 {
		return true
	}
	if len(ip) == net.IPv6len && ip[0] == 0xfe && (ip[1]&0xc0) == 0xc0 {
		return true
	}
	return false
}

// NormalizeURLForComparison normalizes a URL for equality comparison.
// It lowercases scheme+host, strips authentication, trailing slash, and
// standard ports.
func NormalizeURLForComparison(rawURL string) (string, error) {
	if rawURL == "" {
		return "", nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	// Lowercase scheme and host.
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// Strip userinfo.
	u.User = nil

	// Strip standard port.
	if h, p, err := net.SplitHostPort(u.Host); err == nil {
		port := p
		if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
			u.Host = h
		}
	}

	// Strip fragment.
	u.Fragment = ""

	// Normalize path: strip trailing slash.
	u.Path = strings.TrimSuffix(u.Path, "/")

	// Clear RawQuery to avoid ordering issues — we only compare URLs for
	// the purpose of "same target", so query params are part of identity.
	// Keep RawQuery as-is — changing query params changes the target.

	return u.String(), nil
}
