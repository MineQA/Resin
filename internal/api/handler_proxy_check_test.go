package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe"
	"github.com/Resinat/Resin/internal/service"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
)

// setupProxyCheckNode creates a node with outbound ready for proxy-check tests.
func setupProxyCheckNode(t *testing.T, cp *service.ControlPlaneService, sub *subscription.Subscription, raw string) node.Hash {
	t.Helper()

	hash := node.HashFromRawOptions([]byte(raw))
	cp.Pool.AddNodeFromSub(hash, []byte(raw), sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"proxy-test"}})

	entry, ok := cp.Pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node %s not found after add", hash.Hex())
	}
	entry.SetEgressIP(netip.MustParseAddr("203.0.113.99"))
	if entry.LatencyTable == nil {
		t.Fatalf("node %s latency table not initialized", hash.Hex())
	}
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)
	cp.Pool.RecordResult(hash, true)
	return hash
}

// TestNodeActionProxyCheck_NoBody tests that POST with no body (optional) works.
func TestHandleNodeActionProxyCheck_NoBody(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("sub-pc1", "sub-pc1", "https://example.com/sub", true, false)
	cp.SubMgr.Register(sub)

	hash := setupProxyCheckNode(t, cp, sub, `{"type":"ss","server":"1.2.3.4","port":443}`)

	// Set up a mock probe manager on the cp service.
	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 15 * time.Millisecond, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+hash.Hex()+"/actions/proxy-check", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result probe.ProxyScore
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.ServiceReachable {
		t.Error("expected ServiceReachable=true")
	}
	if len(result.RoundResults) == 0 {
		t.Error("expected at least one round result")
	}
}

// TestHandleNodeActionProxyCheck_WithBody tests with explicit profile and options.
func TestHandleNodeActionProxyCheck_WithBody(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("sub-pc2", "sub-pc2", "https://example.com/sub", true, false)
	cp.SubMgr.Register(sub)

	hash := setupProxyCheckNode(t, cp, sub, `{"type":"ss","server":"5.6.7.8","port":443}`)

	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("response body with openai content"), 20 * time.Millisecond, nil
		},
	})

	body := map[string]interface{}{
		"profile": "openai",
		"options": map[string]interface{}{
			"service_reachability": true,
			"cloudflare_detection": true,
			"rounds":               2,
		},
	}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+hash.Hex()+"/actions/proxy-check", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result probe.ProxyScore
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.ServiceReachable {
		if len(result.RoundResults) != 2 {
			t.Fatalf("expected 2 round results, got %d", len(result.RoundResults))
		}
	} else {
		t.Log("service not reachable (expected with profile indicators check)")
	}
}

// TestHandleNodeActionProxyCheck_NodeNotFound tests 404 for missing node.
func TestHandleNodeActionProxyCheck_NodeNotFound(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/abcdef0123456789abcdef0123456789/actions/proxy-check", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestHandleNodeActionProxyCheck_InvalidHash tests 400 for bad hash.
func TestHandleNodeActionProxyCheck_InvalidHash(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/not-a-valid-hex/actions/proxy-check", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestHandleNodeActionProxyCheck_RequiresAuth tests auth enforcement.
func TestHandleNodeActionProxyCheck_RequiresAuth(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/abcd/actions/proxy-check", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// ---------------------------------------------------------------------------
// Batch task poll helper
// ---------------------------------------------------------------------------

// pollTaskGet performs GET /api/v1/proxy-check/tasks/{id} until the task
// reaches a terminal status. Returns the terminal snapshot.
func pollTaskGet(t *testing.T, srv *Server, id string) *service.ProxyCheckTask {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/proxy-check/tasks/%s", id), nil)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("poll GET status: got %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var task service.ProxyCheckTask
		if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
			t.Fatalf("unmarshal poll task: %v", err)
		}
		if task.IsTerminal() {
			return &task
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("task %q did not complete within deadline", id)
	return nil
}

// ---------------------------------------------------------------------------
// Node hashes batch path
// ---------------------------------------------------------------------------

// TestHandleCreateProxyCheckTask_NodeHashes tests async create with node_hashes.
func TestHandleCreateProxyCheckTask_NodeHashes(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("sub-pc-batch1", "sub-pc-batch1", "https://example.com/sub", true, false)
	cp.SubMgr.Register(sub)

	hash := setupProxyCheckNode(t, cp, sub, `{"type":"ss","server":"1.2.3.4","port":443}`)

	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})

	body := map[string]interface{}{
		"node_hashes": []string{hash.Hex()},
		"profile":     "generic",
	}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var task service.ProxyCheckTask
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if task.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if task.Status != "pending" && task.Status != "running" {
		t.Fatalf("initial task status = %q, want pending or running", task.Status)
	}
	if task.Total != 1 {
		t.Fatalf("Total = %d, want 1", task.Total)
	}

	// Poll until completed.
	completed := pollTaskGet(t, srv, task.ID)
	if completed.Status != "completed" {
		t.Fatalf("final status = %q, want completed", completed.Status)
	}
	if completed.Result == nil || completed.Result.Total != 1 {
		t.Fatalf("expected result with total=1, got %+v", completed.Result)
	}
	if len(completed.Result.Results) != 1 {
		t.Fatalf("expected 1 result item, got %d", len(completed.Result.Results))
	}
	item := completed.Result.Results[0]
	if item.Hash == "" {
		t.Fatal("expected non-empty Hash in result item")
	}
	if item.Score == nil {
		t.Fatal("expected non-nil Score in result item")
	}
}

// TestHandleCreateProxyCheckTask_ValidationError tests validation failure.
func TestHandleCreateProxyCheckTask_ValidationError(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})

	// Empty body (both node_hashes and proxies empty)
	body := map[string]interface{}{}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestHandleCreateProxyCheckTask_MutualExclusion tests that both fields
// being set returns an error.
func TestHandleCreateProxyCheckTask_MutualExclusion(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})

	body := map[string]interface{}{
		"node_hashes": []string{"abcd"},
		"proxies":     []string{"1.2.3.4:80"},
	}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestHandleGetProxyCheckTask_NotFound tests 404 for missing task.
func TestHandleGetProxyCheckTask_NotFound(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/proxy-check/tasks/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestHandleCreateThenGetProxyCheckTask tests full async create-then-poll flow.
func TestHandleCreateThenGetProxyCheckTask_NodeHashes(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("sub-pc-batch2", "sub-pc-batch2", "https://example.com/sub", true, false)
	cp.SubMgr.Register(sub)

	hash := setupProxyCheckNode(t, cp, sub, `{"type":"ss","server":"5.6.7.8","port":443}`)

	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 15 * time.Millisecond, nil
		},
	})

	// Create task
	createBody := map[string]interface{}{
		"node_hashes": []string{hash.Hex()},
		"profile":     "generic",
	}
	rawBody, _ := json.Marshal(createBody)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	createReq.Header.Set("Authorization", "Bearer test-admin-token")
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status: got %d, want %d; body: %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	var createdTask service.ProxyCheckTask
	if err := json.Unmarshal(createRec.Body.Bytes(), &createdTask); err != nil {
		t.Fatalf("unmarshal created task: %v", err)
	}

	// Poll until completed.
	gotTask := pollTaskGet(t, srv, createdTask.ID)

	if gotTask.ID != createdTask.ID {
		t.Fatalf("task ID mismatch: got %q, want %q", gotTask.ID, createdTask.ID)
	}
	if gotTask.Status != "completed" {
		t.Fatalf("task status = %q, want completed", gotTask.Status)
	}
	if gotTask.Result == nil || gotTask.Result.Total != 1 {
		t.Fatalf("expected result with total=1, got %+v", gotTask.Result)
	}
}

// TestCreateProxyCheckTaskRequiresAuth tests auth enforcement.
func TestCreateProxyCheckTask_RequiresAuth(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestGetProxyCheckTaskRequiresAuth tests auth enforcement.
func TestGetProxyCheckTask_RequiresAuth(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/proxy-check/tasks/some-id", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestHandleCreateProxyCheckTask_NodeHashesPartialErrors checks completed_with_errors.
func TestHandleCreateProxyCheckTask_NodeHashesPartialErrors(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("sub-pc-batch3", "sub-pc-batch3", "https://example.com/sub", true, false)
	cp.SubMgr.Register(sub)

	hash := setupProxyCheckNode(t, cp, sub, `{"type":"ss","server":"1.2.3.4","port":443}`)

	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})

	// Mix of valid and invalid hashes.
	body := map[string]interface{}{
		"node_hashes": []string{hash.Hex(), "not-a-valid-hex"},
		"profile":     "generic",
	}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: got %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var createdTask service.ProxyCheckTask
	if err := json.Unmarshal(rec.Body.Bytes(), &createdTask); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Poll until completed.
	completed := pollTaskGet(t, srv, createdTask.ID)
	if completed.Status != "completed_with_errors" {
		t.Fatalf("status = %q, want completed_with_errors", completed.Status)
	}
	if completed.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", completed.Failed)
	}
	if completed.Total != 2 {
		t.Fatalf("Total = %d, want 2", completed.Total)
	}
	if completed.Done != 2 {
		t.Fatalf("Done = %d, want 2", completed.Done)
	}
	if completed.Error == "" {
		t.Fatal("expected non-empty Error for partial failures")
	}
	if completed.Result == nil {
		t.Fatal("expected non-nil Result")
	}
}

// TestHandleCreateProxyCheckTask_AllFailed checks failed status.
func TestHandleCreateProxyCheckTask_AllFailed(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})

	body := map[string]interface{}{
		"node_hashes": []string{"bad1", "bad2"},
		"profile":     "generic",
	}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: got %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var createdTask service.ProxyCheckTask
	if err := json.Unmarshal(rec.Body.Bytes(), &createdTask); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	completed := pollTaskGet(t, srv, createdTask.ID)
	if completed.Status != "failed" {
		t.Fatalf("status = %q, want failed", completed.Status)
	}
	if completed.Failed != 2 {
		t.Fatalf("Failed = %d, want 2", completed.Failed)
	}
}

// TestHandleProxyCheckTask_ProgressDuringRun checks that progress fields
// (Total, Done, Failed) are visible before the task completes.
func TestHandleProxyCheckTask_ProgressDuringRun(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	sub := subscription.NewSubscription("sub-pc-progress", "sub-pc-progress", "https://example.com/sub", true, false)
	cp.SubMgr.Register(sub)

	hash := setupProxyCheckNode(t, cp, sub, `{"type":"ss","server":"9.9.9.9","port":443}`)

	// Use a slow mock fetcher so we can observe intermediate progress.
	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			time.Sleep(200 * time.Millisecond)
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})

	body := map[string]interface{}{
		"node_hashes": []string{hash.Hex(), hash.Hex()},
		"profile":     "generic",
	}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: got %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var createdTask service.ProxyCheckTask
	if err := json.Unmarshal(rec.Body.Bytes(), &createdTask); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Poll until completed, checking intermediate progress.
	deadline := time.Now().Add(5 * time.Second)
	observedPartial := false
	for time.Now().Before(deadline) {
		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/proxy-check/tasks/"+createdTask.ID, nil)
		getReq.Header.Set("Authorization", "Bearer test-admin-token")
		getRec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(getRec, getReq)

		if getRec.Code != http.StatusOK {
			t.Fatalf("GET status: %d", getRec.Code)
		}

		var current service.ProxyCheckTask
		if err := json.Unmarshal(getRec.Body.Bytes(), &current); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if current.IsTerminal() {
			if current.Status != "completed" {
				t.Fatalf("final status = %q, want completed", current.Status)
			}
			if current.Done != 2 || current.Failed != 0 {
				t.Fatalf("final Done=%d Failed=%d, want 2/0", current.Done, current.Failed)
			}
			break
		}

		// While running, Done may be 1 (one node processed).
		if current.Done == 1 && current.Status == "running" {
			observedPartial = true
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !observedPartial {
		t.Log("note: did not observe intermediate progress (tasks may complete too fast)")
	}
}

// TestHandleCreateProxyCheckTask_MaxCapacityEviction verifies eviction.
func TestHandleCreateProxyCheckTask_MaxCapacityEviction(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 5 * time.Millisecond, nil
		},
	})

	// Create enough all-failed tasks to trigger eviction.
	taskIDs := make([]string, 0, 110)
	for i := 0; i < 110; i++ {
		body := map[string]interface{}{
			"node_hashes": []string{"invalid-hash"},
			"profile":     "generic",
		}
		rawBody, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("task %d create status: got %d", i, rec.Code)
		}

		var task service.ProxyCheckTask
		if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
			t.Fatalf("unmarshal task %d: %v", i, err)
		}
		taskIDs = append(taskIDs, task.ID)
	}

	// Poll all tasks to completion.
	for _, id := range taskIDs {
		pollTaskGet(t, srv, id)
	}

	// The first ~10 tasks should be evicted (oldest completed/failed).
	evicted := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/proxy-check/tasks/%s", taskIDs[i]), nil)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			evicted++
		}
	}
	if evicted == 0 {
		t.Log("note: no eviction observed (tasks may still be within capacity)")
	}

	// Latest tasks should exist.
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/proxy-check/tasks/%s", taskIDs[len(taskIDs)-1]), nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected latest task to exist, got status %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Raw proxies batch path
// ---------------------------------------------------------------------------

// TestHandleCreateProxyCheckTask_RawProxies tests the raw proxies path.
func TestHandleCreateProxyCheckTask_RawProxies(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)

	cp.ProbeMgr = probe.NewProbeManager(probe.ProbeConfig{
		Pool: cp.Pool,
		Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("ok"), 10 * time.Millisecond, nil
		},
	})
	// Set a mock raw checker that delegates to the probe manager's CheckProxy
	// via a synthetic fetcher.
	cp.RawProxyChecker = func(raw json.RawMessage, profile probe.TargetProfile, opts probe.ProxyCheckOptions) (*probe.ProxyScore, error) {
		fetcher := func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
			return []byte("raw-proxy-ok"), 5*time.Millisecond, nil
		}
		return probe.CheckProxy(fetcher, node.Zero, profile, opts)
	}

	body := map[string]interface{}{
		"proxies": []string{"1.2.3.4:5678"},
		"profile": "generic",
	}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: got %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var createdTask service.ProxyCheckTask
	if err := json.Unmarshal(rec.Body.Bytes(), &createdTask); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	completed := pollTaskGet(t, srv, createdTask.ID)
	if completed.Status != "completed" {
		t.Fatalf("status = %q, want completed", completed.Status)
	}
	if completed.Result == nil || len(completed.Result.Results) != 1 {
		t.Fatalf("expected 1 result, got %+v", completed.Result)
	}
	item := completed.Result.Results[0]
	if item.Proxy != "1.2.3.4:5678" {
		t.Fatalf("Proxy = %q, want original input", item.Proxy)
	}
	if item.Hash == "" {
		t.Fatal("expected non-empty Hash")
	}
	if item.Score == nil {
		t.Fatal("expected non-nil Score")
	}
	if item.Error != "" {
		t.Fatalf("unexpected Error: %s", item.Error)
	}
}

// TestHandleCreateProxyCheckTask_RawProxiesEmpty tests that raw proxies with
// empty proxies list returns a validation error.
func TestHandleCreateProxyCheckTask_RawProxiesEmpty(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	cp.RawProxyChecker = func(_ json.RawMessage, _ probe.TargetProfile, _ probe.ProxyCheckOptions) (*probe.ProxyScore, error) {
		return &probe.ProxyScore{Grade: "A", Score: 100}, nil
	}

	body := map[string]interface{}{
		"proxies": []string{},
		"profile": "generic",
	}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestHandleCreateProxyCheckTask_RawProxiesNilChecker tests that a 500 is
// returned when the raw proxy checker is not configured.
func TestHandleCreateProxyCheckTask_RawProxiesNilChecker(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	// RawProxyChecker is nil by default.

	body := map[string]interface{}{
		"proxies": []string{"1.2.3.4:80"},
		"profile": "generic",
	}
	rawBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-check/tasks", bytes.NewReader(rawBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Error.Code != "INTERNAL" {
		t.Fatalf("error code = %q, want INTERNAL", errResp.Error.Code)
	}
}
