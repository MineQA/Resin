package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Resinat/Resin/internal/service"
)

// ---------------------------------------------------------------------------
// GET /api/v1/proxy-sources
// ---------------------------------------------------------------------------

func TestHandleListProxySources(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/proxy-sources", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var sources []service.ProxySource
	if err := json.Unmarshal(rec.Body.Bytes(), &sources); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("expected non-empty source list")
	}
	// Spot-check known sources.
	found := false
	for _, s := range sources {
		if s.ID == "speedx-http" && s.Name != "" && s.URL != "" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'speedx-http' in source list")
	}
}

func TestHandleListProxySources_NoAuth(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/proxy-sources", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("expected auth failure for unauthenticated request")
	}
}

// ---------------------------------------------------------------------------
// POST /api/v1/proxy-sources/fetch
// ---------------------------------------------------------------------------

func TestHandleFetchProxySources_SingleSource(t *testing.T) {
	// Override fetcher to return mock data.
	origFetcher := service.OverrideHTTPGetBody(func(url string) ([]byte, error) {
		return []byte("1.2.3.4:80\n5.6.7.8:8080\n"), nil
	})
	defer origFetcher()

	srv, cp, _ := newControlPlaneTestServer(t)
	_ = cp // cp needed for route registration, but we don't use it directly

	body := map[string]interface{}{
		"source": "speedx-http",
	}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-sources/fetch", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp service.FetchSourcesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	r := resp.Results[0]
	if r.SourceID != "speedx-http" {
		t.Errorf("SourceID = %q, want %q", r.SourceID, "speedx-http")
	}
	if r.TotalExtracted != 2 {
		t.Errorf("TotalExtracted = %d, want 2", r.TotalExtracted)
	}
	if r.Error != "" {
		t.Errorf("unexpected error: %s", r.Error)
	}
	if len(r.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(r.Candidates))
	}
	if r.Candidates[0].Proxy != "1.2.3.4:80" {
		t.Errorf("candidate[0] = %q, want %q", r.Candidates[0].Proxy, "1.2.3.4:80")
	}
}

func TestHandleFetchProxySources_AllSources(t *testing.T) {
	origFetcher := service.OverrideHTTPGetBody(func(url string) ([]byte, error) {
		return []byte("192.168.1.1:80\n"), nil
	})
	defer origFetcher()

	srv, _, _ := newControlPlaneTestServer(t)

	body := map[string]interface{}{
		"source": "all",
		"limit":  1000,
	}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-sources/fetch", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp service.FetchSourcesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != len(service.BuiltInProxySources) {
		t.Errorf("expected %d results, got %d", len(service.BuiltInProxySources), len(resp.Results))
	}
	for _, r := range resp.Results {
		if r.Error != "" {
			t.Errorf("source %s: unexpected error: %s", r.SourceID, r.Error)
		}
	}
}

func TestHandleFetchProxySources_InvalidSource(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	body := map[string]interface{}{
		"source": "does-not-exist",
	}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-sources/fetch", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestHandleFetchProxySources_EmptyBody(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-sources/fetch", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleFetchProxySources_MissingSource(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	body := map[string]interface{}{}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-sources/fetch", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// POST /api/v1/proxy-check/import
// ---------------------------------------------------------------------------

func TestHandleProxyCheckImport_Success(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	body := map[string]interface{}{
		"proxies":         []string{"1.2.3.4:80", "5.6.7.8:8080", "socks5://9.10.11.12:1080"},
		"confirm_checked": true,
	}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/import", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp service.ImportProxiesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ImportedCount != 3 {
		t.Errorf("ImportedCount = %d, want 3", resp.ImportedCount)
	}
	if resp.SubscriptionID != "proxy-check-import" {
		t.Errorf("SubscriptionID = %q, want %q", resp.SubscriptionID, "proxy-check-import")
	}
	if len(resp.NodeHashes) != 3 {
		t.Errorf("expected 3 node hashes, got %d", len(resp.NodeHashes))
	}
	// All hashes should be unique.
	seen := make(map[string]bool)
	for _, h := range resp.NodeHashes {
		if seen[h] {
			t.Errorf("duplicate hash: %s", h)
		}
		seen[h] = true
	}
}

func TestHandleProxyCheckImport_RequiresConfirmation(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	// Missing confirm_checked should be rejected.
	body := map[string]interface{}{
		"proxies": []string{"1.2.3.4:80"},
	}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/import", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleProxyCheckImport_EmptyProxies(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	body := map[string]interface{}{
		"proxies":         []string{},
		"confirm_checked": true,
	}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/import", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleProxyCheckImport_InvalidFormat(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	body := map[string]interface{}{
		"proxies":         []string{"not-a-proxy-line"},
		"confirm_checked": true,
	}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/import", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleProxyCheckImport_WithoutAuth(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	body := map[string]interface{}{
		"proxies":         []string{"1.2.3.4:80"},
		"confirm_checked": true,
	}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/import", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("expected auth failure for unauthenticated request")
	}
}
