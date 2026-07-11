package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/topology"
)

// ---------------------------------------------------------------------------
// ExtractProxyCandidates
// ---------------------------------------------------------------------------

func TestExtractProxyCandidates_Basic(t *testing.T) {
	data := []byte("1.2.3.4:80\n5.6.7.8:8080\n9.10.11.12:443\n")
	candidates := ExtractProxyCandidates(data, "test-src", 100)
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d: %v", len(candidates), candidates)
	}
	if candidates[0].Proxy != "1.2.3.4:80" {
		t.Errorf("candidate[0] proxy = %q, want %q", candidates[0].Proxy, "1.2.3.4:80")
	}
	if candidates[0].SourceID != "test-src" {
		t.Errorf("candidate[0] source_id = %q, want %q", candidates[0].SourceID, "test-src")
	}
}

func TestExtractProxyCandidates_WithProtocol(t *testing.T) {
	data := []byte("http://1.2.3.4:80\nsocks5://5.6.7.8:1080\nhttps://9.10.11.12:443\n")
	candidates := ExtractProxyCandidates(data, "test-src", 100)
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}
	if candidates[0].Proxy != "http://1.2.3.4:80" {
		t.Errorf("candidate[0] proxy = %q, want %q", candidates[0].Proxy, "http://1.2.3.4:80")
	}
	if candidates[1].Proxy != "socks5://5.6.7.8:1080" {
		t.Errorf("candidate[1] proxy = %q, want %q", candidates[1].Proxy, "socks5://5.6.7.8:1080")
	}
}

func TestExtractProxyCandidates_Dedupe(t *testing.T) {
	data := []byte("1.2.3.4:80\n1.2.3.4:80\nhttp://1.2.3.4:80\n1.2.3.4:80\n")
	candidates := ExtractProxyCandidates(data, "test-src", 100)
	// The first line is ip:port, second is same (deduped), third has protocol (different string). 
	// Actually: "1.2.3.4:80" normalized, "http://1.2.3.4:80" is a different string so not deduped.
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates (1 ip:port + 1 http://ip:port), got %d: %v", len(candidates), candidates)
	}
}

func TestExtractProxyCandidates_DedupeNormalized(t *testing.T) {
	// Same IP:port repeated with different protocol prefixes should dedupe
	// only when the normalized ip:port is the same AND the full string is the same.
	data := []byte("1.2.3.4:80\n1.2.3.4:80\n1.2.3.4:80\n")
	candidates := ExtractProxyCandidates(data, "test-src", 100)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate after dedupe, got %d", len(candidates))
	}
}

func TestExtractProxyCandidates_WithAuth(t *testing.T) {
	data := []byte("1.2.3.4:80:user:pass\n5.6.7.8:3128\n")
	candidates := ExtractProxyCandidates(data, "test-src", 100)
	// Both lines should be extracted; the auth-bearing format is preserved.
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(candidates), candidates)
	}
	if candidates[0].Proxy != "1.2.3.4:80:user:pass" {
		t.Errorf("candidate[0] proxy = %q, want %q", candidates[0].Proxy, "1.2.3.4:80:user:pass")
	}
	if candidates[1].Proxy != "5.6.7.8:3128" {
		t.Errorf("candidate[1] proxy = %q, want %q", candidates[1].Proxy, "5.6.7.8:3128")
	}
}

func TestExtractProxyCandidates_ProtocolWithAuth(t *testing.T) {
	data := []byte("http://user:pass@1.2.3.4:80\nsocks5://user:pass@5.6.7.8:1080\n")
	candidates := ExtractProxyCandidates(data, "test-src", 100)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(candidates), candidates)
	}
	if candidates[0].Proxy != "http://user:pass@1.2.3.4:80" {
		t.Errorf("candidate[0] proxy = %q, want %q", candidates[0].Proxy, "http://user:pass@1.2.3.4:80")
	}
	if candidates[1].Proxy != "socks5://user:pass@5.6.7.8:1080" {
		t.Errorf("candidate[1] proxy = %q, want %q", candidates[1].Proxy, "socks5://user:pass@5.6.7.8:1080")
	}
}

func TestExtractProxyCandidates_DedupeAuthVsPlain(t *testing.T) {
	// Same host:port with and without auth — first occurrence wins.
	data := []byte("1.2.3.4:80:user:pass\n1.2.3.4:80\nhttp://1.2.3.4:80\n")
	candidates := ExtractProxyCandidates(data, "test-src", 100)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate (deduped by host:port), got %d: %v", len(candidates), candidates)
	}
	// The first occurrence (auth-bearing) should be preserved.
	if candidates[0].Proxy != "1.2.3.4:80:user:pass" {
		t.Errorf("candidate[0] proxy = %q, want %q (first occurrence wins)", candidates[0].Proxy, "1.2.3.4:80:user:pass")
	}
}

func TestExtractProxyCandidates_EmptyInput(t *testing.T) {
	candidates := ExtractProxyCandidates([]byte(""), "test-src", 100)
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}

func TestExtractProxyCandidates_NoMatches(t *testing.T) {
	data := []byte("# comment line\nthis is not a proxy\ngarbage\n")
	candidates := ExtractProxyCandidates(data, "test-src", 100)
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}

func TestExtractProxyCandidates_DoesNotExtractSSURI(t *testing.T) {
	data := []byte("ss://YWVzLTEyOC1nY206dGVzdA@example.com:8388")
	candidates := ExtractProxyCandidates(data, "test-src", 100)
	if len(candidates) != 0 {
		t.Fatalf("expected ss URI to be ignored by safe proxy source extractor, got %d", len(candidates))
	}
}

func TestExtractProxyCandidates_Limit(t *testing.T) {
	var lines []byte
	for i := 0; i < 100; i++ {
		lines = append(lines, []byte(fmt.Sprintf("1.2.3.%d:%d\n", i%256, 80+i%100))...)
	}
	candidates := ExtractProxyCandidates(lines, "test-src", 10)
	if len(candidates) != 10 {
		t.Errorf("expected 10 candidates (limit), got %d", len(candidates))
	}
}

func TestExtractProxyCandidates_DefaultLimit(t *testing.T) {
	var lines []byte
	for i := 0; i < 100000; i++ {
		lines = append(lines, []byte(fmt.Sprintf("10.0.0.%d:%d\n", i%256, 80+i%100))...)
	}
	candidates := ExtractProxyCandidates(lines, "test-src", 0) // 0 means default 50000
	if len(candidates) != 50000 {
		t.Errorf("expected 50000 candidates (default limit), got %d", len(candidates))
	}
}

// ---------------------------------------------------------------------------
// FetchProxySources - validation
// ---------------------------------------------------------------------------

func TestFetchProxySources_InvalidSource(t *testing.T) {
	cp := &ControlPlaneService{}
	_, err := cp.FetchProxySources(FetchSourcesRequest{Source: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
	svcErr, ok := err.(*ServiceError)
	if !ok {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", svcErr.Code)
	}
}

func TestFetchProxySources_NegativeLimit(t *testing.T) {
	cp := &ControlPlaneService{}
	negLimit := -1
	_, err := cp.FetchProxySources(FetchSourcesRequest{Source: "speedx-http", Limit: &negLimit})
	if err == nil {
		t.Fatal("expected error for negative limit")
	}
	svcErr, ok := err.(*ServiceError)
	if !ok {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Errorf("code = %q, want INVALID_ARGUMENT", svcErr.Code)
	}
}

func TestFetchProxySources_ExceedsMaxLimit(t *testing.T) {
	cp := &ControlPlaneService{}
	bigLimit := 50001
	_, err := cp.FetchProxySources(FetchSourcesRequest{Source: "speedx-http", Limit: &bigLimit})
	if err == nil {
		t.Fatal("expected error for exceeding max limit")
	}
	svcErr, ok := err.(*ServiceError)
	if !ok {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Errorf("code = %q, want INVALID_ARGUMENT", svcErr.Code)
	}
}

// ---------------------------------------------------------------------------
// FetchProxySources with mock fetcher
// ---------------------------------------------------------------------------

func TestFetchProxySources_SingleSource(t *testing.T) {
	origFetcher := httpGetBody
	httpGetBody = func(url string) ([]byte, error) {
		return []byte("1.2.3.4:80\n5.6.7.8:8080\n"), nil
	}
	defer func() { httpGetBody = origFetcher }()

	cp := &ControlPlaneService{}
	resp, err := cp.FetchProxySources(FetchSourcesRequest{Source: "speedx-http"})
	if err != nil {
		t.Fatalf("FetchProxySources: %v", err)
	}
	if resp.TotalSources != 1 {
		t.Errorf("TotalSources = %d, want 1", resp.TotalSources)
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
	if r.ReturnedCount != 2 {
		t.Errorf("ReturnedCount = %d, want 2", r.ReturnedCount)
	}
	if r.Error != "" {
		t.Errorf("unexpected error: %s", r.Error)
	}
}

func TestFetchProxySources_AllSources_WithMock(t *testing.T) {
	origFetcher := httpGetBody
	httpGetBody = func(url string) ([]byte, error) {
		return []byte("192.168.1.1:80\n"), nil
	}
	defer func() { httpGetBody = origFetcher }()

	cp := &ControlPlaneService{}
	resp, err := cp.FetchProxySources(FetchSourcesRequest{Source: "all", Limit: intPtr(1000)})
	if err != nil {
		t.Fatalf("FetchProxySources: %v", err)
	}
	if resp.TotalSources != len(BuiltInProxySources) {
		t.Errorf("TotalSources = %d, want %d", resp.TotalSources, len(BuiltInProxySources))
	}
	if len(resp.Results) != len(BuiltInProxySources) {
		t.Fatalf("expected %d results, got %d", len(BuiltInProxySources), len(resp.Results))
	}
	for _, r := range resp.Results {
		if r.Error != "" {
			t.Errorf("source %s: unexpected error: %s", r.SourceID, r.Error)
		}
		if r.TotalExtracted != 1 {
			t.Errorf("source %s: TotalExtracted = %d, want 1", r.SourceID, r.TotalExtracted)
		}
	}
}

func TestFetchProxySources_FetchError(t *testing.T) {
	origFetcher := httpGetBody
	httpGetBody = func(url string) ([]byte, error) {
		return nil, errors.New("network error")
	}
	defer func() { httpGetBody = origFetcher }()

	cp := &ControlPlaneService{}
	resp, err := cp.FetchProxySources(FetchSourcesRequest{Source: "clarketm"})
	if err != nil {
		t.Fatalf("FetchProxySources: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	r := resp.Results[0]
	if r.Error == "" {
		t.Error("expected error in result, got empty")
	}
	if r.TotalExtracted != 0 {
		t.Errorf("TotalExtracted = %d, want 0", r.TotalExtracted)
	}
}

// ---------------------------------------------------------------------------
// ImportProxies
// ---------------------------------------------------------------------------

func TestImportProxies_RequiresConfirmation(t *testing.T) {
	cp := &ControlPlaneService{}
	// Nil ConfirmChecked (default) should be rejected.
	_, err := cp.ImportProxies(ImportProxiesRequest{Proxies: []string{"1.2.3.4:80"}})
	if err == nil {
		t.Fatal("expected error when confirm_checked is nil")
	}
	// Explicit false should be rejected.
	f := false
	_, err = cp.ImportProxies(ImportProxiesRequest{Proxies: []string{"1.2.3.4:80"}, ConfirmChecked: &f})
	if err == nil {
		t.Fatal("expected error when confirm_checked is false")
	}
}

func TestImportProxies_EmptyRequest(t *testing.T) {
	cp := &ControlPlaneService{}
	_, err := cp.ImportProxies(ImportProxiesRequest{Proxies: []string{}, ConfirmChecked: boolPtr(true)})
	if err == nil {
		t.Fatal("expected error for empty proxies")
	}
}

func TestImportProxies_TooMany(t *testing.T) {
	cp := &ControlPlaneService{}
	proxies := make([]string, 50001)
	_, err := cp.ImportProxies(ImportProxiesRequest{Proxies: proxies, ConfirmChecked: boolPtr(true)})
	if err == nil {
		t.Fatal("expected error for too many proxies")
	}
}

func TestImportProxies_InvalidFormat(t *testing.T) {
	cp := &ControlPlaneService{}
	_, err := cp.ImportProxies(ImportProxiesRequest{Proxies: []string{"not-a-proxy"}, ConfirmChecked: boolPtr(true)})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestImportProxies_Success(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              nil,
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})
	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
	}

	resp, err := cp.ImportProxies(ImportProxiesRequest{
		Proxies:        []string{"1.2.3.4:80", "5.6.7.8:8080", "socks5://9.10.11.12:1080"},
		ConfirmChecked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("ImportProxies: %v", err)
	}

	if resp.ImportedCount != 3 {
		t.Errorf("ImportedCount = %d, want 3", resp.ImportedCount)
	}
	if resp.SkippedCount != 0 {
		t.Errorf("SkippedCount = %d, want 0", resp.SkippedCount)
	}
	if resp.SubscriptionID != "proxy-check-import" {
		t.Errorf("SubscriptionID = %q, want %q", resp.SubscriptionID, "proxy-check-import")
	}
	if len(resp.NodeHashes) != 3 {
		t.Fatalf("expected 3 node hashes, got %d", len(resp.NodeHashes))
	}

	// Verify nodes were added to pool.
	for _, hStr := range resp.NodeHashes {
		h, err := node.ParseHex(hStr)
		if err != nil {
			t.Fatalf("ParseHex(%q): %v", hStr, err)
		}
		_, ok := pool.GetEntry(h)
		if !ok {
			t.Errorf("node %s not found in pool", hStr)
		}
	}

	// Verify ephemeral subscription was created.
	sub := subMgr.Lookup("proxy-check-import")
	if sub == nil {
		t.Fatal("ephemeral subscription not found")
	}
	if !sub.Ephemeral() {
		t.Error("expected ephemeral subscription")
	}
}

func TestImportProxies_Dedupe(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              nil,
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})
	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
	}

	// Same proxy imported twice — AddNodeFromSub is idempotent.
	resp, err := cp.ImportProxies(ImportProxiesRequest{
		Proxies:        []string{"1.2.3.4:80", "1.2.3.4:80"},
		ConfirmChecked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("ImportProxies: %v", err)
	}
	if resp.ImportedCount != 1 {
		t.Errorf("ImportedCount = %d, want 1", resp.ImportedCount)
	}
	if resp.SkippedCount != 1 {
		t.Errorf("SkippedCount = %d, want 1", resp.SkippedCount)
	}
	// Verify only one unique node entry in pool (deduped by hash).
	if pool.Size() != 1 {
		t.Errorf("pool size = %d, want 1 (deduped)", pool.Size())
	}
}

func TestImportProxies_HTTPAndSocks(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              nil,
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})
	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
	}

	resp, err := cp.ImportProxies(ImportProxiesRequest{
		Proxies:        []string{"http://user:pass@1.2.3.4:80", "socks5://user:pass@5.6.7.8:1080"},
		ConfirmChecked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("ImportProxies: %v", err)
	}
	if resp.ImportedCount != 2 {
		t.Errorf("ImportedCount = %d, want 2", resp.ImportedCount)
	}
	if resp.SkippedCount != 0 {
		t.Errorf("SkippedCount = %d, want 0", resp.SkippedCount)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func intPtr(i int) *int  { return &i }
func boolPtr(b bool) *bool { return &b }
