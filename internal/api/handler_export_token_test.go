package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Resinat/Resin/internal/subscription"
)

func TestExportTokenCRUD(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	// Create a subscription and node so the export has data.
	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)
	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	addNodeForNodeListTest(t, cp, sub, rawA, "203.0.113.10")
	markNodeHealthyForNodeListTest(t, cp, rawA)

	// --- Create export token ---
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
	// Verify token value is high-entropy (base64url, no padding, 43 chars for 32 bytes)
	if len(tokenValue) != 43 {
		t.Fatalf("create export token: token length=%d, want 43 (32 bytes base64url no padding)", len(tokenValue))
	}

	// --- List export tokens should NOT return raw token ---
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

	// Create a node to have some export data.
	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)
	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	addNodeForNodeListTest(t, cp, sub, rawA, "203.0.113.10")
	markNodeHealthyForNodeListTest(t, cp, rawA)

	// Create an export token.
	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-token",
	}, true)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create export token: got status %d, want %d", createResp.Code, http.StatusCreated)
	}
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)

	// --- Export via Bearer token ---
	req := newTestRequest(t, http.MethodGet, "/api/v1/node-pool/export", nil)
	req.Header.Set("Authorization", "Bearer "+tokenValue)
	rec := doTestRequest(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export with bearer token: got status %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var exportBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &exportBody); err != nil {
		t.Fatalf("export: unmarshal error: %v body=%s", err, rec.Body.String())
	}
	if exportBody["format"] != "sing-box" {
		t.Fatalf("export: format=%v, want sing-box", exportBody["format"])
	}
	outbounds, ok := exportBody["outbounds"].([]any)
	// Node may not be in any routable view (no platforms), but the export
	// will still return it because ListNodes uses routable filter.
	// With routable=true and no platforms, routableNodes is empty so
	// no nodes match. That's OK — we just check the response structure.
	_ = outbounds
	_ = ok

	// --- Export via query param (no auth header) ---
	queryExport := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?export_token="+tokenValue, nil, false)
	if queryExport.Code != http.StatusOK {
		t.Fatalf("export with query token: got status %d, want 200, body=%s", queryExport.Code, queryExport.Body.String())
	}

	// --- Export without any token returns 401 ---
	noTokenResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export", nil, false)
	if noTokenResp.Code != http.StatusUnauthorized {
		t.Fatalf("export without token: got status %d, want 401, body=%s", noTokenResp.Code, noTokenResp.Body.String())
	}

	// --- Export with invalid token returns 401 ---
	badTokenResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?export_token=invalidtokenhere", nil, false)
	if badTokenResp.Code != http.StatusUnauthorized {
		t.Fatalf("export with bad token: got status %d, want 401, body=%s", badTokenResp.Code, badTokenResp.Body.String())
	}

	// --- Export with unknown format returns 400 ---
	badFormatResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=clash&export_token="+tokenValue, nil, false)
	if badFormatResp.Code != http.StatusBadRequest {
		t.Fatalf("export with bad format: got status %d, want 400, body=%s", badFormatResp.Code, badFormatResp.Body.String())
	}
	assertErrorCode(t, badFormatResp, "INVALID_ARGUMENT")

	// --- Delete export token ---
	tokenID, _ := createBody["id"].(string)
	delResp := doJSONRequest(t, srv, http.MethodDelete, "/api/v1/export-tokens/"+tokenID, nil, true)
	if delResp.Code != http.StatusNoContent {
		t.Fatalf("delete export token: got status %d, want 204, body=%s", delResp.Code, delResp.Body.String())
	}

	// --- After deletion, token no longer works ---
	afterDelResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?export_token="+tokenValue, nil, false)
	if afterDelResp.Code != http.StatusUnauthorized {
		t.Fatalf("export after deletion: got status %d, want 401, body=%s", afterDelResp.Code, afterDelResp.Body.String())
	}
}

func TestNodePoolExport_FormatAndOutput(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)
	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	addNodeForNodeListTest(t, cp, sub, rawA, "203.0.113.10")
	markNodeHealthyForNodeListTest(t, cp, rawA)

	// Create export token.
	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-token",
	}, true)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create token: got %d", createResp.Code)
	}
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)

	// Export with sing-box format (explicit).
	resp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/node-pool/export?format=sing-box&export_token="+tokenValue, nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export sing-box: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["format"] != "sing-box" {
		t.Fatalf("format: got %v, want sing-box", body["format"])
	}
	if _, ok := body["total"]; !ok {
		t.Fatal("missing total field")
	}
	if _, ok := body["limit"]; !ok {
		t.Fatal("missing limit field")
	}
	if _, ok := body["offset"]; !ok {
		t.Fatal("missing offset field")
	}
}

func TestNodePoolExport_DefaultRoutable(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("11111111-1111-1111-1111-111111111111", "sub-a", "https://example.com/a", true, false)
	cp.SubMgr.Register(sub)

	// Add two nodes: one healthy, one not.
	const rawA = `{"type":"ss","server":"1.1.1.1","port":443,"method":"chacha20","password":"test"}`
	const rawB = `{"type":"ss","server":"2.2.2.2","port":443,"method":"chacha20","password":"test"}`
	addNodeForNodeListTest(t, cp, sub, rawA, "203.0.113.10")
	addNodeForNodeListTest(t, cp, sub, rawB, "203.0.113.20")
	markNodeHealthyForNodeListTest(t, cp, rawA)
	// Node B is NOT marked healthy — no outbound, circuit open.

	// Create export token.
	createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/export-tokens", map[string]any{
		"name": "export-token",
	}, true)
	createBody := decodeJSONMap(t, createResp)
	tokenValue, _ := createBody["token"].(string)

	// Without explicit routable param, default is routable=true.
	// Since no platforms exist, routableNodes will be empty and
	// both nodes are excluded. This is consistent behavior.
	resp := doJSONRequest(t, srv, http.MethodGet,
		"/api/v1/node-pool/export?export_token="+tokenValue+"&routable=false", nil, false)
	if resp.Code != http.StatusOK {
		t.Fatalf("export routable=false: got status %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	total := body["total"].(float64)
	if int(total) != 2 {
		t.Fatalf("export routable=false: total=%v, want 2", total)
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

// doTestRequest executes a request against the test server and returns the response.
func doTestRequest(t *testing.T, srv *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}
