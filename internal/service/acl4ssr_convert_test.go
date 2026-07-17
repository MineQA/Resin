package service

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Error boundary tests
// ---------------------------------------------------------------------------

func TestConvertACL4SSR_InputOverMaxSize(t *testing.T) {
	input := strings.Repeat("x", 512*1024+1)
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "exceeds maximum allowed size") {
		t.Fatalf("expected error about max size exceeded, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_InputAtMaxSize(t *testing.T) {
	// Confirm the guard uses > not >= by constructing input exactly at limit.
	minimal := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	if len(minimal) > 512*1024 {
		t.Fatal("minimal config exceeds 512 KiB")
	}
	padLen := 512*1024 - len(minimal)
	pad := strings.Repeat("#", padLen)
	input := minimal + pad
	if len(input) != 512*1024 {
		t.Fatalf("expected input length %d, got %d", 512*1024, len(input))
	}
	_, svcErr := ConvertACL4SSRCustomINI(input)
	// Must NOT be rejected for size. May succeed or fail at a later validation.
	if svcErr != nil && strings.Contains(svcErr.Message, "exceeds maximum allowed size") {
		t.Fatal("input at exactly 512 KiB must not be rejected for exceeding max size")
	}
}

func TestConvertACL4SSR_MissingCustomSection(t *testing.T) {
	_, svcErr := ConvertACL4SSRCustomINI("some random text\nwithout section\n")
	if svcErr == nil || !strings.Contains(svcErr.Message, "missing required [custom]") {
		t.Fatalf("expected error about missing [custom], got: %v", svcErr)
	}
}

func TestConvertACL4SSR_EmptyInput(t *testing.T) {
	_, svcErr := ConvertACL4SSRCustomINI("")
	if svcErr == nil || !strings.Contains(svcErr.Message, "missing required [custom]") {
		t.Fatalf("expected error about missing [custom], got: %v", svcErr)
	}
}

func TestConvertACL4SSR_OnlyCustomSection(t *testing.T) {
	_, svcErr := ConvertACL4SSRCustomINI("[custom]\n")
	if svcErr == nil || !strings.Contains(svcErr.Message, "at least one custom_proxy_group") {
		t.Fatalf("expected error about missing groups, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_NoGroups(t *testing.T) {
	input := "[custom]\nruleset=Test,https://example.com/r.list\nruleset=World,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "at least one custom_proxy_group") {
		t.Fatalf("expected error about missing groups, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_NoRulesets(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "at least one active ruleset") {
		t.Fatalf("expected error about missing rulesets, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_MalformedGroupTooFewFields(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=bad\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "expected at least 3") {
		t.Fatalf("expected error about too few fields, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_EmptyGroupName(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=`select`.*\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "empty group name") {
		t.Fatalf("expected error about empty name, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_UnsupportedGroupType(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`load-balance`.*\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "unsupported group type") {
		t.Fatalf("expected error about unsupported type, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_DuplicateGroupNames(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Same`select`.*\ncustom_proxy_group=Same`select`.*\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "duplicate group name") {
		t.Fatalf("expected error about duplicate name, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_MissingFinal(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,https://example.com/r.list\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "missing required []FINAL") {
		t.Fatalf("expected error about missing FINAL, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_FinalNotTerminal(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=First,[]FINAL\nruleset=Second,https://example.com/r.list\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "must be the last active") {
		t.Fatalf("expected error about FINAL not terminal, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_MultipleFinal(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,https://example.com/r.list\nruleset=First,[]FINAL\nruleset=Second,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "multiple []FINAL") {
		t.Fatalf("expected error about multiple FINAL, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_NonHTTPSRuleset(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,http://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "non-HTTPS") {
		t.Fatalf("expected error about non-HTTPS, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_RulesetURLUserinfo(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,https://user@example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "userinfo") {
		t.Fatalf("expected error about userinfo, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_RulesetURLFragment(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,https://example.com/r.list#bad\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "fragment") {
		t.Fatalf("expected error about fragment, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_EmptyRulesetLabel(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "empty ruleset label") {
		t.Fatalf("expected error about empty label, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_EmptyGeoIPCountry(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,[]GEOIP,\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "empty GEOIP country") {
		t.Fatalf("expected error about empty GEOIP country, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_SelectGroupNoUsableMembers(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Emptier`select`\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "no usable selectors") {
		t.Fatalf("expected error about no usable selectors, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_GeoIPExtraCommas(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,[]GEOIP,CN,extra\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "extra fields after country code") {
		t.Fatalf("expected error about extra GEOIP fields, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_InvalidURLTestParam(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`url-test`.*`http://example.com/test`notanumber\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "not a number") {
		t.Fatalf("expected error about invalid number, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_URLTestMissingURL(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`url-test`.*\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "url-test requires at least selector and URL") {
		t.Fatalf("expected error about missing URL, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_URLTestEmptyURL(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`url-test`.*``\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "non-empty URL") {
		t.Fatalf("expected error about empty URL, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_URLTestMissingInterval(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`url-test`.*`http://example.com/test`\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "url-test requires interval") {
		t.Fatalf("expected error about missing interval, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_MultipleRegexSelectors(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`(foo)`(bar)\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr == nil || !strings.Contains(svcErr.Message, "multiple distinct bare regex") {
		t.Fatalf("expected error about multiple regexes, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_CommentStyleHash(t *testing.T) {
	// # comments should be ignored.
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,https://example.com/r.list\n# this is a comment\nruleset=Final,[]FINAL\n"
	result, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr != nil {
		t.Fatalf("unexpected error: %v", svcErr)
	}
	if result.GroupCount != 1 {
		t.Errorf("expected 1 group, got %d", result.GroupCount)
	}
}

func TestConvertACL4SSR_UnknownDirectiveWarning(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,https://example.com/r.list\nunknown_key=whatever\nruleset=Final,[]FINAL\n"
	result, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr != nil {
		t.Fatalf("unexpected error: %v", svcErr)
	}
	hasWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "unknown directive") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("expected warning about unknown directive")
	}
}

func TestConvertACL4SSR_UnknownSectionWarning(t *testing.T) {
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Test,https://example.com/r.list\nruleset=Final,[]FINAL\n[unknown_section]\nsomething=else\n"
	result, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr != nil {
		t.Fatalf("unexpected error: %v", svcErr)
	}
	hasWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "unknown directive") || strings.Contains(w, "unknown section") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("expected warning about unknown section")
	}
}

// ---------------------------------------------------------------------------
// Full fixture: ACL4SSR_Online_Full.ini golden test
// ---------------------------------------------------------------------------

func TestConvertACL4SSR_FullFixtureGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.ini")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	result, svcErr := ConvertACL4SSRCustomINI(string(data))
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}

	// Structural assertions
	if result.GroupCount != 29 {
		t.Errorf("expected 29 groups, got %d", result.GroupCount)
	}
	if result.ProviderCount != 31 {
		t.Errorf("expected 31 providers, got %d", result.ProviderCount)
	}
	if result.RuleCount != 33 {
		t.Errorf("expected 33 rules, got %d", result.RuleCount)
	}

	// Generated YAML must pass validation.
	if err := ValidateRuleProfileTemplate(result.TemplateYAML); err != nil {
		t.Fatalf("generated template failed validation: %v", err)
	}

	// Byte-for-byte golden match.
	goldenData, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.golden.yaml")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if result.TemplateYAML != string(goldenData) {
		t.Fatal("generated YAML does not match golden file byte-for-byte")
	}

	// Structural YAML decode assertions.
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(result.TemplateYAML), &doc); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}

	// proxies must be empty sequence.
	proxies, ok := doc["proxies"].([]any)
	if !ok {
		t.Fatal("proxies is not a sequence")
	}
	if len(proxies) != 0 {
		t.Errorf("expected empty proxies, got %d entries", len(proxies))
	}

	// proxy-groups, rule-providers, rules top-level keys present.
	if _, ok := doc["proxy-groups"]; !ok {
		t.Error("missing proxy-groups key")
	}
	if _, ok := doc["rule-providers"]; !ok {
		t.Error("missing rule-providers key")
	}
	if _, ok := doc["rules"]; !ok {
		t.Error("missing rules key")
	}
}

func TestConvertACL4SSR_FullFixtureWarnings(t *testing.T) {
	data, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.ini")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	result, svcErr := ConvertACL4SSRCustomINI(string(data))
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}

	if len(result.Warnings) == 0 {
		t.Fatal("expected warnings for semantic regex selectors")
	}

	hasNetEaseWarn := false
	hasNetflixWarn := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "网易音乐") && strings.Contains(w, "semantic selector") {
			hasNetEaseWarn = true
		}
		if strings.Contains(w, "奈飞节点") && strings.Contains(w, "semantic selector") {
			hasNetflixWarn = true
		}
	}
	if !hasNetEaseWarn {
		t.Error("expected warning for NetEase semantic regex")
	}
	if !hasNetflixWarn {
		t.Error("expected warning for Netflix semantic regex")
	}
}

func TestConvertACL4SSR_FullFixtureRegionRewrites(t *testing.T) {
	data, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.ini")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	result, svcErr := ConvertACL4SSRCustomINI(string(data))
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(result.TemplateYAML), &doc); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}
	groups, ok := doc["proxy-groups"].([]any)
	if !ok {
		t.Fatal("proxy-groups is not a sequence")
	}

	// Collect all groups that have a filter, grouped by region.
	type ccCount struct {
		CC       string
		Suffix   string
		Expected string
	}
	expectedRegions := []ccCount{
		{CC: "HK", Suffix: "香港节点", Expected: "(?:^|/)\\[HK\\](?: [^/]*|)$"},
		{CC: "JP", Suffix: "日本节点", Expected: "(?:^|/)\\[JP\\](?: [^/]*|)$"},
		{CC: "US", Suffix: "美国节点", Expected: "(?:^|/)\\[US\\](?: [^/]*|)$"},
		{CC: "TW", Suffix: "台湾节点", Expected: "(?:^|/)\\[TW\\](?: [^/]*|)$"},
		{CC: "SG", Suffix: "狮城节点", Expected: "(?:^|/)\\[SG\\](?: [^/]*|)$"},
		{CC: "KR", Suffix: "韩国节点", Expected: "(?:^|/)\\[KR\\](?: [^/]*|)$"},
	}

	for _, rg := range expectedRegions {
		found := false
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			name, _ := gm["name"].(string)
			if !strings.HasSuffix(name, rg.Suffix) {
				continue
			}
			found = true
			filter, _ := gm["filter"].(string)
			if filter != rg.Expected {
				t.Errorf("group %q (%s): expected filter %q, got %q", name, rg.CC, rg.Expected, filter)
			}
			// Should also have include-all-proxies and url-test type.
			includeAll, _ := gm["include-all-proxies"].(bool)
			if !includeAll {
				t.Errorf("group %q: expected include-all-proxies: true", name)
			}
			typeStr, _ := gm["type"].(string)
			if typeStr != "url-test" {
				t.Errorf("group %q: expected type url-test, got %q", name, typeStr)
			}
			break
		}
		if !found {
			t.Errorf("no group found with name suffix %q", rg.Suffix)
		}
	}
}

func TestConvertACL4SSR_FullFixtureIncludeAllGroups(t *testing.T) {
	data, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.ini")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	result, svcErr := ConvertACL4SSRCustomINI(string(data))
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(result.TemplateYAML), &doc); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}
	groups, ok := doc["proxy-groups"].([]any)
	if !ok {
		t.Fatal("proxy-groups is not a sequence")
	}

	// Find auto-select group (url-test with .*).
	manualFound := false
	autoFound := false
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		includeAll, _ := gm["include-all-proxies"].(bool)
		_, hasFilter := gm["filter"]
		typeStr, _ := gm["type"].(string)

		if name == "🚀 手动切换" {
			manualFound = true
			if !includeAll {
				t.Error("🚀 手动切换 should have include-all-proxies: true")
			}
			if hasFilter {
				t.Error("🚀 手动切换 should not have a filter")
			}
			if typeStr != "select" {
				t.Errorf("🚀 手动切换 expected type select, got %q", typeStr)
			}
		}

		if strings.HasPrefix(name, "♻️ 自动选择") {
			autoFound = true
			if !includeAll {
				t.Error("♻️ 自动选择 should have include-all-proxies: true")
			}
			if hasFilter {
				t.Error("♻️ 自动选择 should not have a filter")
			}
			if typeStr != "url-test" {
				t.Errorf("♻️ 自动选择 expected type url-test, got %q", typeStr)
			}
			url, _ := gm["url"].(string)
			if url == "" {
				t.Error("♻️ 自动选择 should have a url")
			}
			interval, _ := gm["interval"].(int)
			if interval == 0 {
				t.Error("♻️ 自动选择 should have a non-zero interval")
			}
		}
	}
	if !manualFound {
		t.Error("🚀 手动切换 group not found")
	}
	if !autoFound {
		t.Error("♻️ 自动选择 group not found")
	}
}

func TestConvertACL4SSR_FullFixtureRulesOrder(t *testing.T) {
	data, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.ini")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	result, svcErr := ConvertACL4SSRCustomINI(string(data))
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(result.TemplateYAML), &doc); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}
	rules, ok := doc["rules"].([]any)
	if !ok {
		t.Fatal("rules is not a sequence")
	}
	if len(rules) != 33 {
		t.Fatalf("expected 33 rules, got %d", len(rules))
	}

	// Rule at index 31 (penultimate) must start with "GEOIP,CN,"
	geoipRule, ok := rules[31].(string)
	if !ok {
		t.Fatal("rule at index 31 is not a string")
	}
	if !strings.HasPrefix(geoipRule, "GEOIP,CN,") {
		t.Errorf("expected GEOIP,CN at index 31, got %q", geoipRule)
	}
	if !strings.HasSuffix(geoipRule, ",no-resolve") {
		t.Errorf("expected GEOIP rule to end with ,no-resolve, got %q", geoipRule)
	}

	// Rule at index 32 must start with "MATCH,"
	matchRule, ok := rules[32].(string)
	if !ok {
		t.Fatal("rule at index 32 is not a string")
	}
	if !strings.HasPrefix(matchRule, "MATCH,") {
		t.Errorf("expected MATCH at index 32, got %q", matchRule)
	}
}

func TestConvertACL4SSR_FullFixtureProviderFields(t *testing.T) {
	data, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.ini")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	result, svcErr := ConvertACL4SSRCustomINI(string(data))
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(result.TemplateYAML), &doc); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}
	providers, ok := doc["rule-providers"].(map[string]any)
	if !ok {
		t.Fatal("rule-providers is not a mapping")
	}
	if len(providers) != 31 {
		t.Errorf("expected 31 providers, got %d", len(providers))
	}

	// Verify all providers have the expected fields.
	for name, v := range providers {
		pm, ok := v.(map[string]any)
		if !ok {
			t.Errorf("provider %q value is not a mapping", name)
			continue
		}
		if typeStr, _ := pm["type"].(string); typeStr != "http" {
			t.Errorf("provider %q: expected type http, got %q", name, typeStr)
		}
		if behavior, _ := pm["behavior"].(string); behavior != "classical" {
			t.Errorf("provider %q: expected behavior classical, got %q", name, behavior)
		}
		if format, _ := pm["format"].(string); format != "text" {
			t.Errorf("provider %q: expected format text, got %q", name, format)
		}
		if interval, _ := pm["interval"].(int); interval != 86400 {
			t.Errorf("provider %q: expected interval 86400, got %d", name, interval)
		}
		url, _ := pm["url"].(string)
		if !strings.HasPrefix(url, "https://") {
			t.Errorf("provider %q: expected HTTPS URL, got %q", name, url)
		}
	}
}

func TestConvertACL4SSR_DeterministicRepeat(t *testing.T) {
	data, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.ini")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	input := string(data)
	result1, svcErr1 := ConvertACL4SSRCustomINI(input)
	if svcErr1 != nil {
		t.Fatalf("first convert: %v", svcErr1)
	}
	result2, svcErr2 := ConvertACL4SSRCustomINI(input)
	if svcErr2 != nil {
		t.Fatalf("second convert: %v", svcErr2)
	}
	if result1.TemplateYAML != result2.TemplateYAML {
		t.Fatal("determinism: two conversions of identical input produced different output")
	}
}

func TestConvertACL4SSR_BasenameCollision(t *testing.T) {
	// Two URLs with same basename should get -2 suffix.
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=A,https://example.com/foo.list\nruleset=B,https://other.example/foo.list\nruleset=Final,[]FINAL\n"
	result, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}
	if !strings.Contains(result.TemplateYAML, "foo-2") {
		t.Error("expected basename collision to produce 'foo-2' key")
	}
	if result.ProviderCount != 2 {
		t.Errorf("expected 2 providers, got %d", result.ProviderCount)
	}
}

func TestConvertACL4SSR_SameURLDifferentRules(t *testing.T) {
	// Same URL appearing twice should create two providers with different keys.
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=First,https://example.com/rules.list\nruleset=Second,https://example.com/rules.list\nruleset=Final,[]FINAL\n"
	result, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}
	if result.ProviderCount != 2 {
		t.Errorf("expected 2 providers for duplicate URL, got %d", result.ProviderCount)
	}
	if !strings.Contains(result.TemplateYAML, "rules-2") {
		t.Error("expected second provider key to be 'rules-2'")
	}
}

func TestConvertACL4SSR_CarriageReturnInInput(t *testing.T) {
	// CRLF line endings should be handled.
	input := "[custom]\r\ncustom_proxy_group=Test`select`.*\r\nruleset=Test,https://example.com/r.list\r\nruleset=Final,[]FINAL\r\n"
	_, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr != nil {
		t.Fatalf("CRLF input should work, got: %v", svcErr)
	}
}

func TestConvertACL4SSR_InlineGEOIPCN(t *testing.T) {
	// GEOIP,CN should be emitted with no-resolve.
	input := "[custom]\ncustom_proxy_group=Test`select`.*\nruleset=Proxy,https://example.com/r.list\nruleset=Direct,[]GEOIP,CN\nruleset=Final,[]FINAL\n"
	result, svcErr := ConvertACL4SSRCustomINI(input)
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}
	if !strings.Contains(result.TemplateYAML, "GEOIP,CN,Direct,no-resolve") {
		t.Error("expected GEOIP,CN,Direct,no-resolve in rules")
	}
}

func TestConvertACL4SSR_SemanticRegexPreservedNetEase(t *testing.T) {
	data, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.ini")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, svcErr := ConvertACL4SSRCustomINI(string(data))
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(result.TemplateYAML), &doc); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}
	groups, ok := doc["proxy-groups"].([]any)
	if !ok {
		t.Fatal("proxy-groups is not a sequence")
	}

	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		if !strings.Contains(name, "网易") && !strings.Contains(name, "NetEase") && !strings.Contains(name, "Music") {
			continue
		}
		filter, _ := gm["filter"].(string)
		if filter != "(网易|音乐|解锁|Music|NetEase)" {
			t.Errorf("group %q: expected NetEase regex, got %q", name, filter)
		}
		return
	}
	t.Error("no NetEase group found in output")
}

func TestConvertACL4SSR_SemanticRegexPreservedNetflix(t *testing.T) {
	data, err := os.ReadFile("testdata/acl4ssr/ACL4SSR_Online_Full.ini")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, svcErr := ConvertACL4SSRCustomINI(string(data))
	if svcErr != nil {
		t.Fatalf("convert: %v", svcErr)
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(result.TemplateYAML), &doc); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}
	groups, ok := doc["proxy-groups"].([]any)
	if !ok {
		t.Fatal("proxy-groups is not a sequence")
	}

	// Find the Netflix node group (has regex filter), not the Netflix video select group.
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		if !strings.HasSuffix(name, "奈飞节点") {
			continue
		}
		filter, _ := gm["filter"].(string)
		if filter != "(NF|奈飞|解锁|Netflix|NETFLIX|Media)" {
			t.Errorf("group %q: expected Netflix regex, got %q", name, filter)
		}
		includeAll, _ := gm["include-all-proxies"].(bool)
		if !includeAll {
			t.Errorf("group %q: expected include-all-proxies: true", name)
		}
		typeStr, _ := gm["type"].(string)
		if typeStr != "select" {
			t.Errorf("group %q: expected type select, got %q", name, typeStr)
		}
		return
	}
	t.Error("no Netflix node group found in output")
}
