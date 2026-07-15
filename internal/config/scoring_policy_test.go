package config

import (
	"testing"
)

func TestBalancedScoringPolicy_IsValid(t *testing.T) {
	p := BalancedScoringPolicy()
	if err := p.Validate(); err != nil {
		t.Fatalf("balanced preset should be valid: %v", err)
	}
}

func TestScoringPolicy_Validate_Weights(t *testing.T) {
	tests := []struct {
		name    string
		policy  ScoringPolicy
		wantErr string
	}{
		{
			name: "all zero weights",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.Weights = Weights{}
				return p
			}(),
			wantErr: "at least one weight must be positive",
		},
		{
			name: "negative service weight",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.Weights.Service = -1
				return p
			}(),
			wantErr: `weight "service" must be 0–100`,
		},
		{
			name: "over-100 api weight",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.Weights.API = 101
				return p
			}(),
			wantErr: `weight "api" must be 0–100`,
		},
		{
			name: "only latency positive works",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.Weights = Weights{Latency: 50}
				return p
			}(),
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestScoringPolicy_Validate_GradeThresholds(t *testing.T) {
	tests := []struct {
		name    string
		policy  ScoringPolicy
		wantErr string
	}{
		{
			name: "valid",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.GradeThresholds = GradeThresholds{A: 90, B: 75, C: 60, D: 40}
				return p
			}(),
			wantErr: "",
		},
		{
			name: "A not strictly greater than B",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.GradeThresholds = GradeThresholds{A: 75, B: 75, C: 60, D: 40}
				return p
			}(),
			wantErr: `grade threshold "B" must be < 75`,
		},
		{
			name: "C not strictly greater than D",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.GradeThresholds = GradeThresholds{A: 90, B: 75, C: 40, D: 40}
				return p
			}(),
			wantErr: `grade threshold "D" must be < 40`,
		},
		{
			name: "negative threshold",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.GradeThresholds = GradeThresholds{A: 90, B: 75, C: 60, D: -5}
				return p
			}(),
			wantErr: `grade threshold "D" must be 0–100`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestScoringPolicy_Validate_CFStatusScores(t *testing.T) {
	tests := []struct {
		name    string
		policy  ScoringPolicy
		wantErr string
	}{
		{
			name: "clean must not be null",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.Cloudflare.StatusScores.Clean = nil
				return p
			}(),
			wantErr: `status_score "clean" must not be null`,
		},
		{
			name: "ng null is allowed",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				return p
			}(),
			wantErr: "",
		},
		{
			name: "score out of range",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				v := 150
				p.Cloudflare.StatusScores.Block = &v
				return p
			}(),
			wantErr: `status_score "block" must be 0–100`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestScoringPolicy_Validate_LatencyBands(t *testing.T) {
	tests := []struct {
		name    string
		policy  ScoringPolicy
		wantErr string
	}{
		{
			name: "valid bands",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				return p
			}(),
			wantErr: "",
		},
		{
			name: "no open end band",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.Latency.Bands = []LatencyBand{
					{MaxMS: intPtr(500), Score: 100},
				}
				return p
			}(),
			wantErr: "open-ended band",
		},
		{
			name: "bands not sorted",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.Latency.Bands = []LatencyBand{
					{MaxMS: intPtr(800), Score: 80},
					{MaxMS: intPtr(300), Score: 100},
					{MaxMS: nil, Score: 0},
				}
				return p
			}(),
			wantErr: "strictly greater than previous",
		},
		{
			name: "score out of range",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.Latency.Bands = []LatencyBand{
					{MaxMS: intPtr(300), Score: 200},
					{MaxMS: nil, Score: 0},
				}
				return p
			}(),
			wantErr: "score must be 0–100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestScoringPolicy_Validate_CFGradeCaps(t *testing.T) {
	p := BalancedScoringPolicy()
	// Grade caps are required when policy is score_and_grade.
	err := p.Validate()
	if err != nil {
		t.Fatalf("balanced preset should be valid: %v", err)
	}
}

func TestScoringPolicy_Validate_DimCaps(t *testing.T) {
	tests := []struct {
		name    string
		policy  ScoringPolicy
		wantErr string
	}{
		{
			name: "nil caps valid",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				return p
			}(),
			wantErr: "",
		},
		{
			name: "invalid capacity grade token",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.DimensionCaps.Service = &DimensionCap{BelowScore: 50, GradeCap: "X"}
				return p
			}(),
			wantErr: `grade_cap must be A/B/C/D/F`,
		},
		{
			name: "below_score out of range",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.DimensionCaps.Service = &DimensionCap{BelowScore: 150, GradeCap: "D"}
				return p
			}(),
			wantErr: `below_score must be 0–100`,
		},
		{
			name: "valid dim cap",
			policy: func() ScoringPolicy {
				p := BalancedScoringPolicy()
				p.DimensionCaps.Cloudflare = &DimensionCap{BelowScore: 30, GradeCap: "D"}
				return p
			}(),
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestScoringPolicy_Validate_Version(t *testing.T) {
	p := BalancedScoringPolicy()
	p.Version = 2
	err := p.Validate()
	if err == nil || !contains(err.Error(), "version must be 1") {
		t.Fatalf("expected version error, got: %v", err)
	}
}

func TestNormalizeScoringPolicy_CanonicalWins(t *testing.T) {
	canonical := BalancedScoringPolicy()
	canonical.Weights.Service = 99

	result := NormalizeScoringPolicy(&canonical, NormalizeOpts{
		ServiceReachability: false, // should be ignored
	})
	if result.Weights.Service != 99 {
		t.Fatalf("canonical should win: got service weight %d, want 99", result.Weights.Service)
	}
}

func TestNormalizeScoringPolicy_CloneMutationIsolation(t *testing.T) {
	// Verify that the returned policy from NormalizeScoringPolicy is a deep
	// clone and mutations to it do not affect the original.
	canonical := BalancedScoringPolicy()
	origClean := *canonical.Cloudflare.StatusScores.Clean

	result := NormalizeScoringPolicy(&canonical, NormalizeOpts{})

	// Mutate result's pointer and slice fields.
	*result.Cloudflare.StatusScores.Clean = 1
	result.Latency.Bands[0].Score = 999
	result.Stability.CVBands[0].Score = 888
	result.DimensionCaps.Cloudflare = &DimensionCap{BelowScore: 10, GradeCap: "F"}

	// Original must be unchanged.
	if *canonical.Cloudflare.StatusScores.Clean != origClean {
		t.Fatalf("original clean score changed from %d to %d", origClean, *canonical.Cloudflare.StatusScores.Clean)
	}
	if canonical.Latency.Bands[0].Score == 999 {
		t.Fatal("original latency band score changed via clone")
	}
	if canonical.Stability.CVBands[0].Score == 888 {
		t.Fatal("original stability cv band score changed via clone")
	}
	if canonical.DimensionCaps.Cloudflare != nil {
		t.Fatal("original dimension caps changed via clone")
	}
}

func TestNormalizeScoringPolicy_Legacy(t *testing.T) {
	tests := []struct {
		name string
		opts NormalizeOpts
	}{
		{
			name: "all enabled",
			opts: NormalizeOpts{
				ServiceReachability: true,
				APIReachability:     true,
				CloudflareDetection: true,
				MultiRound:          true,
			},
		},
		{
			name: "service only",
			opts: NormalizeOpts{
				ServiceReachability: true,
				APIReachability:     false,
				CloudflareDetection: false,
				MultiRound:          false,
			},
		},
		{
			name: "all disabled",
			opts: NormalizeOpts{
				ServiceReachability: false,
				APIReachability:     false,
				CloudflareDetection: false,
				MultiRound:          false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeScoringPolicy(nil, tt.opts)
			if result.Version != 1 {
				t.Errorf("version should be 1, got %d", result.Version)
			}
			if !tt.opts.ServiceReachability && result.Weights.Service != 0 {
				t.Errorf("service weight should be 0 when disabled, got %d", result.Weights.Service)
			}
			if tt.opts.ServiceReachability && result.Weights.Service == 0 {
				t.Errorf("service weight should be >0 when enabled")
			}
			if !tt.opts.CloudflareDetection && result.Cloudflare.Policy != CFPolicyObserveOnly {
				t.Errorf("cf policy should be observe_only when detection disabled, got %s", result.Cloudflare.Policy)
			}
			if !tt.opts.MultiRound && result.Weights.Stability != 0 {
				t.Errorf("stability weight should be 0 when multi-round disabled")
			}
		})
	}
}

func TestValidateCustomTargetURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"", false},
		{"https://example.com/cf-check", false},
		{"http://example.com", true},            // must be https
		{"https://user:pass@example.com", true}, // has userinfo
		{"https://example.com#fragment", true},  // has fragment
		{"https://localhost", true},             // localhost
		{"https://localhost:8080", true},        // localhost
		{"https://127.0.0.1", true},             // loopback
		{"https://192.168.1.1", true},           // private
		{"https://10.0.0.1", true},              // private
		{"https://169.254.1.1", true},           // link-local
		{"https://224.0.0.1", true},             // multicast
		{"https://0.0.0.0", true},               // unspecified
		{"https://192.0.2.1", true},             // TEST-NET-1
		{"https://198.51.100.1", true},          // TEST-NET-2
		{"https://203.0.113.1", true},           // TEST-NET-3
		{"https://240.0.0.1", true},             // reserved
		{"https://[::1]", true},                 // IPv6 loopback
		{"https://[fe80::1]", true},             // IPv6 link-local
		{"https://[2001:db8::1]", true},         // documentation
		{"https://[2001:db8:aaaa::1]", true},    // documentation prefix
		{"https://[::]%lo", true},               // unspecified (ignore scope)
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := ValidateCustomTargetURL(tt.url)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for URL %q", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for URL %q: %v", tt.url, err)
			}
		})
	}
}

func TestNormalizeURLForComparison(t *testing.T) {
	tests := []struct {
		url     string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"https://Example.COM/Path", "https://example.com/Path", false},
		{"https://example.com:443/path/", "https://example.com/path", false},
		{"https://example.com:8443/path", "https://example.com:8443/path", false},
		{"https://user:pass@example.com/path", "https://example.com/path", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result, err := NormalizeURLForComparison(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestScoringPolicy_Clone_DeepCopy(t *testing.T) {
	orig := BalancedScoringPolicy()
	clone := orig.Clone()

	// Modify the original — clone must be unaffected.
	*orig.Cloudflare.StatusScores.Clean = 50
	orig.Latency.Bands[0].Score = 0
	orig.DimensionCaps.Cloudflare = &DimensionCap{BelowScore: 50, GradeCap: "D"}

	if *clone.Cloudflare.StatusScores.Clean != 100 {
		t.Fatalf("Clone should preserve clean=100, got %d", *clone.Cloudflare.StatusScores.Clean)
	}
	if clone.Latency.Bands[0].Score != 100 {
		t.Fatalf("Clone should preserve latency band[0].score=100, got %d", clone.Latency.Bands[0].Score)
	}
	if clone.DimensionCaps.Cloudflare != nil {
		t.Fatal("Clone should not reflect new dimension cap on original")
	}

	// Modify the clone — original must be unaffected.
	clone.Latency.Bands[1].MaxMS = nil
	if orig.Latency.Bands[1].MaxMS == nil {
		t.Fatal("Original should not reflect clone's MaxMS=nil")
	}
}

func TestScoringPolicy_Clone_Nil(t *testing.T) {
	clone := (*ScoringPolicy)(nil).Clone()
	if clone != nil {
		t.Fatal("Clone of nil should be nil")
	}
}

func TestScoringPolicy_Clone_IndependentBands(t *testing.T) {
	orig := BalancedScoringPolicy()
	clone := orig.Clone()

	// Mutate clone's latency bands.
	clone.Latency.Bands = append(clone.Latency.Bands, LatencyBand{MaxMS: intPtr(5000), Score: 10})

	if len(orig.Latency.Bands) != 5 {
		t.Fatal("Original bands slice must not be affected by clone append")
	}

	// Mutate clone's CV bands.
	v := 99
	clone.Stability.CVBands[0].MaxPercent = &v
	if *orig.Stability.CVBands[0].MaxPercent != 5 {
		t.Fatal("Original CV bands must not be affected by clone's MaxPercent mutation")
	}

	// Mutate clone's dimension caps.
	clone.DimensionCaps.Service = &DimensionCap{BelowScore: 30, GradeCap: "C"}
	if orig.DimensionCaps.Service != nil {
		t.Fatal("Original dimension caps must not be affected by clone")
	}
}

func TestScoringPolicy_Validate_LatencyBands_OpenBandBeforeEnd(t *testing.T) {
	p := BalancedScoringPolicy()
	// Open (nil) band at index 0, then more bands after it — must be rejected.
	p.Latency.Bands = []LatencyBand{
		{MaxMS: nil, Score: 0},
		{MaxMS: intPtr(500), Score: 100},
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for open band before final index")
	}
	if !contains(err.Error(), "open-ended band must be the final band") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScoringPolicy_Validate_LatencyBands_MissingOpenEnd(t *testing.T) {
	p := BalancedScoringPolicy()
	p.Latency.Bands = []LatencyBand{
		{MaxMS: intPtr(300), Score: 100},
		{MaxMS: intPtr(800), Score: 80},
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for missing final open-ended band")
	}
	if !contains(err.Error(), "open-ended band") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScoringPolicy_Validate_CVBands_OpenBandBeforeEnd(t *testing.T) {
	p := BalancedScoringPolicy()
	p.Stability.CVBands = []CVBand{
		{MaxPercent: nil, Score: 0},
		{MaxPercent: intPtr(30), Score: 100},
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for cv open band before final index")
	}
	if !contains(err.Error(), "open-ended band must be the final band") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScoringPolicy_Validate_CVBands_MissingOpenEnd(t *testing.T) {
	p := BalancedScoringPolicy()
	p.Stability.CVBands = []CVBand{
		{MaxPercent: intPtr(5), Score: 100},
		{MaxPercent: intPtr(15), Score: 80},
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for missing final open-ended cv band")
	}
	if !contains(err.Error(), "open-ended band") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCustomTargetURL_EmptyHost(t *testing.T) {
	err := ValidateCustomTargetURL("https://")
	if err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestValidateCustomTargetURL_NotAbsolute(t *testing.T) {
	err := ValidateCustomTargetURL("//example.com/path")
	if err == nil {
		t.Fatal("expected error for relative URL")
	}
	if !contains(err.Error(), "absolute") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCustomTargetURL_CaseInsensitiveScheme(t *testing.T) {
	err := ValidateCustomTargetURL("HTTPS://example.com/cf-check")
	if err != nil {
		t.Fatalf("expected HTTPS accepted case-insensitively: %v", err)
	}
}

func TestValidateCustomTargetURL_InvalidURL(t *testing.T) {
	err := ValidateCustomTargetURL("https://[invalid::host::]")
	if err == nil {
		t.Fatal("expected error for malformed URL")
	}
}

// contains is a simple substring check helper.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
