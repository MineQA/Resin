package api

import (
	"testing"
)

func TestReconcileExportName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		region string
		want   string
	}{
		// ---- Region missing/invalid → no-op ----
		{name: "empty region", input: "node-name", region: "", want: "node-name"},
		{name: "blank region", input: "node-name", region: "  ", want: "node-name"},
		{name: "invalid code", input: "node-name", region: "xyz", want: "node-name"},
		{name: "three-letter code", input: "node-name", region: "abc", want: "node-name"},
		{name: "reserved aa", input: "node-name", region: "aa", want: "node-name"},
		{name: "reserved eu", input: "node-name", region: "eu", want: "node-name"},
		{name: "reserved zz", input: "node-name", region: "zz", want: "node-name"},

		// ---- No marker → prepend canonical ----
		{name: "simple name", input: "node", region: "us", want: "[US] node"},
		{name: "hyphen name", input: "fast-node", region: "us", want: "[US] fast-node"},
		{name: "subscription prefix", input: "sub-A/node", region: "us", want: "sub-A/[US] node"},
		{name: "nested slash bare marker consumed", input: "a/b/us-node", region: "us", want: "a/b/[US] node"},
		{name: "double slash bare marker consumed", input: "a//hk-node", region: "us", want: "a//[US] node"},
		{name: "trailing slash", input: "prefix/", region: "us", want: "prefix/[US] "},

		// ---- Matching bracketed marker → canonicalize ----
		{name: "bracketed exact", input: "[US] node", region: "us", want: "[US] node"},
		{name: "bracketed lowercase", input: "[us] node", region: "us", want: "[US] node"},
		{name: "bracketed mixed case", input: "[Us] node", region: "us", want: "[US] node"},
		{name: "bracketed no space", input: "[US]node", region: "us", want: "[US] node"},
		{name: "bracketed subscription match", input: "sub-A/[US] node", region: "us", want: "sub-A/[US] node"},
		{name: "bracketed subscription lowercase", input: "sub-A/[us] node", region: "us", want: "sub-A/[US] node"},

		// ---- Mismatching bracketed marker → replace ----
		{name: "bracketed mismatch", input: "[HK] node", region: "us", want: "[US] node"},
		{name: "bracketed mismatch complex", input: "[SG] special-node", region: "us", want: "[US] special-node"},
		{name: "bracketed mismatch subscription", input: "sub-A/[HK] node", region: "us", want: "sub-A/[US] node"},

		// ---- Bare marker with hyphen ----
		{name: "bare hyphen match", input: "hk-node", region: "hk", want: "[HK] node"},
		{name: "bare hyphen mismatch", input: "hk-node", region: "us", want: "[US] node"},
		{name: "bare hyphen sub match", input: "sub/hk-node", region: "hk", want: "sub/[HK] node"},
		{name: "bare hyphen sub mismatch", input: "sub/hk-node", region: "us", want: "sub/[US] node"},
		{name: "bare hyphen lowercase", input: "hk-node", region: "hk", want: "[HK] node"},

		// ---- Bare marker with underscore ----
		{name: "bare underscore match", input: "us_node", region: "us", want: "[US] node"},
		{name: "bare underscore mismatch", input: "hk_node", region: "us", want: "[US] node"},

		// ---- Bare marker with space ----
		{name: "bare space match", input: "HK 01", region: "hk", want: "[HK] 01"},
		{name: "bare space mismatch", input: "US East Coast", region: "hk", want: "[HK] East Coast"},
		{name: "bare space match region", input: "US East Coast", region: "us", want: "[US] East Coast"},

		// ---- Non-country / ambiguous text (not an ISO code) ----
		{name: "non-ISO 2-letter prefix", input: "xx-node", region: "us", want: "[US] xx-node"},
		{name: "non-ISO 2-letter prefix underscore", input: "xx_node", region: "us", want: "[US] xx_node"},

		// ---- Full country words (opaque) ----
		{name: "full country name word", input: "hongkong-fast-01", region: "us", want: "[US] hongkong-fast-01"},
		{name: "full country with subscription", input: "sub/japan-01", region: "us", want: "sub/[US] japan-01"},

		// ---- Flags (opaque) ----
		{name: "flag emoji", input: "\U0001F1FA\U0001F1F8 us-server", region: "us", want: "[US] \U0001F1FA\U0001F1F8 us-server"},
		{name: "flag with subscription", input: "sub/\U0001F1ED\U0001F1F0 hk-server", region: "us", want: "sub/[US] \U0001F1ED\U0001F1F0 hk-server"},

		// ---- Chinese names (opaque) ----
		{name: "chinese plain", input: "机场A/美国节点01", region: "us", want: "机场A/[US] 美国节点01"},
		{name: "chinese with bracketed mismatch", input: "机场A/[HK]香港节点", region: "us", want: "机场A/[US] 香港节点"},
		{name: "chinese with bracketed match", input: "机场A/[US]美国节点01", region: "us", want: "机场A/[US] 美国节点01"},
		{name: "chinese with chinese leaf only", input: "美国节点01", region: "us", want: "[US] 美国节点01"},

		// ---- Empty remainder after removing marker → unchanged ----
		{name: "bracketed marker only", input: "[US]", region: "us", want: "[US]"},
		{name: "bracketed marker only subscription", input: "sub/[US]", region: "hk", want: "sub/[US]"},
		{name: "bracketed marker only mismatch", input: "[US]", region: "hk", want: "[US]"},

		// ---- Just 2-char ISO code (no separator) → not a recognized marker ----
		{name: "bare 2-char code without separator not recognized", input: "US", region: "us", want: "[US] US"},
		{name: "bare 2-char code mismatch", input: "HK", region: "us", want: "[US] HK"},

		// ---- Idempotence (already reconciled output stays the same) ----
		{name: "idempotent simple", input: "[US] node", region: "us", want: "[US] node"},
		{name: "idempotent subscription", input: "sub/[US] node", region: "us", want: "sub/[US] node"},
		{name: "idempotent chinese", input: "机场A/[US] 美国节点01", region: "us", want: "机场A/[US] 美国节点01"},
		{name: "idempotent bare canonicalized", input: "[HK] node", region: "hk", want: "[HK] node"},

		// ---- Region with different case ----
		{name: "region uppercase", input: "node", region: "US", want: "[US] node"},
		{name: "region mixed case", input: "node", region: "Us", want: "[US] node"},

		// ---- Edge: bare XX- with empty remainder ----
		{name: "bare hyphen only", input: "US-", region: "us", want: "US-"},
		{name: "bare hyphen only mismatch", input: "HK-", region: "us", want: "HK-"},

		// ---- Edge: bare XX_ with empty remainder ----
		{name: "bare underscore only", input: "HK_", region: "hk", want: "HK_"},

		// ---- Edge: bare XX␣ with empty remainder ----
		{name: "bare space only", input: "US ", region: "us", want: "US "},

		// ---- Edge: already bracketed [US] with no content after bracket removal ---
		// (covered by "bracketed marker only" above)

		// ---- No slash, various edge cases ----
		{name: "no slash valid code prefix recognized", input: "my-node", region: "sg", want: "[SG] node"},

		// ---- Multiple slashes, only last matters ----
		{name: "three path segments", input: "x/y/z", region: "us", want: "x/y/[US] z"},

		// ---- Slash at start ----
		{name: "leading slash", input: "/node", region: "us", want: "/[US] node"},

		// ---- Single char leaf ----
		{name: "single char leaf", input: "a", region: "us", want: "[US] a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconcileExportName(tt.input, tt.region)
			if got != tt.want {
				t.Errorf("reconcileExportName(%q, %q) = %q; want %q", tt.input, tt.region, got, tt.want)
			}
		})
	}
}

func TestReconcileExportName_Idempotent(t *testing.T) {
	// Verify that applying reconcileExportName a second time produces the
	// same result (idempotence).
	pairs := []struct {
		input  string
		region string
	}{
		{input: "node", region: "us"},
		{input: "[US] node", region: "us"},
		{input: "[us] node", region: "us"},
		{input: "hk-node", region: "us"},
		{input: "hk-node", region: "hk"},
		{input: "sub-A/[HK] node", region: "us"},
		{input: "sub-A/[US] node", region: "us"},
		{input: "机场A/美国节点01", region: "us"},
		{input: "机场A/[US] 美国节点01", region: "us"},
		{input: "US East Coast", region: "us"},
		{input: "US East Coast", region: "hk"},
		{input: "hongkong-01", region: "us"},
		{input: "US", region: "us"},
		{input: "xx-node", region: "us"},
		{input: "", region: "us"},
		{input: "[US]", region: "us"},
		{input: "US-", region: "us"},
	}

	for _, p := range pairs {
		first := reconcileExportName(p.input, p.region)
		second := reconcileExportName(first, p.region)
		if second != first {
			t.Errorf("not idempotent: input=%q region=%q first=%q second=%q",
				p.input, p.region, first, second)
		}
	}
}

func TestIsAssignedISO(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		// Assigned codes (sample)
		{"us", true},
		{"hk", true},
		{"sg", true},
		{"jp", true},
		{"de", true},
		{"gb", true},
		{"cn", true},
		{"tw", true},
		{"fr", true},
		// Uppercase should fail (isAssignedISO is lowercase-only)
		{"US", false},
		{"HK", false},
		// Reserved/exceptionally reserved
		{"aa", false},
		{"eu", false},
		{"zz", false},
		// Invalid
		{"xx", false},
		{"xyz", false},
		{"", false},
		{"12", false},
	}

	for _, tt := range tests {
		got := isAssignedISO(tt.code)
		if got != tt.want {
			t.Errorf("isAssignedISO(%q) = %v; want %v", tt.code, got, tt.want)
		}
	}
}

func TestParseLeafMarker(t *testing.T) {
	tests := []struct {
		name          string
		leaf          string
		wantCode      string
		wantRemainder string
	}{
		// Bracketed
		{name: "bracketed US", leaf: "[US] node", wantCode: "us", wantRemainder: " node"},
		{name: "bracketed lowercase", leaf: "[us] node", wantCode: "us", wantRemainder: " node"},
		{name: "bracketed no space", leaf: "[US]node", wantCode: "us", wantRemainder: "node"},
		{name: "bracketed only", leaf: "[US]", wantCode: "us", wantRemainder: ""},

		// Bare hyphen
		{name: "bare hyphen", leaf: "hk-node", wantCode: "hk", wantRemainder: "node"},
		{name: "bare hyphen lowercase", leaf: "hk-node", wantCode: "hk", wantRemainder: "node"},
		{name: "bare hyphen only", leaf: "US-", wantCode: "us", wantRemainder: ""},

		// Bare underscore
		{name: "bare underscore", leaf: "us_node", wantCode: "us", wantRemainder: "node"},
		{name: "bare underscore only", leaf: "HK_", wantCode: "hk", wantRemainder: ""},

		// Bare space
		{name: "bare space", leaf: "HK 01", wantCode: "hk", wantRemainder: "01"},
		{name: "bare space only", leaf: "US ", wantCode: "us", wantRemainder: ""},
		{name: "bare space US East Coast", leaf: "US East Coast", wantCode: "us", wantRemainder: "East Coast"},

		// Not recognized
		{name: "not ISO", leaf: "xx-node", wantCode: "", wantRemainder: "xx-node"},
		{name: "three letter", leaf: "abc-node", wantCode: "", wantRemainder: "abc-node"},
		{name: "reserved code", leaf: "aa-node", wantCode: "", wantRemainder: "aa-node"},
		{name: "reserved eu", leaf: "eu-token", wantCode: "", wantRemainder: "eu-token"},
		{name: "single char", leaf: "a", wantCode: "", wantRemainder: "a"},
		{name: "empty", leaf: "", wantCode: "", wantRemainder: ""},
		{name: "chinese", leaf: "美国节点01", wantCode: "", wantRemainder: "美国节点01"},
		{name: "flag emoji", leaf: "\U0001F1FA\U0001F1F8", wantCode: "", wantRemainder: "\U0001F1FA\U0001F1F8"},
		{name: "number prefix", leaf: "01-us", wantCode: "", wantRemainder: "01-us"},
		{name: "no separator XX", leaf: "US", wantCode: "", wantRemainder: "US"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, remainder := parseLeafMarker(tt.leaf)
			if code != tt.wantCode {
				t.Errorf("parseLeafMarker(%q) code = %q; want %q", tt.leaf, code, tt.wantCode)
			}
			if remainder != tt.wantRemainder {
				t.Errorf("parseLeafMarker(%q) remainder = %q; want %q", tt.leaf, remainder, tt.wantRemainder)
			}
		})
	}
}
