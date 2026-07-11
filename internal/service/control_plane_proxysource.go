package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/subscription"
)

// ---------------------------------------------------------------------------
// Proxy source definitions
// ---------------------------------------------------------------------------

// ProxySource describes a built-in safe proxy source.
type ProxySource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// BuiltInProxySources is the list of safe, built-in proxy sources.
// Only text/plain sources are included. JavaScript-rendered or
// dynamically-evaluated sources are NOT supported.
var BuiltInProxySources = []ProxySource{
	{
		ID:   "speedx-http",
		Name: "TheSpeedX HTTP Proxy List",
		URL:  "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt",
	},
	{
		ID:   "speedx-socks4",
		Name: "TheSpeedX SOCKS4 Proxy List",
		URL:  "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks4.txt",
	},
	{
		ID:   "speedx-socks5",
		Name: "TheSpeedX SOCKS5 Proxy List",
		URL:  "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks5.txt",
	},
	{
		ID:   "clarketm",
		Name: "clarketm Proxy List (mixed)",
		URL:  "https://raw.githubusercontent.com/clarketm/proxy-list/master/proxy-list-raw.txt",
	},
	{
		ID:   "monosans",
		Name: "monosans Proxy List (all types)",
		URL:  "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/all.txt",
	},
}

// ---------------------------------------------------------------------------
// Fetching
// ---------------------------------------------------------------------------

// httpGetBody is a variable so tests can inject a fake fetcher.
var httpGetBody = defaultHTTPGetBody

// OverrideHTTPGetBody overrides the HTTP GET body function for testing.
// Returns a restore function. Example: defer OverrideHTTPGetBody(fn)()
func OverrideHTTPGetBody(fn func(url string) ([]byte, error)) func() {
	orig := httpGetBody
	httpGetBody = fn
	return func() { httpGetBody = orig }
}

func defaultHTTPGetBody(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5 MB limit
	if err != nil {
		return nil, err
	}
	return body, nil
}

// ---------------------------------------------------------------------------
// Proxy extraction from plain text
// ---------------------------------------------------------------------------

// proxyLinePattern matches common proxy line formats:
//   - ip:port
//   - protocol://ip:port
//   - ip:port:user:pass
//   - protocol://user:pass@ip:port
var proxyLinePattern = regexp.MustCompile(`(?m)^\s*(?:(https?|socks(?:[45]h?)?)://)?(?:[^\s:@]+(?::[^\s:@]+)?@)?(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):(\d{2,5})(?::[^\s:]+(?::[^\s]+)?)?\s*$`)

// ExtractProxyCandidates extracts unique proxy candidates from plain text.
// Returns deduplicated candidates in order of first appearance.
func ExtractProxyCandidates(data []byte, sourceID string, limit int) []ProxyCandidate {
	candidates, _ := extractProxyCandidatesWithTotal(data, sourceID, limit)
	return candidates
}

func extractProxyCandidatesWithTotal(data []byte, sourceID string, limit int) ([]ProxyCandidate, int) {
	if limit <= 0 {
		limit = 50000
	}

	// Dedupe by normalized proxy string.
	seen := make(map[string]bool)
	var candidates []ProxyCandidate
	total := 0

	// Try each non-empty line as-is through the parser first, then regex.
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		trimmed := string(bytes.TrimSpace(line))
		if trimmed == "" {
			continue
		}

		// Attempt regex match.
		matches := proxyLinePattern.FindStringSubmatch(trimmed)
		if matches == nil {
			continue
		}
		// matches[1] = optional protocol, matches[2] = ip, matches[3] = port
		ip := matches[2]
		port := matches[3]
		normalized := ip + ":" + port
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		total++
		if len(candidates) >= limit {
			continue
		}

		// Preserve the original format when it carries protocol prefix or auth
		// credentials (e.g. ip:port:user:pass, protocol://user:pass@ip:port).
		// Deduplication still uses normalized host:port so the same host:port
		// with different auth/protocol is deduped and the first occurrence wins.
		proxyStr := normalized
		if trimmed != normalized {
			// Contains extra content (protocol, auth, or both) — keep as-is.
			proxyStr = trimmed
		}

		candidates = append(candidates, ProxyCandidate{
			Proxy:    proxyStr,
			SourceID: sourceID,
		})
	}
	return candidates, total
}

// ProxyCandidate is a single extracted proxy candidate.
type ProxyCandidate struct {
	Proxy    string `json:"proxy"`
	SourceID string `json:"source_id"`
}

// ---------------------------------------------------------------------------
// Fetch source(s)
// ---------------------------------------------------------------------------

// FetchSourcesRequest is the request body for POST /api/v1/proxy-sources/fetch.
type FetchSourcesRequest struct {
	Source string `json:"source"` // "all" or a source ID
	Limit  *int   `json:"limit,omitempty"`
}

// FetchSourcesResponse is the response body for a fetch operation.
type FetchSourcesResponse struct {
	Results      []SourceFetchResult `json:"results"`
	TotalSources int                 `json:"total_sources"`
}

// SourceFetchResult holds the fetch result for one source.
type SourceFetchResult struct {
	SourceID       string            `json:"source_id"`
	SourceName     string            `json:"source_name"`
	TotalExtracted int               `json:"total_extracted"`
	ReturnedCount  int               `json:"returned_count"`
	Candidates     []ProxyCandidate  `json:"candidates,omitempty"`
	Error          string            `json:"error,omitempty"`
}

// FetchProxySources fetches one or all built-in proxy sources and returns
// extracted candidates. No persistence, no node pool changes.
func (s *ControlPlaneService) FetchProxySources(req FetchSourcesRequest) (*FetchSourcesResponse, error) {
	limit := 50000
	if req.Limit != nil {
		if *req.Limit < 0 {
			return nil, invalidArg("limit: must be non-negative")
		}
		if *req.Limit > 50000 {
			return nil, invalidArg("limit: max 50000")
		}
		if *req.Limit > 0 {
			limit = *req.Limit
		}
	}

	sourceID := strings.TrimSpace(req.Source)
	if sourceID == "" {
		return nil, invalidArg("source: must be a source id or all")
	}

	var sources []ProxySource
	if sourceID == "all" {
		sources = make([]ProxySource, len(BuiltInProxySources))
		copy(sources, BuiltInProxySources)
	} else {
		found := false
		for _, src := range BuiltInProxySources {
			if src.ID == sourceID {
				sources = []ProxySource{src}
				found = true
				break
			}
		}
		if !found {
			return nil, notFound("source not found: " + sourceID)
		}
	}

	// Each source gets the full limit so large sources are not starved.
	// The overall response may be trimmed by the caller if needed.
	perSourceLimit := limit

	var results []SourceFetchResult
	for _, src := range sources {
		result := fetchSingleSource(src, perSourceLimit)
		results = append(results, result)
	}

	return &FetchSourcesResponse{
		Results:      results,
		TotalSources: len(results),
	}, nil
}

// fetchSingleSource fetches one source and extracts candidates.
func fetchSingleSource(src ProxySource, limit int) SourceFetchResult {
	body, err := httpGetBody(src.URL)
	if err != nil {
		return SourceFetchResult{
			SourceID:   src.ID,
			SourceName: src.Name,
			Error:      fmt.Sprintf("fetch failed: %v", err),
		}
	}

	candidates, totalExtracted := extractProxyCandidatesWithTotal(body, src.ID, limit)
	return SourceFetchResult{
		SourceID:       src.ID,
		SourceName:     src.Name,
		TotalExtracted: totalExtracted,
		ReturnedCount:  len(candidates),
		Candidates:     candidates,
	}
}

// ---------------------------------------------------------------------------
// Import proxies into pool
// ---------------------------------------------------------------------------

const (
	proxyCheckImportSubID   = "proxy-check-import"
	proxyCheckImportSubName = "Proxy Check Import"
)

// ImportProxiesRequest is the request body for POST /api/v1/proxy-check/import.
type ImportProxiesRequest struct {
	Proxies []string `json:"proxies"`
	// ConfirmChecked must be explicitly set to true to confirm the proxies have
	// been checked/reviewed before import. Default (false/nil) rejects the
	// request to prevent accidental import of unchecked proxies.
	ConfirmChecked *bool `json:"confirm_checked,omitempty"`
}

// ImportProxiesResponse is the response body for an import operation.
type ImportProxiesResponse struct {
	ImportedCount  int        `json:"imported_count"`
	SkippedCount   int        `json:"skipped_count"`
	SubscriptionID string     `json:"subscription_id"`
	NodeHashes     []string   `json:"node_hashes"`
	Errors         []string   `json:"errors,omitempty"`
}

var importMu sync.Mutex

// ImportProxies parses proxy lines via ParseGeneralSubscription and adds nodes
// to the pool under an in-memory ephemeral subscription. No persistence.
// The caller MUST explicitly set ConfirmChecked=true to confirm the proxies
// were reviewed before import — the default (false/nil) is rejected.
func (s *ControlPlaneService) ImportProxies(req ImportProxiesRequest) (*ImportProxiesResponse, error) {
	if req.ConfirmChecked == nil || !*req.ConfirmChecked {
		return nil, invalidArg("confirm_checked: must be explicitly set to true to confirm proxies were checked before import")
	}
	if len(req.Proxies) == 0 {
		return nil, invalidArg("proxies: must not be empty")
	}
	if len(req.Proxies) > 50000 {
		return nil, invalidArg("proxies: max 50000")
	}

	// Serialize imports to avoid races on the ephemeral subscription's
	// managed-nodes map wiring inside AddNodeFromSub.
	importMu.Lock()
	defer importMu.Unlock()

	// Build raw text from proxy lines.
	var buf strings.Builder
	for _, p := range req.Proxies {
		buf.WriteString(p)
		buf.WriteByte('\n')
	}
	data := []byte(buf.String())

	// Parse using the general subscription parser.
	nodes, err := subscription.ParseGeneralSubscription(data)
	if err != nil {
		return nil, invalidArg(fmt.Sprintf("parse proxies: %v", err))
	}
	if len(nodes) == 0 {
		return nil, invalidArg("no supported proxy formats found in input")
	}

	// Ensure the ephemeral subscription exists.
	sub := s.SubMgr.Lookup(proxyCheckImportSubID)
	if sub == nil {
		sub = subscription.NewSubscription(proxyCheckImportSubID, proxyCheckImportSubName, "", true, true)
		sub.SetSourceType(subscription.SourceTypeLocal)
		sub.SetContent("")
		sub.CreatedAtNs = time.Now().UnixNano()
		sub.UpdatedAtNs = time.Now().UnixNano()
		s.SubMgr.Register(sub)
	}

	// Add each parsed node to the pool.
	resp := &ImportProxiesResponse{
		SubscriptionID: proxyCheckImportSubID,
	}
	seen := make(map[node.Hash]struct{})
	for _, parsed := range nodes {
		rawOpts := parsed.RawOptions
		if len(rawOpts) == 0 {
			resp.SkippedCount++
			continue
		}
		h := node.HashFromRawOptions(rawOpts)
		if h.IsZero() {
			resp.SkippedCount++
			resp.Errors = append(resp.Errors, "hash is zero for proxy: "+parsed.Tag)
			continue
		}
		if _, ok := seen[h]; ok {
			resp.SkippedCount++
			continue
		}
		seen[h] = struct{}{}
		if _, ok := sub.ManagedNodes().LoadNode(h); ok {
			resp.SkippedCount++
			continue
		}
		s.Pool.AddNodeFromSub(h, rawOpts, proxyCheckImportSubID)
		sub.ManagedNodes().StoreNode(h, subscription.ManagedNode{
			Tags: []string{"proxy-check-import"},
		})
		resp.ImportedCount++
		resp.NodeHashes = append(resp.NodeHashes, h.Hex())
	}

	if resp.ImportedCount == 0 && len(resp.Errors) == 0 {
		resp.Errors = append(resp.Errors, "all proxies were skipped (empty options)")
	}

	return resp, nil
}
