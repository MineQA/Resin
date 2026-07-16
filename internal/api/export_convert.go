package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Internal structs for TLS and transport metadata
// ---------------------------------------------------------------------------

// tlsInfo holds extracted TLS fields from a sing-box outbound map.
type tlsInfo struct {
	Enabled           bool
	SNI               string
	SkipCertVerify    bool
	ALPN              []string
	ClientFingerprint string // tls.utls.fingerprint → Mihomo client-fingerprint
	RealityPublicKey  string // tls.reality.public_key
	RealityShortID    string // tls.reality.short_id
}

// transportInfo holds extracted transport fields from a sing-box outbound map.
type transportInfo struct {
	Network             string // ws, grpc, http, h2, quic, tcp, websocket
	Path                string
	Host                string            // primary host (Host header for WS, first host for HTTP/H2)
	Hosts               []string          // all hosts (HTTP/H2 transport)
	ServiceName         string            // gRPC service_name
	Headers             map[string]string // WS headers
	MaxEarlyData        int               // WS max_early_data (numeric)
	EarlyDataHeaderName string            // WS early_data_header_name
}

// ---------------------------------------------------------------------------
// Clash proxy conversion
// ---------------------------------------------------------------------------

// outboundToClashProxy converts an ExportOutbound to a Clash proxy map.
// Returns nil when the node type is unsupported or critical fields are missing.
func outboundToClashProxy(o ExportOutbound) map[string]any {
	if o.Tag == "" || len(o.Raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(o.Raw, &m); err != nil {
		return nil
	}
	typ, _ := m["type"].(string)
	server, _ := m["server"].(string)
	if server == "" {
		return nil
	}
	port := pickPort(m)
	if port <= 0 {
		return nil
	}

	switch typ {
	case "shadowsocks", "ss":
		return convertShadowsocks(o.Tag, m, server, port)
	case "vmess", "vmess1":
		return convertVMess(o.Tag, m, server, port)
	case "trojan":
		return convertTrojan(o.Tag, m, server, port)
	case "vless":
		return convertVLess(o.Tag, m, server, port)
	case "hysteria2", "hy2":
		return convertHysteria2(o.Tag, m, server, port)
	case "hysteria":
		return convertHysteria1(o.Tag, m, server, port)
	case "tuic":
		return convertTUIC(o.Tag, m, server, port)
	case "http":
		return convertHTTP(o.Tag, m, server, port)
	case "socks", "socks5":
		return convertSocks(o.Tag, m, server, port)
	default:
		return nil
	}
}

func pickPort(m map[string]any) int {
	if v, ok := m["server_port"]; ok {
		if f, ok := toFloat(v); ok {
			return int(f)
		}
	}
	if v, ok := m["port"]; ok {
		if f, ok := toFloat(v); ok {
			return int(f)
		}
	}
	return 0
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// toNumberOrFloat returns n as int when mathematically integral, float64 otherwise.
// This preserves canonical integer style for whole numbers while supporting
// fractional values like 10.5.
func toNumberOrFloat(n float64) any {
	if n == float64(int64(n)) {
		return int(n)
	}
	return n
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case float64:
		return b != 0
	case int:
		return b != 0
	case int64:
		return b != 0
	case string:
		return b == "true" || b == "1"
	}
	return false
}

// parseHostList converts a sing-box host value (string, []string, or []any) to []string.
func parseHostList(v any) []string {
	switch t := v.(type) {
	case string:
		if s := strings.TrimSpace(t); s != "" {
			return []string{s}
		}
	case []string:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(toString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// applyClashTransport applies transport fields to a Clash proxy map for
// VMess, Trojan, and VLESS.  It sets the "network" key and the appropriate
// nested opts block (ws-opts, grpc-opts, http-opts, h2-opts).
//
// Normalization notes for transport.type:
//   - "http" → network "http"  with http-opts  (HTTP/1.1 CONNECT)
//   - "h2"   → network "h2"    with h2-opts    (HTTP/2)
//
// Direct raw sing-box data may contain "h2" and it is preserved here.
// However, after Clash ingestion (internal/subscription/parser.go) both
// Clash network=h2 and network=http are normalized to transport.type=http,
// so subscription-derived canonical data always emits http/http-opts.
func applyClashTransport(proxy map[string]any, ti transportInfo) {
	if ti.Network == "" || ti.Network == "tcp" {
		return
	}
	proxy["network"] = ti.Network

	switch ti.Network {
	case "ws", "websocket":
		opts := make(map[string]any)
		if ti.Path != "" {
			opts["path"] = ti.Path
		}
		if len(ti.Headers) > 0 {
			opts["headers"] = ti.Headers
		}
		if ti.MaxEarlyData > 0 {
			opts["max-early-data"] = ti.MaxEarlyData
		}
		if ti.EarlyDataHeaderName != "" {
			opts["early-data-header-name"] = ti.EarlyDataHeaderName
		}
		if len(opts) > 0 {
			proxy["ws-opts"] = opts
		}

	case "grpc":
		opts := make(map[string]any)
		if ti.ServiceName != "" {
			opts["grpc-service-name"] = ti.ServiceName
		}
		if len(opts) > 0 {
			proxy["grpc-opts"] = opts
		}

	case "http":
		opts := make(map[string]any)
		if ti.Path != "" {
			opts["path"] = ti.Path
		}
		if len(ti.Hosts) > 0 {
			opts["host"] = ti.Hosts
		} else if ti.Host != "" {
			opts["host"] = ti.Host
		}
		if len(opts) > 0 {
			proxy["http-opts"] = opts
		}

	case "h2":
		// h2 branch: direct/raw sing-box outbound data may contain "h2".
		// Clash subscription canonical (internal/subscription/parser.go)
		// normalises both Clash network=h2 and network=http to
		// transport.type=http, so canonical data always hits the "http"
		// case above, never "h2".
		opts := make(map[string]any)
		if ti.Path != "" {
			opts["path"] = ti.Path
		}
		if len(ti.Hosts) > 0 {
			opts["host"] = ti.Hosts
		} else if ti.Host != "" {
			opts["host"] = ti.Host
		}
		if len(opts) > 0 {
			proxy["h2-opts"] = opts
		}
	}
}

// applyClashTLS applies TLS fields from tlsInfo to a Clash proxy map.
// sniKey controls the output key for the SNI value ("servername" for
// VMess/VLESS, "sni" for Trojan).  emitDisabled controls whether tls: false
// is written when TLS is not enabled (true for VMess to match its observable
// behaviour; false for Trojan/VLESS which omit the key).
func applyClashTLS(proxy map[string]any, ti tlsInfo, sniKey string, emitDisabled bool) {
	if ti.Enabled {
		proxy["tls"] = true
		if ti.SNI != "" {
			proxy[sniKey] = ti.SNI
		}
		if ti.SkipCertVerify {
			proxy["skip-cert-verify"] = true
		}
		if len(ti.ALPN) > 0 {
			proxy["alpn"] = ti.ALPN
		}
		if ti.ClientFingerprint != "" {
			proxy["client-fingerprint"] = ti.ClientFingerprint
		}
		if ti.RealityPublicKey != "" || ti.RealityShortID != "" {
			reality := make(map[string]any)
			if ti.RealityPublicKey != "" {
				reality["public-key"] = ti.RealityPublicKey
			}
			if ti.RealityShortID != "" {
				reality["short-id"] = ti.RealityShortID
			}
			proxy["reality-opts"] = reality
		}
	} else if emitDisabled {
		proxy["tls"] = false
	}
}

// extractTLS extracts TLS fields from a sing-box outbound map into a tlsInfo struct.
func extractTLS(m map[string]any) tlsInfo {
	var info tlsInfo
	tlsObj, ok := m["tls"].(map[string]any)
	if !ok {
		if enabled, exists := m["tls"]; exists {
			if b, ok := enabled.(bool); ok && b {
				info.Enabled = true
			}
		}
		return info
	}
	if v, ok := tlsObj["enabled"]; ok {
		info.Enabled = toBool(v)
	} else {
		info.Enabled = true
	}
	info.SNI, _ = tlsObj["server_name"].(string)
	if info.SNI == "" {
		info.SNI, _ = tlsObj["sni"].(string)
	}
	if v, ok := tlsObj["insecure"]; ok {
		info.SkipCertVerify = toBool(v)
	}
	if v, ok := tlsObj["alpn"]; ok {
		switch arr := v.(type) {
		case string:
			if s := strings.TrimSpace(arr); s != "" {
				info.ALPN = []string{s}
			}
		case []any:
			info.ALPN = make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					info.ALPN = append(info.ALPN, s)
				}
			}
		case []string:
			info.ALPN = arr
		}
	}
	// uTLS client fingerprint
	if utls, ok := tlsObj["utls"].(map[string]any); ok {
		if fp, _ := utls["fingerprint"].(string); fp != "" {
			info.ClientFingerprint = fp
		}
	}
	// Reality options
	if reality, ok := tlsObj["reality"].(map[string]any); ok {
		if pk, _ := reality["public_key"].(string); pk != "" {
			info.RealityPublicKey = pk
		}
		if sid, _ := reality["short_id"].(string); sid != "" {
			info.RealityShortID = sid
		}
	}
	return info
}

// extractTransport extracts transport fields from a sing-box outbound map into a transportInfo struct.
func extractTransport(m map[string]any) transportInfo {
	var info transportInfo
	t, ok := m["transport"].(map[string]any)
	if !ok {
		return info
	}
	info.Network, _ = t["type"].(string)
	switch info.Network {
	case "ws", "websocket":
		if wp, ok := t["path"].(string); ok {
			info.Path = wp
		}
		if h, ok := t["headers"].(map[string]any); ok {
			info.Headers = make(map[string]string, len(h))
			for k, v := range h {
				info.Headers[k] = toString(v)
			}
		}
		info.Host = info.Headers["Host"]
		// max_early_data is numeric in sing-box
		if ed, ok := t["max_early_data"]; ok {
			switch n := ed.(type) {
			case float64:
				info.MaxEarlyData = int(n)
			case int:
				info.MaxEarlyData = n
			case int64:
				info.MaxEarlyData = int(n)
			case json.Number:
				if i, err := n.Int64(); err == nil {
					info.MaxEarlyData = int(i)
				}
			}
		}
		if edn, ok := t["early_data_header_name"].(string); ok {
			info.EarlyDataHeaderName = edn
		}
	case "grpc":
		info.ServiceName, _ = t["service_name"].(string)
	case "http", "h2":
		if p, ok := t["path"].(string); ok {
			info.Path = p
		}
		// host can be string, []string, or []any in the canonical store
		if h, ok := t["host"]; ok {
			info.Hosts = parseHostList(h)
			if len(info.Hosts) > 0 {
				info.Host = info.Hosts[0]
			}
		}
	case "quic":
		// nothing extra needed
	}
	return info
}

func convertShadowsocks(tag string, m map[string]any, server string, port int) map[string]any {
	method, _ := m["method"].(string)
	password, _ := m["password"].(string)
	if method == "" || password == "" {
		return nil
	}
	proxy := map[string]any{
		"name":     tag,
		"type":     "ss",
		"server":   server,
		"port":     port,
		"cipher":   method,
		"password": password,
		"udp":      true,
	}

	plugin, _ := m["plugin"].(string)
	pluginOpts, _ := m["plugin_opts"].(string)
	if plugin == "" {
		return proxy
	}

	// Map canonical plugin name to Mihomo name
	switch strings.ToLower(strings.TrimSpace(plugin)) {
	case "obfs-local", "simple-obfs":
		proxy["plugin"] = "obfs"
	case "v2ray-plugin":
		proxy["plugin"] = "v2ray-plugin"
	default:
		proxy["plugin"] = plugin
	}

	if pluginOpts == "" {
		return proxy
	}

	parsed := parsePluginOpts(pluginOpts)
	if len(parsed) == 0 {
		return proxy
	}

	// Convert canonical plugin_opts keys to Mihomo nested plugin-opts
	switch strings.ToLower(strings.TrimSpace(plugin)) {
	case "obfs-local", "simple-obfs":
		proxy["plugin-opts"] = convertSimpleObfsOpts(parsed)
	case "v2ray-plugin":
		proxy["plugin-opts"] = convertV2RayPluginOpts(parsed)
	default:
		proxy["plugin-opts"] = parsed
	}
	return proxy
}

// parsePluginOpts parses a semicolon-delimited canonical plugin_opts string
// with backslash-escaped \;, \=, and \\ into a map. Bare flags (no =value) become
// bool true. Empty keys or fragments are silently skipped. Never panics.
func parsePluginOpts(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	parts := splitUnescapedExport(raw, ';')
	opts := make(map[string]any, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		keyRaw, valueRaw, hasValue := cutUnescapedExport(part, '=')
		key := strings.TrimSpace(keyRaw)
		if key == "" {
			continue
		}
		if hasValue {
			value := strings.TrimSpace(valueRaw)
			opts[key] = unescapeValue(value)
		} else {
			opts[key] = true // bare flag
		}
	}
	if len(opts) == 0 {
		return nil
	}
	return opts
}

// splitUnescapedExport splits on delimiter but respects \-escaped delimiters.
func splitUnescapedExport(input string, delimiter byte) []string {
	parts := make([]string, 0, 1)
	start := 0
	escaped := false
	for i := 0; i < len(input); i++ {
		switch {
		case escaped:
			escaped = false
		case input[i] == '\\':
			escaped = true
		case input[i] == delimiter:
			parts = append(parts, input[start:i])
			start = i + 1
		}
	}
	return append(parts, input[start:])
}

// cutUnescapedExport splits on the first unescaped delimiter, returning (before, after, true)
// or (input, "", false) when no delimiter is found.
func cutUnescapedExport(input string, delimiter byte) (string, string, bool) {
	escaped := false
	for i := 0; i < len(input); i++ {
		switch {
		case escaped:
			escaped = false
		case input[i] == '\\':
			escaped = true
		case input[i] == delimiter:
			return input[:i], input[i+1:], true
		}
	}
	return input, "", false
}

// unescapeValue decodes documented backslash-escaped sequences (\;, \=, \\) in a
// value string.  For a backslash before any other character, both the backslash
// and the character are preserved.  A trailing backslash is preserved.
func unescapeValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			switch c {
			case ';', '=', '\\':
				b.WriteByte(c)
			default:
				b.WriteByte('\\')
				b.WriteByte(c)
			}
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		b.WriteByte(c)
	}
	if escaped {
		b.WriteByte('\\')
	}
	return b.String()
}

// convertSimpleObfsOpts maps canonical obfs-local options to Mihomo obfs plugin-opts.
//
//	obfs       → mode
//	obfs-host  → host
//	host       → host
//	unknown    → preserved as string
func convertSimpleObfsOpts(opts map[string]any) map[string]any {
	mapped := make(map[string]any, len(opts))
	for k, v := range opts {
		switch strings.ToLower(k) {
		case "obfs":
			mapped["mode"] = v
		case "obfs-host", "obfs_host", "host":
			mapped["host"] = v
		default:
			mapped[k] = v
		}
	}
	if len(mapped) == 0 {
		return nil
	}
	return mapped
}

// v2rayBoolFromString converts a v2ray-plugin boolean option string to its
// typed value.  Recognized true/false strings (case-insensitive, trimmed) and
// the aliases "1"/"0" produce bool values.  Unrecognized strings are returned
// as-is so they are preserved rather than silently coerced.
func v2rayBoolFromString(s string) any {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "true") || s == "1" {
		return true
	}
	if strings.EqualFold(s, "false") || s == "0" {
		return false
	}
	return s
}

// convertV2RayPluginOpts maps canonical v2ray-plugin options to Mihomo plugin-opts.
// Known booleans (tls, mux, skip-cert-verify, v2ray-http-upgrade) are typed as bool.
// Canonical mux=4 is reversed to mux: true.
// Known string keys (mode, host, path) are preserved as-is.
// Unknown keys are passed through as their parsed type.
func convertV2RayPluginOpts(opts map[string]any) map[string]any {
	knownBools := map[string]bool{
		"tls": true, "mux": true, "skip-cert-verify": true, "v2ray-http-upgrade": true,
	}
	mapped := make(map[string]any, len(opts))
	for k, v := range opts {
		kl := strings.ToLower(k)
		if knownBools[kl] {
			if b, ok := v.(bool); ok {
				mapped[kl] = b
				continue
			}
			if s, ok := v.(string); ok {
				if kl == "mux" && s == "4" {
					// Reverse parser normalization: mux=true → mux=4
					mapped["mux"] = true
					continue
				}
				mapped[kl] = v2rayBoolFromString(s)
				continue
			}
			// Non-string, non-bool → preserve as-is
			mapped[kl] = v
			continue
		}
		// Known string keys
		switch kl {
		case "mode", "host", "path":
			mapped[kl] = v
		default:
			mapped[k] = v
		}
	}
	if len(mapped) == 0 {
		return nil
	}
	return mapped
}

func convertVMess(tag string, m map[string]any, server string, port int) map[string]any {
	uuid, _ := m["uuid"].(string)
	if uuid == "" {
		return nil
	}
	alterID := 0
	if v, ok := m["alter_id"]; ok {
		if f, ok := toFloat(v); ok {
			alterID = int(f)
		}
	}
	security, _ := m["security"].(string)
	if security == "" {
		security, _ = m["cipher"].(string)
	}
	if security == "" {
		security = "auto"
	}
	ti := extractTLS(m)
	tr := extractTransport(m)

	proxy := map[string]any{
		"name":    tag,
		"type":    "vmess",
		"server":  server,
		"port":    port,
		"uuid":    uuid,
		"alterId": alterID,
		"cipher":  security,
		"udp":     true,
		"network": "tcp",
	}
	// TLS
	applyClashTLS(proxy, ti, "servername", true)
	// Transport (shared function for VMess/Trojan/VLESS)
	applyClashTransport(proxy, tr)
	return proxy
}

func convertTrojan(tag string, m map[string]any, server string, port int) map[string]any {
	password, _ := m["password"].(string)
	if password == "" {
		return nil
	}
	ti := extractTLS(m)
	tr := extractTransport(m)

	proxy := map[string]any{
		"name":     tag,
		"type":     "trojan",
		"server":   server,
		"port":     port,
		"password": password,
		"udp":      true,
	}
	// TLS
	applyClashTLS(proxy, ti, "sni", false)
	// Transport (shared function for VMess/Trojan/VLESS)
	applyClashTransport(proxy, tr)
	return proxy
}

func convertVLess(tag string, m map[string]any, server string, port int) map[string]any {
	uuid, _ := m["uuid"].(string)
	if uuid == "" {
		return nil
	}
	flow, _ := m["flow"].(string)
	ti := extractTLS(m)
	tr := extractTransport(m)

	proxy := map[string]any{
		"name":   tag,
		"type":   "vless",
		"server": server,
		"port":   port,
		"uuid":   uuid,
		"udp":    true,
	}
	if flow != "" {
		proxy["flow"] = flow
	}
	// TLS
	applyClashTLS(proxy, ti, "servername", false)
	// Transport (shared function for VMess/Trojan/VLESS)
	applyClashTransport(proxy, tr)
	return proxy
}

func convertHysteria2(tag string, m map[string]any, server string, port int) map[string]any {
	password, _ := m["password"].(string)
	if password == "" {
		return nil
	}
	ti := extractTLS(m)

	proxy := map[string]any{
		"name":     tag,
		"type":     "hysteria2",
		"server":   server,
		"port":     port, // integer server_port; NOT replaced by ports
		"password": password,
		"udp":      true,
	}
	// TLS
	if ti.Enabled {
		proxy["tls"] = true
	}
	if ti.SNI != "" {
		proxy["sni"] = ti.SNI
	}
	if ti.SkipCertVerify {
		proxy["skip-cert-verify"] = true
	}
	if len(ti.ALPN) > 0 {
		proxy["alpn"] = ti.ALPN
	}
	if ti.ClientFingerprint != "" {
		proxy["client-fingerprint"] = ti.ClientFingerprint
	}
	// server_ports → ports (scalar string, not port replacement)
	if portsStr := convertServerPorts(m); portsStr != "" {
		proxy["ports"] = portsStr
	}
	// hop_interval → hop-interval (seconds)
	if hopSec, ok := convertHopInterval(m); ok {
		proxy["hop-interval"] = hopSec
	}
	// up_mbps → up, down_mbps → down (Mihomo unitless defaults Mbps)
	if up, ok := m["up_mbps"]; ok {
		if n, ok := toFloat(up); ok && n > 0 {
			proxy["up"] = toNumberOrFloat(n)
		}
	}
	if down, ok := m["down_mbps"]; ok {
		if n, ok := toFloat(down); ok && n > 0 {
			proxy["down"] = toNumberOrFloat(n)
		}
	}
	// obfs: nested canonical takes precedence; then legacy scalar
	convertHY2Obfs(m, proxy)
	return proxy
}

// convertServerPorts reads canonical server_ports ([]any, []string, or string)
// and returns a Mihomo ports scalar: ":" replaced with "-", entries joined with ",".
func convertServerPorts(m map[string]any) string {
	v, ok := m["server_ports"]
	if !ok || v == nil {
		return ""
	}
	var entries []string
	switch sv := v.(type) {
	case []any:
		for _, item := range sv {
			s := strings.TrimSpace(toString(item))
			if s == "" {
				continue
			}
			entries = append(entries, strings.ReplaceAll(s, ":", "-"))
		}
	case []string:
		for _, s := range sv {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			entries = append(entries, strings.ReplaceAll(s, ":", "-"))
		}
	case string:
		s := strings.TrimSpace(sv)
		if s != "" {
			entries = append(entries, strings.ReplaceAll(s, ":", "-"))
		}
	}
	if len(entries) == 0 {
		return ""
	}
	return strings.Join(entries, ",")
}

// convertHopInterval reads canonical hop_interval (Go duration string like "12s")
// and returns the value in seconds. Exact whole seconds return int; fractional
// seconds return float64. Invalid or nonpositive durations are omitted.
func convertHopInterval(m map[string]any) (any, bool) {
	v, ok := m["hop_interval"]
	if !ok || v == nil {
		return nil, false
	}
	s := toString(v)
	if s == "" {
		return nil, false
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return nil, false
	}
	secs := d.Seconds()
	if secs == float64(int64(secs)) {
		return int(secs), true
	}
	return secs, true
}

// convertHY2Obfs extracts obfs fields from the canonical map and sets them on proxy.
// Canonical nested obfs ({type, password}) takes precedence over legacy scalar keys.
func convertHY2Obfs(m map[string]any, proxy map[string]any) {
	if objs, ok := m["obfs"].(map[string]any); ok {
		if typ, _ := objs["type"].(string); typ != "" {
			proxy["obfs"] = typ
		}
		if pwd, _ := objs["password"].(string); pwd != "" {
			proxy["obfs-password"] = pwd
		}
		return
	}
	// Legacy scalar compatibility
	if obfs, ok := m["obfs"].(string); ok && obfs != "" {
		proxy["obfs"] = obfs
	}
	if obfsPass, ok := m["obfs-password"].(string); ok && obfsPass != "" {
		proxy["obfs-password"] = obfsPass
	}
}

// extractHY2Obfs returns obfs type and password from canonical or legacy scalar storage.
func extractHY2Obfs(m map[string]any) (obfs, obfsPass string) {
	if objs, ok := m["obfs"].(map[string]any); ok {
		typ, _ := objs["type"].(string)
		pwd, _ := objs["password"].(string)
		return typ, pwd
	}
	obfs, _ = m["obfs"].(string)
	obfsPass, _ = m["obfs-password"].(string)
	return
}

// convertTUIC converts a canonical TUIC outbound to a Clash proxy map.
func convertTUIC(tag string, m map[string]any, server string, port int) map[string]any {
	uuid, _ := m["uuid"].(string)
	if uuid == "" {
		return nil
	}
	proxy := map[string]any{
		"name":   tag,
		"type":   "tuic",
		"server": server,
		"port":   port,
		"uuid":   uuid,
		"udp":    true,
	}
	// Password is optional (canonical nodes may lack it without being invalid)
	if password, ok := m["password"].(string); ok && password != "" {
		proxy["password"] = password
	}
	// TLS fields (flattened to top-level Clash keys)
	if tlsObj, ok := m["tls"].(map[string]any); ok {
		if sni, ok := tlsObj["server_name"].(string); ok && sni != "" {
			proxy["sni"] = sni
		}
		if v, ok := tlsObj["insecure"]; ok && toBool(v) {
			proxy["skip-cert-verify"] = true
		}
		if v, ok := tlsObj["alpn"]; ok {
			switch arr := v.(type) {
			case string:
				if s := strings.TrimSpace(arr); s != "" {
					proxy["alpn"] = []string{s}
				}
			case []any:
				alpn := make([]string, 0, len(arr))
				for _, item := range arr {
					if s, ok := item.(string); ok {
						alpn = append(alpn, s)
					}
				}
				if len(alpn) > 0 {
					proxy["alpn"] = alpn
				}
			case []string:
				if len(arr) > 0 {
					proxy["alpn"] = arr
				}
			}
		}
		if v, ok := tlsObj["disable_sni"]; ok && toBool(v) {
			proxy["disable-sni"] = true
		}
	}
	// zero_rtt_handshake → reduce-rtt
	if v, ok := m["zero_rtt_handshake"]; ok && toBool(v) {
		proxy["reduce-rtt"] = true
	}
	// udp_relay_mode → udp-relay-mode
	if urm, ok := m["udp_relay_mode"].(string); ok && urm != "" {
		proxy["udp-relay-mode"] = urm
	}
	// congestion_control → congestion-controller
	if cc, ok := m["congestion_control"].(string); ok && cc != "" {
		proxy["congestion-controller"] = cc
	}
	// heartbeat duration string → heartbeat-interval (milliseconds integer)
	if hb, ok := m["heartbeat"].(string); ok && hb != "" {
		d, err := time.ParseDuration(hb)
		if err == nil && d > 0 {
			proxy["heartbeat-interval"] = int(d.Milliseconds())
		}
	}
	return proxy
}

// convertHysteria1 converts a canonical Hysteria v1 outbound to a Clash proxy map.
func convertHysteria1(tag string, m map[string]any, server string, port int) map[string]any {
	proxy := map[string]any{
		"name":   tag,
		"type":   "hysteria",
		"server": server,
		"port":   port,
		"udp":    true,
	}
	// auth_str / auth → auth-str
	authStr := extractAuth(m)
	if authStr != "" {
		proxy["auth-str"] = authStr
	}
	// up/down – canonical string values (e.g. "30 Mbps") take precedence
	if up, ok := m["up"].(string); ok && up != "" {
		proxy["up"] = up
	} else if upMbps, ok := m["up_mbps"]; ok {
		if n, ok := toFloat(upMbps); ok && n > 0 {
			proxy["up"] = toNumberOrFloat(n)
		}
	}
	if down, ok := m["down"].(string); ok && down != "" {
		proxy["down"] = down
	} else if downMbps, ok := m["down_mbps"]; ok {
		if n, ok := toFloat(downMbps); ok && n > 0 {
			proxy["down"] = toNumberOrFloat(n)
		}
	}
	// TLS fields (flattened to top-level)
	if tlsObj, ok := m["tls"].(map[string]any); ok {
		if sni, ok := tlsObj["server_name"].(string); ok && sni != "" {
			proxy["sni"] = sni
		}
		if v, ok := tlsObj["insecure"]; ok && toBool(v) {
			proxy["skip-cert-verify"] = true
		}
		if v, ok := tlsObj["alpn"]; ok {
			switch arr := v.(type) {
			case string:
				if s := strings.TrimSpace(arr); s != "" {
					proxy["alpn"] = []string{s}
				}
			case []any:
				alpn := make([]string, 0, len(arr))
				for _, item := range arr {
					if s, ok := item.(string); ok {
						alpn = append(alpn, s)
					}
				}
				if len(alpn) > 0 {
					proxy["alpn"] = alpn
				}
			case []string:
				if len(arr) > 0 {
					proxy["alpn"] = arr
				}
			}
		}
		// uTLS fingerprint for Hysteria v1 → "fingerprint" (not client-fingerprint)
		if utls, ok := tlsObj["utls"].(map[string]any); ok {
			if fp, ok := utls["fingerprint"].(string); ok && fp != "" {
				proxy["fingerprint"] = fp
			}
		}
	}
	// obfs scalar
	if obfs, ok := m["obfs"].(string); ok && obfs != "" {
		proxy["obfs"] = obfs
	}
	// server_ports → ports (reuse HY2 helper)
	if portsStr := convertServerPorts(m); portsStr != "" {
		proxy["ports"] = portsStr
	}
	// recv_window_conn → recv-window-conn
	if rwc, ok := m["recv_window_conn"]; ok {
		if n, ok := toFloat(rwc); ok && n > 0 {
			proxy["recv-window-conn"] = int(n)
		}
	}
	// recv_window → recv-window
	if rw, ok := m["recv_window"]; ok {
		if n, ok := toFloat(rw); ok && n > 0 {
			proxy["recv-window"] = int(n)
		}
	}
	// disable_mtu_discovery → disable-mtu-discovery
	if v, ok := m["disable_mtu_discovery"]; ok && toBool(v) {
		proxy["disable-mtu-discovery"] = true
	}
	// hop_interval → hop-interval (seconds, reuse HY2 helper)
	if hopSec, ok := convertHopInterval(m); ok {
		proxy["hop-interval"] = hopSec
	}
	// network → protocol (non-empty)
	if net, ok := m["network"].(string); ok && net != "" {
		proxy["protocol"] = net
	}
	return proxy
}

// extractAuth extracts the auth string from canonical hysteria fields.
// Canonical may store auth_str (string), auth (string), or auth ({"password":"..."}).
func extractAuth(m map[string]any) string {
	if v, ok := m["auth_str"].(string); ok && v != "" {
		return v
	}
	if v, ok := m["auth"].(string); ok && v != "" {
		return v
	}
	if v, ok := m["auth"].(map[string]any); ok {
		if pwd, ok := v["password"].(string); ok && pwd != "" {
			return pwd
		}
	}
	return ""
}

func convertHTTP(tag string, m map[string]any, server string, port int) map[string]any {
	ti := extractTLS(m)
	proxy := map[string]any{
		"name":   tag,
		"type":   "http",
		"server": server,
		"port":   port,
		"udp":    false,
	}
	if user, ok := m["username"].(string); ok && user != "" {
		proxy["username"] = user
	}
	if pass, ok := m["password"].(string); ok && pass != "" {
		proxy["password"] = pass
	}
	if ti.Enabled {
		proxy["tls"] = true
		proxy["sni"] = ti.SNI
		if ti.SkipCertVerify {
			proxy["skip-cert-verify"] = true
		}
	}
	return proxy
}

func convertSocks(tag string, m map[string]any, server string, port int) map[string]any {
	proxy := map[string]any{
		"name":   tag,
		"type":   "socks5",
		"server": server,
		"port":   port,
		"udp":    true,
	}
	if user, ok := m["username"].(string); ok && user != "" {
		proxy["username"] = user
	}
	if pass, ok := m["password"].(string); ok && pass != "" {
		proxy["password"] = pass
	}
	if net, ok := m["network"].(string); ok && net == "tcp" {
		proxy["udp"] = false
	}
	ti := extractTLS(m)
	if ti.Enabled {
		proxy["tls"] = true
		proxy["sni"] = ti.SNI
		if ti.SkipCertVerify {
			proxy["skip-cert-verify"] = true
		}
	}
	return proxy
}

// ---------------------------------------------------------------------------
// URI conversion
// ---------------------------------------------------------------------------

// outboundToURI converts an ExportOutbound to a proxy URI string.
// Returns "" when the node cannot be expressed as a URI.
func outboundToURI(o ExportOutbound) string {
	if o.Tag == "" || len(o.Raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(o.Raw, &m); err != nil {
		return ""
	}
	typ, _ := m["type"].(string)
	server, _ := m["server"].(string)
	if server == "" {
		return ""
	}
	port := pickPort(m)
	if port <= 0 {
		return ""
	}

	switch typ {
	case "shadowsocks", "ss":
		return uriSS(o.Tag, m, server, port)
	case "vmess", "vmess1":
		return uriVMess(o.Tag, m, server, port)
	case "trojan":
		return uriTrojan(o.Tag, m, server, port)
	case "vless":
		return uriVLess(o.Tag, m, server, port)
	case "hysteria2", "hy2":
		return uriHysteria2(o.Tag, m, server, port)
	case "hysteria":
		return "" // Hysteria v1 has no standard URI representation
	case "tuic":
		return uriTUIC(o.Tag, m, server, port)
	case "http":
		return uriHTTP(o.Tag, m, server, port)
	case "socks", "socks5":
		return uriSocks(o.Tag, m, server, port)
	default:
		return ""
	}
}

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func encodeFragment(tag string) string {
	return url.QueryEscape(tag)
}

func uriSS(tag string, m map[string]any, server string, port int) string {
	method, _ := m["method"].(string)
	password, _ := m["password"].(string)
	if method == "" || password == "" {
		return ""
	}
	userInfo := method + ":" + password
	b64UserInfo := base64.StdEncoding.EncodeToString([]byte(userInfo))
	return fmt.Sprintf("ss://%s@%s:%d#%s", b64UserInfo, server, port, encodeFragment(tag))
}

func uriVMess(tag string, m map[string]any, server string, port int) string {
	uuid, _ := m["uuid"].(string)
	if uuid == "" {
		return ""
	}
	alterID := 0
	if v, ok := m["alter_id"]; ok {
		if f, ok := toFloat(v); ok {
			alterID = int(f)
		}
	}
	security, _ := m["security"].(string)
	if security == "" {
		security = "auto"
	}
	ti := extractTLS(m)
	tr := extractTransport(m)

	v := map[string]any{
		"v":    "2",
		"ps":   tag,
		"add":  server,
		"port": port,
		"id":   uuid,
		"aid":  alterID,
		"scy":  security,
	}
	if ti.Enabled {
		v["tls"] = "tls"
	}
	if ti.SNI != "" {
		v["sni"] = ti.SNI
	}
	if ti.SkipCertVerify {
		v["allowInsecure"] = 1
	}

	net := tr.Network
	if net == "" {
		net = "tcp"
	}
	v["net"] = net

	switch net {
	case "ws", "websocket":
		v["type"] = "none"
		if tr.Path != "" {
			v["path"] = tr.Path
		}
		if tr.Host != "" {
			v["host"] = tr.Host
		}
	case "grpc":
		if tr.ServiceName != "" {
			v["path"] = tr.ServiceName
		}
		v["type"] = "grpc"
	case "h2", "http":
		v["type"] = "none"
		if tr.Path != "" {
			v["path"] = tr.Path
		}
		if tr.Host != "" {
			v["host"] = tr.Host
		}
	case "tcp":
		v["type"] = "none"
	default:
		v["type"] = "none"
	}

	data, _ := json.Marshal(v)
	return "vmess://" + b64(string(data))
}

func uriTrojan(tag string, m map[string]any, server string, port int) string {
	password, _ := m["password"].(string)
	if password == "" {
		return ""
	}
	ti := extractTLS(m)

	var params []string
	if ti.Enabled {
		params = append(params, "security=tls")
	}
	if ti.SNI != "" {
		params = append(params, "sni="+url.QueryEscape(ti.SNI))
	}
	if ti.SkipCertVerify {
		params = append(params, "allowInsecure=1")
	}
	params = append(params, "type=tcp")

	// transport
	tr := extractTransport(m)
	if tr.Network == "ws" || tr.Network == "websocket" {
		params = append(params, "type=ws")
		if tr.Path != "" {
			params = append(params, "path="+url.QueryEscape(tr.Path))
		}
		if tr.Host != "" {
			params = append(params, "host="+url.QueryEscape(tr.Host))
		}
	} else if tr.Network == "grpc" {
		params = append(params, "type=grpc")
	} else if tr.Network == "h2" || tr.Network == "http" {
		params = append(params, "type=h2")
		if tr.Path != "" {
			params = append(params, "path="+url.QueryEscape(tr.Path))
		}
		if tr.Host != "" {
			params = append(params, "host="+url.QueryEscape(tr.Host))
		}
	}

	query := ""
	if len(params) > 0 {
		query = "?" + strings.Join(params, "&")
	}
	return fmt.Sprintf("trojan://%s@%s:%d%s#%s", url.QueryEscape(password), server, port, query, encodeFragment(tag))
}

func uriVLess(tag string, m map[string]any, server string, port int) string {
	uuid, _ := m["uuid"].(string)
	if uuid == "" {
		return ""
	}
	ti := extractTLS(m)
	flow, _ := m["flow"].(string)
	tr := extractTransport(m)

	var params []string
	if ti.Enabled {
		params = append(params, "security=tls")
	}
	if ti.SNI != "" {
		params = append(params, "sni="+url.QueryEscape(ti.SNI))
	}
	if ti.SkipCertVerify {
		params = append(params, "allowInsecure=1")
	}
	if flow != "" {
		params = append(params, "flow="+url.QueryEscape(flow))
	}

	net := tr.Network
	if net == "" {
		net = "tcp"
	}
	params = append(params, "type="+net)

	switch net {
	case "ws", "websocket":
		if tr.Path != "" {
			params = append(params, "path="+url.QueryEscape(tr.Path))
		}
		if tr.Host != "" {
			params = append(params, "host="+url.QueryEscape(tr.Host))
		}
	case "grpc":
		if tr.ServiceName != "" {
			params = append(params, "serviceName="+url.QueryEscape(tr.ServiceName))
		}
	case "h2", "http":
		if tr.Path != "" {
			params = append(params, "path="+url.QueryEscape(tr.Path))
		}
		if tr.Host != "" {
			params = append(params, "host="+url.QueryEscape(tr.Host))
		}
	}

	query := ""
	if len(params) > 0 {
		query = "?" + strings.Join(params, "&")
	}
	return fmt.Sprintf("vless://%s@%s:%d%s#%s", url.QueryEscape(uuid), server, port, query, encodeFragment(tag))
}

func uriHysteria2(tag string, m map[string]any, server string, port int) string {
	password, _ := m["password"].(string)
	if password == "" {
		return ""
	}
	ti := extractTLS(m)
	obfs, obfsPass := extractHY2Obfs(m)

	var params []string
	if ti.SNI != "" {
		params = append(params, "sni="+url.QueryEscape(ti.SNI))
	}
	if ti.SkipCertVerify {
		params = append(params, "insecure=1")
	}
	if obfs != "" {
		params = append(params, "obfs="+url.QueryEscape(obfs))
	}
	if obfsPass != "" {
		params = append(params, "obfs-password="+url.QueryEscape(obfsPass))
	}

	query := ""
	if len(params) > 0 {
		query = "?" + strings.Join(params, "&")
	}
	return fmt.Sprintf("hysteria2://%s@%s:%d%s#%s", url.QueryEscape(password), server, port, query, encodeFragment(tag))
}

// uriTUIC builds a tuic:// URI from canonical TUIC outbound fields.
// UUID and password are both required; returns "" when either is missing.
func uriTUIC(tag string, m map[string]any, server string, port int) string {
	uuid, _ := m["uuid"].(string)
	password, _ := m["password"].(string)
	if uuid == "" || password == "" {
		return ""
	}
	u := &url.URL{
		Scheme:   "tuic",
		User:     url.UserPassword(uuid, password),
		Host:     net.JoinHostPort(server, fmt.Sprintf("%d", port)),
		Fragment: tag,
	}
	q := url.Values{}
	if cc, ok := m["congestion_control"].(string); ok && cc != "" {
		q.Set("congestion_control", cc)
	}
	if urm, ok := m["udp_relay_mode"].(string); ok && urm != "" {
		q.Set("udp_relay_mode", urm)
	}
	if tlsObj, ok := m["tls"].(map[string]any); ok {
		if sni, ok := tlsObj["server_name"].(string); ok && sni != "" {
			q.Set("sni", sni)
		}
		if v, ok := tlsObj["insecure"]; ok && toBool(v) {
			q.Set("allow_insecure", "1")
		}
		if v, ok := tlsObj["alpn"]; ok {
			switch arr := v.(type) {
			case string:
				if s := strings.TrimSpace(arr); s != "" {
					q.Add("alpn", s)
				}
			case []any:
				for _, item := range arr {
					if s, ok := item.(string); ok {
						q.Add("alpn", s)
					}
				}
			case []string:
				for _, s := range arr {
					q.Add("alpn", s)
				}
			}
		}
	}
	if hb, ok := m["heartbeat"].(string); ok && hb != "" {
		if d, err := time.ParseDuration(hb); err == nil && d > 0 {
			q.Set("heartbeat", fmt.Sprintf("%d", d.Milliseconds()))
		}
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func uriHTTP(tag string, m map[string]any, server string, port int) string {
	user, _ := m["username"].(string)
	pass, _ := m["password"].(string)
	var userInfo string
	if user != "" {
		if pass != "" {
			userInfo = url.QueryEscape(user) + ":" + url.QueryEscape(pass) + "@"
		} else {
			userInfo = url.QueryEscape(user) + "@"
		}
	}
	return fmt.Sprintf("http://%s%s:%d#%s", userInfo, server, port, encodeFragment(tag))
}

func uriSocks(tag string, m map[string]any, server string, port int) string {
	user, _ := m["username"].(string)
	pass, _ := m["password"].(string)
	var userInfo string
	if user != "" {
		if pass != "" {
			userInfo = url.QueryEscape(user) + ":" + url.QueryEscape(pass) + "@"
		} else {
			userInfo = url.QueryEscape(user) + "@"
		}
	}
	return fmt.Sprintf("socks5://%s%s:%d#%s", userInfo, server, port, encodeFragment(tag))
}
