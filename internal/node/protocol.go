package node

import (
	"encoding/json"
	"sort"
	"strings"
)

// protocolAliases maps lower-cased input protocol names to their canonical form.
//
// Canonical protocol names are used for filtering and display in the UI/API.
// Export converters (Clash/URI/base64) support a narrower set; sing-box format
// preserves RawOptions as-is and does not depend on this map.
var protocolAliases = map[string]string{
	"shadowsocks": "shadowsocks",
	"ss":          "shadowsocks",
	"vmess":       "vmess",
	"vmess1":      "vmess",
	"trojan":      "trojan",
	"vless":       "vless",
	"hysteria2":   "hysteria2",
	"hy2":         "hysteria2",
	"anytls":      "anytls",
	"http":        "http",
	"socks":       "socks",
	"socks5":      "socks",
}

// CanonicalProtocols is the sorted list of all recognised canonical protocol names.
var CanonicalProtocols = func() []string {
	seen := make(map[string]bool)
	var result []string
	for _, v := range protocolAliases {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	sort.Strings(result)
	return result
}()

// NormalizeProtocol returns the canonical protocol name for a given input,
// or empty string if the protocol is not recognised.
func NormalizeProtocol(input string) string {
	if input == "" {
		return ""
	}
	canonical, ok := protocolAliases[strings.ToLower(strings.TrimSpace(input))]
	if !ok {
		return ""
	}
	return canonical
}

// RawOptionsProtocol extracts and normalises the protocol type from a node's
// RawOptions JSON. Returns empty string when the type field is missing,
// non-string, empty, or not a recognised protocol.
func RawOptionsProtocol(raw json.RawMessage) string {
	var outbound struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &outbound); err != nil {
		return ""
	}
	rawType := strings.TrimSpace(outbound.Type)
	if rawType == "" {
		return ""
	}
	return NormalizeProtocol(rawType)
}
