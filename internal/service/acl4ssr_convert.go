package service

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ACL4SSRConversionResult holds the output of converting an ACL4SSR [custom] INI
// to a Resin-compatible Clash/Mihomo YAML template.
type ACL4SSRConversionResult struct {
	TemplateYAML  string   `json:"template_yaml"`
	Warnings      []string `json:"warnings"`
	GroupCount    int      `json:"group_count"`
	ProviderCount int      `json:"provider_count"`
	RuleCount     int      `json:"rule_count"`
}

// ---------------------------------------------------------------------------
// Internal parse types
// ---------------------------------------------------------------------------

type aclGroup struct {
	Name        string
	Type        string   // "select" or "url-test"
	Members     []string // explicit proxy/group references from [] entries
	IncludeAll  bool
	Filter      string // bare regex filter (empty for .* or none)
	KnownRegion bool   // true when Filter was set from a known region mapping
	URL         string // url-test URL
	Interval    int    // url-test seconds
	Timeout     int    // url-test milliseconds
	Tolerance   int    // url-test milliseconds
}

type aclRuleSetKind int

const (
	rsKindRemote aclRuleSetKind = iota
	rsKindGeoIP
	rsKindFinal
)

type aclRuleSet struct {
	Label   string // policy/group label
	Kind    aclRuleSetKind
	URL     string // HTTPS URL (for remote kind)
	GeoIPCC string // country code for GEOIP kind
}

// yamlProvider is the YAML-friendly rule-provider representation.
type yamlProvider struct {
	Type     string
	Behavior string
	Format   string
	URL      string
	Interval int
}

// ---------------------------------------------------------------------------
// Known region regex mappings (Full's actual patterns -> Resin filter)
// ---------------------------------------------------------------------------

type regionMapping struct {
	rawPattern string
	region     string
}

// knownRegionPatterns matches the exact current content of the Full fixture.
// These are the six region-url-test groups' regex selectors.
var knownRegionPatterns = []regionMapping{
	{`(港|HK|hk|Hong Kong|HongKong|hongkong)`, "HK"},
	{`(日本|川日|东京|大阪|泉日|埼玉|沪日|深日|JP|Japan)`, "JP"},
	{`(美|波特兰|达拉斯|俄勒冈|凤凰城|费利蒙|硅谷|拉斯维加斯|洛杉矶|圣何塞|圣克拉拉|西雅图|芝加哥|US|United States)`, "US"},
	{`(台|新北|彰化|TW|Taiwan)`, "TW"},
	{`(新加坡|坡|狮城|SG|Singapore)`, "SG"},
	{`(KR|Korea|KOR|首尔|韩|韓)`, "KR"},
}

// regionFilterFor returns the Resin name filter matching [CC] tags.
// Matches exact contract: (?:^|/)\[CC\](?: [^/]*|)$
func regionFilterFor(region string) string {
	return fmt.Sprintf("(?:^|/)\\[%s\\](?: [^/]*|)$", region)
}

func isKnownRegionPattern(s string) (string, string, bool) {
	for _, m := range knownRegionPatterns {
		if s == m.rawPattern {
			return m.region, regionFilterFor(m.region), true
		}
	}
	return "", "", false
}

// isSemanticRegex returns true for known non-region semantic selectors that
// must be preserved as-is with a warning, never rewritten or broadened.
func isSemanticRegex(s string) bool {
	return s == `(网易|音乐|解锁|Music|NetEase)` ||
		s == `(NF|奈飞|解锁|Netflix|NETFLIX|Media)`
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

func trimBOM(s string) string {
	if len(s) >= 3 && s[0] == 0xEF && s[1] == 0xBB && s[2] == 0xBF {
		return s[3:]
	}
	return s
}

// splitRulesetLine splits on the first comma to separate label from value.
func splitRulesetLine(value string) (label, rest string) {
	idx := strings.IndexByte(value, ',')
	if idx < 0 {
		return "", value
	}
	return value[:idx], value[idx+1:]
}

func providerKeyFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	base := path.Base(u.Path)
	base = strings.TrimSuffix(base, ".list")
	base = strings.TrimSuffix(base, ".txt")
	base = strings.TrimSuffix(base, ".yaml")
	base = strings.TrimSuffix(base, ".yml")
	if base == "" || base == "." || base == "/" {
		return "", fmt.Errorf("cannot derive provider key from URL: %s", rawURL)
	}
	return base, nil
}

// dedupKeys returns deterministic unique keys by appending "-2", "-3", etc.
func dedupKeys(keys []string) []string {
	seen := make(map[string]int, len(keys))
	result := make([]string, len(keys))
	for i, k := range keys {
		candidate := k
		for seq := 2; ; seq++ {
			if _, taken := seen[candidate]; !taken {
				break
			}
			candidate = k + "-" + strconv.Itoa(seq)
		}
		seen[candidate] = i
		result[i] = candidate
	}
	return result
}

// isCommentLine returns true if the trimmed line is a blank or comment
// (starting with ; or #).
func isCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#")
}

// ---------------------------------------------------------------------------
// YAML building helpers (deterministic ordering)
// ---------------------------------------------------------------------------

// yamlStr is a helper to create a scalar node.
func yamlStr(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func yamlInt(v int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)}
}

func yamlBool(v bool) *yaml.Node {
	if v {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
}

func yamlSeq(items []*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: items}
}

func yamlMap(items ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: items}
}

// yamlMapEntry is a helper to create a key-value pair in a mapping's Content slice.
func yamlMapEntry(key *yaml.Node, value *yaml.Node) []*yaml.Node {
	return []*yaml.Node{key, value}
}

// buildGroupNode serializes a single proxy-group as a yaml.Node mapping.
func buildGroupNode(g aclGroup) *yaml.Node {
	var content []*yaml.Node

	content = append(content, yamlMapEntry(yamlStr("name"), yamlStr(g.Name))...)
	content = append(content, yamlMapEntry(yamlStr("type"), yamlStr(g.Type))...)

	if len(g.Members) > 0 {
		var proxyNodes []*yaml.Node
		for _, p := range g.Members {
			proxyNodes = append(proxyNodes, yamlStr(p))
		}
		content = append(content, yamlMapEntry(yamlStr("proxies"), yamlSeq(proxyNodes))...)
	}

	if g.IncludeAll {
		content = append(content, yamlMapEntry(yamlStr("include-all-proxies"), yamlBool(true))...)
	}

	if g.Filter != "" {
		content = append(content, yamlMapEntry(yamlStr("filter"), yamlStr(g.Filter))...)
	}

	if g.Type == "url-test" && g.URL != "" {
		content = append(content, yamlMapEntry(yamlStr("url"), yamlStr(g.URL))...)
		if g.Interval > 0 {
			content = append(content, yamlMapEntry(yamlStr("interval"), yamlInt(g.Interval))...)
		}
		if g.Timeout > 0 {
			content = append(content, yamlMapEntry(yamlStr("timeout"), yamlInt(g.Timeout))...)
		}
		if g.Tolerance > 0 {
			content = append(content, yamlMapEntry(yamlStr("tolerance"), yamlInt(g.Tolerance))...)
		}
	}

	return yamlMap(content...)
}

// buildProviderNode serializes a single rule-provider mapping.
func buildProviderNode(p yamlProvider) *yaml.Node {
	inner := yamlMap(
		yamlMapEntry(yamlStr("type"), yamlStr(p.Type))...,
	)
	inner.Content = append(inner.Content, yamlMapEntry(yamlStr("behavior"), yamlStr(p.Behavior))...)
	inner.Content = append(inner.Content, yamlMapEntry(yamlStr("format"), yamlStr(p.Format))...)
	inner.Content = append(inner.Content, yamlMapEntry(yamlStr("url"), yamlStr(p.URL))...)
	inner.Content = append(inner.Content, yamlMapEntry(yamlStr("interval"), yamlInt(p.Interval))...)
	return inner
}

// ---------------------------------------------------------------------------
// Main conversion entry point
// ---------------------------------------------------------------------------

// maxACL4SSRInputSize is the maximum allowed input size in bytes (512 KiB).
// This protects against unbounded allocation when splitting/parsing arbitrary
// input. The limit matches typical HTTP response body constraints.
const maxACL4SSRInputSize = 512 * 1024

// ConvertACL4SSRCustomINI converts an ACL4SSR [custom] INI string to a
// Resin-compatible Clash/Mihomo YAML template. It parses only the bounded
// subset used by ACL4SSR Full configurations.
func ConvertACL4SSRCustomINI(input string) (*ACL4SSRConversionResult, *ServiceError) {
	input = trimBOM(input)
	if len(input) > maxACL4SSRInputSize {
		return nil, invalidArg(fmt.Sprintf("input exceeds maximum allowed size of %d bytes", maxACL4SSRInputSize))
	}
	var warnings []string

	// ---- Lexing / section discovery ----
	lines := strings.Split(input, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, "\r")
	}

	customStart := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "[custom]" {
			customStart = i
			break
		}
	}
	if customStart < 0 {
		return nil, invalidArg("input: missing required [custom] section")
	}

	var customGroups []aclGroup
	var ruleSets []aclRuleSet
	var unknownDirectives []string

	for i := customStart + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if isCommentLine(line) {
			continue
		}
		// Stop at next section header.
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if line != "[custom]" {
				unknownDirectives = append(unknownDirectives, line)
			}
			break
		}

		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 0 {
			unknownDirectives = append(unknownDirectives, line)
			continue
		}
		key := strings.TrimSpace(line[:eqIdx])
		value := strings.TrimSpace(line[eqIdx+1:])

		switch key {
		case "custom_proxy_group":
			g, errMsg := parseCustomProxyGroupStrict(value)
			if errMsg != "" {
				return nil, invalidArg("custom_proxy_group: " + errMsg)
			}
			if g != nil {
				customGroups = append(customGroups, *g)
			}
		case "ruleset":
			rs, errMsg := parseRuleSetStrict(value)
			if errMsg != "" {
				return nil, invalidArg("ruleset: " + errMsg)
			}
			if rs != nil {
				ruleSets = append(ruleSets, *rs)
			}
		case "enable_rule_generator":
			if value != "true" {
				warnings = append(warnings, fmt.Sprintf("enable_rule_generator=%s: expected true, proceeding", value))
			}
		case "overwrite_original_rules":
			if value != "true" {
				warnings = append(warnings, fmt.Sprintf("overwrite_original_rules=%s: expected true, proceeding", value))
			}
		default:
			unknownDirectives = append(unknownDirectives, line)
		}
	}

	for _, d := range unknownDirectives {
		warnings = append(warnings, fmt.Sprintf("unknown directive or section ignored: %s", d))
	}

	// Check for semantic/unknown regex entries that need warnings.
	for _, g := range customGroups {
		if g.Filter == "" || g.KnownRegion {
			continue
		}
		if isSemanticRegex(g.Filter) {
			warnings = append(warnings, fmt.Sprintf("group %q: regex %q is a semantic selector; it may match no Resin nodes and needs manual review", g.Name, g.Filter))
		} else {
			// Not a region pattern, not a recognized semantic pattern — unknown.
			warnings = append(warnings, fmt.Sprintf("group %q: regex %q is not a known region pattern; it may match no Resin nodes and needs manual review", g.Name, g.Filter))
		}
	}

	// ---- Validation ----
	if len(customGroups) == 0 {
		return nil, invalidArg("input: at least one custom_proxy_group is required")
	}
	if len(ruleSets) == 0 {
		return nil, invalidArg("input: at least one active ruleset is required")
	}

	// Group name uniqueness.
	groupNames := make(map[string]int, len(customGroups))
	for i, g := range customGroups {
		if prev, dup := groupNames[g.Name]; dup {
			return nil, invalidArg(fmt.Sprintf("custom_proxy_group: duplicate group name %q at positions %d and %d", g.Name, prev, i))
		}
		groupNames[g.Name] = i
	}

	// Group type and content check.
	for _, g := range customGroups {
		if g.Type != "select" && g.Type != "url-test" {
			return nil, invalidArg(fmt.Sprintf("custom_proxy_group %q: unsupported group type %q in Phase 1", g.Name, g.Type))
		}
		if g.Type == "select" && len(g.Members) == 0 && !g.IncludeAll && g.Filter == "" {
			return nil, invalidArg(fmt.Sprintf("custom_proxy_group %q: select group has no usable selectors (no [] entries, no .*, no regex)", g.Name))
		}
	}

	// FINAL validation: must exist and be last.
	hasFinal := false
	finalPolicy := ""
	finalCount := 0
	for _, rs := range ruleSets {
		if rs.Kind == rsKindFinal {
			hasFinal = true
			finalPolicy = rs.Label
			finalCount++
		}
	}
	if !hasFinal {
		return nil, invalidArg("ruleset: missing required []FINAL entry")
	}
	if finalCount > 1 {
		return nil, invalidArg("ruleset: multiple []FINAL entries are not allowed")
	}
	if !ruleSets[len(ruleSets)-1].IsFinal() {
		return nil, invalidArg("ruleset: []FINAL must be the last active ruleset entry")
	}

	// ---- Build deterministic YAML document using yaml.Node ----

	// 1. proxy-groups (ordered).
	var groupNodes []*yaml.Node
	for _, g := range customGroups {
		groupNodes = append(groupNodes, buildGroupNode(g))
	}

	// 2. rule-providers (ordered mapping to match ruleset order).
	// Provider keys and their corresponding RULE-SET rules below are both
	// derived from the same ordered list of remote rulesets. The i-th remote
	// URL produces the i-th provider key and later the i-th RULE-SET rule,
	// guaranteeing that each provider key referenced in rules actually exists.
	var rawKeys []string
	var remoteURLs []string
	for _, rs := range ruleSets {
		if rs.Kind == rsKindRemote {
			remoteURLs = append(remoteURLs, rs.URL)
			k, err := providerKeyFromURL(rs.URL)
			if err != nil {
				return nil, invalidArg(fmt.Sprintf("ruleset: cannot derive provider key from URL %s: %v", rs.URL, err))
			}
			rawKeys = append(rawKeys, k)
		}
	}
	uniqueKeys := dedupKeys(rawKeys)

	yamlProviders := make(map[string]yamlProvider, len(remoteURLs))
	var providerKeyOrder []string // ordered keys for rule referencing

	for i, urlStr := range remoteURLs {
		key := uniqueKeys[i]
		yamlProviders[key] = yamlProvider{
			Type:     "http",
			Behavior: "classical",
			Format:   "text",
			URL:      urlStr,
			Interval: 86400,
		}
		providerKeyOrder = append(providerKeyOrder, key)
	}

	// Build ordered provider mapping node.
	var providerContent []*yaml.Node
	for _, key := range providerKeyOrder {
		p := yamlProviders[key]
		providerContent = append(providerContent, yamlStr(key))
		providerContent = append(providerContent, buildProviderNode(p))
	}
	providerMapNode := yamlMap(providerContent...)

	// 3. rules (ordered by ruleset occurrence).
	var ruleNodes []*yaml.Node
	remoteIdx := 0
	for _, rs := range ruleSets {
		switch rs.Kind {
		case rsKindRemote:
			key := providerKeyOrder[remoteIdx]
			remoteIdx++
			ruleNodes = append(ruleNodes, yamlStr(fmt.Sprintf("RULE-SET,%s,%s", key, rs.Label)))
		case rsKindGeoIP:
			ruleNodes = append(ruleNodes, yamlStr(fmt.Sprintf("GEOIP,%s,%s,no-resolve", rs.GeoIPCC, rs.Label)))
		case rsKindFinal:
			ruleNodes = append(ruleNodes, yamlStr(fmt.Sprintf("MATCH,%s", finalPolicy)))
		}
	}

	// 4. Assemble top-level document in deterministic order.
	// Keys ordered as: proxies, proxy-groups, rule-providers, rules
	doc := yamlMap(
		yamlMapEntry(yamlStr("proxies"), yamlSeq(nil))..., // explicit empty sequence
	)
	doc.Content = append(doc.Content, yamlMapEntry(yamlStr("proxy-groups"), yamlSeq(groupNodes))...)
	doc.Content = append(doc.Content, yamlMapEntry(yamlStr("rule-providers"), providerMapNode)...)
	doc.Content = append(doc.Content, yamlMapEntry(yamlStr("rules"), yamlSeq(ruleNodes))...)

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, internal("serialize YAML", err)
	}
	templateYAML := string(out)

	if svcErr := ValidateRuleProfileTemplate(templateYAML); svcErr != nil {
		return nil, internal("generated template validation failed", fmt.Errorf("%v", svcErr))
	}

	result := &ACL4SSRConversionResult{
		TemplateYAML:  templateYAML,
		Warnings:      warnings,
		GroupCount:    len(customGroups),
		ProviderCount: len(yamlProviders),
		RuleCount:     len(ruleNodes),
	}
	return result, nil
}

// IsFinal returns true when the ruleset kind is FINAL.
func (rs *aclRuleSet) IsFinal() bool { return rs.Kind == rsKindFinal }

// ---------------------------------------------------------------------------
// Parsing: custom_proxy_group (strict mode — errors on malformed)
// ---------------------------------------------------------------------------

// parseCustomProxyGroupStrict parses a single custom_proxy_group= value.
// Returns an error string on malformed input (so the caller returns invalidArg).
// Returns (nil, "") only for empty/blank lines that should be silently skipped.
func parseCustomProxyGroupStrict(value string) (*aclGroup, string) {
	fields := strings.Split(value, "`")
	if len(fields) < 3 {
		return nil, fmt.Sprintf("expected at least 3 backtick-delimited fields, got %d", len(fields))
	}

	name := strings.TrimSpace(fields[0])
	groupType := strings.TrimSpace(fields[1])

	if name == "" {
		return nil, "empty group name"
	}
	if groupType != "select" && groupType != "url-test" {
		return nil, fmt.Sprintf("unsupported group type %q in Phase 1 (only select and url-test are supported)", groupType)
	}

	g := &aclGroup{Name: name, Type: groupType}

	if groupType == "select" {
		regexCount := 0
		for _, entry := range fields[2:] {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			if strings.HasPrefix(entry, "[]") {
				ref := strings.TrimSpace(strings.TrimPrefix(entry, "[]"))
				if ref == "" {
					return nil, fmt.Sprintf("group %q: empty reference entry", name)
				}
				g.Members = append(g.Members, ref)
			} else if entry == ".*" {
				g.IncludeAll = true
			} else if strings.HasPrefix(entry, "(") && strings.HasSuffix(entry, ")") {
				regexCount++
				if regexCount > 1 {
					return nil, fmt.Sprintf("group %q: multiple distinct bare regex selectors not supported", name)
				}
				if err := applyRegexEntry(g, name, entry); err != "" {
					return nil, err
				}
			} else {
				return nil, fmt.Sprintf("group %q: unexpected entry %q", name, entry)
			}
		}
	} else {
		// url-test: fields[2]=selector, fields[3]=URL, fields[4]=interval,timeout,tolerance
		if len(fields) < 4 {
			return nil, fmt.Sprintf("group %q: url-test requires at least selector and URL fields", name)
		}
		selector := strings.TrimSpace(fields[2])
		if selector == ".*" {
			g.IncludeAll = true
		} else if strings.HasPrefix(selector, "(") && strings.HasSuffix(selector, ")") {
			if err := applyRegexEntry(g, name, selector); err != "" {
				return nil, err
			}
		} else {
			return nil, fmt.Sprintf("group %q: unsupported url-test selector %q", name, selector)
		}

		urlStr := strings.TrimSpace(fields[3])
		if urlStr == "" {
			return nil, fmt.Sprintf("group %q: url-test requires a non-empty URL", name)
		}
		// Validate URL is absolute (HTTP is allowed for url-test, unlike rule-providers).
		u, err := url.Parse(urlStr)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Sprintf("group %q: invalid url-test URL: %s", name, urlStr)
		}
		g.URL = urlStr

		if len(fields) > 4 {
			params := strings.TrimSpace(fields[4])
			if params == "" {
				return nil, fmt.Sprintf("group %q: url-test requires interval,timeout,tolerance params", name)
			}
			parts := strings.Split(params, ",")
			if len(parts) < 1 || strings.TrimSpace(parts[0]) == "" {
				return nil, fmt.Sprintf("group %q: url-test requires interval as first param", name)
			}
			for j, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				n, err := strconv.Atoi(p)
				if err != nil {
					return nil, fmt.Sprintf("group %q: invalid url-test param %q: not a number", name, p)
				}
				switch j {
				case 0:
					g.Interval = n
				case 1:
					g.Timeout = n
				case 2:
					g.Tolerance = n
				}
			}
		} else {
			return nil, fmt.Sprintf("group %q: url-test requires interval,timeout,tolerance params", name)
		}
	}

	return g, ""
}

// applyRegexEntry sets IncludeAll and Filter on a group from a bare regex entry.
func applyRegexEntry(g *aclGroup, name, entry string) string {
	g.IncludeAll = true
	if _, filter, ok := isKnownRegionPattern(entry); ok {
		g.Filter = filter
		g.KnownRegion = true
	} else if isSemanticRegex(entry) {
		g.Filter = entry
		// Warning added at conversion level; no error here.
	} else {
		// Unknown regex — preserve but it'll get a warning at conversion level.
		g.Filter = entry
	}
	return ""
}

// ---------------------------------------------------------------------------
// Parsing: ruleset (strict mode)
// ---------------------------------------------------------------------------

func parseRuleSetStrict(value string) (*aclRuleSet, string) {
	label, rest := splitRulesetLine(value)
	label = strings.TrimSpace(label)
	rest = strings.TrimSpace(rest)

	if rest == "" {
		return nil, "empty ruleset value"
	}
	if label == "" {
		return nil, "empty ruleset label"
	}

	rs := &aclRuleSet{Label: label}

	if rest == "[]FINAL" {
		rs.Kind = rsKindFinal
		return rs, ""
	}

	if strings.HasPrefix(rest, "[]GEOIP,") {
		geo := strings.TrimSpace(strings.TrimPrefix(rest, "[]GEOIP,"))
		if geo == "" {
			return nil, "empty GEOIP country code"
		}
		if strings.Contains(geo, ",") {
			return nil, fmt.Sprintf("GEOIP value contains extra fields after country code: %q", rest)
		}
		rs.Kind = rsKindGeoIP
		rs.GeoIPCC = geo
		return rs, ""
	}

	// Remote HTTPS list.
	u, err := url.Parse(rest)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Sprintf("invalid ruleset value: %s", rest)
	}
	if u.Scheme != "https" {
		return nil, fmt.Sprintf("non-HTTPS ruleset URL: %s", rest)
	}
	if u.User != nil {
		return nil, fmt.Sprintf("ruleset URL with userinfo not allowed: %s", rest)
	}
	if u.Fragment != "" {
		return nil, fmt.Sprintf("ruleset URL with fragment not allowed: %s", rest)
	}
	rs.Kind = rsKindRemote
	rs.URL = rest
	return rs, ""
}
