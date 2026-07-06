package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

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

// extractTLS extracts common TLS fields from a sing-box outbound map.
// Returns (tls bool, sni, skipCertVerify, alpn).
func extractTLS(m map[string]any) (tls bool, sni string, skipCertVerify bool, alpn string) {
	tlsObj, ok := m["tls"].(map[string]any)
	if !ok {
		if enabled, exists := m["tls"]; exists {
			if b, ok := enabled.(bool); ok && b {
				return true, "", false, ""
			}
		}
		return
	}
	tls = true
	if v, ok := tlsObj["enabled"]; ok {
		tls = toBool(v)
	}
	sni, _ = tlsObj["server_name"].(string)
	if sni == "" {
		sni, _ = tlsObj["sni"].(string)
	}
	if v, ok := tlsObj["insecure"]; ok {
		skipCertVerify = toBool(v)
	}
	if v, ok := tlsObj["alpn"]; ok {
		switch arr := v.(type) {
		case []any:
			if len(arr) > 0 {
				alpn, _ = arr[0].(string)
			}
		case string:
			alpn = arr
		}
	}
	return
}

// extractTransport extracts transport info from sing-box outbound.
// Returns (network, path, host, serviceName, headers, earlyData).
func extractTransport(m map[string]any) (network, path, host, serviceName string, headers map[string]string, earlyData bool) {
	t, ok := m["transport"].(map[string]any)
	if !ok {
		return
	}
	network, _ = t["type"].(string)
	switch network {
	case "ws", "websocket":
		if wp, ok := t["path"].(string); ok {
			path = wp
		}
		if h, ok := t["headers"].(map[string]any); ok {
			headers = make(map[string]string, len(h))
			for k, v := range h {
				headers[k] = toString(v)
			}
		}
		host = headers["Host"]
		if e, ok := t["max_early_data"]; ok {
			earlyData = toBool(e)
		}
	case "grpc":
		serviceName, _ = t["service_name"].(string)
	case "http", "h2":
		if p, ok := t["path"].(string); ok {
			path = p
		}
		if h, ok := t["host"].(string); ok {
			host = h
		}
	case "quic":
		// nothing extra needed
	}
	return
}

func convertShadowsocks(tag string, m map[string]any, server string, port int) map[string]any {
	method, _ := m["method"].(string)
	password, _ := m["password"].(string)
	if method == "" || password == "" {
		return nil
	}
	return map[string]any{
		"name":     tag,
		"type":     "ss",
		"server":   server,
		"port":     port,
		"cipher":   method,
		"password": password,
		"udp":      true,
	}
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
	tls, sni, skipCert, _ := extractTLS(m)
	network, path, host, serviceName, _, _ := extractTransport(m)

	proxy := map[string]any{
		"name":     tag,
		"type":     "vmess",
		"server":   server,
		"port":     port,
		"uuid":     uuid,
		"alterId":  alterID,
		"cipher":   security,
		"tls":      tls,
		"udp":      true,
		"network":  "tcp",
	}
	if network != "" {
		proxy["network"] = network
	}
	if tls && sni != "" {
		proxy["servername"] = sni
	}
	if tls && skipCert {
		proxy["skip-cert-verify"] = true
	}
	if path != "" {
		if network == "ws" || network == "websocket" || network == "h2" || network == "http" {
			proxy["ws-path"] = path
		}
	}
	if host != "" {
		proxy["ws-headers"] = map[string]any{"Host": host}
	}

	// grpc service name
	if network == "grpc" {
		proxy["grpc-opts"] = map[string]any{
			"grpc-service-name": serviceName,
		}
	}

	return proxy
}

func convertTrojan(tag string, m map[string]any, server string, port int) map[string]any {
	password, _ := m["password"].(string)
	if password == "" {
		return nil
	}
	tls, sni, skipCert, _ := extractTLS(m)
	network, path, host, _, _, _ := extractTransport(m)

	proxy := map[string]any{
		"name":     tag,
		"type":     "trojan",
		"server":   server,
		"port":     port,
		"password": password,
		"udp":      true,
	}
	if tls {
		proxy["tls"] = true
	}
	if sni != "" {
		proxy["sni"] = sni
	}
	if skipCert {
		proxy["skip-cert-verify"] = true
	}
	if network == "ws" || network == "websocket" {
		proxy["network"] = "ws"
		if path != "" {
			proxy["ws-path"] = path
		}
		if host != "" {
			proxy["ws-headers"] = map[string]any{"Host": host}
		}
	} else if network == "grpc" {
		// In Clash, grpc network support varies; omit serviceName for now.
	}
	if network == "h2" || network == "http" {
		if path != "" {
			proxy["ws-path"] = path
		}
		if host != "" {
			proxy["ws-headers"] = map[string]any{"Host": host}
		}
	}
	return proxy
}

func convertVLess(tag string, m map[string]any, server string, port int) map[string]any {
	uuid, _ := m["uuid"].(string)
	if uuid == "" {
		return nil
	}
	flow, _ := m["flow"].(string)
	tls, sni, skipCert, _ := extractTLS(m)
	network, path, host, serviceName, _, _ := extractTransport(m)

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
	if tls {
		proxy["tls"] = true
	}
	if sni != "" {
		proxy["servername"] = sni
	}
	if skipCert {
		proxy["skip-cert-verify"] = true
	}
	if network != "" {
		proxy["network"] = network
	}
	if path != "" {
		if network == "ws" || network == "websocket" || network == "h2" || network == "http" {
			proxy["ws-path"] = path
		}
	}
	if host != "" {
		proxy["ws-headers"] = map[string]any{"Host": host}
	}
	if network == "grpc" {
		proxy["grpc-opts"] = map[string]any{
			"grpc-service-name": serviceName,
		}
	}
	return proxy
}

func convertHysteria2(tag string, m map[string]any, server string, port int) map[string]any {
	password, _ := m["password"].(string)
	if password == "" {
		return nil
	}
	_, sni, skipCert, alpn := extractTLS(m)
	// For hysteria2, ports can be "443" or "443-500".
	portStr := strconv.Itoa(port)
	if v, ok := m["ports"].(string); ok && v != "" {
		portStr = v
	}
	proxy := map[string]any{
		"name":     tag,
		"type":     "hysteria2",
		"server":   server,
		"port":     portStr,
		"password": password,
		"udp":      true,
	}
	if sni != "" {
		proxy["sni"] = sni
	}
	if skipCert {
		proxy["skip-cert-verify"] = true
	}
	if alpn != "" {
		proxy["alpn"] = []string{alpn}
	}
	// obfs fields
	if obfs, ok := m["obfs"].(string); ok && obfs != "" {
		proxy["obfs"] = obfs
	}
	if obfsPass, ok := m["obfs-password"].(string); ok && obfsPass != "" {
		proxy["obfs-password"] = obfsPass
	}
	// up / down
	if up, ok := m["up"].(float64); ok && up > 0 {
		proxy["up"] = strconv.FormatFloat(up, 'f', -1, 64)
	}
	if down, ok := m["down"].(float64); ok && down > 0 {
		proxy["down"] = strconv.FormatFloat(down, 'f', -1, 64)
	}
	return proxy
}

func convertHTTP(tag string, m map[string]any, server string, port int) map[string]any {
	tls, sni, skipCert, _ := extractTLS(m)
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
	if tls {
		proxy["tls"] = true
		proxy["sni"] = sni
		if skipCert {
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
	tls, sni, skipCert, _ := extractTLS(m)
	if tls {
		proxy["tls"] = true
		proxy["sni"] = sni
		if skipCert {
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
	tls, sni, skipCert, _ := extractTLS(m)
	network, path, host, serviceName, _, _ := extractTransport(m)

	v := map[string]any{
		"v":    "2",
		"ps":   tag,
		"add":  server,
		"port": port,
		"id":   uuid,
		"aid":  alterID,
		"scy":  security,
	}
	if tls {
		v["tls"] = "tls"
	}
	if sni != "" {
		v["sni"] = sni
	}
	if skipCert {
		v["allowInsecure"] = 1
	}

	net := network
	if net == "" {
		net = "tcp"
	}
	v["net"] = net

	switch net {
	case "ws", "websocket":
		v["type"] = "none"
		if path != "" {
			v["path"] = path
		}
		if host != "" {
			v["host"] = host
		}
	case "grpc":
		if serviceName != "" {
			v["path"] = serviceName
		}
		v["type"] = "grpc"
	case "h2", "http":
		v["type"] = "none"
		if path != "" {
			v["path"] = path
		}
		if host != "" {
			v["host"] = host
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
	tls, sni, skipCert, _ := extractTLS(m)

	var params []string
	if tls {
		params = append(params, "security=tls")
	}
	if sni != "" {
		params = append(params, "sni="+url.QueryEscape(sni))
	}
	if skipCert {
		params = append(params, "allowInsecure=1")
	}
	params = append(params, "type=tcp")

	// transport
	network, path, host, _, _, _ := extractTransport(m)
	if network == "ws" || network == "websocket" {
		params = append(params, "type=ws")
		if path != "" {
			params = append(params, "path="+url.QueryEscape(path))
		}
		if host != "" {
			params = append(params, "host="+url.QueryEscape(host))
		}
	} else if network == "grpc" {
		params = append(params, "type=grpc")
	} else if network == "h2" || network == "http" {
		params = append(params, "type=h2")
		if path != "" {
			params = append(params, "path="+url.QueryEscape(path))
		}
		if host != "" {
			params = append(params, "host="+url.QueryEscape(host))
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
	tls, sni, skipCert, _ := extractTLS(m)
	flow, _ := m["flow"].(string)
	network, path, host, serviceName, _, _ := extractTransport(m)

	var params []string
	if tls {
		params = append(params, "security=tls")
	}
	if sni != "" {
		params = append(params, "sni="+url.QueryEscape(sni))
	}
	if skipCert {
		params = append(params, "allowInsecure=1")
	}
	if flow != "" {
		params = append(params, "flow="+url.QueryEscape(flow))
	}

	net := network
	if net == "" {
		net = "tcp"
	}
	params = append(params, "type="+net)

	switch net {
	case "ws", "websocket":
		if path != "" {
			params = append(params, "path="+url.QueryEscape(path))
		}
		if host != "" {
			params = append(params, "host="+url.QueryEscape(host))
		}
	case "grpc":
		if serviceName != "" {
			params = append(params, "serviceName="+url.QueryEscape(serviceName))
		}
	case "h2", "http":
		if path != "" {
			params = append(params, "path="+url.QueryEscape(path))
		}
		if host != "" {
			params = append(params, "host="+url.QueryEscape(host))
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
	_, sni, skipCert, _ := extractTLS(m)
	obfs, _ := m["obfs"].(string)
	obfsPass, _ := m["obfs-password"].(string)

	var params []string
	if sni != "" {
		params = append(params, "sni="+url.QueryEscape(sni))
	}
	if skipCert {
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
