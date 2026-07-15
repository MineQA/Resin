package platform

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Resinat/Resin/internal/model"
)

func TestBuildFromModel_Success(t *testing.T) {
	mp := model.Platform{
		ID:                               "plat-1",
		Name:                             "Platform-1",
		StickyTTLNs:                      3600,
		RegexFilters:                     []string{`^us-.*$`},
		RegionFilters:                    []string{"us", "jp"},
		ReverseProxyMissAction:           "REJECT",
		ReverseProxyEmptyAccountBehavior: "FIXED_HEADER",
		ReverseProxyFixedAccountHeader:   "x-account-id",
		AllocationPolicy:                 "PREFER_LOW_LATENCY",
		PassiveCircuitBreakerDisabled:    true,
	}

	plat, err := BuildFromModel(mp)
	if err != nil {
		t.Fatalf("BuildFromModel: %v", err)
	}

	if plat.ID != mp.ID || plat.Name != mp.Name {
		t.Fatalf("id/name mismatch: got (%q,%q)", plat.ID, plat.Name)
	}
	if plat.StickyTTLNs != mp.StickyTTLNs {
		t.Fatalf("sticky ttl mismatch: got %d want %d", plat.StickyTTLNs, mp.StickyTTLNs)
	}
	if plat.ReverseProxyMissAction != mp.ReverseProxyMissAction {
		t.Fatalf("miss action mismatch: got %q want %q", plat.ReverseProxyMissAction, mp.ReverseProxyMissAction)
	}
	if plat.ReverseProxyEmptyAccountBehavior != "FIXED_HEADER" {
		t.Fatalf(
			"empty-account behavior mismatch: got %q want %q",
			plat.ReverseProxyEmptyAccountBehavior,
			"FIXED_HEADER",
		)
	}
	if plat.ReverseProxyFixedAccountHeader != "X-Account-Id" {
		t.Fatalf(
			"fixed account header mismatch: got %q want %q",
			plat.ReverseProxyFixedAccountHeader,
			"X-Account-Id",
		)
	}
	if plat.AllocationPolicy != AllocationPolicyPreferLowLatency {
		t.Fatalf("allocation policy mismatch: got %q want %q", plat.AllocationPolicy, AllocationPolicyPreferLowLatency)
	}
	if !plat.PassiveCircuitBreakerDisabled {
		t.Fatal("passive circuit breaker flag mismatch: got false want true")
	}
	if len(plat.RegexFilters) != 1 || !plat.RegexFilters[0].MatchString("us-node") {
		t.Fatalf("regex filters not compiled as expected: %+v", plat.RegexFilters)
	}
	if len(plat.RegionFilters) != 2 || plat.RegionFilters[0] != "us" || plat.RegionFilters[1] != "jp" {
		t.Fatalf("region filters mismatch: %+v", plat.RegionFilters)
	}
}

func TestBuildFromModel_InvalidRegex(t *testing.T) {
	_, err := BuildFromModel(model.Platform{
		ID:           "plat-1",
		RegexFilters: []string{`(broken`},
	})
	if err == nil {
		t.Fatal("expected regex decode error")
	}
	if !strings.Contains(err.Error(), "regex_filters") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildFromModel_InvalidRegionFilters(t *testing.T) {
	_, err := BuildFromModel(model.Platform{
		ID:            "plat-1",
		RegexFilters:  []string{},
		RegionFilters: []string{"US"},
	})
	if err == nil {
		t.Fatal("expected region decode error")
	}
	if !strings.Contains(err.Error(), "region_filters[0]") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildFromModel_InvalidMissAction(t *testing.T) {
	_, err := BuildFromModel(model.Platform{
		ID:                     "plat-1",
		Name:                   "Platform-1",
		RegexFilters:           []string{},
		RegionFilters:          []string{},
		ReverseProxyMissAction: "RANDOM",
		AllocationPolicy:       "BALANCED",
	})
	if err == nil {
		t.Fatal("expected reverse_proxy_miss_action decode error")
	}
	if !strings.Contains(err.Error(), "reverse_proxy_miss_action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildFromModel_InvalidEmptyAccountBehaviorFallsBackToRandom(t *testing.T) {
	plat, err := BuildFromModel(model.Platform{
		ID:                               "plat-1",
		Name:                             "Platform-1",
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "INVALID",
		AllocationPolicy:                 "BALANCED",
	})
	if err != nil {
		t.Fatalf("BuildFromModel: %v", err)
	}
	if plat.ReverseProxyEmptyAccountBehavior != string(ReverseProxyEmptyAccountBehaviorRandom) {
		t.Fatalf(
			"empty-account behavior fallback mismatch: got %q, want %q",
			plat.ReverseProxyEmptyAccountBehavior,
			ReverseProxyEmptyAccountBehaviorRandom,
		)
	}
}

func TestBuildFromModel_FixedHeadersMultiLineNormalized(t *testing.T) {
	plat, err := BuildFromModel(model.Platform{
		ID:                               "plat-1",
		Name:                             "Platform-1",
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "FIXED_HEADER",
		ReverseProxyFixedAccountHeader:   " authorization \nX-Account-Id\nx-account-id",
		AllocationPolicy:                 "BALANCED",
	})
	if err != nil {
		t.Fatalf("BuildFromModel: %v", err)
	}

	if plat.ReverseProxyFixedAccountHeader != "Authorization\nX-Account-Id" {
		t.Fatalf(
			"fixed account header mismatch: got %q, want %q",
			plat.ReverseProxyFixedAccountHeader,
			"Authorization\nX-Account-Id",
		)
	}
	if !reflect.DeepEqual(plat.ReverseProxyFixedAccountHeaders, []string{"Authorization", "X-Account-Id"}) {
		t.Fatalf(
			"fixed account headers mismatch: got %v, want %v",
			plat.ReverseProxyFixedAccountHeaders,
			[]string{"Authorization", "X-Account-Id"},
		)
	}
}

func TestBuildFromModel_FixedHeaderRequiresValidHeaderName(t *testing.T) {
	_, err := BuildFromModel(model.Platform{
		ID:                               "plat-1",
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "FIXED_HEADER",
		ReverseProxyFixedAccountHeader:   "bad header",
	})
	if err == nil {
		t.Fatal("expected fixed header validation error")
	}
	if !strings.Contains(err.Error(), "reverse_proxy_fixed_account_header") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBuildFromModel_WithQualityFilters verifies quality filter fields are
// properly decoded from model to runtime platform.
func TestBuildFromModel_WithQualityFilters(t *testing.T) {
	trueVal := true
	mp := model.Platform{
		ID:                               "plat-q-1",
		Name:                             "Quality-Platform",
		StickyTTLNs:                      3600,
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "RANDOM",
		AllocationPolicy:                 "BALANCED",
		QualityGrade:                     "A",
		QualityMinScore:                  80.0,
		QualityCloudflareChallenged:      &trueVal,
		QualityCloudflareStatuses:        []string{"clean", "block", "clean"},
		QualityCheckedSinceNs:            1000000,
		QualityProfile:                   "openai",
	}

	plat, err := BuildFromModel(mp)
	if err != nil {
		t.Fatalf("BuildFromModel with quality filters: %v", err)
	}

	if plat.QualityGrade != "A" {
		t.Fatalf("QualityGrade = %q, want A", plat.QualityGrade)
	}
	if plat.QualityMinScore != 80.0 {
		t.Fatalf("QualityMinScore = %f, want 80.0", plat.QualityMinScore)
	}
	if plat.QualityCloudflareChallenged == nil || *plat.QualityCloudflareChallenged != true {
		t.Fatal("QualityCloudflareChallenged should be true")
	}
	if len(plat.QualityCloudflareStatuses) != 2 || plat.QualityCloudflareStatuses[0] != "block" || plat.QualityCloudflareStatuses[1] != "clean" {
		t.Fatalf("QualityCloudflareStatuses = %v, want [block clean]", plat.QualityCloudflareStatuses)
	}
	if plat.QualityCheckedSinceNs != 1000000 {
		t.Fatalf("QualityCheckedSinceNs = %d, want 1000000", plat.QualityCheckedSinceNs)
	}
	if plat.QualityProfile != "openai" {
		t.Fatalf("QualityProfile = %q, want openai", plat.QualityProfile)
	}
}

// TestBuildFromModel_WithQualityFilters_NilCF verifies nil CF challenged.
func TestBuildFromModel_WithQualityFilters_NilCF(t *testing.T) {
	mp := model.Platform{
		ID:                               "plat-q-2",
		Name:                             "Quality-Platform-2",
		StickyTTLNs:                      3600,
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "RANDOM",
		AllocationPolicy:                 "BALANCED",
		QualityGrade:                     "",
		QualityMinScore:                  0,
		QualityCloudflareChallenged:      nil,
		QualityCheckedSinceNs:            0,
		QualityProfile:                   "",
	}

	plat, err := BuildFromModel(mp)
	if err != nil {
		t.Fatalf("BuildFromModel with zero quality filters: %v", err)
	}

	if plat.QualityGrade != "" {
		t.Fatalf("QualityGrade should be empty, got %q", plat.QualityGrade)
	}
	if plat.QualityMinScore != 0 {
		t.Fatalf("QualityMinScore should be 0, got %f", plat.QualityMinScore)
	}
	if plat.QualityCloudflareChallenged != nil {
		t.Fatal("QualityCloudflareChallenged should be nil")
	}
	if plat.QualityCheckedSinceNs != 0 {
		t.Fatalf("QualityCheckedSinceNs should be 0, got %d", plat.QualityCheckedSinceNs)
	}
	if plat.QualityProfile != "" {
		t.Fatalf("QualityProfile should be empty, got %q", plat.QualityProfile)
	}
}

func TestBuildFromModel_WithProtocolFilters(t *testing.T) {
	mp := model.Platform{
		ID:                               "plat-1",
		Name:                             "Platform-1",
		StickyTTLNs:                      3600,
		RegexFilters:                     []string{`^us-.*$`},
		RegionFilters:                    []string{"us"},
		ProtocolFilters:                  []string{"shadowsocks", "trojan"},
		ExcludeProtocolFilters:           []string{"vmess"},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "RANDOM",
		AllocationPolicy:                 "BALANCED",
	}

	plat, err := BuildFromModel(mp)
	if err != nil {
		t.Fatalf("BuildFromModel with protocol filters: %v", err)
	}

	if len(plat.ProtocolFilters) != 2 || plat.ProtocolFilters[0] != "shadowsocks" || plat.ProtocolFilters[1] != "trojan" {
		t.Fatalf("protocol_filters mismatch: got %v", plat.ProtocolFilters)
	}
	if len(plat.ExcludeProtocolFilters) != 1 || plat.ExcludeProtocolFilters[0] != "vmess" {
		t.Fatalf("exclude_protocol_filters mismatch: got %v", plat.ExcludeProtocolFilters)
	}
}

func TestBuildFromModel_NormalisesProtocolFilterAliases(t *testing.T) {
	mp := model.Platform{
		ID:                               "plat-2",
		Name:                             "Platform-2",
		StickyTTLNs:                      3600,
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ProtocolFilters:                  []string{"ss", "HY2", "vmess1"}, // aliases
		ExcludeProtocolFilters:           []string{"socks5"},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "RANDOM",
		AllocationPolicy:                 "BALANCED",
	}

	plat, err := BuildFromModel(mp)
	if err != nil {
		t.Fatalf("BuildFromModel: %v", err)
	}

	if !reflect.DeepEqual(plat.ProtocolFilters, []string{"shadowsocks", "hysteria2", "vmess"}) {
		t.Fatalf("protocol_filters after normalisation: got %v, want [shadowsocks hysteria2 vmess]", plat.ProtocolFilters)
	}
	if !reflect.DeepEqual(plat.ExcludeProtocolFilters, []string{"socks"}) {
		t.Fatalf("exclude_protocol_filters after normalisation: got %v, want [socks]", plat.ExcludeProtocolFilters)
	}
}

func TestValidateProtocolFilters_Invalid(t *testing.T) {
	err := ValidateProtocolFilters([]string{"bogus_proto"}, nil)
	if err == nil {
		t.Fatal("expected validation error for bogus protocol")
	}
	if !strings.Contains(err.Error(), "protocol_filters[0]") {
		t.Fatalf("unexpected error: %v", err)
	}

	err = ValidateProtocolFilters(nil, []string{"unknown"})
	if err == nil {
		t.Fatal("expected validation error for unknown exclude protocol")
	}
	if !strings.Contains(err.Error(), "exclude_protocol_filters[0]") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileRegexFilters_Invalid(t *testing.T) {
	_, err := CompileRegexFilters([]string{"(broken"})
	if err == nil {
		t.Fatal("expected compile error")
	}
	if !strings.Contains(err.Error(), "regex_filters[0]") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRegionFilters_Invalid(t *testing.T) {
	err := ValidateRegionFilters([]string{"US"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "region_filters[0]") {
		t.Fatalf("unexpected error: %v", err)
	}
}
