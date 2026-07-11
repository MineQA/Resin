package node

import (
	"encoding/json"
	"sort"
	"testing"
)

// ---------- NormalizeProtocol ----------

func TestNormalizeProtocol(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Known aliases -> canonical
		{"shadowsocks", "shadowsocks"},
		{"ss", "shadowsocks"},
		{"SS", "shadowsocks"},
		{"  ss  ", "shadowsocks"},
		{"Shadowsocks", "shadowsocks"},
		{"vmess", "vmess"},
		{"vmess1", "vmess"},
		{"VMESS", "vmess"},
		{"trojan", "trojan"},
		{"TROJAN", "trojan"},
		{"vless", "vless"},
		{"hysteria2", "hysteria2"},
		{"hy2", "hysteria2"},
		{"HY2", "hysteria2"},
		{"http", "http"},
		{"HTTP", "http"},
		{"socks", "socks"},
		{"socks5", "socks"},
		{"SOCKS5", "socks"},
		// Unknown
		{"tuic", ""},
		{"wireguard", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := NormalizeProtocol(tt.input); got != tt.want {
				t.Errorf("NormalizeProtocol(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------- RawOptionsProtocol ----------

func TestRawOptionsProtocol_Valid(t *testing.T) {
	raw := json.RawMessage(`{"type":"ss","server":"1.1.1.1"}`)
	if got := RawOptionsProtocol(raw); got != "shadowsocks" {
		t.Errorf("RawOptionsProtocol = %q, want %q", got, "shadowsocks")
	}
}

func TestRawOptionsProtocol_MissingType(t *testing.T) {
	raw := json.RawMessage(`{"server":"1.1.1.1"}`)
	if got := RawOptionsProtocol(raw); got != "" {
		t.Errorf("RawOptionsProtocol = %q, want empty", got)
	}
}

func TestRawOptionsProtocol_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`{invalid}`)
	if got := RawOptionsProtocol(raw); got != "" {
		t.Errorf("RawOptionsProtocol = %q, want empty", got)
	}
}

func TestRawOptionsProtocol_EmptyType(t *testing.T) {
	raw := json.RawMessage(`{"type":""}`)
	if got := RawOptionsProtocol(raw); got != "" {
		t.Errorf("RawOptionsProtocol = %q, want empty", got)
	}
}

func TestRawOptionsProtocol_NonStringType(t *testing.T) {
	raw := json.RawMessage(`{"type":123}`)
	if got := RawOptionsProtocol(raw); got != "" {
		t.Errorf("RawOptionsProtocol = %q, want empty", got)
	}
}

func TestRawOptionsProtocol_UnsupportedType(t *testing.T) {
	raw := json.RawMessage(`{"type":"tuic"}`)
	if got := RawOptionsProtocol(raw); got != "" {
		t.Errorf("RawOptionsProtocol = %q, want empty", got)
	}
}

func TestRawOptionsProtocol_NilInput(t *testing.T) {
	if got := RawOptionsProtocol(nil); got != "" {
		t.Errorf("RawOptionsProtocol(nil) = %q, want empty", got)
	}
}

// ---------- CanonicalProtocols ----------

func TestCanonicalProtocols_Sorted(t *testing.T) {
	if !sort.StringsAreSorted(CanonicalProtocols) {
		t.Errorf("CanonicalProtocols is not sorted: %v", CanonicalProtocols)
	}
}

func TestCanonicalProtocols_NoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range CanonicalProtocols {
		if seen[p] {
			t.Errorf("duplicate canonical protocol: %s", p)
		}
		seen[p] = true
	}
}

func TestCanonicalProtocols_ContainsExpectedSet(t *testing.T) {
	expected := []string{"shadowsocks", "vmess", "trojan", "vless", "hysteria2", "http", "socks"}
	for _, exp := range expected {
		found := false
		for _, p := range CanonicalProtocols {
			if p == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CanonicalProtocols missing expected protocol: %s", exp)
		}
	}
	if len(CanonicalProtocols) != len(expected) {
		t.Errorf("CanonicalProtocols length = %d, want %d; got %v", len(CanonicalProtocols), len(expected), CanonicalProtocols)
	}
}
