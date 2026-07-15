package state

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Resinat/Resin/internal/model"
)

func newTestCacheRepo(t *testing.T) *CacheRepo {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDB(dir + "/cache.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateCacheDB(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return newCacheRepo(db)
}

// --- nodes_static ---

func TestCacheRepo_NodesStatic_BulkUpsertAndLoad(t *testing.T) {
	repo := newTestCacheRepo(t)

	nodes := []model.NodeStatic{
		{Hash: "aaa", RawOptions: json.RawMessage(`{"type":"ss"}`), CreatedAtNs: 100},
		{Hash: "bbb", RawOptions: json.RawMessage(`{"type":"vmess"}`), CreatedAtNs: 200},
	}
	if err := repo.BulkUpsertNodesStatic(nodes); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.LoadAllNodesStatic()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(loaded))
	}

	// Idempotent upsert: update existing.
	nodes[0].RawOptions = json.RawMessage(`{"type":"ss","updated":true}`)
	if err := repo.BulkUpsertNodesStatic(nodes[:1]); err != nil {
		t.Fatal(err)
	}
	loaded, _ = repo.LoadAllNodesStatic()
	for _, n := range loaded {
		if n.Hash == "aaa" && string(n.RawOptions) != `{"type":"ss","updated":true}` {
			t.Fatalf("expected updated options, got %s", string(n.RawOptions))
		}
	}
}

func TestCacheRepo_NodesStatic_BulkDelete(t *testing.T) {
	repo := newTestCacheRepo(t)

	nodes := []model.NodeStatic{
		{Hash: "aaa", RawOptions: json.RawMessage(`{}`), CreatedAtNs: 100},
		{Hash: "bbb", RawOptions: json.RawMessage(`{}`), CreatedAtNs: 200},
	}
	repo.BulkUpsertNodesStatic(nodes)

	if err := repo.BulkDeleteNodesStatic([]string{"aaa"}); err != nil {
		t.Fatal(err)
	}
	loaded, _ := repo.LoadAllNodesStatic()
	if len(loaded) != 1 || loaded[0].Hash != "bbb" {
		t.Fatalf("expected only bbb, got %+v", loaded)
	}
}

// --- nodes_dynamic ---

func TestCacheRepo_NodesDynamic_BulkUpsertAndLoad(t *testing.T) {
	repo := newTestCacheRepo(t)

	nodes := []model.NodeDynamic{
		{
			Hash:                               "aaa",
			FailureCount:                       3,
			CircuitOpenSince:                   1000,
			EgressIP:                           "1.2.3.4",
			EgressRegion:                       "us",
			EgressUpdatedAtNs:                  500,
			LastLatencyProbeAttemptNs:          700,
			LastAuthorityLatencyProbeAttemptNs: 800,
			LastEgressUpdateAttemptNs:          900,
		},
	}
	if err := repo.BulkUpsertNodesDynamic(nodes); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.LoadAllNodesDynamic()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].FailureCount != 3 {
		t.Fatalf("unexpected: %+v", loaded)
	}
	if loaded[0].EgressRegion != "us" {
		t.Fatalf("egress_region: got %q, want %q", loaded[0].EgressRegion, "us")
	}
	if loaded[0].LastLatencyProbeAttemptNs != 700 ||
		loaded[0].LastAuthorityLatencyProbeAttemptNs != 800 ||
		loaded[0].LastEgressUpdateAttemptNs != 900 {
		t.Fatalf("unexpected probe attempt fields: %+v", loaded[0])
	}

	// Update.
	nodes[0].FailureCount = 0
	repo.BulkUpsertNodesDynamic(nodes)
	loaded, _ = repo.LoadAllNodesDynamic()
	if loaded[0].FailureCount != 0 {
		t.Fatalf("expected 0 failures after reset, got %d", loaded[0].FailureCount)
	}
}

func TestCacheRepo_NodesDynamic_BulkDelete(t *testing.T) {
	repo := newTestCacheRepo(t)

	repo.BulkUpsertNodesDynamic([]model.NodeDynamic{{Hash: "aaa"}, {Hash: "bbb"}})
	repo.BulkDeleteNodesDynamic([]string{"bbb"})

	loaded, _ := repo.LoadAllNodesDynamic()
	if len(loaded) != 1 || loaded[0].Hash != "aaa" {
		t.Fatalf("expected only aaa, got %+v", loaded)
	}
}

// --- node_latency ---

func TestCacheRepo_NodeLatency_BulkUpsertAndLoad(t *testing.T) {
	repo := newTestCacheRepo(t)

	entries := []model.NodeLatency{
		{NodeHash: "aaa", Domain: "google.com", EwmaNs: 5000, LastUpdatedNs: 100},
		{NodeHash: "aaa", Domain: "github.com", EwmaNs: 8000, LastUpdatedNs: 200},
	}
	if err := repo.BulkUpsertNodeLatency(entries); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.LoadAllNodeLatency()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2, got %d", len(loaded))
	}
}

func TestCacheRepo_NodeLatency_BulkDelete(t *testing.T) {
	repo := newTestCacheRepo(t)

	repo.BulkUpsertNodeLatency([]model.NodeLatency{
		{NodeHash: "aaa", Domain: "google.com", EwmaNs: 5000, LastUpdatedNs: 100},
		{NodeHash: "aaa", Domain: "github.com", EwmaNs: 8000, LastUpdatedNs: 200},
	})

	repo.BulkDeleteNodeLatency([]model.NodeLatencyKey{{NodeHash: "aaa", Domain: "google.com"}})
	loaded, _ := repo.LoadAllNodeLatency()
	if len(loaded) != 1 || loaded[0].Domain != "github.com" {
		t.Fatalf("expected only github.com, got %+v", loaded)
	}
}

// --- node_quality ---

func TestCacheRepo_NodeQuality_BulkUpsertAndLoad(t *testing.T) {
	repo := newTestCacheRepo(t)

	entries := []model.NodeQuality{
		{NodeHash: "aaa", Profile: "generic", Grade: "A", Score: 95.5, Unstable: false, ServiceReachable: true, APIReachable: true, CloudflareChallenged: false, AvgLatencyMs: 120.0, LastCheckedNs: 1000, LastError: ""},
		{NodeHash: "aaa", Profile: "openai", Grade: "B", Score: 80.0, Unstable: true, ServiceReachable: true, APIReachable: false, CloudflareChallenged: true, CloudflareChallengeType: "turnstile", AvgLatencyMs: 250.5, LastCheckedNs: 2000, LastError: "rate limited"},
	}
	if err := repo.BulkUpsertNodeQuality(entries); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.LoadAllNodeQuality()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2, got %d", len(loaded))
	}

	// Verify bool field round-trip.
	for _, e := range loaded {
		if e.NodeHash == "aaa" && e.Profile == "generic" {
			if e.Grade != "A" || e.Score != 95.5 || e.Unstable || !e.ServiceReachable || !e.APIReachable || e.CloudflareChallenged {
				t.Fatalf("unexpected quality values for generic: %+v", e)
			}
			if e.AvgLatencyMs != 120.0 {
				t.Fatalf("avg_latency_ms: got %f, want 120.0", e.AvgLatencyMs)
			}
		}
		if e.NodeHash == "aaa" && e.Profile == "openai" {
			if !e.Unstable || e.CloudflareChallengeType != "turnstile" || e.LastError != "rate limited" {
				t.Fatalf("unexpected quality values for openai: %+v", e)
			}
		}
	}

	// Idempotent upsert: update existing.
	entries[0].Score = 97.0
	if err := repo.BulkUpsertNodeQuality(entries[:1]); err != nil {
		t.Fatal(err)
	}
	loaded, _ = repo.LoadAllNodeQuality()
	for _, e := range loaded {
		if e.NodeHash == "aaa" && e.Profile == "generic" && e.Score != 97.0 {
			t.Fatalf("expected updated score 97.0, got %f", e.Score)
		}
	}
}

func TestCacheRepo_NodeQuality_BulkDelete(t *testing.T) {
	repo := newTestCacheRepo(t)

	repo.BulkUpsertNodeQuality([]model.NodeQuality{
		{NodeHash: "aaa", Profile: "generic", Grade: "A", LastCheckedNs: 100},
		{NodeHash: "aaa", Profile: "openai", Grade: "B", LastCheckedNs: 200},
	})

	repo.BulkDeleteNodeQuality([]model.NodeQualityKey{{NodeHash: "aaa", Profile: "generic"}})
	loaded, _ := repo.LoadAllNodeQuality()
	if len(loaded) != 1 || loaded[0].Profile != "openai" {
		t.Fatalf("expected only openai, got %+v", loaded)
	}
}

// --- leases ---

func TestCacheRepo_Leases_BulkUpsertAndLoad(t *testing.T) {
	repo := newTestCacheRepo(t)

	leases := []model.Lease{
		{PlatformID: "p1", Account: "user1", NodeHash: "n1", EgressIP: "1.2.3.4", CreatedAtNs: 50, ExpiryNs: 9999, LastAccessedNs: 100},
	}
	if err := repo.BulkUpsertLeases(leases); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.LoadAllLeases()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Account != "user1" {
		t.Fatalf("unexpected: %+v", loaded)
	}
	if loaded[0].CreatedAtNs != 50 {
		t.Fatalf("created_at_ns: got %d, want %d", loaded[0].CreatedAtNs, 50)
	}
}

func TestCacheRepo_Leases_BulkDelete(t *testing.T) {
	repo := newTestCacheRepo(t)

	repo.BulkUpsertLeases([]model.Lease{
		{PlatformID: "p1", Account: "user1", NodeHash: "n1", CreatedAtNs: 10, ExpiryNs: 9999, LastAccessedNs: 100},
		{PlatformID: "p1", Account: "user2", NodeHash: "n2", CreatedAtNs: 20, ExpiryNs: 9999, LastAccessedNs: 100},
	})
	repo.BulkDeleteLeases([]model.LeaseKey{{PlatformID: "p1", Account: "user1"}})

	loaded, _ := repo.LoadAllLeases()
	if len(loaded) != 1 || loaded[0].Account != "user2" {
		t.Fatalf("expected only user2, got %+v", loaded)
	}
}

// --- subscription_nodes ---

func TestCacheRepo_SubscriptionNodes_BulkUpsertAndLoad(t *testing.T) {
	repo := newTestCacheRepo(t)

	sns := []model.SubscriptionNode{
		{SubscriptionID: "s1", NodeHash: "n1", Tags: []string{"tag1", "tag2"}, Evicted: true},
		{SubscriptionID: "s1", NodeHash: "n2", Tags: []string{"tag3"}, Evicted: false},
	}
	if err := repo.BulkUpsertSubscriptionNodes(sns); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.LoadAllSubscriptionNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2, got %d", len(loaded))
	}

	// Idempotent upsert: update tags.
	sns[0].Tags = []string{"tag1-updated"}
	sns[0].Evicted = false
	repo.BulkUpsertSubscriptionNodes(sns[:1])
	loaded, _ = repo.LoadAllSubscriptionNodes()
	for _, sn := range loaded {
		if sn.NodeHash == "n1" {
			if !reflect.DeepEqual(sn.Tags, []string{"tag1-updated"}) {
				t.Fatalf("expected updated tags, got %+v", sn.Tags)
			}
			if sn.Evicted {
				t.Fatal("expected evicted=false after idempotent upsert update")
			}
		}
	}
}

func TestCacheRepo_SubscriptionNodes_BulkDelete(t *testing.T) {
	repo := newTestCacheRepo(t)

	repo.BulkUpsertSubscriptionNodes([]model.SubscriptionNode{
		{SubscriptionID: "s1", NodeHash: "n1", Tags: []string{}},
		{SubscriptionID: "s1", NodeHash: "n2", Tags: []string{}},
	})
	repo.BulkDeleteSubscriptionNodes([]model.SubscriptionNodeKey{{SubscriptionID: "s1", NodeHash: "n1"}})

	loaded, _ := repo.LoadAllSubscriptionNodes()
	if len(loaded) != 1 || loaded[0].NodeHash != "n2" {
		t.Fatalf("expected only n2, got %+v", loaded)
	}
}

// --- empty bulk operations ---

func TestCacheRepo_BulkEmpty(t *testing.T) {
	repo := newTestCacheRepo(t)

	// All empty bulk operations should be no-ops.
	if err := repo.BulkUpsertNodesStatic(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkDeleteNodesStatic(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkUpsertNodesDynamic(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkDeleteNodesDynamic(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkUpsertNodeLatency(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkDeleteNodeLatency(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkUpsertLeases(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkDeleteLeases(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkUpsertSubscriptionNodes(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkDeleteSubscriptionNodes(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkUpsertNodeQuality(nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.BulkDeleteNodeQuality(nil); err != nil {
		t.Fatal(err)
	}
}

// TestCacheRepo_FlushTx_RollbackOnFailure verifies that if any step inside
// FlushTx fails, the entire transaction is rolled back and no partial writes
// are committed.
func TestCacheRepo_FlushTx_RollbackOnFailure(t *testing.T) {
	repo := newTestCacheRepo(t)

	// Seed: insert a node_static that should survive the failed FlushTx.
	seed := []model.NodeStatic{
		{Hash: "pre-existing", RawOptions: json.RawMessage(`{"seed":true}`), CreatedAtNs: 1},
	}
	if err := repo.BulkUpsertNodesStatic(seed); err != nil {
		t.Fatal(err)
	}

	// Drop node_latency table so that the upsert_node_latency step in FlushTx
	// will fail. nodes_static upsert runs first and would succeed in isolation.
	if _, err := repo.db.Exec("DROP TABLE node_latency"); err != nil {
		t.Fatal(err)
	}

	// Build a FlushOps that has work for both nodes_static and node_latency.
	ops := FlushOps{
		UpsertNodesStatic: []model.NodeStatic{
			{Hash: "new-node", RawOptions: json.RawMessage(`{"new":true}`), CreatedAtNs: 2},
		},
		UpsertNodeLatency: []model.NodeLatency{
			{NodeHash: "aaa", Domain: "example.com", EwmaNs: 100, LastUpdatedNs: 200},
		},
	}

	err := repo.FlushTx(ops)
	if err == nil {
		t.Fatal("expected FlushTx to fail because node_latency table was dropped")
	}

	// Verify rollback: "new-node" should NOT be committed.
	loaded, err := repo.LoadAllNodesStatic()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 pre-existing node (rollback should prevent new-node), got %d: %+v", len(loaded), loaded)
	}
	if loaded[0].Hash != "pre-existing" {
		t.Fatalf("expected pre-existing node, got %s", loaded[0].Hash)
	}
}

// --- node_quality round-trip with Phase 3B2 metadata ---

// TestCacheRepo_NodeQuality_MetadataFieldsRoundTrip verifies that the three
// additive Phase 3B2 fields (cloudflare_status, scoring_policy_version,
// score_breakdown) are persisted and loaded correctly, including empty legacy
// defaults.
func TestCacheRepo_NodeQuality_MetadataFieldsRoundTrip(t *testing.T) {
	repo := newTestCacheRepo(t)

	entries := []model.NodeQuality{
		{
			NodeHash:             "n1",
			Profile:              "generic",
			Grade:                "A",
			Score:                95,
			CloudflareStatus:     "clean",
			ScoringPolicyVersion: 1,
			ScoreBreakdown:       `{"version":1,"effective_weights":{"service":100},"sub_scores":{"service":{"value":100}},"final_grade":"A","grade_from_score":"A"}`,
		},
		{
			NodeHash:             "n2",
			Profile:              "generic",
			Grade:                "B",
			Score:                75,
			CloudflareStatus:     "js_challenge",
			ScoringPolicyVersion: 1,
			ScoreBreakdown:       `{"version":1,"sub_scores":{"service":{"value":75}},"final_grade":"B","grade_from_score":"B"}`,
		},
		// Legacy row: all new fields at zero/empty default.
		{
			NodeHash:  "n3",
			Profile:   "generic",
			Grade:     "F",
			Score:     0,
			LastError: "timed out",
		},
	}
	if err := repo.BulkUpsertNodeQuality(entries); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.LoadAllNodeQuality()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(loaded))
	}

	for _, e := range loaded {
		switch e.NodeHash {
		case "n1":
			if e.CloudflareStatus != "clean" {
				t.Fatalf("n1 CloudflareStatus = %q, want clean", e.CloudflareStatus)
			}
			if e.ScoringPolicyVersion != 1 {
				t.Fatalf("n1 ScoringPolicyVersion = %d, want 1", e.ScoringPolicyVersion)
			}
			if e.ScoreBreakdown == "" {
				t.Fatal("n1 ScoreBreakdown should not be empty")
			}
		case "n2":
			if e.CloudflareStatus != "js_challenge" {
				t.Fatalf("n2 CloudflareStatus = %q, want js_challenge", e.CloudflareStatus)
			}
			if e.ScoringPolicyVersion != 1 {
				t.Fatalf("n2 ScoringPolicyVersion = %d, want 1", e.ScoringPolicyVersion)
			}
		case "n3":
			if e.CloudflareStatus != "" {
				t.Fatalf("n3 CloudflareStatus = %q, want empty legacy", e.CloudflareStatus)
			}
			if e.ScoringPolicyVersion != 0 {
				t.Fatalf("n3 ScoringPolicyVersion = %d, want 0", e.ScoringPolicyVersion)
			}
			if e.ScoreBreakdown != "" {
				t.Fatalf("n3 ScoreBreakdown = %q, want empty legacy", e.ScoreBreakdown)
			}
		}
	}
}

// TestCacheRepo_FlushTx_NodeQualityMetadata verifies that FlushTx bulk upsert
// correctly round-trips the three Phase 3B2 metadata fields.
func TestCacheRepo_FlushTx_NodeQualityMetadata(t *testing.T) {
	repo := newTestCacheRepo(t)

	nq := model.NodeQuality{
		NodeHash:             "hash-flush",
		Profile:              "generic",
		Grade:                "A",
		Score:                100,
		CloudflareStatus:     "clean",
		ScoringPolicyVersion: 1,
		ScoreBreakdown:       `{"version":1,"grade_from_score":"A","final_grade":"A"}`,
	}

	if err := repo.FlushTx(FlushOps{
		UpsertNodeQuality: []model.NodeQuality{nq},
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.LoadAllNodeQuality()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 quality entry, got %d", len(loaded))
	}
	e := loaded[0]
	if e.CloudflareStatus != "clean" {
		t.Fatalf("CloudflareStatus = %q, want clean", e.CloudflareStatus)
	}
	if e.ScoringPolicyVersion != 1 {
		t.Fatalf("ScoringPolicyVersion = %d, want 1", e.ScoringPolicyVersion)
	}
	if e.ScoreBreakdown != nq.ScoreBreakdown {
		t.Fatalf("ScoreBreakdown = %q, want %q", e.ScoreBreakdown, nq.ScoreBreakdown)
	}
}

func TestCacheMigration000004DownDropsScoringMetadata(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/cache.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := MigrateCacheDB(db); err != nil {
		t.Fatal(err)
	}

	downSQL, err := migrationsFS.ReadFile("migrations/cache/000004_node_quality_scoring_metadata.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(downSQL)); err != nil {
		t.Fatalf("apply cache migration 000004 down: %v", err)
	}

	rows, err := db.Query(`PRAGMA table_info(node_quality)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	removed := map[string]bool{
		"cloudflare_status":      false,
		"scoring_policy_version": false,
		"score_breakdown":        false,
	}
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if _, tracked := removed[name]; tracked {
			removed[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for column, stillPresent := range removed {
		if stillPresent {
			t.Fatalf("column %s still present after cache migration 000004 down", column)
		}
	}
}
