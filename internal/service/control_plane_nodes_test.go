package service

import (
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
