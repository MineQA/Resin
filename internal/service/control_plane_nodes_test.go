package service

import (
	"encoding/json"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/geoip"
	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/platform"
	"github.com/Resinat/Resin/internal/probe"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
	"github.com/Resinat/Resin/internal/topology"
)

func newNodeListTestPool(subMgr *topology.SubscriptionManager) *topology.GlobalNodePool {
	return topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})
}

func addRoutableNodeForSubscription(
	t *testing.T,
	pool *topology.GlobalNodePool,
	sub *subscription.Subscription,
	raw []byte,
	egressIP string,
) node.Hash {
	return addRoutableNodeForSubscriptionWithTag(t, pool, sub, raw, egressIP, "tag")
}

func addRoutableNodeForSubscriptionWithTag(
	t *testing.T,
	pool *topology.GlobalNodePool,
	sub *subscription.Subscription,
	raw []byte,
	egressIP string,
	tag string,
) node.Hash {
	t.Helper()

	hash := node.HashFromRawOptions(raw)
	pool.AddNodeFromSub(hash, raw, sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{tag}})

	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node %s not found after add", hash.Hex())
	}
	entry.SetEgressIP(netip.MustParseAddr(egressIP))
	if entry.LatencyTable == nil {
		t.Fatalf("node %s latency table not initialized", hash.Hex())
	}
	entry.LatencyTable.Update("example.com", 25*time.Millisecond, 10*time.Minute)
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)
	pool.RecordResult(hash, true)
	pool.NotifyNodeDirty(hash)
	return hash
}

func TestListNodes_PlatformAndSubscriptionFiltersReturnIntersection(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	plat := platform.NewPlatform("plat-1", "plat", nil, nil)
	pool.RegisterPlatform(plat)

	subA := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subB := subscription.NewSubscription("sub-b", "sub-b", "https://example.com/b", true, false)
	subMgr.Register(subA)
	subMgr.Register(subB)

	hashA := addRoutableNodeForSubscription(
		t,
		pool,
		subA,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.10",
	)
	_ = addRoutableNodeForSubscription(
		t,
		pool,
		subB,
		[]byte(`{"type":"ss","server":"2.2.2.2","port":443}`),
		"203.0.113.11",
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		GeoIP:  &geoip.Service{},
	}
	filters := NodeFilters{
		PlatformID:     &plat.ID,
		SubscriptionID: &subA.ID,
	}

	nodes, err := cp.ListNodes(filters)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("intersection size = %d, want 1", len(nodes))
	}
	if nodes[0].NodeHash != hashA.Hex() {
		t.Fatalf("intersection node hash = %q, want %q", nodes[0].NodeHash, hashA.Hex())
	}
}

func TestListNodes_SubscriptionFilterSkipsStaleManagedNodes(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	staleHash := node.HashFromRawOptions([]byte(`{"type":"ss","server":"9.9.9.9","port":443}`))
	sub.ManagedNodes().StoreNode(staleHash, subscription.ManagedNode{Tags: []string{"stale"}})

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		GeoIP:  &geoip.Service{},
	}
	filters := NodeFilters{
		SubscriptionID: &sub.ID,
	}

	nodes, err := cp.ListNodes(filters)
	if err != nil {
		t.Fatalf("ListNodes with stale hash: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("nodes with stale managed hash = %d, want 0", len(nodes))
	}

	liveHash := addRoutableNodeForSubscription(
		t,
		pool,
		sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.20",
	)

	nodes, err = cp.ListNodes(filters)
	if err != nil {
		t.Fatalf("ListNodes with stale+live hashes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes with stale+live hashes = %d, want 1", len(nodes))
	}
	if nodes[0].NodeHash != liveHash.Hex() {
		t.Fatalf("live node hash = %q, want %q", nodes[0].NodeHash, liveHash.Hex())
	}
}

func TestListNodes_SubscriptionFilterSkipsEvictedManagedNodes(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	subA := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subB := subscription.NewSubscription("sub-b", "sub-b", "https://example.com/b", true, false)
	subMgr.Register(subA)
	subMgr.Register(subB)

	raw := []byte(`{"type":"ss","server":"7.7.7.7","port":443}`)
	hash := addRoutableNodeForSubscriptionWithTag(t, pool, subA, raw, "203.0.113.40", "a-tag")
	pool.AddNodeFromSub(hash, raw, subB.ID)
	subB.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"b-tag"}})

	managedA, ok := subA.ManagedNodes().LoadNode(hash)
	if !ok {
		t.Fatal("subA managed node missing before eviction")
	}
	managedA.Evicted = true
	subA.ManagedNodes().StoreNode(hash, managedA)
	pool.RemoveNodeFromSub(hash, subA.ID)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		GeoIP:  &geoip.Service{},
	}

	filtersA := NodeFilters{SubscriptionID: &subA.ID}
	nodesA, err := cp.ListNodes(filtersA)
	if err != nil {
		t.Fatalf("ListNodes subA: %v", err)
	}
	if len(nodesA) != 0 {
		t.Fatalf("subA filtered nodes = %d, want 0", len(nodesA))
	}

	filtersB := NodeFilters{SubscriptionID: &subB.ID}
	nodesB, err := cp.ListNodes(filtersB)
	if err != nil {
		t.Fatalf("ListNodes subB: %v", err)
	}
	if len(nodesB) != 1 || nodesB[0].NodeHash != hash.Hex() {
		t.Fatalf("subB filtered nodes = %+v, want [%s]", nodesB, hash.Hex())
	}
}

func TestGetNode_TagIncludesSubscriptionNamePrefix(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	hash := addRoutableNodeForSubscription(
		t,
		pool,
		sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.30",
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		GeoIP:  &geoip.Service{},
	}

	got, err := cp.GetNode(hash.Hex())
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if len(got.Tags) != 1 {
		t.Fatalf("tags len = %d, want 1", len(got.Tags))
	}
	if got.Tags[0].Tag != "sub-a/tag" {
		t.Fatalf("tag = %q, want %q", got.Tags[0].Tag, "sub-a/tag")
	}
}

func TestGetNode_ReferenceLatencyMsUsesAuthorityAverage(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	hash := addRoutableNodeForSubscription(
		t,
		pool,
		sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.30",
	)

	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node %s missing", hash.Hex())
	}
	entry.LatencyTable.LoadEntry("cloudflare.com", node.DomainLatencyStats{
		Ewma:        40 * time.Millisecond,
		LastUpdated: time.Now(),
	})
	entry.LatencyTable.LoadEntry("github.com", node.DomainLatencyStats{
		Ewma:        60 * time.Millisecond,
		LastUpdated: time.Now(),
	})
	entry.LatencyTable.LoadEntry("example.com", node.DomainLatencyStats{
		Ewma:        5 * time.Millisecond,
		LastUpdated: time.Now(),
	})

	runtimeCfg := &atomic.Pointer[config.RuntimeConfig]{}
	cfg := config.NewDefaultRuntimeConfig()
	cfg.LatencyAuthorities = []string{"cloudflare.com", "github.com", "google.com"}
	runtimeCfg.Store(cfg)

	cp := &ControlPlaneService{
		Pool:       pool,
		SubMgr:     subMgr,
		GeoIP:      &geoip.Service{},
		RuntimeCfg: runtimeCfg,
	}

	got, err := cp.GetNode(hash.Hex())
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.ReferenceLatencyMs == nil {
		t.Fatal("reference_latency_ms should be present")
	}
	if *got.ReferenceLatencyMs != 50 {
		t.Fatalf("reference_latency_ms = %v, want 50", *got.ReferenceLatencyMs)
	}
}

func TestListNodes_ProbedSinceUsesLastLatencyProbeAttempt(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	hash := addRoutableNodeForSubscription(
		t,
		pool,
		sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.30",
	)

	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node %s missing", hash.Hex())
	}

	latencyAttempt := time.Now().Add(-2 * time.Minute).UnixNano()
	entry.LastLatencyProbeAttempt.Store(latencyAttempt)
	// Keep egress update older to ensure filter is using LastLatencyProbeAttempt.
	entry.LastEgressUpdate.Store(time.Now().Add(-10 * time.Minute).UnixNano())

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		GeoIP:  &geoip.Service{},
	}

	before := time.Unix(0, latencyAttempt).Add(-1 * time.Minute)
	nodes, err := cp.ListNodes(NodeFilters{ProbedSince: &before})
	if err != nil {
		t.Fatalf("ListNodes(before): %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("ListNodes(before) len = %d, want 1", len(nodes))
	}

	after := time.Unix(0, latencyAttempt).Add(1 * time.Minute)
	nodes, err = cp.ListNodes(NodeFilters{ProbedSince: &after})
	if err != nil {
		t.Fatalf("ListNodes(after): %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("ListNodes(after) len = %d, want 0", len(nodes))
	}
}

func TestListNodes_TagKeywordFuzzyMatchIsCaseInsensitive(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	matchHash := addRoutableNodeForSubscriptionWithTag(
		t,
		pool,
		sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.30",
		"hongkong-fast-01",
	)
	_ = addRoutableNodeForSubscriptionWithTag(
		t,
		pool,
		sub,
		[]byte(`{"type":"ss","server":"2.2.2.2","port":443}`),
		"203.0.113.31",
		"japan-slow-01",
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		GeoIP:  &geoip.Service{},
	}

	keyword := "FAST"
	nodes, err := cp.ListNodes(NodeFilters{TagKeyword: &keyword})
	if err != nil {
		t.Fatalf("ListNodes(tag_keyword): %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("ListNodes(tag_keyword) len = %d, want 1", len(nodes))
	}
	if nodes[0].NodeHash != matchHash.Hex() {
		t.Fatalf("ListNodes(tag_keyword) hash = %q, want %q", nodes[0].NodeHash, matchHash.Hex())
	}
}

func TestListNodes_RegionFilterAndSummaryPreferStoredRegion(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	hash := addRoutableNodeForSubscription(
		t,
		pool,
		sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.40",
	)

	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatalf("node %s missing", hash.Hex())
	}
	entry.SetEgressRegion("jp")

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		GeoIP:  &geoip.Service{}, // empty service returns "", forcing stored-region path
	}

	region := "jp"
	nodes, err := cp.ListNodes(NodeFilters{Region: &region})
	if err != nil {
		t.Fatalf("ListNodes(region): %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeHash != hash.Hex() {
		t.Fatalf("region-filtered nodes = %+v, want [%s]", nodes, hash.Hex())
	}

	got, err := cp.GetNode(hash.Hex())
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Region != "jp" {
		t.Fatalf("summary region: got %q, want %q", got.Region, "jp")
	}
}

func TestListNodes_EnabledFilter(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	subEnabled := subscription.NewSubscription("sub-enabled", "sub-enabled", "https://example.com/enabled", true, false)
	subDisabled := subscription.NewSubscription("sub-disabled", "sub-disabled", "https://example.com/disabled", false, false)
	subMgr.Register(subEnabled)
	subMgr.Register(subDisabled)

	enabledHash := addRoutableNodeForSubscription(
		t,
		pool,
		subEnabled,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.70",
	)
	disabledHash := addRoutableNodeForSubscription(
		t,
		pool,
		subDisabled,
		[]byte(`{"type":"ss","server":"2.2.2.2","port":443}`),
		"203.0.113.71",
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		GeoIP:  &geoip.Service{},
	}

	enabled := true
	nodes, err := cp.ListNodes(NodeFilters{Enabled: &enabled})
	if err != nil {
		t.Fatalf("ListNodes(enabled=true): %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeHash != enabledHash.Hex() {
		t.Fatalf("enabled filter result = %+v, want [%s]", nodes, enabledHash.Hex())
	}

	disabled := false
	nodes, err = cp.ListNodes(NodeFilters{Enabled: &disabled})
	if err != nil {
		t.Fatalf("ListNodes(enabled=false): %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeHash != disabledHash.Hex() {
		t.Fatalf("disabled filter result = %+v, want [%s]", nodes, disabledHash.Hex())
	}
}

func TestListNodes_ProtocolIncludeFilter(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	ssHash := addRoutableNodeForSubscription(
		t, pool, sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.10",
	)
	addRoutableNodeForSubscription(
		t, pool, sub,
		[]byte(`{"type":"vmess","server":"2.2.2.2","port":443,"uuid":"a"}`),
		"203.0.113.11",
	)

	cp := &ControlPlaneService{Pool: pool, SubMgr: subMgr, GeoIP: &geoip.Service{}}

	// Include ss only → 1 node.
	nodes, err := cp.ListNodes(NodeFilters{Protocols: []string{"shadowsocks"}})
	if err != nil {
		t.Fatalf("ListNodes(protocols=shadowsocks): %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeHash != ssHash.Hex() {
		t.Fatalf("protocols=shadowsocks: got %d nodes, want 1 (hash=%s)", len(nodes), ssHash.Hex())
	}
}

func TestListNodes_ProtocolExcludeFilter(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	ssHash := addRoutableNodeForSubscription(
		t, pool, sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.10",
	)
	vmessHash := addRoutableNodeForSubscription(
		t, pool, sub,
		[]byte(`{"type":"vmess","server":"2.2.2.2","port":443,"uuid":"a"}`),
		"203.0.113.11",
	)

	cp := &ControlPlaneService{Pool: pool, SubMgr: subMgr, GeoIP: &geoip.Service{}}

	// Exclude ss → 1 node (vmess).
	nodes, err := cp.ListNodes(NodeFilters{ExcludeProtocols: []string{"shadowsocks"}})
	if err != nil {
		t.Fatalf("ListNodes(exclude=shadowsocks): %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeHash != vmessHash.Hex() {
		t.Fatalf("exclude=shadowsocks: got %d nodes, want 1 (hash=%s)", len(nodes), vmessHash.Hex())
	}

	// Exclude both → 0 nodes.
	nodes, err = cp.ListNodes(NodeFilters{ExcludeProtocols: []string{"shadowsocks", "vmess"}})
	if err != nil {
		t.Fatalf("ListNodes(exclude=both): %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("exclude=both: got %d nodes, want 0", len(nodes))
	}

	// ss hash is unused beyond marking; keep compiler happy.
	_ = ssHash
}

func TestListNodes_ProtocolIncludeExcludeExclusionWins(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	addRoutableNodeForSubscription(
		t, pool, sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.10",
	)
	addRoutableNodeForSubscription(
		t, pool, sub,
		[]byte(`{"type":"vmess","server":"2.2.2.2","port":443,"uuid":"a"}`),
		"203.0.113.11",
	)

	cp := &ControlPlaneService{Pool: pool, SubMgr: subMgr, GeoIP: &geoip.Service{}}

	// Include both but exclude ss → exclusion wins → 1 node (vmess).
	nodes, err := cp.ListNodes(NodeFilters{
		Protocols:        []string{"shadowsocks", "vmess"},
		ExcludeProtocols: []string{"shadowsocks"},
	})
	if err != nil {
		t.Fatalf("ListNodes(include+exclude): %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("include+exclude: got %d nodes, want 1", len(nodes))
	}
}

func TestListNodes_ProtocolFilterMissingTypeExcluded(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	// Node with no type field.
	hashNoType := node.HashFromRawOptions([]byte(`{"server":"1.1.1.1","port":443}`))
	pool.AddNodeFromSub(hashNoType, []byte(`{"server":"1.1.1.1","port":443}`), sub.ID)
	sub.ManagedNodes().StoreNode(hashNoType, subscription.ManagedNode{Tags: []string{"no-type"}})

	cp := &ControlPlaneService{Pool: pool, SubMgr: subMgr, GeoIP: &geoip.Service{}}

	// Include filter active → no-type node excluded.
	nodes, err := cp.ListNodes(NodeFilters{Protocols: []string{"shadowsocks"}})
	if err != nil {
		t.Fatalf("ListNodes(protocols=ss) with missing type: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("missing-type with include filter: got %d nodes, want 0", len(nodes))
	}

	// Exclude filter active → no-type node excluded (conservative).
	nodes, err = cp.ListNodes(NodeFilters{ExcludeProtocols: []string{"vmess"}})
	if err != nil {
		t.Fatalf("ListNodes(exclude=vmess) with missing type: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("missing-type with exclude filter: got %d nodes, want 0", len(nodes))
	}
}

func TestProbeEgress_ReturnsRegion(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := newNodeListTestPool(subMgr)

	sub := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subMgr.Register(sub)

	hash := addRoutableNodeForSubscription(
		t,
		pool,
		sub,
		[]byte(`{"type":"ss","server":"1.1.1.1","port":443}`),
		"203.0.113.60",
	)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
		GeoIP:  &geoip.Service{}, // empty service keeps focus on stored region from loc
		ProbeMgr: probe.NewProbeManager(probe.ProbeConfig{
			Pool: pool,
			Fetcher: func(_ node.Hash, _ string) ([]byte, time.Duration, error) {
				return []byte("ip=198.51.100.88\nloc=JP"), 20 * time.Millisecond, nil
			},
		}),
	}

	got, err := cp.ProbeEgress(hash.Hex())
	if err != nil {
		t.Fatalf("ProbeEgress: %v", err)
	}
	if got.EgressIP != "198.51.100.88" {
		t.Fatalf("egress_ip: got %q, want %q", got.EgressIP, "198.51.100.88")
	}
	if got.Region != "jp" {
		t.Fatalf("region: got %q, want %q", got.Region, "jp")
	}
}

// --- NodeEntryMatchesQualityFilters tests ---

func newNodeEntryForQualityFilter(hash node.Hash) *node.NodeEntry {
	entry := node.NewNodeEntry(hash, []byte(`{"type":"ss","server":"1.1.1.1","port":443}`), time.Now(), 16)
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)
	return entry
}

func TestNodeEntryMatchesQualityFilters_NilFilters(t *testing.T) {
	hash := node.HashFromRawOptions([]byte(`{"type":"nil-filter-test"}`))
	entry := newNodeEntryForQualityFilter(hash)

	filters := NodeFilters{}
	if !nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected true when no quality filters are set")
	}
}

func TestNodeEntryMatchesQualityFilters_Profile(t *testing.T) {
	hash := node.HashFromRawOptions([]byte(`{"type":"profile-filter"}`))
	entry := newNodeEntryForQualityFilter(hash)

	// No quality stored — profile filter should reject.
	profile := "openai"
	filters := NodeFilters{QualityProfile: &profile}
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when quality is nil but profile filter is set")
	}

	// Store quality with a different profile.
	entry.SetQuality(&model.NodeQuality{Profile: "generic", Grade: "A", Score: 95})
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when quality profile does not match")
	}

	// Store quality with matching profile.
	entry.SetQuality(&model.NodeQuality{Profile: "openai", Grade: "A", Score: 95})
	if !nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected true when quality profile matches")
	}
}

func TestNodeEntryMatchesQualityFilters_Grade(t *testing.T) {
	hash := node.HashFromRawOptions([]byte(`{"type":"grade-filter"}`))
	entry := newNodeEntryForQualityFilter(hash)

	grade := "A"
	filters := NodeFilters{QualityGrade: &grade}

	// No quality.
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when quality is nil")
	}

	entry.SetQuality(&model.NodeQuality{Grade: "B", Score: 75})
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when grade does not match")
	}

	entry.SetQuality(&model.NodeQuality{Grade: "A", Score: 95})
	if !nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected true when grade matches")
	}
}

func TestNodeEntryMatchesQualityFilters_MinScore(t *testing.T) {
	hash := node.HashFromRawOptions([]byte(`{"type":"minscore-filter"}`))
	entry := newNodeEntryForQualityFilter(hash)

	minScore := 80.0
	filters := NodeFilters{QualityMinScore: &minScore}

	// No quality.
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when quality is nil")
	}

	entry.SetQuality(&model.NodeQuality{Grade: "B", Score: 75})
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when score < min")
	}

	entry.SetQuality(&model.NodeQuality{Grade: "A", Score: 85})
	if !nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected true when score >= min")
	}

	entry.SetQuality(&model.NodeQuality{Grade: "A", Score: 80})
	if !nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected true when score == min")
	}
}

func TestNodeEntryMatchesQualityFilters_CloudflareChallenged(t *testing.T) {
	hash := node.HashFromRawOptions([]byte(`{"type":"cf-filter"}`))
	entry := newNodeEntryForQualityFilter(hash)

	cfChallenged := true
	filters := NodeFilters{QualityCloudflareChallenged: &cfChallenged}

	// No quality.
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when quality is nil")
	}

	entry.SetQuality(&model.NodeQuality{Grade: "A", Score: 95, CloudflareChallenged: false})
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when CloudflareChallenged=false but filter wants true")
	}

	entry.SetQuality(&model.NodeQuality{Grade: "D", Score: 30, CloudflareChallenged: true})
	if !nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected true when CloudflareChallenged matches")
	}
}

func TestNodeEntryMatchesQualityFilters_CheckedSince(t *testing.T) {
	hash := node.HashFromRawOptions([]byte(`{"type":"checkedsince-filter"}`))
	entry := newNodeEntryForQualityFilter(hash)

	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	checkedSince := &oneHourAgo
	filters := NodeFilters{QualityCheckedSince: checkedSince}

	// No quality.
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when quality is nil")
	}

	// LastChecked before the threshold.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 95,
		LastCheckedNs: now.Add(-2 * time.Hour).UnixNano(),
	})
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when LastCheckedNs < filter threshold")
	}

	// LastChecked after the threshold.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 95,
		LastCheckedNs: now.UnixNano(),
	})
	if !nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected true when LastCheckedNs >= filter threshold")
	}
}

func TestNodeEntryMatchesQualityFilters_Multiple(t *testing.T) {
	hash := node.HashFromRawOptions([]byte(`{"type":"multi-filter"}`))
	entry := newNodeEntryForQualityFilter(hash)

	grade := "A"
	minScore := 80.0
	filters := NodeFilters{
		QualityGrade:    &grade,
		QualityMinScore: &minScore,
	}

	// Match both.
	entry.SetQuality(&model.NodeQuality{Grade: "A", Score: 90})
	if !nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected true when both grade and minScore match")
	}

	// Grade matches but score below min.
	entry.SetQuality(&model.NodeQuality{Grade: "A", Score: 70})
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when grade matches but score < min")
	}

	// Score meets min but grade doesn't match.
	entry.SetQuality(&model.NodeQuality{Grade: "B", Score: 85})
	if nodeEntryMatchesQualityFilters(entry, filters) {
		t.Fatal("expected false when score >= min but grade doesn't match")
	}
}

// TestNodeEntryMatchesQualityFilters_CFStatuses verifies the OR-within-statuses
// and intersection-with-bool semantics for QualityCloudflareStatuses.
func TestNodeEntryMatchesQualityFilters_CFStatuses(t *testing.T) {
	entry := node.NewNodeEntry(node.Hash{}, nil, time.Now(), 16)
	q := &model.NodeQuality{}

	t.Run("no filter always matches", func(t *testing.T) {
		if !nodeEntryMatchesQualityFilters(entry, NodeFilters{}) {
			t.Fatal("expected true when no filters set")
		}
	})

	t.Run("nil quality when filter active returns false", func(t *testing.T) {
		entry.SetQuality(nil)
		if nodeEntryMatchesQualityFilters(entry, NodeFilters{QualityCloudflareStatuses: []string{"clean"}}) {
			t.Fatal("expected false when quality is nil")
		}
	})

	t.Run("single status match", func(t *testing.T) {
		q.CloudflareStatus = "clean"
		entry.SetQuality(q)
		if !nodeEntryMatchesQualityFilters(entry, NodeFilters{QualityCloudflareStatuses: []string{"clean"}}) {
			t.Fatal("expected true when status matches single filter")
		}
	})

	t.Run("OR within selected statuses", func(t *testing.T) {
		q.CloudflareStatus = "block"
		entry.SetQuality(q)
		if !nodeEntryMatchesQualityFilters(entry, NodeFilters{QualityCloudflareStatuses: []string{"clean", "block", "ng"}}) {
			t.Fatal("expected true when status is in multi-select filter")
		}
	})

	t.Run("non-matching status returns false", func(t *testing.T) {
		q.CloudflareStatus = "clean"
		entry.SetQuality(q)
		if nodeEntryMatchesQualityFilters(entry, NodeFilters{QualityCloudflareStatuses: []string{"block", "ng"}}) {
			t.Fatal("expected false when status is not in filter")
		}
	})

	t.Run("empty legacy status normalizes to unchecked", func(t *testing.T) {
		q.CloudflareStatus = ""
		entry.SetQuality(q)
		// "unchecked" is a filter value for display matching
		if nodeEntryMatchesQualityFilters(entry, NodeFilters{QualityCloudflareStatuses: []string{"clean"}}) {
			t.Fatal("expected false: empty legacy status normalizes to unchecked, not clean")
		}
	})

	t.Run("intersection with bool filter - both pass", func(t *testing.T) {
		trueVal := true
		q.CloudflareStatus = "block"
		q.CloudflareChallenged = true
		entry.SetQuality(q)
		if !nodeEntryMatchesQualityFilters(entry, NodeFilters{
			QualityCloudflareStatuses:   []string{"block", "js_challenge"},
			QualityCloudflareChallenged: &trueVal,
		}) {
			t.Fatal("expected true when both filters match")
		}
	})

	t.Run("intersection with bool filter - bool rejects", func(t *testing.T) {
		falseVal := false
		q.CloudflareStatus = "block"
		q.CloudflareChallenged = true
		entry.SetQuality(q)
		if nodeEntryMatchesQualityFilters(entry, NodeFilters{
			QualityCloudflareStatuses:   []string{"block"},
			QualityCloudflareChallenged: &falseVal,
		}) {
			t.Fatal("expected false when bool filter rejects")
		}
	})

	t.Run("intersection with bool filter - status rejects", func(t *testing.T) {
		trueVal := true
		q.CloudflareStatus = "clean"
		q.CloudflareChallenged = true // clean but challenged=false normally - testing intersection
		entry.SetQuality(q)
		if nodeEntryMatchesQualityFilters(entry, NodeFilters{
			QualityCloudflareStatuses:   []string{"block"},
			QualityCloudflareChallenged: &trueVal,
		}) {
			t.Fatal("expected false when status filter rejects")
		}
	})
}

// TestNodeQualitySummary_ProjectsMetadataFields verifies that the node
// summary correctly projects the three Phase 3B2 metadata fields:
// quality_cloudflare_status (persisted, not derived),
// quality_scoring_policy_version, and quality_score_breakdown.
// Legacy empty status normalizes to "unchecked".
func TestNodeQualitySummary_ProjectsMetadataFields(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	sub := subscription.NewSubscription("sub-q", "SubQ", "https://example.com/q", true, false)
	subMgr.Register(sub)
	pool := newNodeListTestPool(subMgr)

	raw := []byte(`{"type":"ss","server":"1.2.3.4","port":443}`)
	hash := addRoutableNodeForSubscription(t, pool, sub, raw, "5.6.7.8")

	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}

	// Quality with canonical CloudflareStatus and breakdown.
	entry.SetQuality(&model.NodeQuality{
		Profile:              "generic",
		Grade:                "A",
		Score:                95,
		CloudflareStatus:     "clean",
		ScoringPolicyVersion: 1,
		ScoreBreakdown:       `{"version":1,"grade_from_score":"A","final_grade":"A"}`,
	})
	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
	}
	ns, err := cp.GetNode(hash.Hex())
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if ns.Quality == nil {
		t.Fatal("expected quality summary")
	}
	// Persisted canonical status.
	if ns.Quality.QualityCloudflareStatus != "clean" {
		t.Fatalf("QualityCloudflareStatus = %q, want clean", ns.Quality.QualityCloudflareStatus)
	}
	// Scoring policy version.
	if ns.Quality.QualityScoringPolicyVersion != 1 {
		t.Fatalf("QualityScoringPolicyVersion = %d, want 1", ns.Quality.QualityScoringPolicyVersion)
	}
	// Score breakdown projected as json.RawMessage (not double-encoded).
	if ns.Quality.QualityScoreBreakdown == nil {
		t.Fatal("expected QualityScoreBreakdown")
	}
	var decoded map[string]any
	if err := json.Unmarshal(*ns.Quality.QualityScoreBreakdown, &decoded); err != nil {
		t.Fatalf("QualityScoreBreakdown not valid JSON: %v", err)
	}
	if decoded["grade_from_score"] != "A" {
		t.Fatalf("breakdown grade_from_score = %v, want A", decoded["grade_from_score"])
	}

	// Legacy empty cloudflare_status normalizes to "unchecked".
	entry.SetQuality(&model.NodeQuality{
		Profile: "generic",
		Grade:   "B",
		Score:   75,
	})
	ns2, _ := cp.GetNode(hash.Hex())
	if ns2.Quality.QualityCloudflareStatus != "unchecked" {
		t.Fatalf("legacy QualityCloudflareStatus = %q, want unchecked", ns2.Quality.QualityCloudflareStatus)
	}
	if ns2.Quality.QualityScoringPolicyVersion != 0 {
		t.Fatalf("legacy QualityScoringPolicyVersion = %d, want 0", ns2.Quality.QualityScoringPolicyVersion)
	}
	if ns2.Quality.QualityScoreBreakdown != nil {
		t.Fatal("legacy QualityScoreBreakdown should be nil")
	}

	// Invalid persisted JSON must be omitted rather than breaking outer JSON
	// serialization for node list/detail responses.
	entry.SetQuality(&model.NodeQuality{
		Profile:              "generic",
		Grade:                "C",
		Score:                60,
		CloudflareStatus:     "not_detected",
		ScoringPolicyVersion: 1,
		ScoreBreakdown:       `{invalid`,
	})
	ns3, _ := cp.GetNode(hash.Hex())
	if ns3.Quality.QualityScoreBreakdown != nil {
		t.Fatal("invalid QualityScoreBreakdown should be omitted")
	}
	if _, err := json.Marshal(ns3); err != nil {
		t.Fatalf("node summary with invalid stored breakdown must marshal: %v", err)
	}
}

// float64Ptr is a helper for creating *float64 literals in test fixtures.
func float64Ptr(v float64) *float64 { return &v }

// TestNodeQualitySummary_ScoredProxyScoreProjection verifies that quality
// data produced via the production ProxyScoreToNodeQuality conversion path
// is correctly projected through nodeEntryToSummary/GetNode. This covers
// the conversion→API projection half of the Phase 3B2 cross-layer
// verification (the conversion→persistence half lives in
// state.TestNodeQuality_ScoredProxyScoreRoundTrip).
//
// Assertions:
//   - QualityCloudflareStatus shows the persisted canonical status
//   - QualityScoringPolicyVersion is set from the ScoringBreakdown
//   - QualityScoreBreakdown is a *json.RawMessage (not double-encoded)
//     containing the expected compact breakdown fields
//   - Legacy nil breakdown produces version 0, nil breakdown, and
//     CloudflareStatus normalised to "unchecked"
func TestNodeQualitySummary_ScoredProxyScoreProjection(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	sub := subscription.NewSubscription("sub-cross", "SubCross", "https://example.com/cross", true, false)
	subMgr.Register(sub)
	pool := newNodeListTestPool(subMgr)

	raw := []byte(`{"type":"ss","server":"10.20.30.40","port":443}`)
	hash := addRoutableNodeForSubscription(t, pool, sub, raw, "9.8.7.6")

	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry not found")
	}

	t.Run("scored_breakdown_with_clean_cf", func(t *testing.T) {
		score := &probe.ProxyScore{
			Grade:            "A",
			Score:            95,
			Unstable:         false,
			ServiceReachable: true,
			APIReachable:     true,
			CloudflareStatus: probe.CFStatusClean,
			AvgLatencyMs:     50.0,
			ScoringBreakdown: &probe.ScoringResult{
				Version:          1,
				Score:            95,
				Grade:            "A",
				GradeFromScore:   "A",
				FinalGrade:       "A",
				EffectiveWeights: map[string]int{"service": 60, "latency": 40},
				SubScores: map[string]*probe.SubScoreEntry{
					"service": {Value: float64Ptr(100)},
					"latency": {Value: float64Ptr(87.5)},
				},
				UnavailableDims: []string{},
				AppliedCaps:     nil,
				TerminalReason:  "",
			},
		}
		nq := probe.ProxyScoreToNodeQuality("generic", score, nil)
		entry.SetQuality(nq)

		cp := &ControlPlaneService{
			Pool:   pool,
			SubMgr: subMgr,
		}
		ns, err := cp.GetNode(hash.Hex())
		if err != nil {
			t.Fatalf("GetNode: %v", err)
		}
		if ns.Quality == nil {
			t.Fatal("expected quality summary")
		}

		// Canonical CF status.
		if ns.Quality.QualityCloudflareStatus != "clean" {
			t.Fatalf("QualityCloudflareStatus = %q, want clean", ns.Quality.QualityCloudflareStatus)
		}
		// Scoring policy version.
		if ns.Quality.QualityScoringPolicyVersion != 1 {
			t.Fatalf("QualityScoringPolicyVersion = %d, want 1", ns.Quality.QualityScoringPolicyVersion)
		}
		// Breakdown as *json.RawMessage (not double-encoded).
		if ns.Quality.QualityScoreBreakdown == nil {
			t.Fatal("expected QualityScoreBreakdown")
		}
		assertBreakdownIsValidObject(t, *ns.Quality.QualityScoreBreakdown)
		// Verify it's not double-encoded: unmarshal should produce map, not string.
		var decoded map[string]any
		if err := json.Unmarshal(*ns.Quality.QualityScoreBreakdown, &decoded); err != nil {
			t.Fatalf("QualityScoreBreakdown unmarshal: %v", err)
		}
		if decoded["version"] != float64(1) {
			t.Fatalf("breakdown version = %v, want 1", decoded["version"])
		}
		if decoded["final_grade"] != "A" {
			t.Fatalf("breakdown final_grade = %v, want A", decoded["final_grade"])
		}
	})

	t.Run("block_challenge_status_projection", func(t *testing.T) {
		score := &probe.ProxyScore{
			Grade:            "C",
			Score:            45,
			Unstable:         true,
			ServiceReachable: true,
			APIReachable:     false,
			CloudflareStatus: probe.CFStatusBlock,
			AvgLatencyMs:     500.0,
			ScoringBreakdown: &probe.ScoringResult{
				Version:          1,
				Score:            45,
				Grade:            "C",
				GradeFromScore:   "C",
				FinalGrade:       "D", // CF grade cap
				EffectiveWeights: map[string]int{"service": 40, "cf": 20},
				SubScores: map[string]*probe.SubScoreEntry{
					"service": {Value: float64Ptr(100)},
					"cf":      {Value: float64Ptr(0)},
				},
				UnavailableDims: []string{"stability"},
				AppliedCaps: []probe.CapApplication{
					{Dimension: "cf", Reason: "cf_status_cap", Cap: "D"},
				},
				TerminalReason: "",
			},
		}
		nq := probe.ProxyScoreToNodeQuality("generic", score, nil)
		entry.SetQuality(nq)

		cp := &ControlPlaneService{
			Pool:   pool,
			SubMgr: subMgr,
		}
		ns, err := cp.GetNode(hash.Hex())
		if err != nil {
			t.Fatalf("GetNode: %v", err)
		}
		if ns.Quality == nil {
			t.Fatal("expected quality summary")
		}

		if ns.Quality.QualityCloudflareStatus != "block" {
			t.Fatalf("QualityCloudflareStatus = %q, want block", ns.Quality.QualityCloudflareStatus)
		}
		if !ns.Quality.CloudflareChallenged {
			t.Fatal("CloudflareChallenged should be true for block")
		}
		if ns.Quality.CloudflareChallengeType != "block" {
			t.Fatalf("CloudflareChallengeType = %q, want block", ns.Quality.CloudflareChallengeType)
		}
		if ns.Quality.QualityScoringPolicyVersion != 1 {
			t.Fatalf("QualityScoringPolicyVersion = %d, want 1", ns.Quality.QualityScoringPolicyVersion)
		}
		if ns.Quality.QualityScoreBreakdown == nil {
			t.Fatal("expected QualityScoreBreakdown")
		}
		assertBreakdownIsValidObject(t, *ns.Quality.QualityScoreBreakdown)
	})

	t.Run("legacy_nil_breakdown_unchecked_status", func(t *testing.T) {
		score := &probe.ProxyScore{
			Grade:            "B",
			Score:            75,
			ServiceReachable: true,
			CloudflareStatus: probe.CFStatusEmpty,
		}
		nq := probe.ProxyScoreToNodeQuality("generic", score, nil)
		entry.SetQuality(nq)

		cp := &ControlPlaneService{
			Pool:   pool,
			SubMgr: subMgr,
		}
		ns, err := cp.GetNode(hash.Hex())
		if err != nil {
			t.Fatalf("GetNode: %v", err)
		}
		if ns.Quality == nil {
			t.Fatal("expected quality summary")
		}

		// Empty legacy cloudflare_status normalises to "unchecked".
		if ns.Quality.QualityCloudflareStatus != "unchecked" {
			t.Fatalf("QualityCloudflareStatus = %q, want unchecked", ns.Quality.QualityCloudflareStatus)
		}
		if ns.Quality.QualityScoringPolicyVersion != 0 {
			t.Fatalf("QualityScoringPolicyVersion = %d, want 0", ns.Quality.QualityScoringPolicyVersion)
		}
		if ns.Quality.QualityScoreBreakdown != nil {
			t.Fatal("legacy QualityScoreBreakdown should be nil")
		}
	})
}

// assertBreakdownIsValidObject verifies that raw JSON bytes decode to a
// JSON object (map[string]any), proving it is not double-encoded as a
// quoted JSON string.
func assertBreakdownIsValidObject(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("breakdown not valid JSON: %v", err)
	}
	if _, ok := decoded.(map[string]any); !ok {
		t.Fatalf("breakdown is double-encoded (decoded as %T, want map[string]any)", decoded)
	}
}
