package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/subscription"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// profileNodeName tests
// ---------------------------------------------------------------------------

func TestProfileNodeName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		region string
		want   string
	}{
		// ---- Assigned ISO ----
		{name: "assigned simple", input: "node", region: "us", want: "[US] node"},
		{name: "assigned keeps prefix", input: "sub-A/node", region: "us", want: "sub-A/[US] node"},
		{name: "assigned strips existing bracketed marker (same)", input: "[US] node", region: "us", want: "[US] node"},
		{name: "assigned strips existing bracketed marker (diff)", input: "[HK] node", region: "us", want: "[US] node"},
		{name: "assigned strips bare marker (same)", input: "hk-node", region: "hk", want: "[HK] node"},
		{name: "assigned strips bare marker (diff)", input: "hk-node", region: "us", want: "[US] node"},
		{name: "assigned region uppercase", input: "node", region: "US", want: "[US] node"},
		{name: "assigned region mixed case", input: "node", region: "Us", want: "[US] node"},

		// ---- Unknown region ----
		{name: "unknown simple", input: "node", region: "xx", want: "[??] node"},
		{name: "unknown strips old bracketed marker", input: "[US] node", region: "xx", want: "[??] node"},
		{name: "unknown strips old bare marker", input: "hk-node", region: "xx", want: "[??] node"},
		{name: "unknown preserves prefix", input: "sub-A/[US] node", region: "xx", want: "sub-A/[??] node"},
		{name: "unknown empty region", input: "node", region: "", want: "[??] node"},
		{name: "unknown invalid code", input: "node", region: "xyz", want: "[??] node"},
		{name: "unknown reserved aa", input: "node", region: "aa", want: "[??] node"},
		{name: "unknown reserved eu", input: "node", region: "eu", want: "[??] node"},
		{name: "unknown reserved zz", input: "node", region: "zz", want: "[??] node"},

		// ---- Marker-only leaf (fallback to original leaf) ----
		{name: "bracketed marker only assigned", input: "[US]", region: "us", want: "[US] [US]"},
		{name: "bracketed marker only unknown", input: "[US]", region: "xx", want: "[??] [US]"},
		{name: "bare marker only assigned", input: "US-", region: "us", want: "[US] US-"},
		{name: "bare marker only unknown", input: "US-", region: "xx", want: "[??] US-"},
		{name: "bare underscore only assigned", input: "HK_", region: "hk", want: "[HK] HK_"},
		{name: "bare underscore only unknown", input: "HK_", region: "xx", want: "[??] HK_"},
		{name: "subscription marker only", input: "sub/[US]", region: "xx", want: "sub/[??] [US]"},

		// ---- Path prefix preserved ----
		{name: "double slash", input: "a//hk-node", region: "us", want: "a//[US] node"},
		{name: "three segments", input: "x/y/z", region: "us", want: "x/y/[US] z"},
		{name: "leading slash", input: "/node", region: "us", want: "/[US] node"},
		{name: "trailing slash", input: "prefix/", region: "us", want: "prefix/[US] "},

		// ---- Full country words (opaque) ----
		{name: "full country name", input: "hongkong-fast-01", region: "us", want: "[US] hongkong-fast-01"},
		{name: "full country name prefix", input: "sub/japan-01", region: "us", want: "sub/[US] japan-01"},

		// ---- Flags (opaque) ----
		{name: "flag emoji assigned", input: "\U0001F1FA\U0001F1F8 us-server", region: "us", want: "[US] \U0001F1FA\U0001F1F8 us-server"},
		{name: "flag emoji unknown", input: "\U0001F1FA\U0001F1F8 us-server", region: "xx", want: "[??] \U0001F1FA\U0001F1F8 us-server"},

		// ---- Chinese names (opaque) ----
		{name: "chinese plain assigned", input: "机场A/美国节点01", region: "us", want: "机场A/[US] 美国节点01"},
		{name: "chinese plain unknown", input: "机场A/美国节点01", region: "xx", want: "机场A/[??] 美国节点01"},

		// ---- 2-char code without separator not recognized as marker ----
		{name: "bare 2-char no sep", input: "US", region: "us", want: "[US] US"},
		{name: "bare 2-char no sep unknown", input: "HK", region: "xx", want: "[??] HK"},

		// ---- Empty input ----
		{name: "empty string assigned", input: "", region: "us", want: "[US] "},
		{name: "empty string unknown", input: "", region: "xx", want: "[??] "},

		// ---- Bracketed with no space after ----
		{name: "no space after bracket assigned", input: "[US]node", region: "us", want: "[US] node"},
		{name: "no space after bracket unknown", input: "[US]node", region: "xx", want: "[??] node"},

		// ---- Non-ISO 2-letter prefix ----
		{name: "non-ISO prefix assigned", input: "xx-node", region: "us", want: "[US] xx-node"},
		{name: "non-ISO prefix unknown", input: "xx-node", region: "xx", want: "[??] xx-node"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := profileNodeName(tt.input, tt.region)
			if got != tt.want {
				t.Errorf("profileNodeName(%q, %q) = %q; want %q", tt.input, tt.region, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// dedupProfileProxies tests
// ---------------------------------------------------------------------------

func TestDedupProfileProxies_NoCollision(t *testing.T) {
	proxies := []profileProxy{
		{proxy: map[string]any{"name": "A"}, nodeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", name: "A"},
		{proxy: map[string]any{"name": "B"}, nodeHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", name: "B"},
	}
	if err := dedupProfileProxies(proxies); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range proxies {
		if strings.Contains(p.proxy["name"].(string), " #") {
			t.Fatalf("non-colliding proxy got suffix: %q", p.proxy["name"])
		}
	}
}

func TestDedupProfileProxies_SimpleCollision(t *testing.T) {
	proxies := []profileProxy{
		{proxy: map[string]any{"name": "A"}, nodeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", name: "A"},
		{proxy: map[string]any{"name": "A"}, nodeHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", name: "A"},
	}
	if err := dedupProfileProxies(proxies); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proxies[0].proxy["name"].(string) != "A #aaaaaaaa" {
		t.Fatalf("proxy[0] name = %q, want %q", proxies[0].proxy["name"], "A #aaaaaaaa")
	}
	if proxies[1].proxy["name"].(string) != "A #bbbbbbbb" {
		t.Fatalf("proxy[1] name = %q, want %q", proxies[1].proxy["name"], "A #bbbbbbbb")
	}
}

func TestDedupProfileProxies_NonColliderUnchanged(t *testing.T) {
	proxies := []profileProxy{
		{proxy: map[string]any{"name": "A"}, nodeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", name: "A"},
		{proxy: map[string]any{"name": "A"}, nodeHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", name: "A"},
		{proxy: map[string]any{"name": "C"}, nodeHash: "cccccccccccccccccccccccccccccccc", name: "C"},
	}
	if err := dedupProfileProxies(proxies); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proxies[2].proxy["name"].(string) != "C" {
		t.Fatalf("non-colliding proxy changed: %q", proxies[2].proxy["name"])
	}
}

func TestDedupProfileProxies_EightCharCollision(t *testing.T) {
	proxies := []profileProxy{
		{proxy: map[string]any{"name": "A"}, nodeHash: "aaaaaaaa123400000000000000000000", name: "A"},
		{proxy: map[string]any{"name": "A"}, nodeHash: "aaaaaaaa567800000000000000000000", name: "A"},
	}
	if err := dedupProfileProxies(proxies); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Sorted by hash: hash1 (aaaaaaaa1234) < hash2 (aaaaaaaa5678).
	// hash1 claims "#aaaaaaaa" (8 chars), hash2 tries same prefix → extends to 12 -> "#aaaaaaaa5678".
	if proxies[0].proxy["name"].(string) != "A #aaaaaaaa" {
		t.Fatalf("proxy[0] name = %q, want %q", proxies[0].proxy["name"], "A #aaaaaaaa")
	}
	if proxies[1].proxy["name"].(string) != "A #aaaaaaaa5678" {
		t.Fatalf("proxy[1] name = %q, want %q", proxies[1].proxy["name"], "A #aaaaaaaa5678")
	}
}

func TestDedupProfileProxies_MissingHashError(t *testing.T) {
	proxies := []profileProxy{
		{proxy: map[string]any{"name": "A"}, nodeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", name: "A"},
		{proxy: map[string]any{"name": "A"}, nodeHash: "", name: "A"},
	}
	gotErr := dedupProfileProxies(proxies)
	if gotErr == nil {
		t.Fatal("expected error for missing hash on colliding proxy")
	}
	if !strings.Contains(gotErr.Error(), "missing node hash") {
		t.Fatalf("error = %v, want 'missing node hash'", gotErr)
	}
}

func TestDedupProfileProxies_OrderReversal(t *testing.T) {
	hash1 := "00000000aaaaaaaaaaaaaaaaaaaaaaaa"
	hash2 := "00000000bbbbbbbbbbbbbbbbbbbbbbbb"

	proxiesA := []profileProxy{
		{proxy: map[string]any{"name": "A"}, nodeHash: hash1, name: "A"},
		{proxy: map[string]any{"name": "A"}, nodeHash: hash2, name: "A"},
	}
	proxiesB := []profileProxy{
		{proxy: map[string]any{"name": "A"}, nodeHash: hash2, name: "A"},
		{proxy: map[string]any{"name": "A"}, nodeHash: hash1, name: "A"},
	}

	if err := dedupProfileProxies(proxiesA); err != nil {
		t.Fatalf("A: %v", err)
	}
	if err := dedupProfileProxies(proxiesB); err != nil {
		t.Fatalf("B: %v", err)
	}

	// Sorted by hash: hash1 (00000000aaaa...) < hash2 (00000000bbbb...).
	// hash1 gets "#00000000" (8 chars), hash2 extends to 12 → "#00000000bbbb".
	// The suffix assignment per hash is deterministic regardless of input order.
	collectNames := func(pp []profileProxy) []string {
		out := make([]string, len(pp))
		for i, p := range pp {
			out[i] = p.proxy["name"].(string)
		}
		return out
	}
	namesA := collectNames(proxiesA)
	namesB := collectNames(proxiesB)

	// Both must contain the same two names.
	if namesA[0] == namesA[1] || namesB[0] == namesB[1] {
		t.Fatal("suffixes must differ")
	}
	has8 := false
	has12 := false
	for _, n := range namesA {
		if n == "A #00000000" {
			has8 = true
		}
		if n == "A #00000000bbbb" {
			has12 = true
		}
	}
	if !has8 || !has12 {
		t.Fatalf("A missing expected names: %v", namesA)
	}
	has8 = false
	has12 = false
	for _, n := range namesB {
		if n == "A #00000000" {
			has8 = true
		}
		if n == "A #00000000bbbb" {
			has12 = true
		}
	}
	if !has8 || !has12 {
		t.Fatalf("B missing expected names: %v", namesB)
	}
}

func TestDedupProfileProxies_MultipleGroups(t *testing.T) {
	proxies := []profileProxy{
		{proxy: map[string]any{"name": "A"}, nodeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", name: "A"},
		{proxy: map[string]any{"name": "A"}, nodeHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", name: "A"},
		{proxy: map[string]any{"name": "B"}, nodeHash: "cccccccccccccccccccccccccccccccc", name: "B"},
		{proxy: map[string]any{"name": "B"}, nodeHash: "dddddddddddddddddddddddddddddddd", name: "B"},
	}
	if err := dedupProfileProxies(proxies); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proxies[0].proxy["name"] != "A #aaaaaaaa" {
		t.Fatalf("A0 = %q", proxies[0].proxy["name"])
	}
	if proxies[2].proxy["name"] != "B #cccccccc" {
		t.Fatalf("B0 = %q", proxies[2].proxy["name"])
	}
	if proxies[3].proxy["name"] != "B #dddddddd" {
		t.Fatalf("B1 = %q", proxies[3].proxy["name"])
	}
}

func TestDedupProfileProxies_HashSuffixMatchesCountryRegex(t *testing.T) {
	proxies := []profileProxy{
		{proxy: map[string]any{"name": "[US] Node"}, nodeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", name: "[US] Node"},
		{proxy: map[string]any{"name": "[US] Node"}, nodeHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", name: "[US] Node"},
	}
	if err := dedupProfileProxies(proxies); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range proxies {
		name := p.proxy["name"].(string)
		if !strings.HasPrefix(name, "[US] Node") {
			t.Fatalf("proxy name lost prefix: %q", name)
		}
		if !strings.Contains(name, " #") {
			t.Fatalf("colliding proxy missing suffix: %q", name)
		}
	}
}

// TestDedupProfileProxies_NonColliderNameMatchesColliderCandidate verifies that
// when an existing non-collider already has a name matching the 8-char hash suffix
// that a collider would prefer, the collider extends to 12 chars. All names remain unique.
//
// Non-collider: proxy name and profile name both "A #aaaaaaaa" (solo, taken immediately)
// Collision group "A":
//   - hash "aaaaaaaa123400000000000000000000": preferred 8-char "A #aaaaaaaa" already taken
//     → extends to 12-char "A #aaaaaaaa1234"
//   - hash "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb": 8-char "A #bbbbbbbb" is free → claimed
func TestDedupProfileProxies_NonColliderNameMatchesColliderCandidate(t *testing.T) {
	// The non-collider's proxy name already contains the hash suffix that one
	// collider would prefer as 8-char. The non-collider's profile name (.name
	// field) is "A #aaaaaaaa" — it's the sole owner, so taken["A #aaaaaaaa"]=true.
	// When the collider with hash aaaa... tries "A #aaaaaaaa", it's taken,
	// forcing extension to 12 chars.
	proxies := []profileProxy{
		{proxy: map[string]any{"name": "A #aaaaaaaa"}, nodeHash: "11111111111111111111111111111111", name: "A #aaaaaaaa"},
		{proxy: map[string]any{"name": "A"}, nodeHash: "aaaaaaaa123400000000000000000000", name: "A"},
		{proxy: map[string]any{"name": "A"}, nodeHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", name: "A"},
	}
	if err := dedupProfileProxies(proxies); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-collider unchanged.
	if proxies[0].proxy["name"].(string) != "A #aaaaaaaa" {
		t.Fatalf("non-collider changed: %q", proxies[0].proxy["name"])
	}

	// Collision group sorted by hash: aaaa... < bbbb...
	// First collider (aaaa...) tries 8-char "A #aaaaaaaa" → taken by non-collider → 12-char "A #aaaaaaaa1234".
	gotFirst := proxies[1].proxy["name"].(string)
	if gotFirst != "A #aaaaaaaa1234" {
		t.Fatalf("first collider = %q, want %q", gotFirst, "A #aaaaaaaa1234")
	}
	// Second collider (bbbb...) claims 8-char "A #bbbbbbbb".
	gotSecond := proxies[2].proxy["name"].(string)
	if gotSecond != "A #bbbbbbbb" {
		t.Fatalf("second collider = %q, want %q", gotSecond, "A #bbbbbbbb")
	}

	// All three names must be unique.
	names := map[string]bool{}
	for _, p := range proxies {
		n := p.proxy["name"].(string)
		if names[n] {
			t.Fatalf("duplicate name: %q", n)
		}
		names[n] = true
	}
}

// ---------------------------------------------------------------------------
// buildClashProxies profile mode tests
// ---------------------------------------------------------------------------

func TestBuildClashProxiesProfile_ValidConversion(t *testing.T) {
	outbounds := []ExportOutbound{
		{
			Tag:      "sub-a/[US] tag",
			Type:     "ss",
			Raw:      json.RawMessage(`{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`),
			NodeHash: node.HashFromRawOptions([]byte(`{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`)).Hex(),
			Region:   "us",
			BaseTag:  "sub-a/tag",
		},
	}
	proxies, err := buildClashProxies(outbounds, clashNamingProfile)
	if err != nil {
		t.Fatalf("buildClashProxies: %v", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}
	name, _ := proxies[0]["name"].(string)
	if !strings.HasPrefix(name, "sub-a/[US] tag") {
		t.Fatalf("proxy name = %q, want prefix sub-a/[US] tag", name)
	}
	typ, _ := proxies[0]["type"].(string)
	if typ != "ss" {
		t.Fatalf("proxy type = %q, want ss", typ)
	}
}

func TestBuildClashProxiesProfile_UnknownRegion(t *testing.T) {
	outbounds := []ExportOutbound{
		{
			Tag:      "sub-a/[HK] node",
			Type:     "ss",
			Raw:      json.RawMessage(`{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`),
			NodeHash: node.HashFromRawOptions([]byte(`{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`)).Hex(),
			Region:   "xx",
			BaseTag:  "sub-a/[HK] node",
		},
	}
	proxies, err := buildClashProxies(outbounds, clashNamingProfile)
	if err != nil {
		t.Fatalf("buildClashProxies: %v", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}
	name, _ := proxies[0]["name"].(string)
	if !strings.HasPrefix(name, "sub-a/[??] node") {
		t.Fatalf("proxy name = %q, want prefix sub-a/[??] node", name)
	}
}

func TestBuildClashProxiesProfile_UnsupportedSkipped(t *testing.T) {
	outbounds := []ExportOutbound{
		{
			Tag:      "sub-a/ss",
			Type:     "ss",
			Raw:      json.RawMessage(`{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`),
			NodeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Region:   "us",
			BaseTag:  "sub-a/ss",
		},
		{
			Tag:      "sub-a/unsupported",
			Type:     "unsupported",
			Raw:      json.RawMessage(`{"type":"unsupported","server":"2.2.2.2","port":443}`),
			NodeHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Region:   "us",
			BaseTag:  "sub-a/unsupported",
		},
	}
	proxies, err := buildClashProxies(outbounds, clashNamingProfile)
	if err != nil {
		t.Fatalf("buildClashProxies: %v", err)
	}
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}
}

func TestBuildClashProxiesProfile_EmptyOutbounds(t *testing.T) {
	proxies, err := buildClashProxies(nil, clashNamingProfile)
	if err != nil {
		t.Fatalf("buildClashProxies: %v", err)
	}
	if len(proxies) != 0 {
		t.Fatalf("expected 0 proxies, got %d", len(proxies))
	}
}

func TestBuildClashProxiesProfile_Dedup(t *testing.T) {
	raw := `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	// Same BaseTag and region → same profile name → collision.
	outbounds := []ExportOutbound{
		{
			Tag:      "sub-a/[US] node",
			Type:     "ss",
			Raw:      json.RawMessage(raw),
			NodeHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Region:   "us",
			BaseTag:  "sub-a/node",
		},
		{
			Tag:      "sub-b/[US] node",
			Type:     "ss",
			Raw:      json.RawMessage(raw),
			NodeHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Region:   "us",
			BaseTag:  "sub-a/node",
		},
	}
	proxies, err := buildClashProxies(outbounds, clashNamingProfile)
	if err != nil {
		t.Fatalf("buildClashProxies: %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(proxies))
	}
	for _, p := range proxies {
		name, _ := p["name"].(string)
		if !strings.Contains(name, " #") {
			t.Fatalf("colliding proxy missing suffix: %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// injectClashProxies pure tests — yaml.Node-level injection verification
// ---------------------------------------------------------------------------

func TestInjectClashProxies_PreservesTemplateKeys(t *testing.T) {
	// A rich Clash template with all semantic keys that must be preserved.
	const templateYAML = `
mode: rule
dns:
  enable: true
  listen: 0.0.0.0:53
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8
  nameserver:
    - https://dns.cloudflare.com/dns-query
tun:
  enable: true
  stack: system
  dns-hijack:
    - any:53
sniffer:
  enable: true
  force-dns-mapping: true
  parse-pure-ip: true
  skip-domain:
    - +.google.com
rule-providers:
  google:
    type: http
    behavior: domain
    url: https://example.com/google.yaml
    interval: 86400
proxy-groups:
  - name: PROXY
    type: select
    include-all-proxies: true
rules:
  - DOMAIN,google.com,PROXY
  - MATCH,PROXY
`

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(templateYAML), &doc); err != nil {
		t.Fatalf("unmarshal template: %v", err)
	}

	proxies := []map[string]any{
		{"name": "sub-a/[US] server1", "type": "ss", "server": "1.1.1.1", "port": 443},
	}
	injectClashProxies(&doc, proxies)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Parse back as generic map to check key preservation.
	var result map[string]any
	if err := yaml.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal result: %v\nyaml=%s", err, string(out))
	}

	// Assert every template key survives and has correct type/value.
	if result["mode"] != "rule" {
		t.Fatalf("mode = %v, want 'rule'", result["mode"])
	}

	dns, ok := result["dns"].(map[string]any)
	if !ok {
		t.Fatal("dns missing or not a mapping")
	}
	if dns["enable"] != true {
		t.Fatalf("dns.enable = %v", dns["enable"])
	}

	tun, ok := result["tun"].(map[string]any)
	if !ok {
		t.Fatal("tun missing or not a mapping")
	}
	if tun["enable"] != true {
		t.Fatalf("tun.enable = %v", tun["enable"])
	}

	sniffer, ok := result["sniffer"].(map[string]any)
	if !ok {
		t.Fatal("sniffer missing or not a mapping")
	}
	if sniffer["enable"] != true {
		t.Fatalf("sniffer.enable = %v", sniffer["enable"])
	}

	providers, ok := result["rule-providers"].(map[string]any)
	if !ok {
		t.Fatal("rule-providers missing or not a mapping")
	}
	googleProv, ok := providers["google"].(map[string]any)
	if !ok {
		t.Fatal("rule-providers.google missing or not a mapping")
	}
	if googleProv["type"] != "http" {
		t.Fatalf("rule-providers.google.type = %v", googleProv["type"])
	}

	groups, ok := result["proxy-groups"].([]any)
	if !ok {
		t.Fatal("proxy-groups missing or not a sequence")
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 proxy-group, got %d", len(groups))
	}

	rules, ok := result["rules"].([]any)
	if !ok {
		t.Fatal("rules missing or not a sequence")
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	// Assert proxies are injected.
	proxiesResult, ok := result["proxies"].([]any)
	if !ok {
		t.Fatal("proxies missing or not a sequence")
	}
	if len(proxiesResult) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxiesResult))
	}
	proxyName, _ := proxiesResult[0].(map[string]any)["name"].(string)
	if proxyName != "sub-a/[US] server1" {
		t.Fatalf("proxy name = %q", proxyName)
	}
}

// TestInjectClashProxies_PreservesAnchor verifies that when the template's
// "proxies" value carries a YAML anchor (&name), the anchor is preserved on
// the injected sequence node so that alias references (*name) elsewhere in
// the document still resolve correctly.
func TestInjectClashProxies_PreservesAnchor(t *testing.T) {
	const templateYAML = `
proxies: &dynamic []

probe-copy: *dynamic

rules:
  - MATCH,PROXY
`

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(templateYAML), &doc); err != nil {
		t.Fatalf("unmarshal template: %v", err)
	}

	proxies := []map[string]any{
		{"name": "node1", "type": "ss", "server": "1.1.1.1", "port": 443},
		{"name": "node2", "type": "ss", "server": "2.2.2.2", "port": 443},
	}
	injectClashProxies(&doc, proxies)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	output := string(out)

	// The output must contain the anchor definition and the alias.
	if !strings.Contains(output, "&dynamic") {
		t.Fatal("output must contain &dynamic anchor")
	}
	if !strings.Contains(output, "*dynamic") {
		t.Fatal("output must contain *dynamic alias")
	}

	// Re-parse to confirm the alias has been resolved and the proxy entries
	// are reachable through both paths.
	var resultMap map[string]any
	if err := yaml.Unmarshal(out, &resultMap); err != nil {
		t.Fatalf("unmarshal result map: %v\nyaml=%s", err, output)
	}

	// Top-level proxies must have the injected content.
	proxiesResult, ok := resultMap["proxies"].([]any)
	if !ok {
		t.Fatal("proxies missing or not a sequence after injection")
	}
	if len(proxiesResult) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(proxiesResult))
	}

	// probe-copy (aliased to the same node) must have the same content.
	probeCopy, ok := resultMap["probe-copy"].([]any)
	if !ok {
		t.Fatal("probe-copy missing or not a sequence after injection — alias broken")
	}
	if len(probeCopy) != 2 {
		t.Fatalf("expected probe-copy to have 2 entries (alias match), got %d", len(probeCopy))
	}

	// Both paths must resolve to the same underlying entries.
	for i := 0; i < 2; i++ {
		p1 := proxiesResult[i].(map[string]any)
		p2 := probeCopy[i].(map[string]any)
		if p1["name"] != p2["name"] {
			t.Fatalf("proxy[%d] name mismatch: %q vs %q", i, p1["name"], p2["name"])
		}
	}

	// Additionally verify that marshal+unmarshal round-trips without error
	// by walking the node tree — an unresolved alias would cause a
	// yaml.Marshal error or silently produce incomplete output.
	var verifyNode yaml.Node
	if err := yaml.Unmarshal(out, &verifyNode); err != nil {
		t.Fatalf("second unmarshal failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// writeClashWithProfile integration tests
// ---------------------------------------------------------------------------

func TestNodePoolExport_RuleProfile_Success(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	// Create a rule profile via admin API.
	const templateYAML = `proxy-groups:
  - name: PROXY
    type: select
    include-all-proxies: true
rules:
  - MATCH,PROXY
`
	profile := mustCreateRuleProfile(t, srv, "export-test", templateYAML)

	// Create an export token.
	tokenValue := mustCreateExportToken(t, srv)

	// Add a node with US region.
	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)
	const rawSS = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20-ietf-poly1305","password":"testpass"}`
	addNodeForNodeListTest(t, cp, sub, rawSS, "203.0.113.10")
	markNodeHealthyForNodeListTest(t, cp, rawSS)

	// Set explicit egress region to US.
	hash := node.HashFromRawOptions([]byte(rawSS))
	entry, ok := cp.Pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node not found")
	}
	entry.SetEgressRegion("us")

	// Export with rule_profile_id.
	url := "/api/v1/node-pool/export?format=clash&export_token=" + tokenValue + "&rule_profile_id=" + profile["id"].(string)
	resp := doJSONRequest(t, srv, http.MethodGet, url, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.Code, resp.Body.String())
	}

	ct := resp.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/yaml") {
		t.Fatalf("content-type: %q, want text/yaml", ct)
	}

	var doc struct {
		ProxyGroups []map[string]any `yaml:"proxy-groups"`
		Rules       []string         `yaml:"rules"`
		Proxies     []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(resp.Body.Bytes(), &doc); err != nil {
		t.Fatalf("yaml unmarshal: %v body=%q", err, resp.Body.String())
	}

	if len(doc.ProxyGroups) != 1 {
		t.Fatalf("expected 1 proxy-group, got %d", len(doc.ProxyGroups))
	}
	if len(doc.Rules) != 1 || doc.Rules[0] != "MATCH,PROXY" {
		t.Fatalf("rules: got %v, want [MATCH,PROXY]", doc.Rules)
	}
	if len(doc.Proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(doc.Proxies))
	}
	name, _ := doc.Proxies[0]["name"].(string)
	if !strings.HasPrefix(name, "sub-a/[US]") {
		t.Fatalf("proxy name = %q, want sub-a/[US] prefix", name)
	}
}

func TestNodePoolExport_RuleProfile_TemplatePreservation(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	// A rich template with all semantic keys.
	const templateYAML = `
mode: rule
dns:
  enable: true
  listen: 0.0.0.0:53
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8
tun:
  enable: true
  stack: system
  dns-hijack:
    - any:53
sniffer:
  enable: true
  force-dns-mapping: true
  skip-domain:
    - +.google.com
rule-providers:
  google:
    type: http
    behavior: domain
    url: https://example.com/google.yaml
    interval: 86400
proxy-groups:
  - name: PROXY
    type: select
    include-all-proxies: true
rules:
  - DOMAIN,google.com,PROXY
  - MATCH,PROXY
`
	profile := mustCreateRuleProfile(t, srv, "preservation-test", templateYAML)
	tokenValue := mustCreateExportToken(t, srv)

	// Add one node.
	sub := subscription.NewSubscription("22222222-2222-2222-2222-222222222222", "sub-a", "https://example.com/b", true, false)
	cp.SubMgr.Register(sub)
	const rawSS = `{"type":"ss","server":"2.2.2.2","port":8443,"method":"chacha20-ietf-poly1305","password":"pass123"}`
	addNodeForNodeListTest(t, cp, sub, rawSS, "203.0.113.20")
	markNodeHealthyForNodeListTest(t, cp, rawSS)
	hash := node.HashFromRawOptions([]byte(rawSS))
	if entry, ok := cp.Pool.GetEntry(hash); ok {
		entry.SetEgressRegion("us")
	}

	url := "/api/v1/node-pool/export?format=clash&export_token=" + tokenValue + "&rule_profile_id=" + profile["id"].(string)
	resp := doJSONRequest(t, srv, http.MethodGet, url, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	if err := yaml.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v body=%q", err, resp.Body.String())
	}

	// Non-proxies keys must survive.
	if result["mode"] != "rule" {
		t.Fatalf("mode = %v", result["mode"])
	}
	if _, ok := result["dns"].(map[string]any); !ok {
		t.Fatal("dns missing or not a mapping")
	}
	if _, ok := result["tun"].(map[string]any); !ok {
		t.Fatal("tun missing or not a mapping")
	}
	if _, ok := result["sniffer"].(map[string]any); !ok {
		t.Fatal("sniffer missing or not a mapping")
	}
	if _, ok := result["rule-providers"].(map[string]any); !ok {
		t.Fatal("rule-providers missing or not a mapping")
	}
	if groups, ok := result["proxy-groups"].([]any); !ok || len(groups) == 0 {
		t.Fatal("proxy-groups missing or empty")
	}
	if rules, ok := result["rules"].([]any); !ok || len(rules) == 0 {
		t.Fatal("rules missing or empty")
	}

	// Proxies must be injected.
	if proxies, ok := result["proxies"].([]any); !ok || len(proxies) != 1 {
		t.Fatalf("proxies: expected 1, got %v", proxies)
	}
}

func TestNodePoolExport_RuleProfile_InvalidUUID(t *testing.T) {
	srv, _, tokenValue := setupExportTest(t)
	url := "/api/v1/node-pool/export?format=clash&export_token=" + tokenValue + "&rule_profile_id=not-a-uuid"
	resp := doJSONRequest(t, srv, http.MethodGet, url, nil, false)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestNodePoolExport_RuleProfile_NonClashFormat(t *testing.T) {
	srv, _, tokenValue := setupExportTest(t)
	url := "/api/v1/node-pool/export?format=sing-box&export_token=" + tokenValue + "&rule_profile_id=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeffff"
	resp := doJSONRequest(t, srv, http.MethodGet, url, nil, false)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	assertErrorCode(t, resp, "INVALID_ARGUMENT")
}

func TestNodePoolExport_RuleProfile_Missing(t *testing.T) {
	srv, _, tokenValue := setupExportTest(t)
	url := "/api/v1/node-pool/export?format=clash&export_token=" + tokenValue + "&rule_profile_id=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeffff"
	resp := doJSONRequest(t, srv, http.MethodGet, url, nil, false)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	assertErrorCode(t, resp, "RULE_PROFILE_UNAVAILABLE")
	// Error body must NOT leak template content or any hash.
	body := resp.Body.String()
	if strings.Contains(body, "proxy-groups") || strings.Contains(body, "aaaaaaaa") {
		t.Fatalf("error body must not contain template content or hash, got: %s", body)
	}
}

func TestNodePoolExport_RuleProfile_Disabled(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          "disabled",
		"template_yaml": "rules:\n  - MATCH,Proxy\n",
		"enabled":       false,
	}, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create disabled profile: status=%d body=%s", rec.Code, rec.Body.String())
	}
	profile := decodeJSONMap(t, rec)
	tokenValue := mustCreateExportToken(t, srv)

	url := "/api/v1/node-pool/export?format=clash&export_token=" + tokenValue + "&rule_profile_id=" + profile["id"].(string)
	resp := doJSONRequest(t, srv, http.MethodGet, url, nil, false)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	assertErrorCode(t, resp, "RULE_PROFILE_UNAVAILABLE")
	// Error body must NOT leak template content.
	body := resp.Body.String()
	if strings.Contains(body, "MATCH") {
		t.Fatalf("error body must not contain template content, got: %s", body)
	}
}

func TestNodePoolExport_RuleProfile_DisabledEqualsMissing(t *testing.T) {
	srv, _, tokenValue := setupExportTest(t)
	missingURL := "/api/v1/node-pool/export?format=clash&export_token=" + tokenValue + "&rule_profile_id=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeffff"
	missingResp := doJSONRequest(t, srv, http.MethodGet, missingURL, nil, false)

	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          "disabled",
		"template_yaml": "rules:\n  - MATCH,Proxy\n",
		"enabled":       false,
	}, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create disabled: status=%d body=%s", rec.Code, rec.Body.String())
	}
	profile := decodeJSONMap(t, rec)
	disabledURL := "/api/v1/node-pool/export?format=clash&export_token=" + tokenValue + "&rule_profile_id=" + profile["id"].(string)
	disabledResp := doJSONRequest(t, srv, http.MethodGet, disabledURL, nil, false)

	if missingResp.Code != disabledResp.Code || missingResp.Body.String() != disabledResp.Body.String() {
		t.Fatal("disabled profile should return identical error to nonexistent profile")
	}
}

func TestNodePoolExport_RuleProfile_EmptyProxies(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	const templateYAML = `proxy-groups:
  - name: PROXY
    type: select
    include-all-proxies: true
rules:
  - MATCH,PROXY
`
	profile := mustCreateRuleProfile(t, srv, "empty-test", templateYAML)
	tokenValue := mustCreateExportToken(t, srv)

	// No nodes added — empty pool.
	url := "/api/v1/node-pool/export?format=clash&export_token=" + tokenValue + "&rule_profile_id=" + profile["id"].(string)
	resp := doJSONRequest(t, srv, http.MethodGet, url, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.Code, resp.Body.String())
	}

	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
		Rules   []string         `yaml:"rules"`
	}
	if err := yaml.Unmarshal(resp.Body.Bytes(), &doc); err != nil {
		t.Fatalf("yaml unmarshal: %v body=%q", err, resp.Body.String())
	}
	if len(doc.Proxies) != 0 {
		t.Fatalf("expected empty proxies, got %d", len(doc.Proxies))
	}
	if len(doc.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(doc.Rules))
	}
}

func TestNodePoolExport_NoProfile_ExistingBehavior(t *testing.T) {
	srv, _, tokenValue := setupExportTest(t)

	resp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	ct := resp.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/yaml") {
		t.Fatalf("content-type: %q, want text/yaml", ct)
	}
	var doc struct {
		Proxies     []map[string]any `yaml:"proxies"`
		ProxyGroups []map[string]any `yaml:"proxy-groups"`
		Rules       []string         `yaml:"rules"`
	}
	if err := yaml.Unmarshal(resp.Body.Bytes(), &doc); err != nil {
		t.Fatalf("yaml unmarshal: %v body=%q", err, resp.Body.String())
	}
	if len(doc.Proxies) == 0 {
		t.Fatal("expected non-empty proxies")
	}
	if len(doc.ProxyGroups) != 0 {
		t.Fatal("no profile: should not contain proxy-groups")
	}
	if len(doc.Rules) != 0 {
		t.Fatal("no profile: should not contain rules")
	}
}

// ---------------------------------------------------------------------------
// Helper: mustCreateExportToken
// ---------------------------------------------------------------------------

func mustCreateExportToken(t *testing.T, srv *Server) string {
	t.Helper()
	resp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "test-export",
	}, true)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create export token: status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := decodeJSONMap(t, resp)
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatal("export token is empty")
	}
	return token
}
