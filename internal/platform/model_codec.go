package platform

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/node"
)

func isLowerAlpha2(s string) bool {
	if len(s) != 2 {
		return false
	}
	return s[0] >= 'a' && s[0] <= 'z' && s[1] >= 'a' && s[1] <= 'z'
}

// ValidateRegionFilters validates region filters against lowercase ISO alpha-2 format.
// Entries may optionally be prefixed with "!" to indicate negation (e.g. !hk).
func ValidateRegionFilters(regionFilters []string) error {
	for i, r := range regionFilters {
		code := r
		if len(r) > 0 && r[0] == '!' {
			code = r[1:]
		}
		if !isLowerAlpha2(code) {
			return fmt.Errorf("region_filters[%d]: must be a 2-letter lowercase ISO 3166-1 alpha-2 code (e.g. us, jp) or negation (e.g. !hk)", i)
		}
	}
	return nil
}

// CompileRegexFilters compiles regex filters in order.
func CompileRegexFilters(regexFilters []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(regexFilters))
	for i, re := range regexFilters {
		c, err := regexp.Compile(re)
		if err != nil {
			return nil, fmt.Errorf("regex_filters[%d]: invalid regex: %v", i, err)
		}
		compiled = append(compiled, c)
	}
	return compiled, nil
}

// NewConfiguredPlatform builds a runtime platform with non-filter settings applied.
// Protocol filter values are normalised to their canonical form defensively.
// Unknown/unrecognised values are kept as-is (caller is expected to validate first).
func NewConfiguredPlatform(
	id, name string,
	regexFilters []*regexp.Regexp,
	regionFilters []string,
	protocolFilters []string,
	excludeProtocolFilters []string,
	stickyTTLNs int64,
	missAction string,
	emptyAccountBehavior string,
	fixedAccountHeader string,
	allocationPolicy string,
	passiveCircuitBreakerDisabled bool,
) *Platform {
	return NewConfiguredPlatformWithQuality(
		id, name, regexFilters, regionFilters, protocolFilters, excludeProtocolFilters,
		stickyTTLNs, missAction, emptyAccountBehavior, fixedAccountHeader, allocationPolicy,
		passiveCircuitBreakerDisabled,
		"", 0, nil, 0, "",
	)
}

// NewConfiguredPlatformWithQuality builds a runtime platform with quality filter
// settings applied. This is the full constructor; NewConfiguredPlatform is a
// convenience wrapper that leaves all quality filters at their zero values.
func NewConfiguredPlatformWithQuality(
	id, name string,
	regexFilters []*regexp.Regexp,
	regionFilters []string,
	protocolFilters []string,
	excludeProtocolFilters []string,
	stickyTTLNs int64,
	missAction string,
	emptyAccountBehavior string,
	fixedAccountHeader string,
	allocationPolicy string,
	passiveCircuitBreakerDisabled bool,
	qualityGrade string,
	qualityMinScore float64,
	qualityCloudflareChallenged *bool,
	qualityCheckedSinceNs int64,
	qualityProfile string,
) *Platform {
	normalizedFixedHeaders, fixedHeaders, err := NormalizeFixedAccountHeaders(fixedAccountHeader)
	if err != nil {
		normalizedFixedHeaders = strings.TrimSpace(fixedAccountHeader)
		fixedHeaders = nil
	}
	plat := NewPlatform(id, name, regexFilters, regionFilters)
	plat.ProtocolFilters = normalizeProtocolSlice(protocolFilters)
	plat.ExcludeProtocolFilters = normalizeProtocolSlice(excludeProtocolFilters)
	plat.StickyTTLNs = stickyTTLNs
	plat.ReverseProxyMissAction = missAction
	plat.ReverseProxyEmptyAccountBehavior = emptyAccountBehavior
	plat.ReverseProxyFixedAccountHeader = normalizedFixedHeaders
	plat.ReverseProxyFixedAccountHeaders = append([]string(nil), fixedHeaders...)
	plat.AllocationPolicy = ParseAllocationPolicy(allocationPolicy)
	plat.PassiveCircuitBreakerDisabled = passiveCircuitBreakerDisabled
	plat.QualityGrade = qualityGrade
	plat.QualityMinScore = qualityMinScore
	plat.QualityCloudflareChallenged = qualityCloudflareChallenged
	plat.QualityCheckedSinceNs = qualityCheckedSinceNs
	plat.QualityProfile = qualityProfile
	return plat
}

// normalizeProtocolSlice normalises each entry to its canonical form and
// deduplicates. Unknown entries are kept verbatim so callers that validate
// first still get meaningful error positions.
func normalizeProtocolSlice(s []string) []string {
	if s == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(s))
	out := make([]string, 0, len(s))
	for _, f := range s {
		c := node.NormalizeProtocol(f)
		if c == "" {
			c = f // keep original; caller should have validated
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// ValidateProtocolFilters validates that each entry in include/exclude is a
// recognised canonical protocol name. Empty slices are valid (no filtering).
func ValidateProtocolFilters(include, exclude []string) error {
	for i, f := range include {
		if node.NormalizeProtocol(f) == "" {
			return fmt.Errorf("protocol_filters[%d]: unsupported protocol %q", i, f)
		}
	}
	for i, f := range exclude {
		if node.NormalizeProtocol(f) == "" {
			return fmt.Errorf("exclude_protocol_filters[%d]: unsupported protocol %q", i, f)
		}
	}
	return nil
}

// CompileModelRegexFilters compiles regex filters from persisted model values.
func CompileModelRegexFilters(platformID string, regexFilters []string) ([]*regexp.Regexp, error) {
	compiled, err := CompileRegexFilters(regexFilters)
	if err != nil {
		return nil, fmt.Errorf("decode platform %s regex_filters: %w", platformID, err)
	}
	return compiled, nil
}

// BuildFromModel builds a runtime platform from a persisted model.Platform.
func BuildFromModel(mp model.Platform) (*Platform, error) {
	regexFilters, err := CompileModelRegexFilters(mp.ID, mp.RegexFilters)
	if err != nil {
		return nil, err
	}
	if err := ValidateRegionFilters(mp.RegionFilters); err != nil {
		return nil, err
	}
	if err := ValidateProtocolFilters(mp.ProtocolFilters, mp.ExcludeProtocolFilters); err != nil {
		return nil, err
	}
	emptyAccountBehavior := mp.ReverseProxyEmptyAccountBehavior
	if !ReverseProxyEmptyAccountBehavior(emptyAccountBehavior).IsValid() {
		emptyAccountBehavior = string(ReverseProxyEmptyAccountBehaviorRandom)
	}
	missAction := NormalizeReverseProxyMissAction(mp.ReverseProxyMissAction)
	if missAction == "" {
		return nil, fmt.Errorf(
			"decode platform %s reverse_proxy_miss_action: invalid value %q",
			mp.ID,
			mp.ReverseProxyMissAction,
		)
	}
	fixedHeader, _, err := NormalizeFixedAccountHeaders(mp.ReverseProxyFixedAccountHeader)
	if err != nil {
		return nil, fmt.Errorf("decode platform %s reverse_proxy_fixed_account_header: %w", mp.ID, err)
	}
	if emptyAccountBehavior == string(ReverseProxyEmptyAccountBehaviorFixedHeader) && fixedHeader == "" {
		return nil, fmt.Errorf(
			"decode platform %s reverse_proxy_fixed_account_header: required when reverse_proxy_empty_account_behavior is %s",
			mp.ID,
			ReverseProxyEmptyAccountBehaviorFixedHeader,
		)
	}

	// Copy quality filter fields with proper nullable bool handling.
	var qualityCF *bool
	if mp.QualityCloudflareChallenged != nil {
		v := *mp.QualityCloudflareChallenged
		qualityCF = &v
	}

	return NewConfiguredPlatformWithQuality(
		mp.ID,
		mp.Name,
		regexFilters,
		append([]string(nil), mp.RegionFilters...),
		append([]string(nil), mp.ProtocolFilters...),
		append([]string(nil), mp.ExcludeProtocolFilters...),
		mp.StickyTTLNs,
		string(missAction),
		emptyAccountBehavior,
		fixedHeader,
		mp.AllocationPolicy,
		mp.PassiveCircuitBreakerDisabled,
		mp.QualityGrade,
		mp.QualityMinScore,
		qualityCF,
		mp.QualityCheckedSinceNs,
		mp.QualityProfile,
	), nil
}
