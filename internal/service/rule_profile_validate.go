package service

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidateRuleProfileTemplate validates a Clash YAML template for rule-mode export.
// It checks structural requirements without implementing a full Mihomo parser:
//   - Single YAML document, top-level mapping
//   - proxies: must be absent, null, or empty sequence (Resin owns this key)
//   - proxy-groups: if present, must be a sequence; each item a mapping with non-empty unique name
//   - rules: must be present, non-empty sequence of strings; last trimmed item must start
//     with "MATCH," and the target after the comma must be non-empty
//   - rule-providers / proxy-providers: if present, must be a mapping
//   - provider items with type http (case-insensitive) must have url = absolute HTTPS,
//     host non-empty, no userinfo, no fragment
//
// Unknown top-level keys are allowed (forwarded as-is).
func ValidateRuleProfileTemplate(templateYAML string) *ServiceError {
	dec := yaml.NewDecoder(strings.NewReader(templateYAML))

	// Must be a single document.
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return invalidArg(fmt.Sprintf("template_yaml: invalid YAML: %v", err))
	}

	// Reject trailing content after first document.
	// io.EOF means clean end-of-stream; any other value or nil means extra content.
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return invalidArg("template_yaml: YAML must contain exactly one document")
	} else if !errors.Is(err, io.EOF) {
		return invalidArg(fmt.Sprintf("template_yaml: invalid trailing YAML content: %v", err))
	}

	// Must be a top-level mapping.
	root, ok := doc.(map[string]any)
	if !ok {
		return invalidArg("template_yaml: top-level value must be a mapping (object)")
	}

	// --- proxies validation ---
	if err := validateProxies(root); err != nil {
		return err
	}

	// --- proxy-groups validation ---
	if err := validateProxyGroups(root); err != nil {
		return err
	}

	// --- rules validation ---
	if err := validateRules(root); err != nil {
		return err
	}

	// --- rule-providers and proxy-providers validation ---
	if err := validateProviders(root, "rule-providers"); err != nil {
		return err
	}
	if err := validateProviders(root, "proxy-providers"); err != nil {
		return err
	}

	return nil
}

func validateProxies(root map[string]any) *ServiceError {
	raw, ok := root["proxies"]
	if !ok {
		return nil // absent is fine
	}
	if raw == nil {
		return nil // null is fine
	}
	proxies, ok := raw.([]any)
	if !ok {
		return invalidArg("template_yaml: proxies must be a sequence (array) if present, or null/omitted")
	}
	if len(proxies) > 0 {
		return invalidArg("template_yaml: proxies must be empty when present; Resin injects proxies dynamically")
	}
	return nil
}

func validateProxyGroups(root map[string]any) *ServiceError {
	raw, ok := root["proxy-groups"]
	if !ok {
		return nil // optional
	}
	if raw == nil {
		return nil // null is fine
	}
	groups, ok := raw.([]any)
	if !ok {
		return invalidArg("template_yaml: proxy-groups must be a sequence (array) if present")
	}
	names := make(map[string]bool)
	for i, g := range groups {
		group, ok := g.(map[string]any)
		if !ok {
			return invalidArg(fmt.Sprintf("template_yaml: proxy-groups[%d]: each item must be a mapping", i))
		}
		nameRaw, ok := group["name"]
		if !ok {
			return invalidArg(fmt.Sprintf("template_yaml: proxy-groups[%d]: missing 'name' field", i))
		}
		name, ok := nameRaw.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return invalidArg(fmt.Sprintf("template_yaml: proxy-groups[%d]: name must be a non-empty string", i))
		}
		name = strings.TrimSpace(name)
		if names[name] {
			return invalidArg(fmt.Sprintf("template_yaml: proxy-groups: duplicate group name %q", name))
		}
		names[name] = true
	}
	return nil
}

func validateRules(root map[string]any) *ServiceError {
	raw, ok := root["rules"]
	if !ok {
		return invalidArg("template_yaml: rules is required and must be a non-empty sequence of strings")
	}
	if raw == nil {
		return invalidArg("template_yaml: rules is required and must be a non-empty sequence of strings")
	}
	rules, ok := raw.([]any)
	if !ok {
		return invalidArg("template_yaml: rules must be a sequence (array)")
	}
	if len(rules) == 0 {
		return invalidArg("template_yaml: rules must be a non-empty sequence")
	}
	for i, r := range rules {
		_, ok := r.(string)
		if !ok {
			return invalidArg(fmt.Sprintf("template_yaml: rules[%d]: must be a string", i))
		}
	}

	// Last rule must be "MATCH,<target>"
	lastRule, ok := rules[len(rules)-1].(string)
	if !ok {
		return invalidArg("template_yaml: last rule must be a string")
	}
	trimmed := strings.TrimSpace(lastRule)
	if !strings.HasPrefix(trimmed, "MATCH,") {
		return invalidArg("template_yaml: last rule must start with 'MATCH,' (e.g. 'MATCH,Proxy')")
	}
	target := strings.TrimSpace(trimmed[len("MATCH,"):])
	if target == "" {
		return invalidArg("template_yaml: MATCH rule must have a non-empty target (e.g. 'MATCH,Proxy')")
	}
	return nil
}

func validateProviders(root map[string]any, key string) *ServiceError {
	raw, ok := root[key]
	if !ok {
		return nil // optional
	}
	if raw == nil {
		return nil // null is fine
	}
	providers, ok := raw.(map[string]any)
	if !ok {
		return invalidArg(fmt.Sprintf("template_yaml: %s must be a mapping if present", key))
	}
	for name, item := range providers {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return invalidArg(fmt.Sprintf("template_yaml: %s.%s: each provider must be a mapping", key, name))
		}
		if err := validateProviderItem(key, name, itemMap); err != nil {
			return err
		}
	}
	return nil
}

func validateProviderItem(providerKey, name string, item map[string]any) *ServiceError {
	typeRaw, ok := item["type"]
	if !ok {
		return nil // type not specified, skip URL checks
	}
	typeStr, ok := typeRaw.(string)
	if !ok {
		return invalidArg(fmt.Sprintf("template_yaml: %s.%s.type: must be a string", providerKey, name))
	}
	if !strings.EqualFold(strings.TrimSpace(typeStr), "http") {
		return nil // non-http type, skip URL checks
	}

	// type is http — url must be present, absolute HTTPS.
	urlRaw, ok := item["url"]
	if !ok {
		return invalidArg(fmt.Sprintf("template_yaml: %s.%s: url is required when type is http", providerKey, name))
	}
	urlStr, ok := urlRaw.(string)
	if !ok {
		return invalidArg(fmt.Sprintf("template_yaml: %s.%s.url: must be a string", providerKey, name))
	}
	urlStr = strings.TrimSpace(urlStr)
	if urlStr == "" {
		return invalidArg(fmt.Sprintf("template_yaml: %s.%s.url: must not be empty", providerKey, name))
	}

	parsed, err := url.Parse(urlStr)
	if err != nil {
		return invalidArg(fmt.Sprintf("template_yaml: %s.%s.url: invalid URL: %v", providerKey, name, err))
	}
	if parsed.Scheme != "https" {
		return invalidArg(fmt.Sprintf("template_yaml: %s.%s.url: scheme must be https, got %q", providerKey, name, parsed.Scheme))
	}
	if parsed.Host == "" {
		return invalidArg(fmt.Sprintf("template_yaml: %s.%s.url: host must not be empty", providerKey, name))
	}
	if parsed.User != nil {
		return invalidArg(fmt.Sprintf("template_yaml: %s.%s.url: userinfo is not allowed", providerKey, name))
	}
	if parsed.Fragment != "" {
		return invalidArg(fmt.Sprintf("template_yaml: %s.%s.url: fragment is not allowed", providerKey, name))
	}

	return nil
}
