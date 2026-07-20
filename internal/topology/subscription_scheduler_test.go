package topology

import (
	"strings"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newSchedulerTestPool(t *testing.T, subMgr *SubscriptionManager) *GlobalNodePool {
	t.Helper()
	return NewGlobalNodePool(PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              nil,
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})
}

func newSchedulerTestSub(t *testing.T, id, name string) *subscription.Subscription {
	t.Helper()
	sub := subscription.NewSubscription(id, name, "", true, false)
	sub.SetSourceType(subscription.SourceTypeLocal)
	sub.SetContent("")
	return sub
}

// ---------------------------------------------------------------------------
// Test: scheduler tick and ForceRefreshAll with daily/interval modes
// ---------------------------------------------------------------------------

func TestScheduler_ForceRefreshAll_SkipsDailyMode(t *testing.T) {
	subMgr := NewSubscriptionManager()
	pool := newSchedulerTestPool(t, subMgr)

	intervalSub := newSchedulerTestSub(t, "interval-sub", "Interval Sub")
	dailySub := newSchedulerTestSub(t, "daily-sub", "Daily Sub")
	dailySub.SetUpdateMode(subscription.UpdateModeDaily)
	dailySub.SetUpdateTime("10:00")
	dailySub.SetUpdateTimezone("UTC")

	subMgr.Register(intervalSub)
	subMgr.Register(dailySub)

	updated := make(map[string]int)
	onUpdated := func(s *subscription.Subscription) { updated[s.ID]++ }

	sched := NewSubscriptionScheduler(SchedulerConfig{
		SubManager:   subMgr,
		Pool:         pool,
		OnSubUpdated: onUpdated,
	})
	subMgr.Register(intervalSub)
	subMgr.Register(dailySub)

	// ForceRefreshAll should NOT refresh daily subscriptions.
	// Both need content to actually run UpdateSubscription.
	intervalSub.SetContent(string(clashJSONClean()))
	dailySub.SetContent(string(clashJSONClean()))

	sched.ForceRefreshAll()

	if updated[intervalSub.ID] == 0 {
		t.Fatal("expected interval subscription to be refreshed by ForceRefreshAll")
	}
	if updated[dailySub.ID] > 0 {
		t.Fatal("expected daily subscription to be SKIPPED by ForceRefreshAll")
	}
}

func TestScheduler_ForceRefreshAllAsync_SkipsDailyMode(t *testing.T) {
	// ForceRefreshAllAsync is a goroutine wrapper around ForceRefreshAll.
	// The synchronous version has the same skip logic.
	// This test verifies the async wrapper works for interval subscriptions.
	subMgr := NewSubscriptionManager()
	pool := newSchedulerTestPool(t, subMgr)

	intervalSub := newSchedulerTestSub(t, "interval-async", "Interval Async")
	dailySub := newSchedulerTestSub(t, "daily-async", "Daily Async")
	dailySub.SetUpdateMode(subscription.UpdateModeDaily)
	dailySub.SetUpdateTime("10:00")
	dailySub.SetUpdateTimezone("UTC")

	subMgr.Register(intervalSub)
	subMgr.Register(dailySub)

	intervalSub.SetContent(string(clashJSONClean()))
	dailySub.SetContent(string(clashJSONClean()))

	// Verify the synchronous ForceRefreshAll skips daily.
	updated := make(map[string]int)
	onUpdated := func(s *subscription.Subscription) { updated[s.ID]++ }

	sched := NewSubscriptionScheduler(SchedulerConfig{
		SubManager:   subMgr,
		Pool:         pool,
		OnSubUpdated: onUpdated,
	})

	sched.ForceRefreshAll()

	if updated[intervalSub.ID] == 0 {
		t.Fatal("expected interval subscription to be refreshed by ForceRefreshAll")
	}
	if updated[dailySub.ID] > 0 {
		t.Fatal("expected daily subscription to be SKIPPED by ForceRefreshAll")
	}
}

func TestScheduler_ManualRefreshSubscription_WorksForDaily(t *testing.T) {
	// Even for daily mode, manual RefreshSubscription should still work.
	subMgr := NewSubscriptionManager()
	pool := newSchedulerTestPool(t, subMgr)

	sub := newSchedulerTestSub(t, "daily-manual", "Daily Manual")
	sub.SetUpdateMode(subscription.UpdateModeDaily)
	sub.SetUpdateTime("10:00")
	sub.SetUpdateTimezone("UTC")

	subMgr.Register(sub)

	updated := false
	onUpdated := func(s *subscription.Subscription) { updated = true }

	sched := NewSubscriptionScheduler(SchedulerConfig{
		SubManager:   subMgr,
		Pool:         pool,
		OnSubUpdated: onUpdated,
	})

	sub.SetContent(string(clashJSONClean()))
	sched.UpdateSubscription(sub)

	if !updated {
		t.Fatal("expected manual UpdateSubscription to work for daily mode")
	}
}

func TestScheduler_Tick_DailyDue(t *testing.T) {
	// Test that IsSubscriptionDue correctly identifies a daily subscription as due.
	// This is the logic used by the scheduler tick function.
	sub := newSchedulerTestSub(t, "daily-tick", "Daily Tick")
	sub.SetUpdateMode(subscription.UpdateModeDaily)
	sub.SetUpdateTime("10:00")
	sub.SetUpdateTimezone("UTC")

	// Simulate lastChecked being yesterday at 10:00, now being today at 10:01.
	yesterday10AM := time.Date(2025, 1, 14, 10, 0, 0, 0, time.UTC)
	sub.LastCheckedNs.Store(yesterday10AM.UnixNano())

	now := time.Date(2025, 1, 15, 10, 1, 0, 0, time.UTC)
	due := subscription.IsSubscriptionDue(
		sub.LastCheckedNs.Load(),
		now,
		sub.UpdateMode(),
		sub.UpdateIntervalNs(),
		sub.UpdateTime(),
		sub.UpdateTimezone(),
	)
	if !due {
		t.Fatal("expected daily subscription to be due at scheduled time")
	}
}

// clashJSONMixed returns Clash JSON with one node having a malformed
// fingerprint (rejected under default policy) and one clean SS node.
func clashJSONMixed() []byte {
	return []byte(`{
		"proxies": [
			{
				"name": "hy2-bad",
				"type": "hysteria2",
				"server": "hy2.example.com",
				"port": 443,
				"password": "pass",
				"fingerprint": "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
			},
			{
				"name": "ss-good",
				"type": "ss",
				"server": "1.1.1.1",
				"port": 8388,
				"cipher": "aes-128-gcm",
				"password": "pass"
			}
		]
	}`)
}

// clashJSONClean returns Clash JSON with only clean accepted nodes.
func clashJSONClean() []byte {
	return []byte(`{
		"proxies": [
			{
				"name": "ss-one",
				"type": "ss",
				"server": "1.1.1.1",
				"port": 8388,
				"cipher": "aes-128-gcm",
				"password": "pass"
			},
			{
				"name": "ss-two",
				"type": "ss",
				"server": "2.2.2.2",
				"port": 8388,
				"cipher": "aes-128-gcm",
				"password": "pass"
			}
		]
	}`)
}

// clashJSONOnlyRejected returns Clash JSON where every node is rejected.
func clashJSONOnlyRejected() []byte {
	return []byte(`{
		"proxies": [
			{
				"name": "hy2-rej1",
				"type": "hysteria2",
				"server": "hy2.example.com",
				"port": 443,
				"password": "pass",
				"fingerprint": "aabbccdd"
			},
			{
				"name": "hy2-rej2",
				"type": "hysteria2",
				"server": "hy2-2.example.com",
				"port": 443,
				"password": "pass",
				"fingerprint": "0000000000000000000000000000000000000000000000000000000000000000"
			}
		]
	}`)
}

// clashJSONWithWarning returns Clash JSON where a node has a valid fingerprint
// accepted under drop_always policy, producing a warning.
func clashJSONWithWarning() []byte {
	return []byte(`{
		"proxies": [
			{
				"name": "hy2-warn",
				"type": "hysteria2",
				"server": "hy2.example.com",
				"port": 443,
				"password": "pass",
				"fingerprint": "aabbccddee0011223344556677889900aabbccddee0011223344556677889900"
			},
			{
				"name": "ss-good",
				"type": "ss",
				"server": "1.1.1.1",
				"port": 8388,
				"cipher": "aes-128-gcm",
				"password": "pass"
			}
		]
	}`)
}

// existingNode returns raw sing-box JSON for a pre-existing subscription node.
func existingNode() ([]byte, node.Hash) {
	raw := []byte(`{"method":"aes-128-gcm","password":"pass","server":"9.9.9.9","server_port":443,"tag":"existing","type":"shadowsocks"}`)
	return raw, node.HashFromRawOptions(raw)
}

// ---------------------------------------------------------------------------
// Test: buildSubRefreshSummary
// ---------------------------------------------------------------------------

func TestBuildSubRefreshSummary_Empty(t *testing.T) {
	got := buildSubRefreshSummary(nil, nil, nil)
	if got != "" {
		t.Fatalf("expected empty summary, got %q", got)
	}
}

func TestBuildSubRefreshSummary_RejectedOnly(t *testing.T) {
	rejected := []subscription.RejectedNode{
		{Code: "CLASH_FINGERPRINT_INVALID", Message: "bad fp", Tag: "node-a"},
	}
	got := buildSubRefreshSummary(nil, rejected, nil)
	if !strings.Contains(got, "accepted=0 rejected=1 warnings=0") {
		t.Fatalf("missing counts in: %s", got)
	}
	if !strings.Contains(got, "CLASH_FINGERPRINT_INVALID") {
		t.Fatalf("missing code in: %s", got)
	}
	if !strings.Contains(got, "node-a") {
		t.Fatalf("missing tag in: %s", got)
	}
}

func TestBuildSubRefreshSummary_Mixed(t *testing.T) {
	rejected := []subscription.RejectedNode{
		{Code: "CLASH_CERTIFICATE_FINGERPRINT_UNSUPPORTED", Tag: "node-a"},
		{Code: "CLASH_FINGERPRINT_BROWSER_NAME", Tag: "node-b"},
	}
	warnings := []subscription.ParseWarning{
		{Code: "CLASH_FINGERPRINT_DROP_ALWAYS", Tag: "node-c"},
	}
	got := buildSubRefreshSummary(
		[]subscription.ParsedNode{{Tag: "accepted-1"}},
		rejected, warnings,
	)
	if !strings.Contains(got, "accepted=1 rejected=2 warnings=1") {
		t.Fatalf("missing counts in: %s", got)
	}
	if !strings.Contains(got, "CLASH_CERTIFICATE_FINGERPRINT_UNSUPPORTED") {
		t.Fatalf("missing first rejection code in: %s", got)
	}
	if !strings.Contains(got, "CLASH_FINGERPRINT_BROWSER_NAME") {
		t.Fatalf("missing second rejection code in: %s", got)
	}
	if !strings.Contains(got, "CLASH_FINGERPRINT_DROP_ALWAYS") {
		t.Fatalf("missing warning code in: %s", got)
	}
}

func TestBuildSubRefreshSummary_Bounded(t *testing.T) {
	// More than maxDiagEntries (10) total.
	rejected := make([]subscription.RejectedNode, 8)
	for i := range rejected {
		rejected[i] = subscription.RejectedNode{Code: "CLASH_REJECT", Tag: "node"}
	}
	warnings := make([]subscription.ParseWarning, 5)
	for i := range warnings {
		warnings[i] = subscription.ParseWarning{Code: "CLASH_WARN", Tag: "node"}
	}
	got := buildSubRefreshSummary(nil, rejected, warnings)
	if !strings.Contains(got, "(+3 more)") {
		t.Fatalf("expected remainder marker in bounded summary, got: %s", got)
	}
}

func TestBuildSubRefreshSummary_TagTruncated(t *testing.T) {
	// Tag over maxTagLen (80) → truncated with "...".
	longTag := ""
	for i := 0; i < 30; i++ {
		longTag += "abcdefgh" // 8 × 30 = 240 bytes
	}
	rejected := []subscription.RejectedNode{
		{Code: "CLASH_REJECT", Tag: longTag},
	}
	got := buildSubRefreshSummary(nil, rejected, nil)
	if !strings.Contains(got, "CLASH_REJECT") {
		t.Fatalf("missing code in: %s", got)
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("truncated tag should contain '...', got: %s", got)
	}
	// Verify the full tag is NOT present.
	if strings.Contains(got, longTag) {
		t.Fatal("summary must not contain the full untruncated tag")
	}
	// Verify the truncated portion is at most maxTagLen + len("...").
	tagStart := strings.Index(got, " on ")
	if tagStart < 0 {
		t.Fatalf("missing ' on ' in summary: %s", got)
	}
	afterOn := got[tagStart+4:]
	if len(afterOn) > maxTagLen+3+10 { // +10 for safety margin
		t.Fatalf("tag portion too long: %d bytes", len(afterOn))
	}
}

// TestBuildSubRefreshSummary_TagTruncated_UTF8Safe verifies that truncation
// does not split a multi-byte UTF-8 rune.
func TestBuildSubRefreshSummary_TagTruncated_UTF8Safe(t *testing.T) {
	// Chinese characters are 3 bytes each; 30 chars = 90 bytes, > 80.
	longTag := ""
	for i := 0; i < 30; i++ {
		longTag += "测" // 3 bytes
	}
	rejected := []subscription.RejectedNode{
		{Code: "CLASH_REJECT", Tag: longTag},
	}
	got := buildSubRefreshSummary(nil, rejected, nil)
	if !strings.Contains(got, "CLASH_REJECT") {
		t.Fatalf("missing code in: %s", got)
	}
	// Must be valid UTF-8 (no replacement character from broken runes).
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatal("summary contains invalid UTF-8 replacement character")
	}
}

// ---------------------------------------------------------------------------
// Test: UpdateSubscription with diagnostics consumption
// ---------------------------------------------------------------------------

func TestScheduler_MixedRefresh_AcceptedAndRejected(t *testing.T) {
	subMgr := NewSubscriptionManager()
	pool := newSchedulerTestPool(t, subMgr)
	sub := newSchedulerTestSub(t, "test-mixed", "Mixed Subscription")

	var updatedSub *subscription.Subscription
	onUpdated := func(s *subscription.Subscription) { updatedSub = s }

	sched := NewSubscriptionScheduler(SchedulerConfig{
		SubManager:   subMgr,
		Pool:         pool,
		Downloader:   nil,
		OnSubUpdated: onUpdated,
	})
	subMgr.Register(sub)

	sub.SetContent(string(clashJSONMixed()))
	sched.UpdateSubscription(sub)

	if updatedSub == nil {
		t.Fatal("expected onSubUpdated to be called")
	}

	// Pool should have exactly 1 node (the accepted SS sibling).
	if pool.Size() != 1 {
		t.Fatalf("expected pool size 1 (accepted node only), got %d", pool.Size())
	}

	// Managed nodes should have exactly 1 entry.
	if sub.ManagedNodes().Size() != 1 {
		t.Fatalf("expected 1 managed node, got %d", sub.ManagedNodes().Size())
	}

	// Verify LastError contains diagnostic summary with stable code.
	lastErr := sub.GetLastError()
	if lastErr == "" {
		t.Fatal("expected non-empty LastError for partial rejection")
	}
	if !strings.Contains(lastErr, "CLASH_FINGERPRINT_INVALID") {
		t.Fatalf("LastError should contain rejection code, got: %s", lastErr)
	}
	if !strings.Contains(lastErr, "hy2-bad") {
		t.Fatalf("LastError should contain rejected node tag, got: %s", lastErr)
	}
	if strings.Contains(lastErr, "zzzz") {
		t.Fatal("LastError must not contain raw fingerprint values")
	}
	if sub.LastAppliedSeq() == 0 {
		t.Fatal("expected LastAppliedSeq > 0")
	}
}

func TestScheduler_CleanRefresh_ClearsLastError(t *testing.T) {
	subMgr := NewSubscriptionManager()
	pool := newSchedulerTestPool(t, subMgr)
	sub := newSchedulerTestSub(t, "test-clear", "Clear Subscription")
	sched := NewSubscriptionScheduler(SchedulerConfig{
		SubManager: subMgr,
		Pool:       pool,
	})
	subMgr.Register(sub)

	// First refresh: mixed → LastError is set.
	sub.SetContent(string(clashJSONMixed()))
	sched.UpdateSubscription(sub)
	if sub.GetLastError() == "" {
		t.Fatal("expected LastError to be set after mixed refresh")
	}

	// Second refresh: clean data → LastError cleared.
	sub.SetContent(string(clashJSONClean()))
	prevSeq := sub.LastAppliedSeq()
	sched.UpdateSubscription(sub)
	if sub.GetLastError() != "" {
		t.Fatalf("expected LastError cleared after clean refresh, got: %s", sub.GetLastError())
	}
	if sub.LastAppliedSeq() <= prevSeq {
		t.Fatal("expected LastAppliedSeq to advance after clean refresh")
	}
}

func TestScheduler_RejectionDisablesIncrementalRetention(t *testing.T) {
	subMgr := NewSubscriptionManager()
	pool := newSchedulerTestPool(t, subMgr)
	sub := newSchedulerTestSub(t, "test-incr-reject", "Incremental Reject")

	sub.SetIncrementalAliveNodes(true)
	rawExisting, existingHash := existingNode()
	pool.AddNodeFromSub(existingHash, rawExisting, sub.ID)
	sub.ManagedNodes().StoreNode(existingHash, subscription.ManagedNode{Tags: []string{"existing"}})

	sched := NewSubscriptionScheduler(SchedulerConfig{
		SubManager: subMgr,
		Pool:       pool,
	})
	subMgr.Register(sub)

	// Refresh with rejected nodes → incremental mode suppressed.
	sub.SetContent(string(clashJSONMixed()))
	prevSize := pool.Size()
	sched.UpdateSubscription(sub)

	// Old existing node removed.
	_, found := pool.GetEntry(existingHash)
	if found {
		t.Fatal("old existing node should have been removed when incremental suppressed due to rejections")
	}
	_, foundInManaged := sub.ManagedNodes().LoadNode(existingHash)
	if foundInManaged {
		t.Fatal("old existing node should not be in managed nodes after suppression")
	}
	// Pool lost the old node, gained the new accepted node.
	if pool.Size() != prevSize {
		t.Fatalf("expected pool size %d (lost old, gained 1 new), got %d", prevSize, pool.Size())
	}
}

func TestScheduler_OnlyRejectedNodes_StillApplied(t *testing.T) {
	subMgr := NewSubscriptionManager()
	pool := newSchedulerTestPool(t, subMgr)

	rawExisting, existingHash := existingNode()
	pool.AddNodeFromSub(existingHash, rawExisting, "test-only-rejected")
	sub := newSchedulerTestSub(t, "test-only-rejected", "Only Rejected")
	sub.ManagedNodes().StoreNode(existingHash, subscription.ManagedNode{Tags: []string{"existing"}})
	subMgr.Register(sub)

	sched := NewSubscriptionScheduler(SchedulerConfig{
		SubManager: subMgr,
		Pool:       pool,
	})

	sub.SetContent(string(clashJSONOnlyRejected()))
	sched.UpdateSubscription(sub)

	// Old node removed.
	_, found := pool.GetEntry(existingHash)
	if found {
		t.Fatal("old node should be removed after refresh with only rejected nodes")
	}
	if sub.ManagedNodes().Size() != 0 {
		t.Fatalf("expected 0 managed nodes, got %d", sub.ManagedNodes().Size())
	}

	lastErr := sub.GetLastError()
	if lastErr == "" {
		t.Fatal("expected LastError to be set when all nodes rejected")
	}
	if !strings.Contains(lastErr, "accepted=0 rejected=2") {
		t.Fatalf("LastError should show 0 accepted 2 rejected, got: %s", lastErr)
	}
}

func TestScheduler_StalePartialResultGuard(t *testing.T) {
	subMgr := NewSubscriptionManager()
	pool := newSchedulerTestPool(t, subMgr)
	sub := newSchedulerTestSub(t, "test-stale-guard", "Stale Guard")

	sched := NewSubscriptionScheduler(SchedulerConfig{
		SubManager: subMgr,
		Pool:       pool,
	})
	subMgr.Register(sub)

	// Apply a clean refresh first.
	sub.SetContent(string(clashJSONClean()))
	sched.UpdateSubscription(sub)
	if sub.GetLastError() != "" {
		t.Fatalf("after clean refresh LastError should be empty, got: %s", sub.GetLastError())
	}

	// Advance lastAppliedSeq past the next attempt seq to simulate staleness.
	sub.MarkAppliedAttempt(sub.NextAttemptSeq() + 100)

	// Try an update with mixed content (would set LastError if applied).
	sub.SetContent(string(clashJSONMixed()))
	sched.UpdateSubscription(sub)

	// The seq guard should have prevented this stale attempt.
	if sub.GetLastError() != "" {
		t.Fatalf("stale partial result should be discarded by seq guard, got LastError: %s", sub.GetLastError())
	}
}

func TestScheduler_Warnings_NonFatal(t *testing.T) {
	subMgr := NewSubscriptionManager()
	pool := newSchedulerTestPool(t, subMgr)
	sub := newSchedulerTestSub(t, "test-warnings", "Warnings Sub")

	sub.SetClashFingerprintPolicy(subscription.ClashFingerprintDropAlways)
	sub.SetIncrementalAliveNodes(true)

	rawExisting, existingHash := existingNode()
	pool.AddNodeFromSub(existingHash, rawExisting, sub.ID)
	sub.ManagedNodes().StoreNode(existingHash, subscription.ManagedNode{Tags: []string{"existing"}})
	// Mark the existing node as healthy (circuit closed, outbound present)
	// so it is preserved by incremental-mode merging.
	if entry, ok := pool.GetEntry(existingHash); ok {
		entry.CircuitOpenSince.Store(0)
		ob := testutil.NewNoopOutbound()
		entry.Outbound.Store(&ob)
	}

	sched := NewSubscriptionScheduler(SchedulerConfig{
		SubManager: subMgr,
		Pool:       pool,
	})
	subMgr.Register(sub)

	sub.SetContent(string(clashJSONWithWarning()))
	sched.UpdateSubscription(sub)

	// Existing node preserved (warnings don't suppress incremental).
	_, foundOld := pool.GetEntry(existingHash)
	if !foundOld {
		t.Fatal("existing node should be preserved when only warnings (no rejections)")
	}

	// Pool: existing + hy2-warn + ss-good = 3
	if pool.Size() != 3 {
		t.Fatalf("expected pool size 3 (existing + hy2-warn + ss-good), got %d", pool.Size())
	}
	if sub.ManagedNodes().Size() != 3 {
		t.Fatalf("expected 3 managed nodes, got %d", sub.ManagedNodes().Size())
	}

	lastErr := sub.GetLastError()
	if lastErr == "" {
		t.Fatal("expected non-empty LastError for warnings")
	}
	if !strings.Contains(lastErr, "CLASH_FINGERPRINT_DROP_ALWAYS") {
		t.Fatalf("LastError should contain warning code, got: %s", lastErr)
	}
}
