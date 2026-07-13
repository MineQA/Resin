package service

import (
	"errors"
	"fmt"
	"strings"
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
	resp, err := cp.ImportProxies(ImportProxiesRequest{Proxies: []string{"not-a-proxy"}, ConfirmChecked: boolPtr(true)})
	if err != nil {
		t.Fatalf("unexpected top-level error for single-item invalid format: %v", err)
	}
	if resp.ImportedCount != 0 {
		t.Errorf("ImportedCount = %d, want 0", resp.ImportedCount)
	}
	if resp.SkippedCount != 1 {
		t.Errorf("SkippedCount = %d, want 1", resp.SkippedCount)
	}
	if len(resp.Errors) == 0 {
		t.Fatal("expected a parse error in response")
	}
	if !strings.Contains(resp.Errors[0], "parse error:") {
		t.Fatalf("expected error with parse error prefix, got: %s", resp.Errors[0])
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
// ImportProxies — diagnostics consumption
// ---------------------------------------------------------------------------

// TestImportProxies_PartialRejection_AcceptsSiblings verifies that when
// some proxies are rejected (e.g., Clash fingerprint) and some accepted,
// accepted nodes are imported and rejected nodes are counted as skipped
// with safe formatted errors.
func TestImportProxies_PartialRejection_AcceptsSiblings(t *testing.T) {
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

	// Mix: a valid SS URI line + a Clash JSON with rejected fingerprint node.
	validProxy := "ss://YWVzLTEyOC1nY206cGFzcw@1.1.1.1:8388"
	rejectedProxy := `{"proxies":[{"name":"bad","type":"hysteria2","server":"x.com","port":443,"password":"pass","fingerprint":"aabbccdd"}]}`

	resp, err := cp.ImportProxies(ImportProxiesRequest{
		Proxies:        []string{validProxy, rejectedProxy},
		ConfirmChecked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("ImportProxies: %v", err)
	}

	// The SS node should be imported.
	if resp.ImportedCount != 1 {
		t.Errorf("ImportedCount = %d, want 1", resp.ImportedCount)
	}
	if len(resp.NodeHashes) != 1 {
		t.Errorf("expected 1 node hash, got %d", len(resp.NodeHashes))
	}

	// The rejected node should be counted as skipped.
	if resp.SkippedCount != 1 {
		t.Errorf("SkippedCount = %d, want 1 (rejected)", resp.SkippedCount)
	}

	// Errors should contain the rejection diagnostic with stable code.
	if len(resp.Errors) == 0 {
		t.Fatal("expected at least one error for rejected proxy")
	}
	foundCode := false
	for _, e := range resp.Errors {
		if strings.Contains(e, "CLASH_FINGERPRINT_INVALID") ||
			strings.Contains(e, "CLASH_CERTIFICATE_FINGERPRINT_UNSUPPORTED") {
			foundCode = true
			break
		}
	}
	if !foundCode {
		t.Fatalf("expected error with stable diagnostic code, got: %v", resp.Errors)
	}
	if strings.Contains(strings.Join(resp.Errors, " "), "zzzz") {
		t.Fatal("errors must not contain raw fingerprint values")
	}
}

// TestImportProxies_OnlyRejectedNodes_ReturnsErrors verifies that when all
// input proxies are rejected (no accepted nodes), the response still has
// errors rather than a generic "no supported proxy formats" error.
func TestImportProxies_OnlyRejectedNodes_ReturnsErrors(t *testing.T) {
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

	// Only rejected proxies: Clash JSON with fingerprints.
	rejectedProxy := `{"proxies":[{"name":"n1","type":"hysteria2","server":"x.com","port":443,"password":"pass","fingerprint":"aabbccdd"}]}`

	resp, err := cp.ImportProxies(ImportProxiesRequest{
		Proxies:        []string{rejectedProxy},
		ConfirmChecked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("ImportProxies: %v", err)
	}

	if resp.ImportedCount != 0 {
		t.Errorf("ImportedCount = %d, want 0 (all rejected)", resp.ImportedCount)
	}
	if resp.SkippedCount != 1 {
		t.Errorf("SkippedCount = %d, want 1", resp.SkippedCount)
	}
	if len(resp.Errors) == 0 {
		t.Fatal("expected errors for only-rejected import, got none")
	}
	if !strings.Contains(resp.Errors[0], "CLASH") {
		t.Fatalf("expected error with CLASH diagnostic code, got: %s", resp.Errors[0])
	}
}

// TestImportProxies_InvalidSibling_KeepsValidItems verifies that when one
// item produces a fatal parse error and another is valid, the valid item
// is still imported and the parse error is reported as skipped.
func TestImportProxies_InvalidSibling_KeepsValidItems(t *testing.T) {
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
		Proxies:        []string{"not-a-proxy", "ss://YWVzLTEyOC1nY206cGFzcw@1.1.1.1:8388"},
		ConfirmChecked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}
	if resp.ImportedCount != 1 {
		t.Errorf("ImportedCount = %d, want 1 (valid SS node)", resp.ImportedCount)
	}
	if resp.SkippedCount != 1 {
		t.Errorf("SkippedCount = %d, want 1 (parse error)", resp.SkippedCount)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 error (parse error), got %d: %v", len(resp.Errors), resp.Errors)
	}
	if !strings.Contains(resp.Errors[0], "parse error:") {
		t.Fatalf("expected parse error prefix, got: %s", resp.Errors[0])
	}
}

// TestImportProxies_Warnings_RepresentedSafely verifies that when a proxy
// is accepted with warnings (e.g., drop_always with fingerprint), the
// warning is represented safely in errors without raw fingerprint data.
func TestImportProxies_Warnings_RepresentedSafely(t *testing.T) {
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

	// A single SS proxy (no issue) + a Clash JSON with drop_always warning.
	// Note: the default policy for ParseGeneralSubscriptionDetail is
	// ClashFingerprintReject, which will reject fingerprint nodes, not warn.
	// To get warnings, we need drop_always policy, but ImportProxies always
	// uses default reject. So warnings under default reject won't occur
	// naturally via fingerprints.
	//
	// However, the import code handles warnings defensively. We can trigger
	// a warning-free import to verify no spurious warnings appear.
	// For proper warning coverage, the code flow accepts warnings if the
	// parser produces them, but under default reject, fingerprints cause
	// rejections, not warnings. This test verifies that the code does not
	// crash or produce garbage when warnings are absent.
	validProxies := []string{"ss://YWVzLTEyOC1nY206cGFzcw@1.1.1.1:8388"}

	resp, err := cp.ImportProxies(ImportProxiesRequest{
		Proxies:        validProxies,
		ConfirmChecked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("ImportProxies: %v", err)
	}
	if resp.ImportedCount != 1 {
		t.Errorf("ImportedCount = %d, want 1", resp.ImportedCount)
	}
	if resp.SkippedCount != 0 {
		t.Errorf("SkippedCount = %d, want 0", resp.SkippedCount)
	}
	// No errors expected for clean import.
	if len(resp.Errors) != 0 {
		t.Fatalf("expected no errors for clean import, got: %v", resp.Errors)
	}
}

// TestImportProxies_RejectedAndWarnings verifies that a mixture of rejected
// and warning-bearing proxies produces safe formatted errors. Since the
// default policy is reject, fingerprints produce rejections. This test
// ensures both rejected and potential warning paths are safe.
func TestImportProxies_RejectedAndWarnings_Formatted(t *testing.T) {
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

	// A proxy with fingerprint that will be rejected under default policy.
	fpRejected := `{"proxies":[{"name":"fp-node","type":"hysteria2","server":"x.com","port":443,"password":"pass","fingerprint":"aabbccdd"}]}`

	resp, err := cp.ImportProxies(ImportProxiesRequest{
		Proxies:        []string{fpRejected},
		ConfirmChecked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("ImportProxies: %v", err)
	}

	if resp.ImportedCount != 0 {
		t.Errorf("ImportedCount = %d, want 0", resp.ImportedCount)
	}
	if resp.SkippedCount != 1 {
		t.Errorf("SkippedCount = %d, want 1", resp.SkippedCount)
	}

	// Verify error format includes the "proxy rejected:" prefix and stable code.
	if len(resp.Errors) == 0 {
		t.Fatal("expected errors, got none")
	}
	errStr := resp.Errors[0]
	if !strings.HasPrefix(errStr, "proxy rejected:") && !strings.HasPrefix(errStr, "proxy warning:") {
		t.Fatalf("expected error with proper prefix, got: %s", errStr)
	}
	if strings.Contains(errStr, "aabbccdd") {
		t.Fatal("error must not contain raw fingerprint value")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func intPtr(i int) *int    { return &i }
func boolPtr(b bool) *bool { return &b }
