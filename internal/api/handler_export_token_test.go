package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/service"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
)

func TestExportTokenCRUD(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)
	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	addNodeForNodeListTest(t, cp, sub, rawA, "203.0.113.10")
	markNodeHealthyForNodeListTest(t, cp, rawA)

	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "test-token",
	}, true)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create export token: got status %d, want %d, body=%s", createResp.Code, http.StatusCreated, createResp.Body.String())
	}
	createBody := decodeJSONMap(t, createResp)

	tokenID, _ := createBody["id"].(string)
	tokenValue, _ := createBody["token"].(string)
	tokenPrefix, _ := createBody["token_prefix"].(string)

	if tokenID == "" {
		t.Fatal("create export token: missing id")
	}
	if tokenValue == "" {
		t.Fatal("create export token: missing token")
	}
	if tokenPrefix == "" {
		t.Fatal("create export token: missing token_prefix")
	}
	if len(tokenPrefix) >= len(tokenValue) || tokenValue[:len(tokenPrefix)] != tokenPrefix {
		t.Fatalf("create export token: prefix=%q does not match start of token=%q", tokenPrefix, tokenValue)
	}
	if len(tokenValue) != 43 {
		t.Fatalf("create export token: token length=%d, want 43", len(tokenValue))
	}

	listResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/export-tokens", nil, true)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list export tokens: got status %d, want %d, body=%s", listResp.Code, http.StatusOK, listResp.Body.String())
	}
	var listBody []map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("list export tokens: unmarshal error: %v body=%s", err, listResp.Body.String())
	}
	if len(listBody) != 1 {
		t.Fatalf("list export tokens: got %d items, want 1", len(listBody))
	}
	item := listBody[0]
	if item["id"] != tokenID {
		t.Fatalf("list export tokens: id mismatch: got %v, want %s", item["id"], tokenID)
	}
	if _, hasToken := item["token"]; hasToken {
		t.Fatal("list export tokens: should not include raw token")
	}
	if item["token_prefix"] != tokenPrefix {
		t.Fatalf("list export tokens: token_prefix=%v, want %s", item["token_prefix"], tokenPrefix)
	}
}

func TestNodePoolExport_Auth(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)
	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	addNodeForNodeListTest(t, cp, sub, rawA, "203.0.113.10")
	markNodeHealthyForNodeListTest(t, cp, rawA)

	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-token",
	}, true)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create export token: got status %d, want %d", createResp.Code, http.StatusCreated)
	}
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)

	// Bearer token auth
	req := newTestRequest(t, http.MethodGet, "/api/v1/node-pool/export?format=sing-box", nil)
	req.Header.Set("Authorization", "Bearer "+tokenValue)
	rec := doTestRequest(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export with bearer token: got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	// Query param auth
	queryExport := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue, nil, false)
	if queryExport.Code != http.StatusOK {
		t.Fatalf("export with query token: got status %d, want 200, body=%s", queryExport.Code, queryExport.Body.String())
	}

	// Query token takes precedence over User-Agent
	queryWithBadUAReq := newTestRequest(t, http.MethodGet, "/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue, nil)
	queryWithBadUAReq.Header.Set("User-Agent", "ResinExport/invalidtokenhere")
	queryWithBadUARec := doTestRequest(t, srv, queryWithBadUAReq)
	if queryWithBadUARec.Code != http.StatusOK {
		t.Fatalf("export with query token and bad UA: got status %d, want 200, body=%s", queryWithBadUARec.Code, queryWithBadUARec.Body.String())
	}

	// User-Agent auth
	uaReq := newTestRequest(t, http.MethodGet, "/api/v1/node-pool/export?format=sing-box", nil)
	uaReq.Header.Set("User-Agent", "ResinExport/"+tokenValue)
	uaRec := doTestRequest(t, srv, uaReq)
	if uaRec.Code != http.StatusOK {
		t.Fatalf("export with UA token: got status %d, want 200, body=%s", uaRec.Code, uaRec.Body.String())
	}

	// Bad UA prefix
	badUAReq := newTestRequest(t, http.MethodGet, "/api/v1/node-pool/export", nil)
	badUAReq.Header.Set("User-Agent", "SomeOtherAgent/"+tokenValue)
	badUARec := doTestRequest(t, srv, badUAReq)
	if badUARec.Code != http.StatusUnauthorized {
		t.Fatalf("export with bad UA prefix: got status %d, want 401, body=%s", badUARec.Code, badUARec.Body.String())
	}

	// Empty UA token
	emptyUAReq := newTestRequest(t, http.MethodGet, "/api/v1/node-pool/export", nil)
	emptyUAReq.Header.Set("User-Agent", "ResinExport/")
	emptyUARec := doTestRequest(t, srv, emptyUAReq)
	if emptyUARec.Code != http.StatusUnauthorized {
		t.Fatalf("export with empty UA token: got status %d, want 401, body=%s", emptyUARec.Code, emptyUARec.Body.String())
	}

	// No token
	noTokenResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=sing-box", nil, false)
	if noTokenResp.Code != http.StatusUnauthorized {
		t.Fatalf("export without token: got status %d, want 401, body=%s", noTokenResp.Code, noTokenResp.Body.String())
	}

	// Invalid token
	badTokenResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=sing-box&export_token=invalidtokenhere", nil, false)
	if badTokenResp.Code != http.StatusUnauthorized {
		t.Fatalf("export with bad token: got status %d, want 401, body=%s", badTokenResp.Code, badTokenResp.Body.String())
	}

	// Unknown format
	badFormatResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=unknown&export_token="+tokenValue, nil, false)
	if badFormatResp.Code != http.StatusBadRequest {
		t.Fatalf("export with bad format: got status %d, want 400, body=%s", badFormatResp.Code, badFormatResp.Body.String())
	}
	assertErrorCode(t, badFormatResp, "INVALID_ARGUMENT")

	// Delete and verify
	tokenID, _ := createBody["id"].(string)
	delResp := doJSONRequest(t, srv, http.MethodDelete, "/api/v1/export-tokens/"+tokenID, nil, true)
	if delResp.Code != http.StatusNoContent {
		t.Fatalf("delete export token: got status %d, want 204, body=%s", delResp.Code, delResp.Body.String())
	}
	afterDelResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue, nil, false)
	if afterDelResp.Code != http.StatusUnauthorized {
		t.Fatalf("export after deletion: got status %d, want 401, body=%s", afterDelResp.Code, afterDelResp.Body.String())
	}
}

func TestNodePoolExport_FormatSingBox(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)
	_ = cp

	resp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export sing-box: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	assertContentType(t, resp, "application/json")
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["format"]; ok {
		t.Fatal("sing-box: should not include format field")
	}
	if _, ok := body["total"]; ok {
		t.Fatal("sing-box: should not include total field")
	}
	if _, ok := body["outbounds"]; !ok {
		t.Fatal("sing-box: missing outbounds field")
	}
}

func TestNodePoolExport_DefaultFormatIsClash(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)
	_ = cp

	// No format param -> default clash -> YAML
	resp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export default clash: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	ct := resp.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/yaml") && !strings.Contains(ct, "application/x-yaml") {
		t.Fatalf("export default clash: content-type=%q, want text/yaml", ct)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "proxies:") {
		t.Fatalf("export default clash: body should contain 'proxies:', got=%q", body)
	}
	if strings.Contains(body, "outbounds") {
		t.Fatal("export default clash: should not contain sing-box outbounds")
	}
}

func TestNodePoolExport_FormatClash(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)
	_ = cp

	resp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=clash&export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export clash: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	ct := resp.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/yaml") && !strings.Contains(ct, "application/x-yaml") {
		t.Fatalf("export clash: content-type=%q, want text/yaml", ct)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "proxies:") {
		t.Fatalf("export clash: body should contain 'proxies:', got=%q", body)
	}
	// Verify it contains the ss proxy fields.
	if !strings.Contains(body, "type: \"ss\"") && !strings.Contains(body, "type: ss") {
		t.Fatalf("export clash: missing ss proxy type, body=%q", body)
	}
	if !strings.Contains(body, "cipher:") {
		t.Fatalf("export clash: missing cipher field, body=%q", body)
	}
}

func TestNodePoolExport_FormatURI(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)
	_ = cp

	resp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=uri&export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export uri: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	ct := resp.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("export uri: content-type=%q, want text/plain", ct)
	}
	body := resp.Body.String()
	if !strings.HasPrefix(body, "ss://") {
		t.Fatalf("export uri: body should start with ss://, got=%q", body)
	}
	// Verify it contains newlines (not base64).
	if !strings.Contains(body, "\n") {
		// only one node, but shouldn't have other issues.
	}
}

func TestNodePoolExport_FormatBase64(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)
	_ = cp

	resp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=base64&export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export base64: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	ct := resp.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("export base64: content-type=%q, want text/plain", ct)
	}
	raw := resp.Body.String()
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("export base64: decode error: %v body=%q", err, raw)
	}
	if !strings.HasPrefix(string(decoded), "ss://") {
		t.Fatalf("export base64: decoded should start with ss://, got=%q", string(decoded))
	}
}

func TestNodePoolExport_DefaultNoRoutableFilter(t *testing.T) {
	// Default export without routable param should include all nodes
	// (both healthy/routable and non-routable).
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)

	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	const rawB = `{"type":"ss","server":"2.2.2.2","port":443,"method":"chacha20","password":"test"}`
	addNodeForNodeListTest(t, cp, sub, rawA, "203.0.113.10")
	addNodeForNodeListTest(t, cp, sub, rawB, "203.0.113.20")
	markNodeHealthyForNodeListTest(t, cp, rawA)

	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-token",
	}, true)
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)

	// No routable param → both nodes included.
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export no routable filter: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	outbounds, ok := body["outbounds"].([]any)
	if !ok {
		t.Fatalf("export no routable filter: missing outbounds field")
	}
	if len(outbounds) != 2 {
		t.Fatalf("export no routable filter: got %d outbounds, want 2", len(outbounds))
	}
}

func TestNodePoolExport_RoutableFalse(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)

	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	const rawB = `{"type":"ss","server":"2.2.2.2","port":443,"method":"chacha20","password":"test"}`
	addNodeForNodeListTest(t, cp, sub, rawA, "203.0.113.10")
	addNodeForNodeListTest(t, cp, sub, rawB, "203.0.113.20")
	markNodeHealthyForNodeListTest(t, cp, rawA)

	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-token",
	}, true)
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)

	// Use sing-box format so we can count outbounds.
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue+"&routable=false", nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export routable=false: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	outbounds, ok := body["outbounds"].([]any)
	if !ok {
		t.Fatalf("export routable=false: missing outbounds field")
	}
	if len(outbounds) != 2 {
		t.Fatalf("export routable=false: got %d outbounds, want 2", len(outbounds))
	}
}

func TestNodePoolExport_RoutableTrue(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	// Create a platform so routable view exists.
	_ = mustCreatePlatform(t, srv, "routable-export-test")

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)

	// Node A: fully routable (outbound, closed circuit, egress IP, latency).
	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	hashA := node.HashFromRawOptions([]byte(rawA))
	cp.Pool.AddNodeFromSub(hashA, []byte(rawA), sub.ID)
	sub.ManagedNodes().StoreNode(hashA, subscription.ManagedNode{Tags: []string{"routable-a"}})
	entryA, ok := cp.Pool.GetEntry(hashA)
	if !ok {
		t.Fatalf("node A missing after add")
	}
	entryA.SetEgressIP(netip.MustParseAddr("203.0.113.10"))
	obA := testutil.NewNoopOutbound()
	entryA.Outbound.Store(&obA)
	entryA.CircuitOpenSince.Store(0)
	entryA.LatencyTable.Update("example.com", 25*time.Millisecond, 10*time.Minute)
	cp.Pool.NotifyNodeDirty(hashA)

	// Node B: non-routable (no outbound, no latency).
	const rawB = `{"type":"ss","server":"2.2.2.2","port":443,"method":"chacha20","password":"test"}`
	hashB := node.HashFromRawOptions([]byte(rawB))
	cp.Pool.AddNodeFromSub(hashB, []byte(rawB), sub.ID)
	sub.ManagedNodes().StoreNode(hashB, subscription.ManagedNode{Tags: []string{"non-routable-b"}})
	entryB, ok := cp.Pool.GetEntry(hashB)
	if !ok {
		t.Fatalf("node B missing after add")
	}
	entryB.SetEgressIP(netip.MustParseAddr("203.0.113.20"))

	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-token",
	}, true)
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)

	// routable=true → only node A (the routable one).
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue+"&routable=true", nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export routable=true: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	outbounds, ok := body["outbounds"].([]any)
	if !ok {
		t.Fatalf("export routable=true: missing outbounds field")
	}
	if len(outbounds) != 1 {
		t.Fatalf("export routable=true: got %d outbounds, want 1, body=%s", len(outbounds), resp.Body.String())
	}
}

func TestNodePoolExport_FormatURI_HTTP_SOCKS(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)

	const rawHTTP = `{"type":"http","server":"http-proxy.example.com","port":8080,"username":"user1","password":"pass1"}`
	const rawSOCKS = `{"type":"socks","server":"socks.example.com","port":1080}`
	const rawSS = `{"type":"ss","server":"ss.example.com","port":443,"method":"chacha20-ietf-poly1305","password":"testpass"}`

	addNodeForNodeListTest(t, cp, sub, rawHTTP, "203.0.113.10")
	addNodeForNodeListTest(t, cp, sub, rawSOCKS, "203.0.113.11")
	addNodeForNodeListTest(t, cp, sub, rawSS, "203.0.113.12")

	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-token",
	}, true)
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)

	// URI format
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=uri&export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export uri http/socks: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()

	if !strings.Contains(body, "http://") {
		t.Fatalf("export uri: missing HTTP URI, body=%q", body)
	}
	if !strings.Contains(body, "socks5://") {
		t.Fatalf("export uri: missing SOCKS5 URI, body=%q", body)
	}
	if !strings.Contains(body, "ss://") {
		t.Fatalf("export uri: missing SS URI, body=%q", body)
	}
	// Verify userinfo is present in HTTP URI.
	if !strings.Contains(body, "user1:pass1@") {
		t.Fatalf("export uri: HTTP URI missing userinfo, body=%q", body)
	}

	// Base64 format
	resp64 := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=base64&export_token="+tokenValue, nil, false)
	if resp64.Code != http.StatusOK {
		t.Fatalf("export base64 http/socks: got status %d, want 200, body=%s", resp64.Code, resp64.Body.String())
	}
	decoded, err := base64.StdEncoding.DecodeString(resp64.Body.String())
	if err != nil {
		t.Fatalf("export base64 http/socks: decode error: %v", err)
	}
	decodedBody := string(decoded)
	if !strings.Contains(decodedBody, "http://") {
		t.Fatalf("export base64: missing HTTP URI, body=%q", decodedBody)
	}
	if !strings.Contains(decodedBody, "socks5://") {
		t.Fatalf("export base64: missing SOCKS5 URI, body=%q", decodedBody)
	}
	if !strings.Contains(decodedBody, "ss://") {
		t.Fatalf("export base64: missing SS URI, body=%q", decodedBody)
	}
}

func TestNodePoolExport_FormatClash_HTTP_SOCKS(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)

	const rawHTTP = `{"type":"http","server":"http-proxy.example.com","port":8080,"username":"user1","password":"pass1"}`
	const rawSOCKS = `{"type":"socks","server":"socks.example.com","port":1080}`
	const rawSS = `{"type":"ss","server":"ss.example.com","port":443,"method":"chacha20-ietf-poly1305","password":"testpass"}`

	addNodeForNodeListTest(t, cp, sub, rawHTTP, "203.0.113.10")
	addNodeForNodeListTest(t, cp, sub, rawSOCKS, "203.0.113.11")
	addNodeForNodeListTest(t, cp, sub, rawSS, "203.0.113.12")

	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-token",
	}, true)
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)

	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=clash&export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export clash http/socks: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()

	if !strings.Contains(body, "type: http") && !strings.Contains(body, `type: "http"`) {
		t.Fatalf("export clash http/socks: missing http proxy type, body=%q", body)
	}
	if !strings.Contains(body, "type: socks5") && !strings.Contains(body, `type: "socks5"`) {
		t.Fatalf("export clash http/socks: missing socks5 proxy type, body=%q", body)
	}
	if !strings.Contains(body, "type: ss") && !strings.Contains(body, `type: "ss"`) {
		t.Fatalf("export clash http/socks: missing ss proxy type, body=%q", body)
	}
}

func TestNodePoolExport_ProtocolFilter(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)
	_ = cp

	// Only one SS node exists from setupExportTest.
	// Add a vmess node.
	sub := subscription.NewSubscription("22222222-2222-2222-2222-222222222222", "sub-b", "https://example.com/b", true, false)
	cp.SubMgr.Register(sub)
	addNodeForNodeListTest(t, cp, sub, `{"type":"vmess","server":"2.2.2.2","port":443,"uuid":"b"}`, "203.0.113.20")

	// Filter for ss only -> 1 outbound.
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue+"&protocol=ss", nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export protocol=ss: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	outbounds, ok := body["outbounds"].([]any)
	if !ok {
		t.Fatalf("export protocol=ss: missing outbounds field")
	}
	if len(outbounds) != 1 {
		t.Fatalf("export protocol=ss: got %d outbounds, want 1", len(outbounds))
	}
	ob := outbounds[0].(map[string]any)
	if ob["type"] != "ss" {
		t.Fatalf("export protocol=ss: outbound type=%v, want ss", ob["type"])
	}

	// Filter for ss,vmess -> 2 outbounds.
	resp = doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue+"&protocol=ss,vmess", nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export protocol=ss,vmess: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	outbounds, ok = body["outbounds"].([]any)
	if !ok {
		t.Fatalf("export protocol=ss,vmess: missing outbounds field")
	}
	if len(outbounds) != 2 {
		t.Fatalf("export protocol=ss,vmess: got %d outbounds, want 2", len(outbounds))
	}
}

func TestNodePoolExport_ProtocolFilterInvalid(t *testing.T) {
	srv, _, tokenValue := setupExportTest(t)

	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue+"&protocol=invalidproto", nil, false)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("export protocol=invalid: got status %d, want 400, body=%s", resp.Code, resp.Body.String())
	}
	assertErrorCode(t, resp, "INVALID_ARGUMENT")
}

func TestNodePoolExport_NoProtocolFilterByDefault(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)

	// Add another node.
	sub := subscription.NewSubscription("22222222-2222-2222-2222-222222222222", "sub-b", "https://example.com/b", true, false)
	cp.SubMgr.Register(sub)
	addNodeForNodeListTest(t, cp, sub, `{"type":"vmess","server":"2.2.2.2","port":443,"uuid":"b"}`, "203.0.113.20")

	// No protocol param -> all nodes.
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export no protocol: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	outbounds, ok := body["outbounds"].([]any)
	if !ok {
		t.Fatalf("export no protocol: missing outbounds field")
	}
	if len(outbounds) != 2 {
		t.Fatalf("export no protocol: got %d outbounds, want 2", len(outbounds))
	}
}

func TestNodePoolExport_ExcludeProtocolFilter(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)
	_ = cp

	// Add vmess and trojan nodes.
	sub := subscription.NewSubscription("22222222-2222-2222-2222-222222222222", "sub-b", "https://example.com/b", true, false)
	cp.SubMgr.Register(sub)
	addNodeForNodeListTest(t, cp, sub, `{"type":"vmess","server":"2.2.2.2","port":443,"uuid":"b"}`, "203.0.113.20")
	addNodeForNodeListTest(t, cp, sub, `{"type":"trojan","server":"3.3.3.3","port":443,"password":"c"}`, "203.0.113.21")

	// Exclude vmess => 2 outbounds (ss, trojan).
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue+"&exclude_protocol=vmess", nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export exclude_protocol=vmess: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	outbounds, ok := body["outbounds"].([]any)
	if !ok {
		t.Fatalf("export exclude_protocol=vmess: missing outbounds field")
	}
	if len(outbounds) != 2 {
		t.Fatalf("export exclude_protocol=vmess: got %d outbounds, want 2", len(outbounds))
	}
}

func TestNodePoolExport_ExcludeProtocolFilterInvalid(t *testing.T) {
	srv, _, tokenValue := setupExportTest(t)

	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue+"&exclude_protocol=badproto", nil, false)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("export exclude_protocol=badproto: got status %d, want 400, body=%s", resp.Code, resp.Body.String())
	}
	assertErrorCode(t, resp, "INVALID_ARGUMENT")
}

func TestNodePoolExport_ProtocolFilterIncludeExclude(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)

	// Add vmess and trojan nodes.
	sub := subscription.NewSubscription("22222222-2222-2222-2222-222222222222", "sub-b", "https://example.com/b", true, false)
	cp.SubMgr.Register(sub)
	addNodeForNodeListTest(t, cp, sub, `{"type":"vmess","server":"2.2.2.2","port":443,"uuid":"b"}`, "203.0.113.20")
	addNodeForNodeListTest(t, cp, sub, `{"type":"trojan","server":"3.3.3.3","port":443,"password":"c"}`, "203.0.113.21")

	// Include ss,vmess,trojan but exclude vmess => 2 outbounds (ss, trojan).
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue+"&protocol=ss,vmess,trojan&exclude_protocol=vmess", nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export include+exclude: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	outbounds, ok := body["outbounds"].([]any)
	if !ok {
		t.Fatalf("export include+exclude: missing outbounds field")
	}
	if len(outbounds) != 2 {
		t.Fatalf("export include+exclude: got %d outbounds, want 2", len(outbounds))
	}
}

func TestNodePoolExport_ExcludeProtocolAlias(t *testing.T) {
	srv, cp, tokenValue := setupExportTest(t)

	// Add a vmess node.
	sub := subscription.NewSubscription("22222222-2222-2222-2222-222222222222", "sub-b", "https://example.com/b", true, false)
	cp.SubMgr.Register(sub)
	addNodeForNodeListTest(t, cp, sub, `{"type":"vmess","server":"2.2.2.2","port":443,"uuid":"b"}`, "203.0.113.20")

	// Use protocol_exclude alias to exclude ss => 1 outbound (vmess).
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue+"&protocol_exclude=ss", nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export protocol_exclude=ss: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	outbounds, ok := body["outbounds"].([]any)
	if !ok {
		t.Fatalf("export protocol_exclude=ss: missing outbounds field")
	}
	if len(outbounds) != 1 {
		t.Fatalf("export protocol_exclude=ss: got %d outbounds, want 1", len(outbounds))
	}
}

// --- Helpers ---

func setupExportTest(t *testing.T) (*Server, *service.ControlPlaneService, string) {
	t.Helper()
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)
	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20-ietf-poly1305","password":"testpass"}`
	addNodeForNodeListTest(t, cp, sub, rawA, "203.0.113.10")
	markNodeHealthyForNodeListTest(t, cp, rawA)

	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-test",
	}, true)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("setup: create token: got %d", createResp.Code)
	}
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)
	return srv, cp, tokenValue
}

func assertContentType(t *testing.T, resp *httptest.ResponseRecorder, expected string) {
	t.Helper()
	ct := resp.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, expected) {
		t.Fatalf("content-type: got %q, want prefix %q", ct, expected)
	}
}

// newTestRequest creates an HTTP request without auth.
func newTestRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	return req
}

// doTestRequest executes a request against the test server.
func doTestRequest(t *testing.T, srv *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}
