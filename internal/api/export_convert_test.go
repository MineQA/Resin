package api

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Resinat/Resin/internal/subscription"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// convertOutbound unmarshals raw JSON into an ExportOutbound and runs
// outboundToClashProxy, returning the proxy map (or nil on failure).
func convertOutbound(t *testing.T, rawJSON, tag string) map[string]any {
	t.Helper()
	o := ExportOutbound{Tag: tag, Raw: json.RawMessage(rawJSON)}
	return outboundToClashProxy(o)
}

// diffMaps reports a diff string when want != got, using reflect.DeepEqual.
// Returns "" when equal.
func diffMaps(want, got any) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	wj, _ := json.MarshalIndent(want, "", "  ")
	gj, _ := json.MarshalIndent(got, "", "  ")
	return "(-want)\n" + string(wj) + "\n(+got)\n" + string(gj)
}

// ---------------------------------------------------------------------------
// outboundToClashProxy — VMess structured tests
// ---------------------------------------------------------------------------

func TestConvertVMess_WS_Full(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-ws.example.com","server_port":8443,
		"uuid":"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		"tls":{
			"enabled":true,"server_name":"ws.example.com",
			"alpn":["h2","http/1.1"],
			"utls":{"enabled":true,"fingerprint":"chrome"}
		},
		"transport":{
			"type":"ws","path":"/api/ws",
			"headers":{"Host":"ws.example.com","X-Custom":"val1"},
			"max_early_data":2048,
			"early_data_header_name":"Sec-WebSocket-Protocol"
		}
	}`
	want := map[string]any{
		"name":               "vmess-ws-full",
		"type":               "vmess",
		"server":             "vm-ws.example.com",
		"port":               8443,
		"uuid":               "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		"alterId":            0,
		"cipher":             "auto",
		"udp":                true,
		"network":            "ws",
		"tls":                true,
		"servername":         "ws.example.com",
		"alpn":               []string{"h2", "http/1.1"},
		"client-fingerprint": "chrome",
		"ws-opts": map[string]any{
			"path":                   "/api/ws",
			"headers":                map[string]string{"Host": "ws.example.com", "X-Custom": "val1"},
			"max-early-data":         2048,
			"early-data-header-name": "Sec-WebSocket-Protocol",
		},
	}
	got := convertOutbound(t, raw, "vmess-ws-full")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVMess_GRPC(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-grpc.example.com","server_port":443,
		"uuid":"b2c3d4e5-f6a7-8901-bcde-f12345678901",
		"tls":{"enabled":true,"server_name":"grpc.example.com"},
		"transport":{"type":"grpc","service_name":"MyService"}
	}`
	want := map[string]any{
		"name":       "vmess-grpc",
		"type":       "vmess",
		"server":     "vm-grpc.example.com",
		"port":       443,
		"uuid":       "b2c3d4e5-f6a7-8901-bcde-f12345678901",
		"alterId":    0,
		"cipher":     "auto",
		"udp":        true,
		"network":    "grpc",
		"tls":        true,
		"servername": "grpc.example.com",
		"grpc-opts":  map[string]any{"grpc-service-name": "MyService"},
	}
	got := convertOutbound(t, raw, "vmess-grpc")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVMess_HTTP(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-http.example.com","server_port":443,
		"uuid":"c3d4e5f6-a7b8-9012-cdef-123456789012",
		"tls":{"enabled":true},
		"transport":{"type":"http","path":"/api","host":"http.example.com"}
	}`
	want := map[string]any{
		"name":    "vmess-http",
		"type":    "vmess",
		"server":  "vm-http.example.com",
		"port":    443,
		"uuid":    "c3d4e5f6-a7b8-9012-cdef-123456789012",
		"alterId": 0,
		"cipher":  "auto",
		"udp":     true,
		"network": "http",
		"tls":     true,
		"http-opts": map[string]any{
			"path": "/api",
			"host": []string{"http.example.com"},
		},
	}
	got := convertOutbound(t, raw, "vmess-http")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVMess_H2(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-h2.example.com","server_port":443,
		"uuid":"d4e5f6a7-b8c9-0123-defa-234567890123",
		"tls":{"enabled":true},
		"transport":{"type":"h2","path":"/api","host":"h2.example.com"}
	}`
	want := map[string]any{
		"name":    "vmess-h2",
		"type":    "vmess",
		"server":  "vm-h2.example.com",
		"port":    443,
		"uuid":    "d4e5f6a7-b8c9-0123-defa-234567890123",
		"alterId": 0,
		"cipher":  "auto",
		"udp":     true,
		"network": "h2",
		"tls":     true,
		"h2-opts": map[string]any{
			"path": "/api",
			"host": []string{"h2.example.com"},
		},
	}
	got := convertOutbound(t, raw, "vmess-h2")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVMess_HTTP_MultipleHosts(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-mhost.example.com","server_port":443,
		"uuid":"e5f6a7b8-c9d0-1234-efab-345678901234",
		"tls":{"enabled":true},
		"transport":{"type":"http","path":"/","host":["a.example.com","b.example.com"]}
	}`
	want := map[string]any{
		"name":    "vmess-mhost",
		"type":    "vmess",
		"server":  "vm-mhost.example.com",
		"port":    443,
		"uuid":    "e5f6a7b8-c9d0-1234-efab-345678901234",
		"alterId": 0,
		"cipher":  "auto",
		"udp":     true,
		"network": "http",
		"tls":     true,
		"http-opts": map[string]any{
			"path": "/",
			"host": []string{"a.example.com", "b.example.com"},
		},
	}
	got := convertOutbound(t, raw, "vmess-mhost")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVMess_H2_HostList(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-h2list.example.com","server_port":443,
		"uuid":"f6a7b8c9-d0e1-2345-fabc-456789012345",
		"tls":{"enabled":true},
		"transport":{"type":"h2","path":"/","host":["x.example.com","y.example.com"]}
	}`
	want := map[string]any{
		"name":    "vmess-h2list",
		"type":    "vmess",
		"server":  "vm-h2list.example.com",
		"port":    443,
		"uuid":    "f6a7b8c9-d0e1-2345-fabc-456789012345",
		"alterId": 0,
		"cipher":  "auto",
		"udp":     true,
		"network": "h2",
		"tls":     true,
		"h2-opts": map[string]any{
			"path": "/",
			"host": []string{"x.example.com", "y.example.com"},
		},
	}
	got := convertOutbound(t, raw, "vmess-h2list")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVMess_NoTLS(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-notls.example.com","server_port":80,
		"uuid":"a7b8c9d0-e1f2-3456-abcd-567890123456"
	}`
	want := map[string]any{
		"name":    "vmess-notls",
		"type":    "vmess",
		"server":  "vm-notls.example.com",
		"port":    80,
		"uuid":    "a7b8c9d0-e1f2-3456-abcd-567890123456",
		"alterId": 0,
		"cipher":  "auto",
		"udp":     true,
		"network": "tcp",
		"tls":     false,
	}
	got := convertOutbound(t, raw, "vmess-notls")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVMess_TLSReality(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-reality.example.com","server_port":443,
		"uuid":"b8c9d0e1-f2a3-4567-bcde-678901234567",
		"tls":{
			"enabled":true,"server_name":"reality.example.com",
			"utls":{"enabled":true,"fingerprint":"chrome"},
			"reality":{"enabled":true,"public_key":"abc123public","short_id":"def456"}
		},
		"transport":{"type":"tcp"}
	}`
	want := map[string]any{
		"name":               "vmess-reality",
		"type":               "vmess",
		"server":             "vm-reality.example.com",
		"port":               443,
		"uuid":               "b8c9d0e1-f2a3-4567-bcde-678901234567",
		"alterId":            0,
		"cipher":             "auto",
		"udp":                true,
		"network":            "tcp",
		"tls":                true,
		"servername":         "reality.example.com",
		"client-fingerprint": "chrome",
		"reality-opts": map[string]any{
			"public-key": "abc123public",
			"short-id":   "def456",
		},
	}
	got := convertOutbound(t, raw, "vmess-reality")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVMess_EmptyOptionalValues(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-minimal.example.com","server_port":443,
		"uuid":"c9d0e1f2-a3b4-5678-cdef-789012345678",
		"tls":{"enabled":true},
		"transport":{"type":"ws"}
	}`
	want := map[string]any{
		"name":    "vmess-minimal",
		"type":    "vmess",
		"server":  "vm-minimal.example.com",
		"port":    443,
		"uuid":    "c9d0e1f2-a3b4-5678-cdef-789012345678",
		"alterId": 0,
		"cipher":  "auto",
		"udp":     true,
		"network": "ws",
		"tls":     true,
	}
	got := convertOutbound(t, raw, "vmess-minimal")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVMess_EmptyUUID(t *testing.T) {
	raw := `{"type":"vmess","server":"vm-nouuid.example.com","server_port":443,"uuid":""}`
	got := convertOutbound(t, raw, "vmess-nouuid")
	if got != nil {
		t.Errorf("expected nil for empty uuid, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// outboundToClashProxy — Trojan structured tests
// ---------------------------------------------------------------------------

func TestConvertTrojan_WS_Full(t *testing.T) {
	raw := `{
		"type":"trojan","server":"tr-ws.example.com","server_port":443,
		"password":"secret123",
		"tls":{"enabled":true,"server_name":"tr.example.com","alpn":["h2"]},
		"transport":{"type":"ws","path":"/trojan","headers":{"Host":"tr.example.com"},"max_early_data":1024}
	}`
	want := map[string]any{
		"name":     "trojan-ws",
		"type":     "trojan",
		"server":   "tr-ws.example.com",
		"port":     443,
		"password": "secret123",
		"udp":      true,
		"tls":      true,
		"sni":      "tr.example.com",
		"alpn":     []string{"h2"},
		"network":  "ws",
		"ws-opts": map[string]any{
			"path":           "/trojan",
			"headers":        map[string]string{"Host": "tr.example.com"},
			"max-early-data": 1024,
		},
	}
	got := convertOutbound(t, raw, "trojan-ws")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertTrojan_GRPC(t *testing.T) {
	// Validates the gRPC fix: Trojan now emits grpc-opts with service name.
	raw := `{
		"type":"trojan","server":"tr-grpc.example.com","server_port":443,
		"password":"grpcpass",
		"tls":{"enabled":true,"server_name":"grpc.example.com"},
		"transport":{"type":"grpc","service_name":"TrojanService"}
	}`
	want := map[string]any{
		"name":      "trojan-grpc",
		"type":      "trojan",
		"server":    "tr-grpc.example.com",
		"port":      443,
		"password":  "grpcpass",
		"udp":       true,
		"tls":       true,
		"sni":       "grpc.example.com",
		"network":   "grpc",
		"grpc-opts": map[string]any{"grpc-service-name": "TrojanService"},
	}
	got := convertOutbound(t, raw, "trojan-grpc")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertTrojan_HTTP(t *testing.T) {
	raw := `{
		"type":"trojan","server":"tr-http.example.com","server_port":443,
		"password":"httppass",
		"tls":{"enabled":true},
		"transport":{"type":"http","path":"/tr","host":"http.example.com"}
	}`
	want := map[string]any{
		"name":     "trojan-http",
		"type":     "trojan",
		"server":   "tr-http.example.com",
		"port":     443,
		"password": "httppass",
		"udp":      true,
		"tls":      true,
		"network":  "http",
		"http-opts": map[string]any{
			"path": "/tr",
			"host": []string{"http.example.com"},
		},
	}
	got := convertOutbound(t, raw, "trojan-http")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertTrojan_H2(t *testing.T) {
	raw := `{
		"type":"trojan","server":"tr-h2.example.com","server_port":443,
		"password":"h2pass",
		"tls":{"enabled":true},
		"transport":{"type":"h2","path":"/tr","host":"h2.example.com"}
	}`
	want := map[string]any{
		"name":     "trojan-h2",
		"type":     "trojan",
		"server":   "tr-h2.example.com",
		"port":     443,
		"password": "h2pass",
		"udp":      true,
		"tls":      true,
		"network":  "h2",
		"h2-opts": map[string]any{
			"path": "/tr",
			"host": []string{"h2.example.com"},
		},
	}
	got := convertOutbound(t, raw, "trojan-h2")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertTrojan_NoTLS(t *testing.T) {
	raw := `{
		"type":"trojan","server":"tr-notls.example.com","server_port":80,
		"password":"plainpass"
	}`
	want := map[string]any{
		"name":     "trojan-notls",
		"type":     "trojan",
		"server":   "tr-notls.example.com",
		"port":     80,
		"password": "plainpass",
		"udp":      true,
	}
	got := convertOutbound(t, raw, "trojan-notls")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertTrojan_EmptyPassword(t *testing.T) {
	raw := `{"type":"trojan","server":"tr-nopass.example.com","server_port":443,"password":""}`
	got := convertOutbound(t, raw, "trojan-nopass")
	if got != nil {
		t.Errorf("expected nil for empty password, got %v", got)
	}
}

func TestConvertTrojan_TLSReality(t *testing.T) {
	raw := `{
		"type":"trojan","server":"tr-reality.example.com","server_port":443,
		"password":"realitypass",
		"tls":{
			"enabled":true,"server_name":"reality.example.com",
			"utls":{"enabled":true,"fingerprint":"firefox"},
			"reality":{"enabled":true,"public_key":"pk123","short_id":"sid456"}
		}
	}`
	want := map[string]any{
		"name":               "trojan-reality",
		"type":               "trojan",
		"server":             "tr-reality.example.com",
		"port":               443,
		"password":           "realitypass",
		"udp":                true,
		"tls":                true,
		"sni":                "reality.example.com",
		"client-fingerprint": "firefox",
		"reality-opts": map[string]any{
			"public-key": "pk123",
			"short-id":   "sid456",
		},
	}
	got := convertOutbound(t, raw, "trojan-reality")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

// ---------------------------------------------------------------------------
// outboundToClashProxy — VLESS structured tests
// ---------------------------------------------------------------------------

func TestConvertVLess_WS_Full(t *testing.T) {
	raw := `{
		"type":"vless","server":"vl-ws.example.com","server_port":443,
		"uuid":"d0e1f2a3-b4c5-6789-defa-890123456789",
		"flow":"xtls-rprx-vision",
		"tls":{"enabled":true,"server_name":"ws.example.com","alpn":["h2","http/1.1"]},
		"transport":{"type":"ws","path":"/vless","headers":{"Host":"ws.example.com"},"max_early_data":4096}
	}`
	want := map[string]any{
		"name":       "vless-ws",
		"type":       "vless",
		"server":     "vl-ws.example.com",
		"port":       443,
		"uuid":       "d0e1f2a3-b4c5-6789-defa-890123456789",
		"flow":       "xtls-rprx-vision",
		"udp":        true,
		"tls":        true,
		"servername": "ws.example.com",
		"alpn":       []string{"h2", "http/1.1"},
		"network":    "ws",
		"ws-opts": map[string]any{
			"path":           "/vless",
			"headers":        map[string]string{"Host": "ws.example.com"},
			"max-early-data": 4096,
		},
	}
	got := convertOutbound(t, raw, "vless-ws")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVLess_GRPC(t *testing.T) {
	raw := `{
		"type":"vless","server":"vl-grpc.example.com","server_port":443,
		"uuid":"e1f2a3b4-c5d6-7890-efab-901234567890",
		"tls":{"enabled":true},
		"transport":{"type":"grpc","service_name":"VLessService"}
	}`
	want := map[string]any{
		"name":      "vless-grpc",
		"type":      "vless",
		"server":    "vl-grpc.example.com",
		"port":      443,
		"uuid":      "e1f2a3b4-c5d6-7890-efab-901234567890",
		"udp":       true,
		"tls":       true,
		"network":   "grpc",
		"grpc-opts": map[string]any{"grpc-service-name": "VLessService"},
	}
	got := convertOutbound(t, raw, "vless-grpc")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVLess_HTTP(t *testing.T) {
	raw := `{
		"type":"vless","server":"vl-http.example.com","server_port":443,
		"uuid":"f2a3b4c5-d6e7-8901-fabc-012345678901",
		"tls":{"enabled":true},
		"transport":{"type":"http","path":"/vl","host":"http.example.com"}
	}`
	want := map[string]any{
		"name":    "vless-http",
		"type":    "vless",
		"server":  "vl-http.example.com",
		"port":    443,
		"uuid":    "f2a3b4c5-d6e7-8901-fabc-012345678901",
		"udp":     true,
		"tls":     true,
		"network": "http",
		"http-opts": map[string]any{
			"path": "/vl",
			"host": []string{"http.example.com"},
		},
	}
	got := convertOutbound(t, raw, "vless-http")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVLess_H2(t *testing.T) {
	raw := `{
		"type":"vless","server":"vl-h2.example.com","server_port":443,
		"uuid":"a3b4c5d6-e7f8-9012-abcd-123456789012",
		"tls":{"enabled":true},
		"transport":{"type":"h2","path":"/h2","host":"h2.example.com"}
	}`
	want := map[string]any{
		"name":    "vless-h2",
		"type":    "vless",
		"server":  "vl-h2.example.com",
		"port":    443,
		"uuid":    "a3b4c5d6-e7f8-9012-abcd-123456789012",
		"udp":     true,
		"tls":     true,
		"network": "h2",
		"h2-opts": map[string]any{
			"path": "/h2",
			"host": []string{"h2.example.com"},
		},
	}
	got := convertOutbound(t, raw, "vless-h2")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVLess_NoTransportDefaults(t *testing.T) {
	raw := `{
		"type":"vless","server":"vl-tcp.example.com","server_port":443,
		"uuid":"b4c5d6e7-f8a9-0123-bcde-234567890123",
		"tls":{"enabled":true,"server_name":"tcp.example.com"}
	}`
	want := map[string]any{
		"name":       "vless-tcp",
		"type":       "vless",
		"server":     "vl-tcp.example.com",
		"port":       443,
		"uuid":       "b4c5d6e7-f8a9-0123-bcde-234567890123",
		"udp":        true,
		"tls":        true,
		"servername": "tcp.example.com",
	}
	got := convertOutbound(t, raw, "vless-tcp")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertVLess_EmptyUUID(t *testing.T) {
	raw := `{"type":"vless","server":"vl-nouuid.example.com","server_port":443,"uuid":""}`
	got := convertOutbound(t, raw, "vless-nouuid")
	if got != nil {
		t.Errorf("expected nil for empty uuid, got %v", got)
	}
}

func TestConvertVLess_Reality(t *testing.T) {
	raw := `{
		"type":"vless","server":"vl-reality.example.com","server_port":443,
		"uuid":"c5d6e7f8-a9b0-1234-cdef-345678901234",
		"tls":{
			"enabled":true,"server_name":"reality.example.com",
			"utls":{"enabled":true,"fingerprint":"safari"},
			"reality":{"enabled":true,"public_key":"realitypk","short_id":"realitysid"}
		}
	}`
	want := map[string]any{
		"name":               "vless-reality",
		"type":               "vless",
		"server":             "vl-reality.example.com",
		"port":               443,
		"uuid":               "c5d6e7f8-a9b0-1234-cdef-345678901234",
		"udp":                true,
		"tls":                true,
		"servername":         "reality.example.com",
		"client-fingerprint": "safari",
		"reality-opts": map[string]any{
			"public-key": "realitypk",
			"short-id":   "realitysid",
		},
	}
	got := convertOutbound(t, raw, "vless-reality")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

// ---------------------------------------------------------------------------
// Edge cases and negative coverage
// ---------------------------------------------------------------------------

func TestConvert_EmptyTagReturnsNil(t *testing.T) {
	raw := `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	o := ExportOutbound{Tag: "", Raw: json.RawMessage(raw)}
	got := outboundToClashProxy(o)
	if got != nil {
		t.Error("expected nil for empty tag")
	}
}

func TestConvert_EmptyServerReturnsNil(t *testing.T) {
	raw := `{"type":"ss","server":"","port":443,"method":"chacha20","password":"test"}`
	o := ExportOutbound{Tag: "test", Raw: json.RawMessage(raw)}
	got := outboundToClashProxy(o)
	if got != nil {
		t.Error("expected nil for empty server")
	}
}

func TestConvert_UnsupportedTypeReturnsNil(t *testing.T) {
	raw := `{"type":"unsupported","server":"1.1.1.1","port":443}`
	o := ExportOutbound{Tag: "unsup", Raw: json.RawMessage(raw)}
	got := outboundToClashProxy(o)
	if got != nil {
		t.Error("expected nil for unsupported type")
	}
}

// ---------------------------------------------------------------------------
// extractTLS unit tests
// ---------------------------------------------------------------------------

func TestExtractTLS_EnabledObject(t *testing.T) {
	m := map[string]any{
		"tls": map[string]any{
			"enabled":     true,
			"server_name": "example.com",
			"alpn":        []any{"h2", "http/1.1"},
			"utls":        map[string]any{"enabled": true, "fingerprint": "chrome"},
			"reality":     map[string]any{"enabled": true, "public_key": "pk", "short_id": "sid"},
		},
	}
	ti := extractTLS(m)
	if !ti.Enabled {
		t.Error("Enabled = false, want true")
	}
	if ti.SNI != "example.com" {
		t.Errorf("SNI = %q, want %q", ti.SNI, "example.com")
	}
	if ti.SkipCertVerify {
		t.Error("SkipCertVerify = true, want false")
	}
	if len(ti.ALPN) != 2 || ti.ALPN[0] != "h2" || ti.ALPN[1] != "http/1.1" {
		t.Errorf("ALPN = %v, want [h2 http/1.1]", ti.ALPN)
	}
	if ti.ClientFingerprint != "chrome" {
		t.Errorf("ClientFingerprint = %q, want %q", ti.ClientFingerprint, "chrome")
	}
	if ti.RealityPublicKey != "pk" {
		t.Errorf("RealityPublicKey = %q, want %q", ti.RealityPublicKey, "pk")
	}
	if ti.RealityShortID != "sid" {
		t.Errorf("RealityShortID = %q, want %q", ti.RealityShortID, "sid")
	}
}

func TestExtractTLS_BoolTrue(t *testing.T) {
	m := map[string]any{"tls": true}
	ti := extractTLS(m)
	if !ti.Enabled {
		t.Error("Enabled = false, want true")
	}
	if ti.SNI != "" {
		t.Errorf("SNI = %q, want empty", ti.SNI)
	}
}

func TestExtractTLS_BoolFalse(t *testing.T) {
	m := map[string]any{"tls": false}
	ti := extractTLS(m)
	if ti.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestExtractTLS_NoTLS(t *testing.T) {
	m := map[string]any{"server": "1.1.1.1"}
	ti := extractTLS(m)
	if ti.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestExtractTLS_DisabledWithFields(t *testing.T) {
	m := map[string]any{
		"tls": map[string]any{
			"enabled":     false,
			"server_name": "ignored.example.com",
		},
	}
	ti := extractTLS(m)
	if ti.Enabled {
		t.Error("Enabled = true, want false")
	}
	if ti.SNI != "ignored.example.com" {
		t.Errorf("SNI = %q, want %q (extracted even when disabled)", ti.SNI, "ignored.example.com")
	}
}

func TestExtractTLS_ALPNString(t *testing.T) {
	m := map[string]any{
		"tls": map[string]any{
			"enabled": true,
			"alpn":    "h2",
		},
	}
	ti := extractTLS(m)
	if len(ti.ALPN) != 1 || ti.ALPN[0] != "h2" {
		t.Errorf("ALPN = %v, want [h2]", ti.ALPN)
	}
}

func TestExtractTLS_NoEnabledField(t *testing.T) {
	m := map[string]any{
		"tls": map[string]any{
			"server_name": "noenabled.example.com",
			"insecure":    true,
		},
	}
	ti := extractTLS(m)
	if !ti.Enabled {
		t.Error("Enabled = false, want true (default when enabled is absent)")
	}
	if ti.SNI != "noenabled.example.com" {
		t.Errorf("SNI = %q, want %q", ti.SNI, "noenabled.example.com")
	}
	if !ti.SkipCertVerify {
		t.Error("SkipCertVerify = false, want true")
	}
}

// ---------------------------------------------------------------------------
// extractTransport unit tests
// ---------------------------------------------------------------------------

func TestExtractTransport_WS_AllFields(t *testing.T) {
	m := map[string]any{
		"transport": map[string]any{
			"type":                   "ws",
			"path":                   "/ws",
			"headers":                map[string]any{"Host": "example.com", "X-Extra": "val"},
			"max_early_data":         float64(2048),
			"early_data_header_name": "Sec-WebSocket-Protocol",
		},
	}
	tr := extractTransport(m)
	if tr.Network != "ws" {
		t.Errorf("Network = %q, want %q", tr.Network, "ws")
	}
	if tr.Path != "/ws" {
		t.Errorf("Path = %q, want %q", tr.Path, "/ws")
	}
	if tr.Host != "example.com" {
		t.Errorf("Host = %q, want %q", tr.Host, "example.com")
	}
	if len(tr.Headers) != 2 || tr.Headers["Host"] != "example.com" || tr.Headers["X-Extra"] != "val" {
		t.Errorf("Headers = %v, want {Host:example.com X-Extra:val}", tr.Headers)
	}
	if tr.MaxEarlyData != 2048 {
		t.Errorf("MaxEarlyData = %d, want %d", tr.MaxEarlyData, 2048)
	}
	if tr.EarlyDataHeaderName != "Sec-WebSocket-Protocol" {
		t.Errorf("EarlyDataHeaderName = %q, want %q", tr.EarlyDataHeaderName, "Sec-WebSocket-Protocol")
	}
}

func TestExtractTransport_WS_MaxEarlyDataInt(t *testing.T) {
	m := map[string]any{
		"transport": map[string]any{
			"type":           "ws",
			"max_early_data": 1024,
		},
	}
	tr := extractTransport(m)
	if tr.MaxEarlyData != 1024 {
		t.Errorf("MaxEarlyData = %d, want %d", tr.MaxEarlyData, 1024)
	}
}

func TestExtractTransport_NoTransport(t *testing.T) {
	m := map[string]any{"server": "1.1.1.1"}
	tr := extractTransport(m)
	if tr.Network != "" {
		t.Errorf("Network = %q, want empty", tr.Network)
	}
}

func TestExtractTransport_GRPC(t *testing.T) {
	m := map[string]any{
		"transport": map[string]any{
			"type":         "grpc",
			"service_name": "MyGRPC",
		},
	}
	tr := extractTransport(m)
	if tr.Network != "grpc" {
		t.Errorf("Network = %q, want %q", tr.Network, "grpc")
	}
	if tr.ServiceName != "MyGRPC" {
		t.Errorf("ServiceName = %q, want %q", tr.ServiceName, "MyGRPC")
	}
}

func TestExtractTransport_HTTP_StringHost(t *testing.T) {
	m := map[string]any{
		"transport": map[string]any{
			"type": "http",
			"path": "/api",
			"host": "single.example.com",
		},
	}
	tr := extractTransport(m)
	if tr.Network != "http" {
		t.Errorf("Network = %q, want %q", tr.Network, "http")
	}
	if tr.Path != "/api" {
		t.Errorf("Path = %q, want %q", tr.Path, "/api")
	}
	if len(tr.Hosts) != 1 || tr.Hosts[0] != "single.example.com" {
		t.Errorf("Hosts = %v, want [single.example.com]", tr.Hosts)
	}
	if tr.Host != "single.example.com" {
		t.Errorf("Host = %q, want %q", tr.Host, "single.example.com")
	}
}

func TestExtractTransport_HTTP_SliceHost(t *testing.T) {
	m := map[string]any{
		"transport": map[string]any{
			"type": "h2",
			"host": []any{"a.example.com", "b.example.com"},
		},
	}
	tr := extractTransport(m)
	if tr.Network != "h2" {
		t.Errorf("Network = %q, want %q", tr.Network, "h2")
	}
	if len(tr.Hosts) != 2 || tr.Hosts[0] != "a.example.com" || tr.Hosts[1] != "b.example.com" {
		t.Errorf("Hosts = %v, want [a.example.com b.example.com]", tr.Hosts)
	}
}

// ---------------------------------------------------------------------------
// parseHostList tests
// ---------------------------------------------------------------------------

func TestParseHostList_String(t *testing.T) {
	got := parseHostList("example.com")
	if len(got) != 1 || got[0] != "example.com" {
		t.Errorf("got %v, want [example.com]", got)
	}
}

func TestParseHostList_StringSlice(t *testing.T) {
	got := parseHostList([]string{"a.com", "b.com"})
	if len(got) != 2 || got[0] != "a.com" || got[1] != "b.com" {
		t.Errorf("got %v, want [a.com b.com]", got)
	}
}

func TestParseHostList_AnySlice(t *testing.T) {
	got := parseHostList([]any{"x.com", "y.com"})
	if len(got) != 2 || got[0] != "x.com" || got[1] != "y.com" {
		t.Errorf("got %v, want [x.com y.com]", got)
	}
}

func TestParseHostList_EmptyString(t *testing.T) {
	got := parseHostList("")
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestParseHostList_Nil(t *testing.T) {
	got := parseHostList(nil)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// URI conversion — smoke tests verifying call sites still compile and
// preserve expected URI behavior.
// ---------------------------------------------------------------------------

func TestURI_Trojan_Basic(t *testing.T) {
	raw := `{"type":"trojan","server":"tr.example.com","server_port":443,"password":"testpass"}`
	o := ExportOutbound{Tag: "trojan-basic", Raw: json.RawMessage(raw)}
	uri := outboundToURI(o)
	if uri == "" {
		t.Fatal("expected non-empty URI")
	}
	if !strings.HasPrefix(uri, "trojan://") {
		t.Errorf("URI = %q, want trojan:// prefix", uri)
	}
}

func TestURI_VMess_Basic(t *testing.T) {
	raw := `{"type":"vmess","server":"vm.example.com","server_port":443,"uuid":"a1b2c3d4-e5f6-7890-abcd-ef1234567890"}`
	o := ExportOutbound{Tag: "vmess-basic", Raw: json.RawMessage(raw)}
	uri := outboundToURI(o)
	if uri == "" {
		t.Fatal("expected non-empty URI")
	}
	if !strings.HasPrefix(uri, "vmess://") {
		t.Errorf("URI = %q, want vmess:// prefix", uri)
	}
}

func TestURI_VLess_Basic(t *testing.T) {
	raw := `{"type":"vless","server":"vl.example.com","server_port":443,"uuid":"b2c3d4e5-f6a7-8901-bcde-f12345678901"}`
	o := ExportOutbound{Tag: "vless-basic", Raw: json.RawMessage(raw)}
	uri := outboundToURI(o)
	if uri == "" {
		t.Fatal("expected non-empty URI")
	}
	if !strings.HasPrefix(uri, "vless://") {
		t.Errorf("URI = %q, want vless:// prefix", uri)
	}
}

func TestURI_VMess_WS_TLS(t *testing.T) {
	raw := `{
		"type":"vmess","server":"vm-ws.example.com","server_port":443,
		"uuid":"c3d4e5f6-a7b8-9012-cdef-123456789012",
		"tls":{"enabled":true,"server_name":"ws.example.com"},
		"transport":{"type":"ws","path":"/ws","headers":{"Host":"ws.example.com"}}
	}`
	o := ExportOutbound{Tag: "vmess-ws-uri", Raw: json.RawMessage(raw)}
	uri := outboundToURI(o)
	if uri == "" {
		t.Fatal("expected non-empty URI")
	}
	if len(uri) < 8 || uri[:8] != "vmess://" {
		t.Errorf("URI = %q, want vmess:// prefix", uri)
	}
	// Decode the base64 payload and verify key fields.
	encoded := uri[8:]
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}
	var vm map[string]any
	if err := json.Unmarshal(decoded, &vm); err != nil {
		t.Fatalf("JSON unmarshal error: %v decoded=%q", err, string(decoded))
	}
	if vm["net"] != "ws" {
		t.Errorf("net = %v, want ws", vm["net"])
	}
	if vm["path"] != "/ws" {
		t.Errorf("path = %v, want /ws", vm["path"])
	}
	if vm["host"] != "ws.example.com" {
		t.Errorf("host = %v, want ws.example.com", vm["host"])
	}
	if vm["tls"] != "tls" {
		t.Errorf("tls = %v, want tls", vm["tls"])
	}
}

func TestURI_Trojan_WS(t *testing.T) {
	raw := `{
		"type":"trojan","server":"tr-ws.example.com","server_port":443,
		"password":"trojanpass",
		"tls":{"enabled":true,"server_name":"ws.example.com"},
		"transport":{"type":"ws","path":"/ws","headers":{"Host":"ws.example.com"}}
	}`
	o := ExportOutbound{Tag: "trojan-ws-uri", Raw: json.RawMessage(raw)}
	uri := outboundToURI(o)
	if uri == "" {
		t.Fatal("expected non-empty URI")
	}
	if !strings.HasPrefix(uri, "trojan://") {
		t.Errorf("URI = %q, want trojan:// prefix", uri)
	}
}

func TestURI_VLess_GRPC(t *testing.T) {
	raw := `{
		"type":"vless","server":"vl-grpc.example.com","server_port":443,
		"uuid":"d0e1f2a3-b4c5-6789-defa-890123456789",
		"tls":{"enabled":true},
		"transport":{"type":"grpc","service_name":"MyService"}
	}`
	o := ExportOutbound{Tag: "vless-grpc-uri", Raw: json.RawMessage(raw)}
	uri := outboundToURI(o)
	if uri == "" {
		t.Fatal("expected non-empty URI")
	}
	if !strings.HasPrefix(uri, "vless://") {
		t.Errorf("URI = %q, want vless:// prefix", uri)
	}
}

func TestURI_EmptyPasswordTrojan(t *testing.T) {
	raw := `{"type":"trojan","server":"tr.example.com","server_port":443,"password":""}`
	o := ExportOutbound{Tag: "empty", Raw: json.RawMessage(raw)}
	uri := outboundToURI(o)
	if uri != "" {
		t.Errorf("expected empty URI for empty password, got %q", uri)
	}
}

// ---------------------------------------------------------------------------
// outboundToClashProxy — Hysteria2 Phase 2 structured tests
// ---------------------------------------------------------------------------

func TestConvertHysteria2_Full(t *testing.T) {
	raw := `{
		"type":"hysteria2","server":"hy2.example.com","server_port":443,
		"password":"hy2pass",
		"server_ports":["443","8080:9090"],
		"hop_interval":"12s",
		"up_mbps":100,"down_mbps":500,
		"obfs":{"type":"salamander","password":"obfspass"},
		"tls":{
			"enabled":true,"server_name":"hy2.example.com",
			"alpn":["h3"],
			"utls":{"enabled":true,"fingerprint":"chrome"}
		}
	}`
	want := map[string]any{
		"name":               "hy2-full",
		"type":               "hysteria2",
		"server":             "hy2.example.com",
		"port":               443,
		"password":           "hy2pass",
		"udp":                true,
		"tls":                true,
		"sni":                "hy2.example.com",
		"alpn":               []string{"h3"},
		"client-fingerprint": "chrome",
		"ports":              "443,8080-9090",
		"hop-interval":       12,
		"up":                 100,
		"down":               500,
		"obfs":               "salamander",
		"obfs-password":      "obfspass",
	}
	got := convertOutbound(t, raw, "hy2-full")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertHysteria2_InvalidOptionals(t *testing.T) {
	// All optional fields have invalid/nonpositive/empty values; they must be omitted.
	raw := `{
		"type":"hysteria2","server":"hy2-safe.example.com","server_port":443,
		"password":"hy2pass",
		"server_ports":null,
		"hop_interval":"",
		"up_mbps":0,"down_mbps":-1
	}`
	want := map[string]any{
		"name":     "hy2-safe",
		"type":     "hysteria2",
		"server":   "hy2-safe.example.com",
		"port":     443,
		"password": "hy2pass",
		"udp":      true,
	}
	got := convertOutbound(t, raw, "hy2-safe")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertHysteria2_ObfsNestedTakesPrecedence(t *testing.T) {
	// Nested canonical obfs should be used, ignoring legacy scalar keys.
	raw := `{
		"type":"hysteria2","server":"hy2-nest.example.com","server_port":443,
		"password":"nestpass",
		"obfs":{"type":"nested-type","password":"nested-pass"},
		"obfs-password":"legacy-pass"
	}`
	want := map[string]any{
		"name":          "hy2-nest",
		"type":          "hysteria2",
		"server":        "hy2-nest.example.com",
		"port":          443,
		"password":      "nestpass",
		"udp":           true,
		"obfs":          "nested-type",
		"obfs-password": "nested-pass",
	}
	got := convertOutbound(t, raw, "hy2-nest")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertHysteria2_ObfsScalarLegacy(t *testing.T) {
	// Without nested obfs, fall back to legacy scalar fields.
	raw := `{
		"type":"hysteria2","server":"hy2-leg.example.com","server_port":443,
		"password":"legpass",
		"obfs":"salamander",
		"obfs-password":"scalar-pass"
	}`
	want := map[string]any{
		"name":          "hy2-leg",
		"type":          "hysteria2",
		"server":        "hy2-leg.example.com",
		"port":          443,
		"password":      "legpass",
		"udp":           true,
		"obfs":          "salamander",
		"obfs-password": "scalar-pass",
	}
	got := convertOutbound(t, raw, "hy2-leg")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertHysteria2_FractionalBandwidth(t *testing.T) {
	// Fractional up_mbps/down_mbps must not be truncated to integers.
	raw := `{
		"type":"hysteria2","server":"hy2-frac.example.com","server_port":443,
		"password":"hy2frac",
		"up_mbps":10.5,"down_mbps":20.25
	}`
	want := map[string]any{
		"name":     "hy2-frac",
		"type":     "hysteria2",
		"server":   "hy2-frac.example.com",
		"port":     443,
		"password": "hy2frac",
		"udp":      true,
		"up":       10.5,
		"down":     20.25,
	}
	got := convertOutbound(t, raw, "hy2-frac")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

// ---------------------------------------------------------------------------
// outboundToClashProxy — Shadowsocks Phase 2 structured tests
// ---------------------------------------------------------------------------

func TestConvertShadowsocks_ObfsLocal(t *testing.T) {
	// obfs-local canonical → plugin obfs + nested mode/host + unknown preserved.
	raw := `{
		"type":"ss","server":"ss-obfs.example.com","server_port":8443,
		"method":"aes-128-gcm","password":"sspass",
		"plugin":"obfs-local",
		"plugin_opts":"obfs=http;obfs-host=edge.example.com;unknown=keep;custom=hello"
	}`
	want := map[string]any{
		"name":     "ss-obfs",
		"type":     "ss",
		"server":   "ss-obfs.example.com",
		"port":     8443,
		"cipher":   "aes-128-gcm",
		"password": "sspass",
		"udp":      true,
		"plugin":   "obfs",
		"plugin-opts": map[string]any{
			"mode":    "http",
			"host":    "edge.example.com",
			"unknown": "keep",
			"custom":  "hello",
		},
	}
	got := convertOutbound(t, raw, "ss-obfs")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertShadowsocks_ObfsLocalEscaped(t *testing.T) {
	// Escaped \; and \= in values.
	raw := `{
		"type":"ss","server":"ss-esc.example.com","server_port":443,
		"method":"chacha20-ietf-poly1305","password":"escapepass",
		"plugin":"obfs-local",
		"plugin_opts":"obfs=http;obfs-host=edge\\;host;custom=/a\\=b"
	}`
	want := map[string]any{
		"name":     "ss-esc",
		"type":     "ss",
		"server":   "ss-esc.example.com",
		"port":     443,
		"cipher":   "chacha20-ietf-poly1305",
		"password": "escapepass",
		"udp":      true,
		"plugin":   "obfs",
		"plugin-opts": map[string]any{
			"mode":   "http",
			"host":   "edge;host",
			"custom": "/a=b",
		},
	}
	got := convertOutbound(t, raw, "ss-esc")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertShadowsocks_V2RayPlugin(t *testing.T) {
	// v2ray-plugin with booleans, strings, mux=4 reversal, bare flag, and unknown.
	raw := `{
		"type":"ss","server":"ss-v2ray.example.com","server_port":443,
		"method":"chacha20-ietf-poly1305","password":"ssv2ray",
		"plugin":"v2ray-plugin",
		"plugin_opts":"tls;mode=websocket;host=api.example.com;path=/ws;mux=4;unknown_flag;extra=val"
	}`
	want := map[string]any{
		"name":     "ss-v2ray",
		"type":     "ss",
		"server":   "ss-v2ray.example.com",
		"port":     443,
		"cipher":   "chacha20-ietf-poly1305",
		"password": "ssv2ray",
		"udp":      true,
		"plugin":   "v2ray-plugin",
		"plugin-opts": map[string]any{
			"tls":          true,
			"mode":         "websocket",
			"host":         "api.example.com",
			"path":         "/ws",
			"mux":          true,
			"unknown_flag": true,
			"extra":        "val",
		},
	}
	got := convertOutbound(t, raw, "ss-v2ray")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertShadowsocks_NoPluginOpts(t *testing.T) {
	// Plugin exists with no opts → emit plugin only, no plugin-opts.
	raw := `{
		"type":"ss","server":"ss-nopts.example.com","server_port":443,
		"method":"aes-256-gcm","password":"nopts",
		"plugin":"obfs-local"
	}`
	want := map[string]any{
		"name":     "ss-nopts",
		"type":     "ss",
		"server":   "ss-nopts.example.com",
		"port":     443,
		"cipher":   "aes-256-gcm",
		"password": "nopts",
		"udp":      true,
		"plugin":   "obfs",
	}
	got := convertOutbound(t, raw, "ss-nopts")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertShadowsocks_Malformed(t *testing.T) {
	// Malformed fragments: empty, bare '=', empty keys → gracefully skipped.
	raw := `{
		"type":"ss","server":"ss-malf.example.com","server_port":443,
		"method":"aes-256-gcm","password":"malformed",
		"plugin":"obfs-local",
		"plugin_opts":"obfs=http;;;=;obfs-host=host;;;"
	}`
	want := map[string]any{
		"name":     "ss-malf",
		"type":     "ss",
		"server":   "ss-malf.example.com",
		"port":     443,
		"cipher":   "aes-256-gcm",
		"password": "malformed",
		"udp":      true,
		"plugin":   "obfs",
		"plugin-opts": map[string]any{
			"mode": "http",
			"host": "host",
		},
	}
	got := convertOutbound(t, raw, "ss-malf")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertShadowsocks_SimpleObfsAlias(t *testing.T) {
	// simple-obfs alias maps to plugin: obfs with same opts conversion.
	raw := `{
		"type":"ss","server":"ss-simp.example.com","server_port":443,
		"method":"aes-128-gcm","password":"simplepass",
		"plugin":"simple-obfs",
		"plugin_opts":"obfs=tls;obfs-host=simple.example.com"
	}`
	want := map[string]any{
		"name":     "ss-simp",
		"type":     "ss",
		"server":   "ss-simp.example.com",
		"port":     443,
		"cipher":   "aes-128-gcm",
		"password": "simplepass",
		"udp":      true,
		"plugin":   "obfs",
		"plugin-opts": map[string]any{
			"mode": "tls",
			"host": "simple.example.com",
		},
	}
	got := convertOutbound(t, raw, "ss-simp")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertShadowsocks_NoPlugin(t *testing.T) {
	// No plugin at all → no plugin/plugin-opts keys.
	raw := `{
		"type":"ss","server":"ss-plain.example.com","server_port":443,
		"method":"aes-256-gcm","password":"plainpass"
	}`
	want := map[string]any{
		"name":     "ss-plain",
		"type":     "ss",
		"server":   "ss-plain.example.com",
		"port":     443,
		"cipher":   "aes-256-gcm",
		"password": "plainpass",
		"udp":      true,
	}
	got := convertOutbound(t, raw, "ss-plain")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

// ---------------------------------------------------------------------------
// unescapeValue unit tests  (Phase 2 Oracle fix 1)
// ---------------------------------------------------------------------------

func TestUnescapeValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "escaped semicolon",
			input: "\\;",
			want:  ";",
		},
		{
			name:  "escaped equals",
			input: "\\=",
			want:  "=",
		},
		{
			name:  "escaped backslash",
			input: "\\\\",
			want:  "\\",
		},
		{
			name:  "preserve backslash before ordinary char",
			input: "\\p",
			want:  "\\p",
		},
		{
			name:  "trailing backslash preserved",
			input: "end\\",
			want:  "end\\",
		},
		{
			name:  "backslash path preserved",
			input: "C:\\path\\to\\file",
			want:  "C:\\path\\to\\file",
		},
		{
			name:  "mixed escapes and literals",
			input: "edge\\;host",
			want:  "edge;host",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no escapes",
			input: "plain",
			want:  "plain",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := unescapeValue(tc.input)
			if got != tc.want {
				t.Errorf("unescapeValue(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Shadowsocks — v2ray-plugin boolean recognition (Phase 2 Oracle fix 2)
// ---------------------------------------------------------------------------

func TestConvertShadowsocks_V2RayPluginBoolValues(t *testing.T) {
	// Known bool keys: true/false, 1/0, case-insensitive, mux=4 → true.
	raw := `{
		"type":"ss","server":"ss-v2ray-bool.example.com","server_port":443,
		"method":"chacha20-ietf-poly1305","password":"ssv2bool",
		"plugin":"v2ray-plugin",
		"plugin_opts":"tls=true;mux=false;skip-cert-verify=1;v2ray-http-upgrade=0;mux=4"
	}`
	want := map[string]any{
		"name":     "ss-v2ray-bool",
		"type":     "ss",
		"server":   "ss-v2ray-bool.example.com",
		"port":     443,
		"cipher":   "chacha20-ietf-poly1305",
		"password": "ssv2bool",
		"udp":      true,
		"plugin":   "v2ray-plugin",
		"plugin-opts": map[string]any{
			"tls":                true,
			"mux":                true, // mux=false then mux=4 wins (last)
			"skip-cert-verify":   true,
			"v2ray-http-upgrade": false,
		},
	}
	got := convertOutbound(t, raw, "ss-v2ray-bool")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

func TestConvertShadowsocks_V2RayPluginUnrecognizedBool(t *testing.T) {
	// Unrecognized string values for known bool keys stay as strings.
	raw := `{
		"type":"ss","server":"ss-v2ray-weird.example.com","server_port":443,
		"method":"chacha20-ietf-poly1305","password":"ssv2weird",
		"plugin":"v2ray-plugin",
		"plugin_opts":"tls=weird;mux=4"
	}`
	want := map[string]any{
		"name":     "ss-v2ray-weird",
		"type":     "ss",
		"server":   "ss-v2ray-weird.example.com",
		"port":     443,
		"cipher":   "chacha20-ietf-poly1305",
		"password": "ssv2weird",
		"udp":      true,
		"plugin":   "v2ray-plugin",
		"plugin-opts": map[string]any{
			"tls": "weird", // not coerced to false
			"mux": true,    // mux=4 → true (special case)
		},
	}
	got := convertOutbound(t, raw, "ss-v2ray-weird")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

// ---------------------------------------------------------------------------
// Shadowsocks — backslash preservation in plugin_opts (Phase 2 Oracle fix 1)
// ---------------------------------------------------------------------------

func TestConvertShadowsocks_ObfsLocalBackslashPreserved(t *testing.T) {
	// Backslashes before non-special characters are preserved.
	// The canonical value C:\path\to\file keeps its backslashes.
	raw := `{
		"type":"ss","server":"ss-bs.example.com","server_port":443,
		"method":"aes-128-gcm","password":"bspass",
		"plugin":"obfs-local",
		"plugin_opts":"obfs=http;obfs-host=edge.example.com;custom=C:\\path\\to\\file"
	}`
	want := map[string]any{
		"name":     "ss-bs",
		"type":     "ss",
		"server":   "ss-bs.example.com",
		"port":     443,
		"cipher":   "aes-128-gcm",
		"password": "bspass",
		"udp":      true,
		"plugin":   "obfs",
		"plugin-opts": map[string]any{
			"mode":   "http",
			"host":   "edge.example.com",
			"custom": "C:\\path\\to\\file",
		},
	}
	got := convertOutbound(t, raw, "ss-bs")
	if diff := diffMaps(want, got); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}
}

// ---------------------------------------------------------------------------
// URI — Hysteria2 nested obfs
// ---------------------------------------------------------------------------

func TestURI_Hysteria2_NestedObfs(t *testing.T) {
	raw := `{
		"type":"hysteria2","server":"hy2.example.com","server_port":443,
		"password":"hy2pass",
		"obfs":{"type":"salamander","password":"sally"}
	}`
	o := ExportOutbound{Tag: "hy2-obfs-uri", Raw: json.RawMessage(raw)}
	uri := outboundToURI(o)
	if uri == "" {
		t.Fatal("expected non-empty URI")
	}
	if !strings.HasPrefix(uri, "hysteria2://") {
		t.Errorf("URI = %q, want hysteria2:// prefix", uri)
	}
	if !strings.Contains(uri, "obfs=salamander") {
		t.Errorf("URI = %q, want obfs=salamander", uri)
	}
	if !strings.Contains(uri, "obfs-password=sally") {
		t.Errorf("URI = %q, want obfs-password=sally", uri)
	}
}

func TestURI_Hysteria2_ScalarObfs(t *testing.T) {
	// URI with legacy scalar obfs should still work.
	raw := `{
		"type":"hysteria2","server":"hy2-leg.example.com","server_port":443,
		"password":"legpass",
		"obfs":"salamander",
		"obfs-password":"legacy-sally"
	}`
	o := ExportOutbound{Tag: "hy2-leg-uri", Raw: json.RawMessage(raw)}
	uri := outboundToURI(o)
	if uri == "" {
		t.Fatal("expected non-empty URI")
	}
	if !strings.Contains(uri, "obfs=salamander") {
		t.Errorf("URI = %q, want obfs=salamander", uri)
	}
	if !strings.Contains(uri, "obfs-password=legacy-sally") {
		t.Errorf("URI = %q, want obfs-password=legacy-sally", uri)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: Clash JSON → canonical sing-box → Clash proxy map
// ---------------------------------------------------------------------------

// parseClashRoundTrip parses Clash JSON, extracts the first (only) node,
// wraps it as an ExportOutbound, and runs outboundToClashProxy.
func parseClashRoundTrip(t *testing.T, clashJSON string) map[string]any {
	t.Helper()
	nodes, err := subscription.ParseGeneralSubscription([]byte(clashJSON))
	if err != nil {
		t.Fatalf("ParseGeneralSubscription: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	o := ExportOutbound{Tag: nodes[0].Tag, Raw: nodes[0].RawOptions}
	return outboundToClashProxy(o)
}

// TestRoundTrip_ClashIngest_Export verifies semantic Clash-ingest →
// canonical RawOptions → outboundToClashProxy round trips.  Each subtest
// feeds a Clash JSON proxy, parses it through the subscription package,
// converts back via outboundToClashProxy, and asserts semantically relevant
// fields in the output map.
func TestRoundTrip_ClashIngest_Export(t *testing.T) {
	t.Run("VMess_WS_full", func(t *testing.T) {
		input := `{"proxies":[{
			"name":"vmess-ws-rt","type":"vmess","server":"vm-ws.example.com","port":443,
			"uuid":"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			"tls":true,"servername":"ws.example.com",
			"alpn":["h2","http/1.1"],"client-fingerprint":"chrome",
			"network":"ws",
			"ws-opts":{"path":"/api/ws","headers":{"Host":"ws.example.com","X-Custom":"val1"},
			           "max-early-data":2048,"early-data-header-name":"Sec-WebSocket-Protocol"}
		}]}`
		got := parseClashRoundTrip(t, input)
		want := map[string]any{
			"name":               "vmess-ws-rt",
			"type":               "vmess",
			"server":             "vm-ws.example.com",
			"port":               443,
			"uuid":               "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			"alterId":            0,
			"cipher":             "auto",
			"udp":                true,
			"network":            "ws",
			"tls":                true,
			"servername":         "ws.example.com",
			"client-fingerprint": "chrome",
			"ws-opts": map[string]any{
				"path":                   "/api/ws",
				"headers":                map[string]string{"Host": "ws.example.com", "X-Custom": "val1"},
				"max-early-data":         2048,
				"early-data-header-name": "Sec-WebSocket-Protocol",
			},
		}
		// ALPN: parser drops h2 for WS transport, keeping only http/1.1.
		want["alpn"] = []string{"http/1.1"}
		if diff := diffMaps(want, got); diff != "" {
			t.Errorf("mismatch:\n%s", diff)
		}
		// Assert NO top-level legacy ws-path or ws-headers.
		if _, ok := got["ws-path"]; ok {
			t.Error("output has legacy ws-path key")
		}
		if _, ok := got["ws-headers"]; ok {
			t.Error("output has legacy ws-headers key")
		}
	})

	t.Run("Trojan_GRPC", func(t *testing.T) {
		input := `{"proxies":[{
			"name":"tr-grpc-rt","type":"trojan","server":"tr-grpc.example.com","port":443,
			"password":"grpcpass",
			"network":"grpc",
			"grpc-opts":{"grpc-service-name":"TrojanGRPC"}
		}]}`
		got := parseClashRoundTrip(t, input)
		want := map[string]any{
			"name":      "tr-grpc-rt",
			"type":      "trojan",
			"server":    "tr-grpc.example.com",
			"port":      443,
			"password":  "grpcpass",
			"udp":       true,
			"tls":       true,
			"sni":       "tr-grpc.example.com",
			"network":   "grpc",
			"grpc-opts": map[string]any{"grpc-service-name": "TrojanGRPC"},
		}
		if diff := diffMaps(want, got); diff != "" {
			t.Errorf("mismatch:\n%s", diff)
		}
	})

	t.Run("VMess_H2_normalized_to_http", func(t *testing.T) {
		// Clash h2 input is normalized by the parser to transport.type=http,
		// so the Clash export must output network=http + http-opts.
		input := `{"proxies":[{
			"name":"vmess-h2-rt","type":"vmess","server":"vm-h2.example.com","port":443,
			"uuid":"d4e5f6a7-b8c9-0123-defa-234567890123",
			"tls":true,
			"network":"h2",
			"h2-opts":{"path":"/h2-path","host":["h2a.example.com","h2b.example.com"]}
		}]}`
		got := parseClashRoundTrip(t, input)
		want := map[string]any{
			"name":    "vmess-h2-rt",
			"type":    "vmess",
			"server":  "vm-h2.example.com",
			"port":    443,
			"uuid":    "d4e5f6a7-b8c9-0123-defa-234567890123",
			"alterId": 0,
			"cipher":  "auto",
			"udp":     true,
			"network": "http", // parser normalised h2→http
			"tls":     true,
			"http-opts": map[string]any{
				"path": "/h2-path",
				"host": []string{"h2a.example.com", "h2b.example.com"},
			},
		}
		if diff := diffMaps(want, got); diff != "" {
			t.Errorf("mismatch:\n%s", diff)
		}
		// Assert NOT h2-opts / network=h2.
		if _, ok := got["h2-opts"]; ok {
			t.Error("output has h2-opts key when parser normalised h2→http")
		}
	})

	t.Run("VLESS_Reality", func(t *testing.T) {
		input := `{"proxies":[{
			"name":"vless-reality-rt","type":"vless","server":"vl-real.example.com","port":443,
			"uuid":"c5d6e7f8-a9b0-1234-cdef-345678901234",
			"tls":true,"servername":"real.example.com",
			"client-fingerprint":"safari",
			"network":"tcp",
			"reality-opts":{"public-key":"pk12345","short-id":"sid67890"}
		}]}`
		got := parseClashRoundTrip(t, input)
		want := map[string]any{
			"name":               "vless-reality-rt",
			"type":               "vless",
			"server":             "vl-real.example.com",
			"port":               443,
			"uuid":               "c5d6e7f8-a9b0-1234-cdef-345678901234",
			"udp":                true,
			"tls":                true,
			"servername":         "real.example.com",
			"client-fingerprint": "safari",
			"reality-opts": map[string]any{
				"public-key": "pk12345",
				"short-id":   "sid67890",
			},
		}
		if diff := diffMaps(want, got); diff != "" {
			t.Errorf("mismatch:\n%s", diff)
		}
	})

	t.Run("SS_Obfs_plugin", func(t *testing.T) {
		input := `{"proxies":[{
			"name":"ss-obfs-rt","type":"ss","server":"ss-obfs.example.com","port":8443,
			"cipher":"aes-128-gcm","password":"sspass",
			"plugin":"obfs",
			"plugin-opts":{"mode":"http","host":"edge.example.com"}
		}]}`
		got := parseClashRoundTrip(t, input)
		want := map[string]any{
			"name":     "ss-obfs-rt",
			"type":     "ss",
			"server":   "ss-obfs.example.com",
			"port":     8443,
			"cipher":   "aes-128-gcm",
			"password": "sspass",
			"udp":      true,
			"plugin":   "obfs",
			"plugin-opts": map[string]any{
				"mode": "http",
				"host": "edge.example.com",
			},
		}
		if diff := diffMaps(want, got); diff != "" {
			t.Errorf("mismatch:\n%s", diff)
		}
	})

	t.Run("HY2_full", func(t *testing.T) {
		input := `{"proxies":[{
			"name":"hy2-rt","type":"hysteria2","server":"hy2.example.com","port":443,
			"password":"hy2pass",
			"ports":"443,8080-9090","hop-interval":12,
			"up":100,"down":500,
			"obfs":"salamander","obfs-password":"secret",
			"tls":true,"servername":"hy2.example.com",
			"alpn":["h3"],"client-fingerprint":"chrome"
		}]}`
		got := parseClashRoundTrip(t, input)
		want := map[string]any{
			"name":               "hy2-rt",
			"type":               "hysteria2",
			"server":             "hy2.example.com",
			"port":               443,
			"password":           "hy2pass",
			"udp":                true,
			"tls":                true,
			"sni":                "hy2.example.com",
			"alpn":               []string{"h3"},
			"client-fingerprint": "chrome",
			"ports":              "443,8080-9090",
			"hop-interval":       12,
			"up":                 100,
			"down":               500,
			"obfs":               "salamander",
			"obfs-password":      "secret",
		}
		if diff := diffMaps(want, got); diff != "" {
			t.Errorf("mismatch:\n%s", diff)
		}
	})

	// Edge: unsupported outbound type still yields nil/empty without panic.
	t.Run("unsupported_type", func(t *testing.T) {
		input := `{"proxies":[{"name":"bad-type","type":"snell","server":"1.1.1.1","port":443}]}`
		nodes, err := subscription.ParseGeneralSubscription([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 0 {
			t.Fatalf("expected 0 nodes for unsupported type, got %d", len(nodes))
		}
	})

	// Edge: malformed/zero optional HY2 fields produce sensible output
	// (HY2 parser always enables TLS, so export always includes tls/sni).
	t.Run("HY2_invalid_optionals", func(t *testing.T) {
		input := `{"proxies":[{
			"name":"hy2-bad-opt","type":"hysteria2","server":"hy2-bad.example.com","port":443,
			"password":"pass",
			"ports":"","hop-interval":0,"up":0,"down":0
		}]}`
		got := parseClashRoundTrip(t, input)
		want := map[string]any{
			"name":     "hy2-bad-opt",
			"type":     "hysteria2",
			"server":   "hy2-bad.example.com",
			"port":     443,
			"password": "pass",
			"udp":      true,
			"tls":      true,
			"sni":      "hy2-bad.example.com",
		}
		if diff := diffMaps(want, got); diff != "" {
			t.Errorf("mismatch:\n%s", diff)
		}
	})
}

// TestURI_ClashIngest verifies that URIs exported from Clash-ingested nodes
// preserve network/path/host/service fields correctly.
func TestURI_ClashIngest(t *testing.T) {
	t.Run("VMess_WS", func(t *testing.T) {
		input := `{"proxies":[{
			"name":"vmess-ws-uri","type":"vmess","server":"vm-ws.example.com","port":443,
			"uuid":"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			"tls":true,"servername":"ws.example.com",
			"network":"ws",
			"ws-opts":{"path":"/api/ws","headers":{"Host":"ws.example.com"}}
		}]}`
		nodes, err := subscription.ParseGeneralSubscription([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 1 {
			t.Fatalf("expected 1 node, got %d", len(nodes))
		}
		o := ExportOutbound{Tag: nodes[0].Tag, Raw: nodes[0].RawOptions}
		uri := outboundToURI(o)
		if uri == "" {
			t.Fatal("expected non-empty URI")
		}
		if !strings.HasPrefix(uri, "vmess://") {
			t.Errorf("URI = %q, want vmess:// prefix", uri)
		}
		encoded := uri[8:]
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatalf("base64 decode error: %v (%q)", err, encoded)
		}
		var vm map[string]any
		if err := json.Unmarshal(decoded, &vm); err != nil {
			t.Fatalf("JSON unmarshal error: %v decoded=%q", err, string(decoded))
		}
		if vm["net"] != "ws" {
			t.Errorf("net = %v, want ws", vm["net"])
		}
		if vm["path"] != "/api/ws" {
			t.Errorf("path = %v, want /api/ws", vm["path"])
		}
		if vm["host"] != "ws.example.com" {
			t.Errorf("host = %v, want ws.example.com", vm["host"])
		}
		if vm["tls"] != "tls" {
			t.Errorf("tls = %v, want tls", vm["tls"])
		}
	})
}
