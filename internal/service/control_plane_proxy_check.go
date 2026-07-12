package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe"
	"github.com/Resinat/Resin/internal/subscription"
)

// ---------------------------------------------------------------------------
// RawProxyChecker — injected closure that executes a proxy check against a
// raw outbound configuration (not from the node pool). The closure is
// responsible for building the outbound, performing the HTTP request,
// and closing the outbound. service layer does NOT import sing-box/outbound.
// ---------------------------------------------------------------------------

// RawProxyChecker executes a proxy check for an ad-hoc outbound configuration.
// rawOptions is the sing-box compatible outbound JSON.
// Score is nil when the check could not be performed (build/network error).
// Score is non-nil when a grade was assigned (even F).
type RawProxyChecker func(rawOptions json.RawMessage, profile probe.TargetProfile, opts probe.ProxyCheckOptions) (*probe.ProxyScore, error)

// ---------------------------------------------------------------------------
// Proxy check (single node, synchronous)
// ---------------------------------------------------------------------------

// ProxyCheckRequest is the optional request body for the node action endpoint.
type ProxyCheckRequest struct {
	Profile string                 `json:"profile,omitempty"`
	Options *probe.ProxyCheckOptions `json:"options,omitempty"`
}

// CheckProxyCheck performs a synchronous proxy check for a single node.
// It validates the node hash and existence, then delegates to CheckProxySync.
// On completion, it writes back the quality state via RecordNodeQuality.
// This is called by the node action endpoint POST /api/v1/nodes/{hash}/actions/proxy-check.
func (s *ControlPlaneService) CheckProxyCheck(hashStr string, req ProxyCheckRequest) (*probe.ProxyScore, error) {
	h, err := node.ParseHex(hashStr)
	if err != nil {
		return nil, invalidArg("node_hash: invalid format")
	}
	if _, ok := s.Pool.GetEntry(h); !ok {
		return nil, notFound("node not found")
	}
	if s.ProbeMgr == nil {
		return nil, internal("proxy check not available", fmt.Errorf("probe manager not initialized"))
	}

	profile := probe.LookupProfile(req.Profile)

	opts := resolveOptions(req.Options)
	if opts.Rounds > maxProxyCheckRounds {
		opts.Rounds = maxProxyCheckRounds
	}

	result, err := s.ProbeMgr.CheckProxySync(h, profile, opts)
	if err != nil {
		// Check failed with an error but no score — record a quality entry
		// with the error so the node's quality state reflects the attempt.
		nq := probe.ProxyScoreToNodeQuality(profile.Name, nil, err)
		s.Pool.RecordNodeQuality(h, nq)
		return nil, internal("proxy check failed", err)
	}

	// Successful check — map ProxyScore to NodeQuality and record.
	nq := probe.ProxyScoreToNodeQuality(profile.Name, result, nil)
	s.Pool.RecordNodeQuality(h, nq)

	return result, nil
}

// ---------------------------------------------------------------------------
// Proxy check batch (in-memory task manager, async execution)
// ---------------------------------------------------------------------------

const (
	maxProxyCheckBatchNodes = 200
	maxProxyCheckRounds     = 3
	maxProxyCheckTasks      = 100
)

// ProxyCheckBatchRequest is the request body for creating a batch proxy check task.
// Exactly one of NodeHashes or Proxies must be set (mutually exclusive).
type ProxyCheckBatchRequest struct {
	NodeHashes []string                 `json:"node_hashes,omitempty"`
	Proxies    []string                 `json:"proxies,omitempty"`
	Profile    string                   `json:"profile,omitempty"`
	Options    *probe.ProxyCheckOptions `json:"options,omitempty"`
}

// ProxyCheckResultItem wraps an individual proxy check result with the input
// identifier and result. Semantics:
//   - For node_hash inputs: Hash is set, Proxy is empty, Score is non-nil.
//   - For raw proxy inputs: Proxy is set to the original proxy string, Hash
//     is the computed node hash from the parsed outbound config.
//   - Score is non-nil when the check completed (grade may be F for failure).
//   - Score is nil and Error is non-empty when the input could not be parsed
//     or the outbound could not be built.
type ProxyCheckResultItem struct {
	Proxy string            `json:"proxy,omitempty"`
	Hash  string            `json:"hash,omitempty"`
	Score *probe.ProxyScore `json:"score,omitempty"`
	Error string            `json:"error,omitempty"`
}

// ProxyCheckBatchResult holds the aggregated results of a completed batch proxy check.
type ProxyCheckBatchResult struct {
	Results []ProxyCheckResultItem `json:"results"`
	Total   int                    `json:"total"`
	Done    int                    `json:"done"`
	Failed  int                    `json:"failed"`
	Summary string                 `json:"summary,omitempty"`
}

// ProxyCheckTask represents a batch proxy check task with status and progress.
// Status transitions: pending → running → completed | completed_with_errors | failed.
type ProxyCheckTask struct {
	ID          string                   `json:"id"`
	Status      string                   `json:"status"`
	CreatedAt   time.Time                `json:"created_at"`
	CompletedAt *time.Time               `json:"completed_at,omitempty"`
	Request     ProxyCheckBatchRequest   `json:"request"`
	Result      *ProxyCheckBatchResult   `json:"result,omitempty"`
	Error       string                   `json:"error,omitempty"`
	Total       int                      `json:"total"`
	Done        int                      `json:"done"`
	Failed      int                      `json:"failed"`

	mu *sync.Mutex
}

// snapshot returns a consistent copy safe for concurrent access.
func (t *ProxyCheckTask) snapshot() ProxyCheckTask {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := *t
	s.mu = nil // snapshots are response DTOs and must not carry task locks
	if s.Result != nil {
		r := *s.Result
		s.Result = &r
	}
	return s
}

// IsTerminal returns true when the task has reached a final status.
func (t *ProxyCheckTask) IsTerminal() bool {
	return t.Status == "completed" ||
		t.Status == "completed_with_errors" ||
		t.Status == "failed"
}

// updateProgress safely sets the Done and Failed counters.
func (t *ProxyCheckTask) updateProgress(done, failed int) {
	t.mu.Lock()
	t.Done = done
	t.Failed = failed
	t.mu.Unlock()
}

// ProxyCheckTaskManager holds in-memory proxy check tasks.
// It is safe for concurrent use.
type ProxyCheckTaskManager struct {
	mu     sync.Mutex
	tasks  map[string]*ProxyCheckTask
	nextID atomic.Int64
}

// NewProxyCheckTaskManager creates a new ProxyCheckTaskManager.
func NewProxyCheckTaskManager() *ProxyCheckTaskManager {
	return &ProxyCheckTaskManager{
		tasks: make(map[string]*ProxyCheckTask),
	}
}

// validateBatchRequest checks mutual exclusivity and limits for node_hashes
// and proxies fields. Returns the computed total count or an error.
func validateBatchRequest(req ProxyCheckBatchRequest) (int, error) {
	hasHashes := len(req.NodeHashes) > 0
	hasProxies := len(req.Proxies) > 0

	if !hasHashes && !hasProxies {
		return 0, invalidArg("must provide either node_hashes or proxies")
	}
	if hasHashes && hasProxies {
		return 0, invalidArg("node_hashes and proxies are mutually exclusive; provide only one")
	}

	if hasHashes {
		if len(req.NodeHashes) > maxProxyCheckBatchNodes {
			return 0, invalidArg(fmt.Sprintf("node_hashes: max %d nodes", maxProxyCheckBatchNodes))
		}
		return len(req.NodeHashes), nil
	}

	if len(req.Proxies) > maxProxyCheckBatchNodes {
		return 0, invalidArg(fmt.Sprintf("proxies: max %d entries", maxProxyCheckBatchNodes))
	}
	return len(req.Proxies), nil
}

// CreateTask validates the request, creates a pending task, stores it,
// launches a background goroutine to execute the proxy checks asynchronously,
// and returns immediately. The returned task is a safe snapshot.
//
// probeMgr is used for the node_hashes path. rawChecker is used for the
// raw proxies path. Both may be nil but at least one must be non-nil
// depending on the request type.
func (mgr *ProxyCheckTaskManager) CreateTask(req ProxyCheckBatchRequest, probeMgr *probe.ProbeManager, rawChecker RawProxyChecker) (*ProxyCheckTask, error) {
	opts := resolveOptions(req.Options)
	if opts.Rounds > maxProxyCheckRounds {
		return nil, invalidArg(fmt.Sprintf("rounds: max %d", maxProxyCheckRounds))
	}

	total, err := validateBatchRequest(req)
	if err != nil {
		return nil, err
	}

	if len(req.NodeHashes) > 0 && probeMgr == nil {
		return nil, internal("proxy check not available", fmt.Errorf("probe manager not initialized"))
	}
	if len(req.Proxies) > 0 && rawChecker == nil {
		return nil, internal("proxy check not available for raw proxies", fmt.Errorf("raw proxy checker not initialized"))
	}

	id := fmt.Sprintf("pct-%d", mgr.nextID.Add(1))
	now := time.Now()
	task := &ProxyCheckTask{
		ID:        id,
		Status:    "pending",
		CreatedAt: now,
		Request:   req,
		Total:     total,
		mu:        &sync.Mutex{},
	}

	// Store task before launching the goroutine so GetTask finds it immediately.
	mgr.mu.Lock()
	mgr.tasks[id] = task
	mgr.mu.Unlock()

	go mgr.executeTask(task, req, probeMgr, rawChecker)

	snap := task.snapshot()
	return &snap, nil
}

// executeTask runs the proxy checks in a background goroutine and updates the task.
func (mgr *ProxyCheckTaskManager) executeTask(task *ProxyCheckTask, req ProxyCheckBatchRequest, probeMgr *probe.ProbeManager, rawChecker RawProxyChecker) {
	defer func() {
		if r := recover(); r != nil {
			task.mu.Lock()
			task.Status = "failed"
			task.Error = fmt.Sprintf("panic: %v", r)
			now := time.Now()
			task.CompletedAt = &now
			task.mu.Unlock()
			mgr.evictIfNeeded()
		}
	}()

	// Transition to running.
	task.mu.Lock()
	task.Status = "running"
	task.mu.Unlock()

	profile := probe.LookupProfile(req.Profile)
	opts := resolveOptions(req.Options)

	items := make([]ProxyCheckResultItem, 0, task.Total)
	var firstErr string
	var failedCount int

	if len(req.NodeHashes) > 0 {
		// ----- Node hash path (pool nodes) -----
		for i, hashStr := range req.NodeHashes {
			h, err := node.ParseHex(hashStr)
			if err != nil {
				if firstErr == "" {
					firstErr = fmt.Sprintf("invalid node_hash %q: %v", hashStr, err)
				}
				items = append(items, ProxyCheckResultItem{
					Hash:  hashStr,
					Error: err.Error(),
				})
				failedCount++
				task.updateProgress(i+1, failedCount)
				continue
			}

			score, err := probeMgr.CheckProxySync(h, profile, opts)
			if err != nil {
				if firstErr == "" {
					firstErr = fmt.Sprintf("check failed for %s: %v", hashStr, err)
				}
				items = append(items, ProxyCheckResultItem{
					Hash:  hashStr,
					Score: &probe.ProxyScore{Grade: "F", Score: 0},
					Error: err.Error(),
				})
				failedCount++
			} else {
				items = append(items, ProxyCheckResultItem{
					Hash:  hashStr,
					Score: score,
				})
			}
			task.updateProgress(i+1, failedCount)
		}
	} else {
		// ----- Raw proxy path (ad-hoc outbounds, MVP serial execution) -----
		for i, proxyStr := range req.Proxies {
			// Parse each proxy string individually to preserve original input.
			parsed, parseErr := subscription.ParseGeneralSubscription([]byte(proxyStr))
			if parseErr != nil || len(parsed) == 0 {
				if firstErr == "" {
					firstErr = fmt.Sprintf("proxy parse failed for %q: %v", proxyStr, parseErr)
				}
				items = append(items, ProxyCheckResultItem{
					Proxy: proxyStr,
					Error: fmt.Sprintf("parse: %v", parseErr),
				})
				failedCount++
				task.updateProgress(i+1, failedCount)
				continue
			}

			pn := parsed[0]
			h := node.HashFromRawOptions(pn.RawOptions)

			score, err := rawChecker(pn.RawOptions, profile, opts)
			if err != nil {
				if firstErr == "" {
					firstErr = fmt.Sprintf("check failed for %s: %v", proxyStr, err)
				}
				items = append(items, ProxyCheckResultItem{
					Proxy: proxyStr,
					Hash:  h.Hex(),
					Error: err.Error(),
				})
				failedCount++
			} else {
				items = append(items, ProxyCheckResultItem{
					Proxy: proxyStr,
					Hash:  h.Hex(),
					Score: score,
				})
			}
			task.updateProgress(i+1, failedCount)
		}
	}

	// Determine final status.
	completedAt := time.Now()
	finalStatus := "completed"
	if failedCount == task.Total {
		finalStatus = "failed"
	} else if failedCount > 0 {
		finalStatus = "completed_with_errors"
	}

	task.mu.Lock()
	task.Status = finalStatus
	task.CompletedAt = &completedAt
	task.Result = &ProxyCheckBatchResult{
		Results: items,
		Total:   len(items),
		Done:    len(items),
		Failed:  failedCount,
		Summary: fmt.Sprintf("%d/%d checked, %d failed", len(items), task.Total, failedCount),
	}
	if firstErr != "" {
		task.Error = firstErr
	}
	task.Done = len(items)
	task.Failed = failedCount
	task.mu.Unlock()

	mgr.evictIfNeeded()
}

// evictIfNeeded removes the oldest completed/failed tasks when the map
// exceeds maxProxyCheckTasks. Pending and running tasks are never evicted.
func (mgr *ProxyCheckTaskManager) evictIfNeeded() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if len(mgr.tasks) <= maxProxyCheckTasks {
		return
	}

	type agedTask struct {
		id          string
		completedAt time.Time
	}
	var candidates []agedTask
	for id, t := range mgr.tasks {
		t.mu.Lock()
		terminal := t.IsTerminal()
		var ca *time.Time
		if terminal {
			ca = t.CompletedAt
		}
		t.mu.Unlock()
		if terminal && ca != nil {
			candidates = append(candidates, agedTask{id, *ca})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].completedAt.Before(candidates[j].completedAt)
	})

	excess := len(mgr.tasks) - maxProxyCheckTasks
	for i := 0; i < excess && i < len(candidates); i++ {
		delete(mgr.tasks, candidates[i].id)
	}
}

// GetTask retrieves a snapshot of a task by ID. Returns a notFound error if
// the task does not exist or has been evicted.
func (mgr *ProxyCheckTaskManager) GetTask(id string) (*ProxyCheckTask, error) {
	mgr.mu.Lock()
	task, ok := mgr.tasks[id]
	mgr.mu.Unlock()
	if !ok {
		return nil, notFound("task not found")
	}
	snap := task.snapshot()
	return &snap, nil
}

// getProxyCheckTaskManager returns the task manager, creating it lazily on first
// access using sync.Once for thread-safe initialisation.
func (s *ControlPlaneService) getProxyCheckTaskManager() *ProxyCheckTaskManager {
	s.proxyCheckTaskMgrOnce.Do(func() {
		s.proxyCheckTaskMgr = NewProxyCheckTaskManager()
	})
	return s.proxyCheckTaskMgr
}

// CreateProxyCheckBatchTask creates a batch proxy check task and starts its
// execution asynchronously. Returns immediately with the initial task snapshot.
func (s *ControlPlaneService) CreateProxyCheckBatchTask(req ProxyCheckBatchRequest) (*ProxyCheckTask, error) {
	return s.getProxyCheckTaskManager().CreateTask(req, s.ProbeMgr, s.RawProxyChecker)
}

// GetProxyCheckTask retrieves a batch proxy check task by ID.
func (s *ControlPlaneService) GetProxyCheckTask(id string) (*ProxyCheckTask, error) {
	return s.getProxyCheckTaskManager().GetTask(id)
}

// resolveOptions returns the effective ProxyCheckOptions.
// If opts is nil, probe.DefaultOptions() is returned.
// If opts is non-nil, zero/negative Rounds are defaulted to 1.
// All other fields are used as-is, allowing explicit false values to
// disable checks. Callers may additionally cap Rounds as appropriate.
func resolveOptions(opts *probe.ProxyCheckOptions) probe.ProxyCheckOptions {
	if opts == nil {
		return probe.DefaultOptions()
	}
	resolved := *opts
	if resolved.Rounds <= 0 {
		resolved.Rounds = 1
	}
	return resolved
}
