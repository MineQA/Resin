package service

import (
	"testing"
)

func TestValidateRuleProfileTemplate_ValidMinimal(t *testing.T) {
	yaml := "rules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidateRuleProfileTemplate_ValidFull(t *testing.T) {
	yaml := `
proxy-groups:
  - name: PROXY
    type: select
    include-all-proxies: true
  - name: US
    type: url-test
    include-all-proxies: true
    filter: "(?i)\\[US\\]"
rule-providers:
  reject:
    type: http
    url: https://example.com/reject.yaml
    interval: 86400
    behavior: classical
  ai:
    type: http
    url: https://example.com/ai.yaml
    interval: 86400
    behavior: classical
proxy-providers:
  custom:
    type: http
    url: https://example.com/custom.yaml
    interval: 3600
rules:
  - RULE-SET,reject,REJECT
  - RULE-SET,ai,PROXY
  - MATCH,PROXY
mode: Rule
ipv6: true
`
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("expected valid full template, got: %v", err)
	}
}

func TestValidateRuleProfileTemplate_InvalidYAML(t *testing.T) {
	yaml := "{invalid: yaml: unclosed"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidateRuleProfileTemplate_MultiDocument(t *testing.T) {
	yaml := "rules:\n  - MATCH,Proxy\n---\nrules:\n  - MATCH,DIRECT\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for multi-document YAML")
	}
}

func TestValidateRuleProfileTemplate_TrailingMalformed(t *testing.T) {
	yaml := "rules:\n  - MATCH,Proxy\n---\n{invalid garbage yaml\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for trailing malformed YAML")
	}
}

func TestValidateRuleProfileTemplate_NotMapping(t *testing.T) {
	yaml := "- just\n- a\n- list\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-mapping top-level")
	}
}

func TestValidateRuleProfileTemplate_ProxiesNonEmpty(t *testing.T) {
	yaml := "proxies:\n  - name: myproxy\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-empty proxies")
	}
}

func TestValidateRuleProfileTemplate_ProxiesNotSequence(t *testing.T) {
	yaml := "proxies: bad\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-sequence proxies")
	}
}

func TestValidateRuleProfileTemplate_ProxiesNull(t *testing.T) {
	yaml := "proxies:\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("null proxies should be valid: %v", err)
	}
}

func TestValidateRuleProfileTemplate_ProxiesEmpty(t *testing.T) {
	yaml := "proxies: []\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("empty proxies should be valid: %v", err)
	}
}

func TestValidateRuleProfileTemplate_ProxyGroupsNotSequence(t *testing.T) {
	yaml := "proxy-groups: bad\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-sequence proxy-groups")
	}
}

func TestValidateRuleProfileTemplate_ProxyGroupsItemNotMapping(t *testing.T) {
	yaml := "proxy-groups:\n  - just a string\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-mapping group item")
	}
}

func TestValidateRuleProfileTemplate_ProxyGroupsMissingName(t *testing.T) {
	yaml := "proxy-groups:\n  - type: select\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateRuleProfileTemplate_ProxyGroupsEmptyName(t *testing.T) {
	yaml := "proxy-groups:\n  - name: \"\"\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidateRuleProfileTemplate_ProxyGroupsDuplicateName(t *testing.T) {
	yaml := `proxy-groups:
  - name: PROXY
  - name: PROXY
rules:
  - MATCH,Proxy
`
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for duplicate group name")
	}
}

func TestValidateRuleProfileTemplate_ProxyGroupsNull(t *testing.T) {
	yaml := "proxy-groups:\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("null proxy-groups should be valid: %v", err)
	}
}

func TestValidateRuleProfileTemplate_RulesRequired(t *testing.T) {
	yaml := "proxy-groups:\n  - name: PROXY\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for missing rules")
	}
}

func TestValidateRuleProfileTemplate_RulesNull(t *testing.T) {
	yaml := "rules:\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for null rules")
	}
}

func TestValidateRuleProfileTemplate_RulesNotSequence(t *testing.T) {
	yaml := "rules: bad\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-sequence rules")
	}
}

func TestValidateRuleProfileTemplate_RulesEmpty(t *testing.T) {
	yaml := "rules: []\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for empty rules")
	}
}

func TestValidateRuleProfileTemplate_RulesItemNotString(t *testing.T) {
	yaml := "rules:\n  - 123\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-string rule item")
	}
}

func TestValidateRuleProfileTemplate_LastRuleNotMATCH(t *testing.T) {
	yaml := "rules:\n  - DOMAIN-SUFFIX,example.com,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for last rule not starting with MATCH,")
	}
}

func TestValidateRuleProfileTemplate_LastRuleMATCHNoTarget(t *testing.T) {
	yaml := "rules:\n  - DOMAIN-SUFFIX,example.com,Proxy\n  - MATCH,\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for MATCH with empty target")
	}
}

func TestValidateRuleProfileTemplate_ProviderNotMapping(t *testing.T) {
	yaml := "rule-providers:\n  - bad\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-mapping rule-providers")
	}
}

func TestValidateRuleProfileTemplate_ProviderItemNotMapping(t *testing.T) {
	yaml := "rule-providers:\n  reject: just a string\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-mapping provider item")
	}
}

func TestValidateRuleProfileTemplate_ProviderHTTPNoURL(t *testing.T) {
	yaml := "rule-providers:\n  reject:\n    type: http\n    behavior: classical\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for http provider without url")
	}
}

func TestValidateRuleProfileTemplate_ProviderHTTPNonHTTPS(t *testing.T) {
	yaml := "rule-providers:\n  reject:\n    type: http\n    url: http://example.com/r.yaml\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for non-https url")
	}
}

func TestValidateRuleProfileTemplate_ProviderHTTPUserinfo(t *testing.T) {
	yaml := "rule-providers:\n  reject:\n    type: http\n    url: https://user:pass@example.com/r.yaml\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for url with userinfo")
	}
}

func TestValidateRuleProfileTemplate_ProviderHTTPEmptyHost(t *testing.T) {
	yaml := "rule-providers:\n  reject:\n    type: http\n    url: https:///path\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for url with empty host")
	}
}

func TestValidateRuleProfileTemplate_ProviderHTTPFragment(t *testing.T) {
	yaml := "rule-providers:\n  reject:\n    type: http\n    url: https://example.com/r.yaml#section\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err == nil {
		t.Fatal("expected error for url with fragment")
	}
}

func TestValidateRuleProfileTemplate_ProviderNonHTTPNoURLCheck(t *testing.T) {
	yaml := "rule-providers:\n  reject:\n    type: file\n    behavior: classical\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("non-http provider should skip url validation: %v", err)
	}
}

func TestValidateRuleProfileTemplate_UnknownKeysAllowed(t *testing.T) {
	yaml := "mode: Rule\nipv6: true\nport: 7890\nsocks-port: 7891\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("unknown top-level keys should be allowed: %v", err)
	}
}

func TestValidateRuleProfileTemplate_TypeCaseInsensitive(t *testing.T) {
	// type: HTTP should be treated same as type: http
	yaml := "rule-providers:\n  reject:\n    type: HTTP\n    url: https://example.com/r.yaml\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("case-insensitive type should be valid: %v", err)
	}
}

func TestValidateRuleProfileTemplate_ProxyProvidersHTTP(t *testing.T) {
	yaml := "proxy-providers:\n  custom:\n    type: http\n    url: https://example.com/custom.yaml\nrules:\n  - MATCH,Proxy\n"
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("proxy-providers http should be valid: %v", err)
	}
}

func TestValidateRuleProfileTemplate_MATCHTargetTrimmed(t *testing.T) {
	yaml := "rules:\n  - MATCH,  Proxy  \n"
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("MATCH target with surrounding spaces should be valid: %v", err)
	}
}

func TestValidateRuleProfileTemplate_YAMLAnchorsAllowed(t *testing.T) {
	// Templates may use YAML anchors/aliases; the validator must not reject them.
	// anchors.yaml is decoded to generic any and aliases are resolved — only the
	// resolved values are checked.
	yaml := `
proxies: &dynamic []

probe-copy: *dynamic

proxy-groups:
  - name: AUTO
    type: url-test
    include-all-proxies: true

rules:
  - DOMAIN,example.com,AUTO
  - MATCH,PROXY
`
	if err := ValidateRuleProfileTemplate(yaml); err != nil {
		t.Fatalf("YAML anchors should be allowed: %v", err)
	}
}
