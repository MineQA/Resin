package service

import (
	"errors"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/platform"
	"github.com/Resinat/Resin/internal/state"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
	"github.com/Resinat/Resin/internal/topology"
)

type previewFilterFixture struct {
	cp          *ControlPlaneService
	hkHash      string
	usHash      string
	unknownHash string
}

func buildPreviewFilterFixture(t *testing.T) previewFilterFixture {
	t.Helper()

	subMgr := topology.NewSubscriptionManager()
	sub := subscription.NewSubscription("sub-1", "sub-1", "https://example.com/sub", true, false)
	subMgr.Register(sub)

	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	hkRaw := []byte(`{"type":"ss","server":"1.1.1.1","port":443}`)
	hkHash := node.HashFromRawOptions(hkRaw)
	pool.AddNodeFromSub(hkHash, hkRaw, sub.ID)
	sub.ManagedNodes().StoreNode(hkHash, subscription.ManagedNode{Tags: []string{"all", "hk"}})

	usRaw := []byte(`{"type":"ss","server":"2.2.2.2","port":443}`)
	usHash := node.HashFromRawOptions(usRaw)
	pool.AddNodeFromSub(usHash, usRaw, sub.ID)
	sub.ManagedNodes().StoreNode(usHash, subscription.ManagedNode{Tags: []string{"all", "us"}})

	unknownRaw := []byte(`{"type":"ss","server":"3.3.3.3","port":443}`)
	unknownHash := node.HashFromRawOptions(unknownRaw)
	pool.AddNodeFromSub(unknownHash, unknownRaw, sub.ID)
	sub.ManagedNodes().StoreNode(unknownHash, subscription.ManagedNode{Tags: []string{"all", "unknown"}})

	hkEntry, ok := pool.GetEntry(hkHash)
	if !ok {
		t.Fatal("hk entry missing")
	}
	hkOutbound := testutil.NewNoopOutbound()
	hkEntry.Outbound.Store(&hkOutbound)
	hkEntry.SetEgressIP(netip.MustParseAddr("1.1.1.1"))
	hkEntry.SetEgressRegion("hk")

	usEntry, ok := pool.GetEntry(usHash)
	if !ok {
		t.Fatal("us entry missing")
	}
	usOutbound := testutil.NewNoopOutbound()
	usEntry.Outbound.Store(&usOutbound)
	usEntry.SetEgressIP(netip.MustParseAddr("2.2.2.2"))
	usEntry.SetEgressRegion("us")

	unknownEntry, ok := pool.GetEntry(unknownHash)
	if !ok {
		t.Fatal("unknown entry missing")
	}
	unknownOutbound := testutil.NewNoopOutbound()
	unknownEntry.Outbound.Store(&unknownOutbound)
	unknownEntry.SetEgressIP(netip.MustParseAddr("3.3.3.3"))

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
	}
	return previewFilterFixture{
		cp:          cp,
		hkHash:      hkHash.Hex(),
		usHash:      usHash.Hex(),
		unknownHash: unknownHash.Hex(),
	}
}

func TestPreviewFilter_RegionNegation(t *testing.T) {
	fixture := buildPreviewFilterFixture(t)

	nodes, err := fixture.cp.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			RegexFilters:  []string{".*"},
			RegionFilters: []string{"!hk"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1", len(nodes))
	}
	if nodes[0].NodeHash != fixture.usHash {
		t.Fatalf("matched node = %s, want %s", nodes[0].NodeHash, fixture.usHash)
	}
	if nodes[0].NodeHash == fixture.hkHash {
		t.Fatalf("hk node %s should have been excluded", fixture.hkHash)
	}
}

func TestPreviewFilter_RegionMixedIncludeExclude(t *testing.T) {
	fixture := buildPreviewFilterFixture(t)

	nodes, err := fixture.cp.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			RegexFilters:  []string{".*"},
			RegionFilters: []string{"hk", "!us"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1", len(nodes))
	}
	if nodes[0].NodeHash != fixture.hkHash {
		t.Fatalf("matched node = %s, want %s", nodes[0].NodeHash, fixture.hkHash)
	}

	nodes, err = fixture.cp.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			RegexFilters:  []string{".*"},
			RegionFilters: []string{"hk", "!hk"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("nodes len = %d, want 0", len(nodes))
	}
}

func TestPreviewFilter_RegionNegation_UnknownRegionExcluded(t *testing.T) {
	fixture := buildPreviewFilterFixture(t)

	nodes, err := fixture.cp.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			RegexFilters:  []string{".*"},
			RegionFilters: []string{"!hk"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}

	for _, node := range nodes {
		if node.NodeHash == fixture.unknownHash {
			t.Fatalf("node with unknown region %s should not match region filters", fixture.unknownHash)
		}
	}
}

// --- PreviewFilter protocol filter tests ---

func newPreviewFilterTestService(t *testing.T) (*ControlPlaneService, node.Hash, node.Hash) {
	t.Helper()

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	subA := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(subA)

	// Node 1: shadowsocks
	ssRaw := []byte(`{"type":"ss","server":"1.1.1.1","port":443}`)
	ssHash := addRoutableNodeForSubscriptionWithTag(t, pool, subA, ssRaw, "203.0.113.10", "ss-node")

	// Node 2: vmess
	vmessRaw := []byte(`{"type":"vmess","server":"2.2.2.2","port":443}`)
	vmessHash := addRoutableNodeForSubscriptionWithTag(t, pool, subA, vmessRaw, "203.0.113.11", "vmess-node")

	runtimeCfg := &atomic.Pointer[config.RuntimeConfig]{}
	runtimeCfg.Store(config.NewDefaultRuntimeConfig())

	svc := &ControlPlaneService{
		Pool:       pool,
		SubMgr:     subMgr,
		RuntimeCfg: runtimeCfg,
	}
	return svc, ssHash, vmessHash
}

func TestPreviewFilter_ProtocolFilterInclude(t *testing.T) {
	svc, ssHash, _ := newPreviewFilterTestService(t)

	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			ProtocolFilters: []string{"shadowsocks"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].NodeHash != ssHash.Hex() {
		t.Fatalf("expected node %s, got %s", ssHash.Hex(), result[0].NodeHash)
	}
}

func TestPreviewFilter_ProtocolFilterIncludeMatchAlias(t *testing.T) {
	svc, ssHash, _ := newPreviewFilterTestService(t)

	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			ProtocolFilters: []string{"ss"}, // alias — should normalise to shadowsocks
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].NodeHash != ssHash.Hex() {
		t.Fatalf("expected node %s, got %s", ssHash.Hex(), result[0].NodeHash)
	}
}

func TestPreviewFilter_ProtocolFilterExclude(t *testing.T) {
	svc, ssHash, _ := newPreviewFilterTestService(t)

	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			ExcludeProtocolFilters: []string{"shadowsocks"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	// Only vmess node should remain
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].NodeHash == ssHash.Hex() {
		t.Fatal("shadowsocks node should be excluded")
	}
}

func TestPreviewFilter_ProtocolFilterIncludeAndExcludeExclusionWins(t *testing.T) {
	svc, _, _ := newPreviewFilterTestService(t)

	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			ProtocolFilters:        []string{"shadowsocks", "vmess"},
			ExcludeProtocolFilters: []string{"shadowsocks"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	// Should only have the vmess node (shadowsocks is excluded despite being in include)
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].Protocol != "vmess" {
		t.Fatalf("expected vmess node, got protocol %q", result[0].Protocol)
	}
}

func TestPreviewFilter_ProtocolFilterUnknownProtocolIncludedRejected(t *testing.T) {
	svc, _, _ := newPreviewFilterTestService(t)

	_, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			ProtocolFilters: []string{"tuic"},
		},
	})
	if err == nil {
		t.Fatal("expected error for unsupported protocol in filter")
	}
}

func TestPreviewFilter_ProtocolFilterUnknownProtocolExcludedRejected(t *testing.T) {
	svc, _, _ := newPreviewFilterTestService(t)

	_, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			ExcludeProtocolFilters: []string{"wireguard"},
		},
	})
	if err == nil {
		t.Fatal("expected error for unsupported protocol in exclude filter")
	}
}

func TestPreviewFilter_ProtocolFilterUnknownTypeNodeWithIncludeExcluded(t *testing.T) {
	svc, _, _ := newPreviewFilterTestService(t)

	// Add a node with unknown protocol type
	raw := []byte(`{"type":"tuic","server":"3.3.3.3"}`)
	hash := node.HashFromRawOptions(raw)

	// Mock GetEntry by directly putting the node in the pool
	entry := node.NewNodeEntry(hash, raw, time.Now(), 16)
	entry.AddSubscriptionID("sub-a")
	entry.SetEgressIP(netip.MustParseAddr("203.0.113.12"))
	entry.LatencyTable.Update("example.com", 25*time.Millisecond, 10*time.Minute)
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)
	svc.Pool.NotifyNodeDirty(hash)

	// Harder: need to get the entry into the pool properly
	// Let's just check that the PreviewFilter doesn't crash with such entries
	// The unknown protocol node will be excluded by the include filter at evaluation time
	svc.Pool.AddNodeFromSub(hash, raw, "sub-a")
	if entry, ok := svc.Pool.GetEntry(hash); ok {
		entry.SetEgressIP(netip.MustParseAddr("203.0.113.12"))
		entry.LatencyTable.Update("example.com", 25*time.Millisecond, 10*time.Minute)
		entry2 := testutil.NewNoopOutbound()
		entry.Outbound.Store(&entry2)
		svc.Pool.NotifyNodeDirty(hash)
	}

	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			ProtocolFilters: []string{"shadowsocks"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	// Unknown protocol node should be excluded; only shadowsocks from fixture remains
	for _, r := range result {
		if r.Protocol == "" {
			t.Fatal("unknown-protocol node should not appear in result")
		}
	}
}

func TestPreviewFilter_ProtocolExcludeWithPlatformID(t *testing.T) {
	svc, _, _ := newPreviewFilterTestService(t)
	engine, closer, err := state.PersistenceBootstrap(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	// Create a platform that excludes shadowsocks
	platID := "plat-preview-exclude"
	plat := platform.NewConfiguredPlatform(
		platID, "ExcludePreview",
		nil, nil, // regex, region
		nil,               // protocolFilters (include all)
		[]string{"vmess"}, // excludeProtocolFilters
		int64(30*time.Minute),
		"TREAT_AS_EMPTY", "RANDOM", "", "BALANCED", false,
	)
	if err := engine.UpsertPlatform(model.Platform{
		ID:                     platID,
		Name:                   "ExcludePreview",
		StickyTTLNs:            int64(30 * time.Minute),
		RegexFilters:           []string{},
		RegionFilters:          []string{},
		ProtocolFilters:        []string{},
		ExcludeProtocolFilters: []string{"vmess"},
		ReverseProxyMissAction: "TREAT_AS_EMPTY",
		AllocationPolicy:       "BALANCED",
		UpdatedAtNs:            time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}
	svc.Pool.RegisterPlatform(plat)

	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformID: &platID,
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	// Only shadowsocks node should remain (vmess excluded)
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].Protocol != "shadowsocks" {
		t.Fatalf("expected shadowsocks, got %q", result[0].Protocol)
	}
}

// TestPreviewFilter_QualityGradeFilter verifies quality grade filter in preview.
func TestPreviewFilter_QualityGradeFilter(t *testing.T) {
	svc, ssHash, _ := newPreviewFilterTestService(t)

	// Set quality grade A on the ss node.
	entry, ok := svc.Pool.GetEntry(ssHash)
	if !ok {
		t.Fatal("ss entry not found")
	}
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 95, ServiceReachable: true, Profile: "generic",
	})

	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityGrade: "A",
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node matching quality grade A, got %d", len(result))
	}
	if result[0].NodeHash != ssHash.Hex() {
		t.Fatalf("expected node %s, got %s", ssHash.Hex(), result[0].NodeHash)
	}

	// Filter for grade B should return 0.
	result, err = svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityGrade: "B",
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 nodes for grade B, got %d", len(result))
	}
}

// TestPreviewFilter_QualityMinScoreFilter verifies quality min score filter in preview.
func TestPreviewFilter_QualityMinScoreFilter(t *testing.T) {
	svc, ssHash, _ := newPreviewFilterTestService(t)

	entry, ok := svc.Pool.GetEntry(ssHash)
	if !ok {
		t.Fatal("ss entry not found")
	}
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true, Profile: "generic",
	})

	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityMinScore: 85,
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node for min score 85, got %d", len(result))
	}

	result, err = svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityMinScore: 95,
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 nodes for min score 95, got %d", len(result))
	}
}

// TestPreviewFilter_QualityProfileFilter verifies quality profile filter in preview.
func TestPreviewFilter_QualityProfileFilter(t *testing.T) {
	svc, ssHash, _ := newPreviewFilterTestService(t)

	entry, ok := svc.Pool.GetEntry(ssHash)
	if !ok {
		t.Fatal("ss entry not found")
	}
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 95, ServiceReachable: true, Profile: "generic",
	})

	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityProfile: "generic",
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node for generic profile, got %d", len(result))
	}

	result, err = svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityProfile: "openai",
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 nodes for openai profile, got %d", len(result))
	}
}

// --- PreviewFilter quality_cloudflare_statuses tests ---

func TestPreviewFilter_QualityCloudflareStatusOR(t *testing.T) {
	svc, ssHash, vmessHash := newPreviewFilterTestService(t)

	ssEntry, ok := svc.Pool.GetEntry(ssHash)
	if !ok {
		t.Fatal("ss entry not found")
	}
	ssEntry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 95, ServiceReachable: true, Profile: "generic",
		CloudflareStatus: "clean",
	})

	vmessEntry, ok := svc.Pool.GetEntry(vmessHash)
	if !ok {
		t.Fatal("vmess entry not found")
	}
	vmessEntry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 95, ServiceReachable: true, Profile: "generic",
		CloudflareStatus: "block",
	})

	// Single status: only "clean" node matches.
	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityCloudflareStatuses: []string{"clean"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node for status clean, got %d", len(result))
	}
	if result[0].NodeHash != ssHash.Hex() {
		t.Fatalf("expected node %s, got %s", ssHash.Hex(), result[0].NodeHash)
	}

	// OR filter ["clean", "block"]: both nodes match.
	result, err = svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityCloudflareStatuses: []string{"clean", "block"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter (OR): %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 nodes for OR filter, got %d", len(result))
	}

	// Status that no node has: zero matches.
	result, err = svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityCloudflareStatuses: []string{"js_challenge"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter (no match): %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 nodes for non-matching status, got %d", len(result))
	}
}

func TestPreviewFilter_QualityCloudflareStatusUncheckedMatchesLegacyEmpty(t *testing.T) {
	svc, ssHash, _ := newPreviewFilterTestService(t)

	ssEntry, ok := svc.Pool.GetEntry(ssHash)
	if !ok {
		t.Fatal("ss entry not found")
	}
	ssEntry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 95, ServiceReachable: true, Profile: "generic",
		CloudflareStatus: "", // legacy empty
	})

	// "unchecked" matches empty/legacy status.
	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityCloudflareStatuses: []string{"unchecked"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter (unchecked): %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node for unchecked filter, got %d", len(result))
	}
	if result[0].NodeHash != ssHash.Hex() {
		t.Fatalf("expected node %s, got %s", ssHash.Hex(), result[0].NodeHash)
	}

	// "clean" does NOT match empty/legacy status.
	result, err = svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityCloudflareStatuses: []string{"clean"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewFilter (clean): %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 nodes for clean filter with legacy empty, got %d", len(result))
	}
}

func TestPreviewFilter_QualityCloudflareStatusRejectsUnknown(t *testing.T) {
	svc, _, _ := newPreviewFilterTestService(t)

	_, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformSpec: &PlatformSpecFilter{
			QualityCloudflareStatuses: []string{"bogus"},
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown cloudflare status in platform_spec")
	}
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Fatalf("expected INVALID_ARGUMENT, got %s", svcErr.Code)
	}
	if !strings.Contains(svcErr.Message, "unknown status") {
		t.Fatalf("error message = %q, expected unknown status", svcErr.Message)
	}
}

func TestPreviewFilter_QualityCloudflareStatusWithPlatformID(t *testing.T) {
	svc, ssHash, vmessHash := newPreviewFilterTestService(t)

	ssEntry, ok := svc.Pool.GetEntry(ssHash)
	if !ok {
		t.Fatal("ss entry not found")
	}
	ssEntry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 95, ServiceReachable: true, Profile: "generic",
		CloudflareStatus: "clean",
	})

	vmessEntry, ok := svc.Pool.GetEntry(vmessHash)
	if !ok {
		t.Fatal("vmess entry not found")
	}
	vmessEntry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 95, ServiceReachable: true, Profile: "generic",
		CloudflareStatus: "block",
	})

	// Create a platform with quality_cloudflare_statuses = ["clean"].
	engine, closer, err := state.PersistenceBootstrap(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	platID := "plat-cf-filter"
	plat := platform.NewConfiguredPlatformWithQuality(
		platID, "CFStatusPlatform",
		nil, nil,
		nil, nil,
		int64(30*time.Minute),
		"TREAT_AS_EMPTY", "RANDOM", "", "BALANCED", false,
		"", 0, nil,
		[]string{"clean"}, 0, "",
	)
	if err := engine.UpsertPlatform(model.Platform{
		ID:                        platID,
		Name:                      "CFStatusPlatform",
		StickyTTLNs:               int64(30 * time.Minute),
		ReverseProxyMissAction:    "TREAT_AS_EMPTY",
		AllocationPolicy:          "BALANCED",
		QualityCloudflareStatuses: []string{"clean"},
		UpdatedAtNs:               time.Now().UnixNano(),
	}); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}
	svc.Pool.RegisterPlatform(plat)
	svc.Pool.RebuildPlatform(plat)

	// PreviewFilter with platform_id uses the registered platform's status list.
	result, err := svc.PreviewFilter(PreviewFilterRequest{
		PlatformID: &platID,
	})
	if err != nil {
		t.Fatalf("PreviewFilter: %v", err)
	}
	// Only the "clean" status node should match.
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].NodeHash != ssHash.Hex() {
		t.Fatalf("expected node %s, got %s", ssHash.Hex(), result[0].NodeHash)
	}
}
