package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
	"github.com/Resinat/Resin/internal/topology"
)

func newNodeAndProbeTestPool(t *testing.T, subMgr *topology.SubscriptionManager) (*topology.GlobalNodePool, node.Hash) {
	t.Helper()

	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              nil,
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	sub := subscription.NewSubscription("proxy-check-sub", "proxy-check-sub", "https://example.com/sub", true, false)
	subMgr.Register(sub)

	raw := []byte(`{"type":"ss","server":"1.1.1.1","port":443}`)
	hash := node.HashFromRawOptions(raw)
	pool.AddNodeFromSub(hash, raw, sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"proxy-check"}})

	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node not found after add")
	}
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)
	pool.RecordResult(hash, true)

	return pool, hash
}

func newMockFetcher(body []byte, latency time.Duration, err error) probe.Fetcher {
	return func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
		return body, latency, err
	}
}

// newMockRawChecker returns a RawProxyChecker that uses the body/latency/err
// from the given fetcher for every call. The raw options are ignored.
func newMockRawChecker(body []byte, latency time.Duration, err error) RawProxyChecker {
	return func(_ json.RawMessage, profile probe.TargetProfile, opts probe.ProxyCheckOptions) (*probe.ProxyScore, error) {
		if err != nil {
			return nil, err
		}
		fetcher := func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return body, latency, nil
		}
		return probe.CheckProxy(fetcher, node.Zero, profile, opts)
	}
}

// --- CheckProxyCheck ---

func TestCheckProxyCheck_InvalidHash(t *testing.T) {
	cp := &ControlPlaneService{}
	_, err := cp.CheckProxyCheck("not-a-hex", ProxyCheckRequest{})
	if err == nil {
		t.Fatal("expected error for invalid hash")
	}
	svcErr, ok := err.(*ServiceError)
	if !ok {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Fatalf("code = %q, want INVALID_ARGUMENT", svcErr.Code)
	}
}

func TestCheckProxyCheck_NodeNotFound(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              nil,
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})
	cp := &ControlPlaneService{Pool: pool}
	h := node.HashFromRawOptions([]byte(`{"type":"ss","server":"9.9.9.9","port":443}`))
	_, err := cp.CheckProxyCheck(h.Hex(), ProxyCheckRequest{})
	if err == nil {
		t.Fatal("expected error for missing node")
	}
	svcErr, ok := err.(*ServiceError)
	if !ok {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "NOT_FOUND" {
		t.Fatalf("code = %q, want NOT_FOUND", svcErr.Code)
	}
}

func TestCheckProxyCheck_Success(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool, hash := newNodeAndProbeTestPool(t, subMgr)

	mockFetcher := newMockFetcher(
		[]byte("ok"),
		50*time.Millisecond,
		nil,
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		ProbeMgr: probe.NewProbeManager(probe.ProbeConfig{
			Pool:    pool,
			Fetcher: mockFetcher,
		}),
	}

	result, err := cp.CheckProxyCheck(hash.Hex(), ProxyCheckRequest{
		Profile: "generic",
		Options: &probe.ProxyCheckOptions{
			ServiceReachability: true,
			Rounds:              1,
		},
	})
	if err != nil {
		t.Fatalf("CheckProxyCheck: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.ServiceReachable {
		t.Error("expected ServiceReachable=true")
	}
	if result.Score <= 0 {
		t.Errorf("expected positive score, got %f", result.Score)
	}
}

func TestCheckProxyCheck_WithProfileOpenAI(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool, hash := newNodeAndProbeTestPool(t, subMgr)

	mockFetcher := newMockFetcher(
		[]byte("openai chatgpt response body"),
		30*time.Millisecond,
		nil,
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		ProbeMgr: probe.NewProbeManager(probe.ProbeConfig{
			Pool:    pool,
			Fetcher: mockFetcher,
		}),
	}

	result, err := cp.CheckProxyCheck(hash.Hex(), ProxyCheckRequest{
		Profile: "openai",
		Options: &probe.ProxyCheckOptions{
			ServiceReachability: true,
			CloudflareDetection: true,
			Rounds:              1,
		},
	})
	if err != nil {
		t.Fatalf("CheckProxyCheck: %v", err)
	}
	if !result.ServiceReachable {
		t.Error("expected ServiceReachable=true for openai")
	}
	if len(result.RoundResults) != 1 {
		t.Fatalf("expected 1 round result, got %d", len(result.RoundResults))
	}
}

func TestCheckProxyCheck_WithInvalidHashString(t *testing.T) {
	cp := &ControlPlaneService{}
	_, err := cp.CheckProxyCheck("", ProxyCheckRequest{})
	if err == nil {
		t.Fatal("expected error for empty hash")
	}
	_, err = cp.CheckProxyCheck("zzz", ProxyCheckRequest{})
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

// --- Request validation tests ---

func TestProxyCheckRequest_EmptyProfileDefaultsToGeneric(t *testing.T) {
	profile := probe.LookupProfile("")
	if profile.Name != "generic" {
		t.Fatalf("expected generic profile, got %q", profile.Name)
	}
}

func TestProxyCheckRequest_UnknownProfileDefaultsToGeneric(t *testing.T) {
	profile := probe.LookupProfile("nonexistent")
	if profile.Name != "generic" {
		t.Fatalf("expected generic profile for unknown name, got %q", profile.Name)
	}
}

// --- validateBatchRequest ---

func TestValidateBatchRequest_MutualExclusion(t *testing.T) {
	t.Run("both empty", func(t *testing.T) {
		_, err := validateBatchRequest(ProxyCheckBatchRequest{})
		if err == nil {
			t.Fatal("expected error for both empty")
		}
	})

	t.Run("both non-empty", func(t *testing.T) {
		_, err := validateBatchRequest(ProxyCheckBatchRequest{
			NodeHashes: []string{"a"},
			Proxies:    []string{"b"},
		})
		if err == nil {
			t.Fatal("expected error for both non-empty")
		}
	})

	t.Run("node hashes only", func(t *testing.T) {
		n, err := validateBatchRequest(ProxyCheckBatchRequest{
			NodeHashes: []string{"a", "b"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 2 {
			t.Fatalf("count = %d, want 2", n)
		}
	})

	t.Run("proxies only", func(t *testing.T) {
		n, err := validateBatchRequest(ProxyCheckBatchRequest{
			Proxies: []string{"x", "y", "z"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 3 {
			t.Fatalf("count = %d, want 3", n)
		}
	})

	t.Run("node hashes exceeds max", func(t *testing.T) {
		hashes := make([]string, 201)
		_, err := validateBatchRequest(ProxyCheckBatchRequest{
			NodeHashes: hashes,
		})
		if err == nil {
			t.Fatal("expected error for exceeding max")
		}
	})

	t.Run("proxies exceeds max", func(t *testing.T) {
		proxies := make([]string, 201)
		_, err := validateBatchRequest(ProxyCheckBatchRequest{
			Proxies: proxies,
		})
		if err == nil {
			t.Fatal("expected error for exceeding max")
		}
	})
}

// --- ProxyCheckTaskManager — node_hashes path ---

func TestProxyCheckTaskManager_Validation(t *testing.T) {
	mgr := NewProxyCheckTaskManager()

	t.Run("empty both", func(t *testing.T) {
		_, err := mgr.CreateTask(ProxyCheckBatchRequest{}, nil, nil)
		if err == nil {
			t.Fatal("expected error for empty request")
		}
	})

	t.Run("both fields", func(t *testing.T) {
		_, err := mgr.CreateTask(ProxyCheckBatchRequest{
			NodeHashes: []string{"a"},
			Proxies:    []string{"b"},
		}, nil, nil)
		if err == nil {
			t.Fatal("expected error for both fields set")
		}
	})

	t.Run("too many rounds", func(t *testing.T) {
		_, err := mgr.CreateTask(ProxyCheckBatchRequest{
			NodeHashes: []string{"abcd"},
			Options:    &probe.ProxyCheckOptions{Rounds: 10},
		}, nil, nil)
		if err == nil {
			t.Fatal("expected error for excessive rounds")
		}
	})

	t.Run("too many nodes", func(t *testing.T) {
		hashes := make([]string, 201)
		for i := range hashes {
			hashes[i] = "a"
		}
		_, err := mgr.CreateTask(ProxyCheckBatchRequest{
			NodeHashes: hashes,
		}, nil, nil)
		if err == nil {
			t.Fatal("expected error for too many nodes")
		}
	})
}

func TestProxyCheckTaskManager_GetTaskNotFound(t *testing.T) {
	mgr := NewProxyCheckTaskManager()
	_, err := mgr.GetTask("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	svcErr, ok := err.(*ServiceError)
	if !ok {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "NOT_FOUND" {
		t.Fatalf("code = %q, want NOT_FOUND", svcErr.Code)
	}
}

// pollTask waits up to 5s for a task to reach a terminal status.
func pollTask(t *testing.T, mgr *ProxyCheckTaskManager, id string) *ProxyCheckTask {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, err := mgr.GetTask(id)
		if err != nil {
			t.Fatalf("GetTask(%q): %v", id, err)
		}
		if task.IsTerminal() {
			return task
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("task %q did not complete within deadline", id)
	return nil
}

func TestProxyCheckTaskManager_NodeHashesCreateAndGetAsync(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool, hash := newNodeAndProbeTestPool(t, subMgr)

	mockFetcher := newMockFetcher(
		[]byte("ok"),
		10*time.Millisecond,
		nil,
	)

	probeMgr := probe.NewProbeManager(probe.ProbeConfig{
		Pool:    pool,
		Fetcher: mockFetcher,
	})

	mgr := NewProxyCheckTaskManager()

	// Create — returns immediately with pending status.
	task, err := mgr.CreateTask(ProxyCheckBatchRequest{
		NodeHashes: []string{hash.Hex()},
		Profile:    "generic",
		Options: &probe.ProxyCheckOptions{
			ServiceReachability: true,
			Rounds:              1,
		},
	}, probeMgr, nil) // nil rawChecker for node hashes path
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if task.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if task.Status != "pending" {
		t.Fatalf("initial status = %q, want pending", task.Status)
	}
	if task.Total != 1 {
		t.Fatalf("Total = %d, want 1", task.Total)
	}
	if task.Done != 0 {
		t.Fatalf("initial Done = %d, want 0", task.Done)
	}

	// Poll until terminal.
	completed := pollTask(t, mgr, task.ID)

	if completed.Status != "completed" {
		t.Fatalf("final status = %q, want completed", completed.Status)
	}
	if completed.Result == nil {
		t.Fatal("expected non-nil result")
	}
	if completed.Result.Total != 1 {
		t.Fatalf("total = %d, want 1", completed.Result.Total)
	}
	if len(completed.Result.Results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(completed.Result.Results))
	}
	item := completed.Result.Results[0]
	if item.Hash != hash.Hex() {
		t.Fatalf("result hash = %q, want %q", item.Hash, hash.Hex())
	}
	if item.Score == nil {
		t.Fatal("expected non-nil Score")
	}
	if !item.Score.ServiceReachable {
		t.Error("expected ServiceReachable=true")
	}
	if completed.Done != 1 {
		t.Fatalf("Done = %d, want 1", completed.Done)
	}
	if completed.Failed != 0 {
		t.Fatalf("Failed = %d, want 0", completed.Failed)
	}
}

func TestProxyCheckTaskManager_NodeHashesPartialErrors(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool, hash := newNodeAndProbeTestPool(t, subMgr)

	mockFetcher := newMockFetcher(
		[]byte("ok"),
		10*time.Millisecond,
		nil,
	)

	probeMgr := probe.NewProbeManager(probe.ProbeConfig{
		Pool:    pool,
		Fetcher: mockFetcher,
	})

	mgr := NewProxyCheckTaskManager()

	// Mix of valid and invalid hashes.
	task, err := mgr.CreateTask(ProxyCheckBatchRequest{
		NodeHashes: []string{hash.Hex(), "not-a-valid-hex"},
		Profile:    "generic",
		Options: &probe.ProxyCheckOptions{
			ServiceReachability: true,
			Rounds:              1,
		},
	}, probeMgr, nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	completed := pollTask(t, mgr, task.ID)

	if completed.Status != "completed_with_errors" {
		t.Fatalf("final status = %q, want completed_with_errors", completed.Status)
	}
	if completed.Total != 2 {
		t.Fatalf("Total = %d, want 2", completed.Total)
	}
	if completed.Done != 2 {
		t.Fatalf("Done = %d, want 2", completed.Done)
	}
	if completed.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", completed.Failed)
	}
	if completed.Error == "" {
		t.Fatal("expected non-empty Error for partial failures")
	}
	if completed.Result == nil {
		t.Fatal("expected non-nil Result")
	}
	if len(completed.Result.Results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(completed.Result.Results))
	}
	// First result (valid hash) should be reachable.
	if completed.Result.Results[0].Score == nil || !completed.Result.Results[0].Score.ServiceReachable {
		t.Error("expected first result to have reachable score")
	}
	// Second result (invalid hash) should have Error.
	if completed.Result.Results[1].Error == "" {
		t.Error("expected second result to have non-empty Error")
	}
	if completed.Result.Results[1].Hash != "not-a-valid-hex" {
		t.Errorf("second result Hash = %q, want input string", completed.Result.Results[1].Hash)
	}
}

func TestProxyCheckTaskManager_NodeHashesAllFailed(t *testing.T) {
	mgr := NewProxyCheckTaskManager()
	// All invalid hashes → all fail.
	task, err := mgr.CreateTask(ProxyCheckBatchRequest{
		NodeHashes: []string{"bad1", "bad2"},
		Profile:    "generic",
	}, nil, nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	completed := pollTask(t, mgr, task.ID)

	if completed.Status != "failed" {
		t.Fatalf("final status = %q, want failed", completed.Status)
	}
	if completed.Total != 2 {
		t.Fatalf("Total = %d, want 2", completed.Total)
	}
	if completed.Failed != 2 {
		t.Fatalf("Failed = %d, want 2", completed.Failed)
	}
	if completed.Error == "" {
		t.Fatal("expected non-empty Error when all fail")
	}
}

func TestProxyCheckTaskManager_MaxCapacityEviction(t *testing.T) {
	// Create tasks that all fail (invalid hashes) so they complete quickly.
	mgr := NewProxyCheckTaskManager()

	const extra = 5
	numTasks := maxProxyCheckTasks + extra
	ids := make([]string, 0, numTasks)

	for i := 0; i < numTasks; i++ {
		task, err := mgr.CreateTask(ProxyCheckBatchRequest{
			NodeHashes: []string{"invalid-hash"},
			Profile:    "generic",
		}, nil, nil)
		if err != nil {
			t.Fatalf("CreateTask %d: %v", i, err)
		}
		ids = append(ids, task.ID)
	}

	// Wait for all tasks to complete.
	for _, id := range ids {
		pollTask(t, mgr, id)
	}

	// The manager should now have at most maxProxyCheckTasks tasks.
	// The oldest extra tasks should have been evicted.
	for i := 0; i < extra; i++ {
		_, err := mgr.GetTask(ids[i])
		if err == nil {
			t.Errorf("expected oldest task %q (index %d) to be evicted, but it exists", ids[i], i)
		}
	}

	// The newest tasks (last maxProxyCheckTasks ones) should still be present.
	for i := extra; i < numTasks; i++ {
		_, err := mgr.GetTask(ids[i])
		if err != nil {
			t.Errorf("expected newer task %q (index %d) to exist, got error: %v", ids[i], i, err)
		}
	}
}

// --- ProxyCheckTaskManager — raw proxies path ---

func TestProxyCheckTaskManager_RawProxiesSuccess(t *testing.T) {
	mgr := NewProxyCheckTaskManager()
	rawChecker := newMockRawChecker([]byte("ok"), 10*time.Millisecond, nil)

	task, err := mgr.CreateTask(ProxyCheckBatchRequest{
		Proxies: []string{"1.2.3.4:5678"},
		Profile: "generic",
		Options: &probe.ProxyCheckOptions{
			ServiceReachability: true,
			Rounds:              1,
		},
	}, nil, rawChecker)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if task.Status != "pending" {
		t.Fatalf("initial status = %q, want pending", task.Status)
	}
	if task.Total != 1 {
		t.Fatalf("Total = %d, want 1", task.Total)
	}

	completed := pollTask(t, mgr, task.ID)
	if completed.Status != "completed" {
		t.Fatalf("final status = %q, want completed", completed.Status)
	}
	if completed.Result == nil || len(completed.Result.Results) != 1 {
		t.Fatalf("expected 1 result, got %+v", completed.Result)
	}

	item := completed.Result.Results[0]
	if item.Proxy != "1.2.3.4:5678" {
		t.Fatalf("Proxy = %q, want original input", item.Proxy)
	}
	if item.Hash == "" {
		t.Fatal("expected non-empty Hash from parsed outbound")
	}
	if item.Score == nil {
		t.Fatal("expected non-nil Score")
	}
	if !item.Score.ServiceReachable {
		t.Error("expected ServiceReachable=true")
	}
	if item.Error != "" {
		t.Fatalf("unexpected Error: %s", item.Error)
	}
}

func TestProxyCheckTaskManager_RawProxiesParseFailure(t *testing.T) {
	mgr := NewProxyCheckTaskManager()
	rawChecker := newMockRawChecker([]byte("ok"), 10*time.Millisecond, nil)

	// Completely invalid input that cannot be parsed as any subscription format.
	task, err := mgr.CreateTask(ProxyCheckBatchRequest{
		Proxies: []string{"\x00\x01\x02invalid-binary"},
		Profile: "generic",
	}, nil, rawChecker)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	completed := pollTask(t, mgr, task.ID)
	if completed.Status != "completed_with_errors" && completed.Status != "failed" {
		t.Fatalf("status = %q, want completed_with_errors or failed", completed.Status)
	}
	if completed.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", completed.Failed)
	}
	if len(completed.Result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(completed.Result.Results))
	}
	item := completed.Result.Results[0]
	if item.Error == "" {
		t.Fatal("expected non-empty Error for parse failure")
	}
	if item.Score != nil {
		t.Fatal("expected nil Score for parse failure")
	}
}

func TestProxyCheckTaskManager_RawProxiesMixedSuccessAndFail(t *testing.T) {
	mgr := NewProxyCheckTaskManager()

	call := 0
	mixedChecker := func(_ json.RawMessage, _ probe.TargetProfile, _ probe.ProxyCheckOptions) (*probe.ProxyScore, error) {
		call++
		if call == 1 {
			return &probe.ProxyScore{Grade: "A", Score: 100, ServiceReachable: true}, nil
		}
		return nil, fmt.Errorf("mock network error")
	}

	task, err := mgr.CreateTask(ProxyCheckBatchRequest{
		Proxies: []string{"1.2.3.4:80", "http://user:pass@5.6.7.8:8080"},
		Profile: "generic",
	}, nil, mixedChecker)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	completed := pollTask(t, mgr, task.ID)
	if completed.Status != "completed_with_errors" {
		t.Fatalf("status = %q, want completed_with_errors", completed.Status)
	}
	if len(completed.Result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(completed.Result.Results))
	}
	first := completed.Result.Results[0]
	if first.Proxy == "" || first.Hash == "" || first.Score == nil || first.Error != "" {
		t.Fatalf("unexpected first success item: %+v", first)
	}
	second := completed.Result.Results[1]
	if second.Proxy == "" || second.Hash == "" {
		t.Fatalf("unexpected second identity fields: %+v", second)
	}
	if second.Score != nil {
		t.Fatalf("second Score = %+v, want nil for checker error", second.Score)
	}
	if second.Error == "" {
		t.Fatal("second Error is empty, expected checker error")
	}
}

func TestProxyCheckTaskManager_RawProxiesNilCheckerReturnsError(t *testing.T) {
	mgr := NewProxyCheckTaskManager()
	_, err := mgr.CreateTask(ProxyCheckBatchRequest{
		Proxies: []string{"1.2.3.4:80"},
		Profile: "generic",
	}, nil, nil) // nil checker
	if err == nil {
		t.Fatal("expected error when raw checker is nil")
	}
	svcErr, ok := err.(*ServiceError)
	if !ok {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "INTERNAL" {
		t.Fatalf("code = %q, want INTERNAL", svcErr.Code)
	}
}

func TestProxyCheckTaskManager_RawProxiesMaxLimit(t *testing.T) {
	mgr := NewProxyCheckTaskManager()
	proxies := make([]string, 201)
	for i := range proxies {
		proxies[i] = "1.2.3.4:80"
	}
	_, err := mgr.CreateTask(ProxyCheckBatchRequest{
		Proxies: proxies,
	}, nil, newMockRawChecker(nil, 0, nil))
	if err == nil {
		t.Fatal("expected error for exceeding max proxies")
	}
}

// Test proxy string is preserved through parse-and-check.
func TestProxyCheckTaskManager_RawProxiesPreservesInputOrder(t *testing.T) {
	mgr := NewProxyCheckTaskManager()
	rawChecker := newMockRawChecker([]byte("ok"), 10*time.Millisecond, nil)

	proxies := []string{
		"1.1.1.1:1111",
		"socks5://2.2.2.2:2222",
		"http://user:pass@3.3.3.3:3333",
		"4.4.4.4:4444",
	}
	task, err := mgr.CreateTask(ProxyCheckBatchRequest{
		Proxies: proxies,
		Profile: "generic",
	}, nil, rawChecker)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	completed := pollTask(t, mgr, task.ID)
	if completed.Status != "completed" {
		t.Fatalf("status = %q, want completed", completed.Status)
	}
	if len(completed.Result.Results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(completed.Result.Results))
	}
	for i, item := range completed.Result.Results {
		if item.Proxy != proxies[i] {
			t.Fatalf("result[%d] Proxy = %q, want %q", i, item.Proxy, proxies[i])
		}
		if item.Hash == "" {
			t.Errorf("result[%d] has empty Hash", i)
		}
	}
}

// --- Manual CheckProxyCheck writeback tests ---

func TestCheckProxyCheck_WritesBackQualityOnSuccess(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool, hash := newNodeAndProbeTestPool(t, subMgr)
	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}

	mockFetcher := newMockFetcher(
		[]byte("ok"),
		50*time.Millisecond,
		nil,
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		ProbeMgr: probe.NewProbeManager(probe.ProbeConfig{
			Pool:    pool,
			Fetcher: mockFetcher,
		}),
	}

	result, err := cp.CheckProxyCheck(hash.Hex(), ProxyCheckRequest{
		Profile: "generic",
		Options: &probe.ProxyCheckOptions{
			ServiceReachability: true,
			Rounds:              1,
		},
	})
	if err != nil {
		t.Fatalf("CheckProxyCheck: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Verify quality was written back.
	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality to be written back via RecordNodeQuality")
	}
	if q.Profile != "generic" {
		t.Fatalf("quality profile = %q, want generic", q.Profile)
	}
	if q.Grade != result.Grade {
		t.Fatalf("quality grade = %q, want %q", q.Grade, result.Grade)
	}
	if q.Score != result.Score {
		t.Fatalf("quality score = %f, want %f", q.Score, result.Score)
	}
	if q.ServiceReachable != result.ServiceReachable {
		t.Fatalf("quality ServiceReachable = %v, want %v", q.ServiceReachable, result.ServiceReachable)
	}
	if q.LastCheckedNs == 0 {
		t.Fatal("expected LastCheckedNs to be set")
	}
	if q.NodeHash == "" {
		t.Fatal("expected NodeHash to be set")
	}
}

func TestCheckProxyCheck_WritesBackQualityOnError(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool, hash := newNodeAndProbeTestPool(t, subMgr)
	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}

	mockFetcher := newMockFetcher(
		nil,
		0,
		fmt.Errorf("connection timeout"),
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		ProbeMgr: probe.NewProbeManager(probe.ProbeConfig{
			Pool:    pool,
			Fetcher: mockFetcher,
		}),
	}

	_, err := cp.CheckProxyCheck(hash.Hex(), ProxyCheckRequest{
		Profile: "openai",
		Options: &probe.ProxyCheckOptions{
			ServiceReachability: true,
			Rounds:              1,
		},
	})
	if err == nil {
		t.Fatal("expected error from CheckProxyCheck")
	}

	// Verify quality was written back even on error.
	q := entry.GetQuality()
	if q == nil {
		t.Fatal("expected quality to be written back via RecordNodeQuality on error")
	}
	if q.Profile != "openai" {
		t.Fatalf("quality profile = %q, want openai", q.Profile)
	}
	if q.Grade != "F" {
		t.Fatalf("quality grade = %q, want F for error case", q.Grade)
	}
	if q.LastError == "" {
		t.Fatal("expected LastError to be set on check error")
	}
	if q.NodeHash == "" {
		t.Fatal("expected NodeHash to be set")
	}
}

// TestCheckProxyCheck_BatchNodeHashesDoesNotWriteQuality verifies that the
// batch node-hash path does not write back quality. The batch executeTask
// only stores results in the task's items slice and does not call
// RecordNodeQuality.
func TestCheckProxyCheck_BatchNodeHashesDoesNotWriteQuality(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool, hash := newNodeAndProbeTestPool(t, subMgr)

	mockFetcher := newMockFetcher(
		[]byte("ok"),
		50*time.Millisecond,
		nil,
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		ProbeMgr: probe.NewProbeManager(probe.ProbeConfig{
			Pool:    pool,
			Fetcher: mockFetcher,
		}),
	}

	// Create a batch task with the node hash.
	mgr := NewProxyCheckTaskManager()
	task, err := mgr.CreateTask(ProxyCheckBatchRequest{
		NodeHashes: []string{hash.Hex()},
		Profile:    "generic",
		Options: &probe.ProxyCheckOptions{
			ServiceReachability: true,
			Rounds:              1,
		},
	}, cp.ProbeMgr, nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	completed := pollTask(t, mgr, task.ID)
	if completed.Status != "completed" {
		t.Fatalf("status = %q, want completed", completed.Status)
	}
	if len(completed.Result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(completed.Result.Results))
	}

	// Verify quality was NOT written back for the node.
	entry, _ := pool.GetEntry(hash)
	if entryQuality := entry.GetQuality(); entryQuality != nil {
		t.Fatal("batch task should NOT write back quality to node entry")
	}
}
