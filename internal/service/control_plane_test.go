package service

import (
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/platform"
	"github.com/Resinat/Resin/internal/proxy"
	"github.com/Resinat/Resin/internal/state"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
	"github.com/Resinat/Resin/internal/topology"
	"golang.org/x/net/http/httpguts"
)

// --- HTTP header field name validation ---

func TestHeaderTokenValid(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"X-Account-ID", true},
		{"Content-Type", true},
		{"Authorization", true},
		{"x-custom_header", true},
		{"X-My.Header", true},
		{"", false},
		{"Header Name", false},      // space not allowed
		{"Header\tName", false},     // tab not allowed
		{"Header:Name", false},      // colon not allowed
		{"日本語", false},              // non-ASCII
		{"Ł", false},                // non-ASCII rune whose low byte is ASCII 'A'
		{"Ａ", false},                // fullwidth ASCII confusable (U+FF21)
		{"X-Header(1)", false},      // parentheses not allowed
		{"X-Header[1]", false},      // brackets not allowed
		{"Header\"Quoted\"", false}, // double quotes not allowed
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := httpguts.ValidHeaderFieldName(tt.input); got != tt.want {
				t.Errorf("ValidHeaderFieldName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// --- validateRuntimeConfig ---

func TestValidateRuntimeConfig_NegativeByteFields(t *testing.T) {
	cfg := newDefaultCfg()
	cfg.ReverseProxyLogReqHeadersMaxBytes = -1
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for negative ReverseProxyLogReqHeadersMaxBytes")
	}

	cfg = newDefaultCfg()
	cfg.ReverseProxyLogReqBodyMaxBytes = -1
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for negative ReverseProxyLogReqBodyMaxBytes")
	}

	cfg = newDefaultCfg()
	cfg.ReverseProxyLogRespHeadersMaxBytes = -1
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for negative ReverseProxyLogRespHeadersMaxBytes")
	}

	cfg = newDefaultCfg()
	cfg.ReverseProxyLogRespBodyMaxBytes = -1
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for negative ReverseProxyLogRespBodyMaxBytes")
	}
}

func TestValidateRuntimeConfig_NegativeDurations(t *testing.T) {
	cfg := newDefaultCfg()
	cfg.MaxLatencyTestInterval = -1
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for negative MaxLatencyTestInterval")
	}

	cfg = newDefaultCfg()
	cfg.P2CLatencyWindow = -1
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for negative P2CLatencyWindow")
	}
}

func TestValidateRuntimeConfig_InvalidURL(t *testing.T) {
	cfg := newDefaultCfg()
	cfg.LatencyTestURL = "not a url"
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for invalid LatencyTestURL")
	}

	cfg = newDefaultCfg()
	cfg.LatencyTestURL = ""
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for empty LatencyTestURL")
	}
}

func TestValidateRuntimeConfig_ProbeIntervalsMinimum30s(t *testing.T) {
	cfg := newDefaultCfg()
	cfg.MaxLatencyTestInterval = config.Duration(29 * time.Second)
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for max_latency_test_interval < 30s")
	}

	cfg = newDefaultCfg()
	cfg.MaxAuthorityLatencyTestInterval = config.Duration(29 * time.Second)
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for max_authority_latency_test_interval < 30s")
	}

	cfg = newDefaultCfg()
	cfg.MaxEgressTestInterval = config.Duration(29 * time.Second)
	if err := validateRuntimeConfig(cfg); err == nil {
		t.Error("expected error for max_egress_test_interval < 30s")
	}

	cfg = newDefaultCfg()
	cfg.MaxLatencyTestInterval = config.Duration(30 * time.Second)
	cfg.MaxAuthorityLatencyTestInterval = config.Duration(30 * time.Second)
	cfg.MaxEgressTestInterval = config.Duration(30 * time.Second)
	if err := validateRuntimeConfig(cfg); err != nil {
		t.Fatalf("expected 30s boundary to be valid, got %v", err)
	}
}

func TestValidateRuntimeConfig_LatencyURLAutoAddsAuthority(t *testing.T) {
	cfg := newDefaultCfg()
	cfg.LatencyAuthorities = []string{"cloudflare.com"}
	cfg.LatencyTestURL = "https://www.gstatic.com/generate_204"
	if err := validateRuntimeConfig(cfg); err != nil {
		t.Fatalf("validateRuntimeConfig returned error: %v", err)
	}

	found := false
	for _, authority := range cfg.LatencyAuthorities {
		if authority == "gstatic.com" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected gstatic.com to be appended to authorities, got %v", cfg.LatencyAuthorities)
	}
}

func TestValidateRuntimeConfig_LatencyURLDoesNotDuplicateAuthority(t *testing.T) {
	cfg := newDefaultCfg()
	cfg.LatencyAuthorities = []string{"GSTATIC.COM"}
	cfg.LatencyTestURL = "https://www.gstatic.com/generate_204"
	if err := validateRuntimeConfig(cfg); err != nil {
		t.Fatalf("validateRuntimeConfig returned error: %v", err)
	}
	if len(cfg.LatencyAuthorities) != 1 {
		t.Fatalf("expected no duplicate authority, got %v", cfg.LatencyAuthorities)
	}
}

func TestValidateRuntimeConfig_ValidConfig(t *testing.T) {
	cfg := newDefaultCfg()
	if err := validateRuntimeConfig(cfg); err != nil {
		t.Errorf("unexpected error for valid config: %v", err)
	}
}

func TestRuntimeConfigPatchAllowlist_StaysInSyncWithRuntimeConfigJSONFields(t *testing.T) {
	rt := reflect.TypeOf(config.RuntimeConfig{})
	jsonFields := make(map[string]struct{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		jsonFields[name] = struct{}{}
	}

	allow := make(map[string]struct{}, len(runtimeConfigAllowedFields))
	for key := range runtimeConfigAllowedFields {
		allow[key] = struct{}{}
	}

	var onlyInJSON []string
	for key := range jsonFields {
		if _, ok := allow[key]; !ok {
			onlyInJSON = append(onlyInJSON, key)
		}
	}
	sort.Strings(onlyInJSON)

	var onlyInAllow []string
	for key := range allow {
		if _, ok := jsonFields[key]; !ok {
			onlyInAllow = append(onlyInAllow, key)
		}
	}
	sort.Strings(onlyInAllow)

	if len(onlyInJSON) > 0 || len(onlyInAllow) > 0 {
		t.Fatalf("runtime_config fields and patch allowlist drifted: onlyInJSON=%v onlyInAllow=%v", onlyInJSON, onlyInAllow)
	}
}

func newDefaultCfg() *config.RuntimeConfig {
	return config.NewDefaultRuntimeConfig()
}

func strPtr(s string) *string { return &s }

func TestResolveAccountHeaderRule_UsesEscapedPathSegments(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = closer.Close()
	})

	rules := []model.AccountHeaderRule{
		{
			URLPrefix: "api.example.com/v1",
			Headers:   []string{"x-base"},
		},
		{
			URLPrefix: "api.example.com/v1/team%2Fa",
			Headers:   []string{"x-special"},
		},
	}
	for _, rule := range rules {
		if _, err := engine.UpsertAccountHeaderRuleWithCreated(rule); err != nil {
			t.Fatalf("UpsertAccountHeaderRule(%q): %v", rule.URLPrefix, err)
		}
	}

	loaded, err := engine.ListAccountHeaderRules()
	if err != nil {
		t.Fatalf("ListAccountHeaderRules: %v", err)
	}

	cp := &ControlPlaneService{
		Engine:         engine,
		MatcherRuntime: proxy.NewAccountMatcherRuntime(proxy.BuildAccountMatcher(loaded)),
	}

	res, err := cp.ResolveAccountHeaderRule("https://api.example.com/v1/team%2Fa/profile?x=1")
	if err != nil {
		t.Fatalf("ResolveAccountHeaderRule: %v", err)
	}

	if res.MatchedURLPrefix != "api.example.com/v1/team%2Fa" {
		t.Fatalf("matched_url_prefix = %q, want %q", res.MatchedURLPrefix, "api.example.com/v1/team%2Fa")
	}
	if !reflect.DeepEqual(res.Headers, []string{"x-special"}) {
		t.Fatalf("headers = %v, want %v", res.Headers, []string{"x-special"})
	}
}

func TestUpsertAccountHeaderRule_NormalizesHostPrefix(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = closer.Close()
	})

	cp := &ControlPlaneService{
		Engine:         engine,
		MatcherRuntime: proxy.NewAccountMatcherRuntime(nil),
	}

	createdRule, created, err := cp.UpsertAccountHeaderRule("API.Example.COM/v1", []string{"Authorization"})
	if err != nil {
		t.Fatalf("UpsertAccountHeaderRule: %v", err)
	}
	if !created {
		t.Fatal("expected created=true for first upsert")
	}
	if createdRule.URLPrefix != "api.example.com/v1" {
		t.Fatalf("created rule prefix = %q, want %q", createdRule.URLPrefix, "api.example.com/v1")
	}

	rules, err := engine.ListAccountHeaderRules()
	if err != nil {
		t.Fatalf("ListAccountHeaderRules: %v", err)
	}
	if len(rules) != 1 || rules[0].URLPrefix != "api.example.com/v1" {
		t.Fatalf("persisted rules = %+v, want single normalized rule", rules)
	}

	resolved, err := cp.ResolveAccountHeaderRule("https://api.example.com/v1/orders/1")
	if err != nil {
		t.Fatalf("ResolveAccountHeaderRule: %v", err)
	}
	if resolved.MatchedURLPrefix != "api.example.com/v1" {
		t.Fatalf("matched_url_prefix = %q, want %q", resolved.MatchedURLPrefix, "api.example.com/v1")
	}
	if !reflect.DeepEqual(resolved.Headers, []string{"Authorization"}) {
		t.Fatalf("headers = %v, want %v", resolved.Headers, []string{"Authorization"})
	}
}

func TestDeleteAccountHeaderRule_DoesNotFallbackForLegacyMixedCaseRows(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = closer.Close()
	})

	legacy := model.AccountHeaderRule{
		URLPrefix:   "API.Example.COM/v1",
		Headers:     []string{"Authorization"},
		UpdatedAtNs: time.Now().UnixNano(),
	}
	if _, err := engine.UpsertAccountHeaderRuleWithCreated(legacy); err != nil {
		t.Fatalf("seed legacy rule: %v", err)
	}

	cp := &ControlPlaneService{
		Engine:         engine,
		MatcherRuntime: proxy.NewAccountMatcherRuntime(nil),
	}
	err = cp.DeleteAccountHeaderRule("api.example.com/v1")
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("DeleteAccountHeaderRule error type = %T, want *ServiceError", err)
	}
	if svcErr.Code != "NOT_FOUND" {
		t.Fatalf("DeleteAccountHeaderRule error code = %q, want NOT_FOUND", svcErr.Code)
	}

	rules, err := engine.ListAccountHeaderRules()
	if err != nil {
		t.Fatalf("ListAccountHeaderRules: %v", err)
	}
	if len(rules) != 1 || rules[0].URLPrefix != legacy.URLPrefix {
		t.Fatalf("expected legacy rule to remain, got %+v", rules)
	}
}

func TestDeleteAccountHeaderRule_RejectsFallbackRule(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = closer.Close()
	})

	fallback := model.AccountHeaderRule{
		URLPrefix:   "*",
		Headers:     []string{"Authorization", "x-api-key"},
		UpdatedAtNs: time.Now().UnixNano(),
	}
	if _, err := engine.UpsertAccountHeaderRuleWithCreated(fallback); err != nil {
		t.Fatalf("seed fallback rule: %v", err)
	}

	cp := &ControlPlaneService{
		Engine:         engine,
		MatcherRuntime: proxy.NewAccountMatcherRuntime(nil),
	}
	err = cp.DeleteAccountHeaderRule("*")
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("DeleteAccountHeaderRule error type = %T, want *ServiceError", err)
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Fatalf("DeleteAccountHeaderRule error code = %q, want INVALID_ARGUMENT", svcErr.Code)
	}

	rules, err := engine.ListAccountHeaderRules()
	if err != nil {
		t.Fatalf("ListAccountHeaderRules: %v", err)
	}
	if len(rules) != 1 || rules[0].URLPrefix != "*" {
		t.Fatalf("expected fallback rule to remain, got %+v", rules)
	}
}

func TestCreatePlatform_BuildsRoutableViewBeforePublish(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = closer.Close()
	})

	subMgr := topology.NewSubscriptionManager()
	sub := subscription.NewSubscription("sub-1", "sub", "https://example.com/sub", true, false)
	subMgr.Register(sub)
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	// Seed one fully-routable node into the pool before platform creation.
	raw := []byte(`{"type":"ss","server":"1.1.1.1","port":443}`)
	hash := node.HashFromRawOptions(raw)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"seed"}})
	entry := node.NewNodeEntry(hash, raw, time.Now(), 16)
	entry.AddSubscriptionID(sub.ID)
	entry.SetEgressIP(netip.MustParseAddr("1.2.3.4"))
	entry.LatencyTable.LoadEntry("cloudflare.com", node.DomainLatencyStats{
		Ewma:        50 * time.Millisecond,
		LastUpdated: time.Now(),
	})
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)
	pool.LoadNodeFromBootstrap(entry)

	runtimeCfg := &atomic.Pointer[config.RuntimeConfig]{}
	runtimeCfg.Store(config.NewDefaultRuntimeConfig())

	cp := &ControlPlaneService{
		Engine:     engine,
		Pool:       pool,
		SubMgr:     subMgr,
		RuntimeCfg: runtimeCfg,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              30 * time.Minute,
			DefaultPlatformRegexFilters:           []string{},
			DefaultPlatformRegionFilters:          []string{},
			DefaultPlatformReverseProxyMissAction: "TREAT_AS_EMPTY",
			DefaultPlatformAllocationPolicy:       "BALANCED",
		},
	}

	name := "new-platform"
	created, err := cp.CreatePlatform(CreatePlatformRequest{Name: &name})
	if err != nil {
		t.Fatalf("CreatePlatform: %v", err)
	}

	plat, ok := pool.GetPlatform(created.ID)
	if !ok {
		t.Fatalf("platform %s was not registered in pool", created.ID)
	}
	if got := plat.View().Size(); got != 1 {
		t.Fatalf("new platform view size = %d, want 1", got)
	}
	if !plat.View().Contains(hash) {
		t.Fatalf("new platform view should contain seeded hash %s", hash.Hex())
	}
	if created.PassiveCircuitBreakerDisabled {
		t.Fatal("new platform should default passive circuit breaker to not disabled")
	}
	if plat.PassiveCircuitBreakerDisabled {
		t.Fatal("runtime platform should default passive circuit breaker to not disabled")
	}
}

func TestCreatePlatform_RejectsReservedAPIName(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = closer.Close()
	})

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              30 * time.Minute,
			DefaultPlatformRegexFilters:           []string{},
			DefaultPlatformRegionFilters:          []string{},
			DefaultPlatformReverseProxyMissAction: "TREAT_AS_EMPTY",
			DefaultPlatformAllocationPolicy:       "BALANCED",
		},
	}

	name := "api"
	_, err = cp.CreatePlatform(CreatePlatformRequest{Name: &name})
	if err == nil {
		t.Fatal("expected CreatePlatform to reject reserved platform name api")
	}

	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Fatalf("service error code = %q, want %q", svcErr.Code, "INVALID_ARGUMENT")
	}
	if !strings.Contains(svcErr.Message, "name:") || !strings.Contains(svcErr.Message, "reserved") {
		t.Fatalf("service error message = %q, expected reserved-name hint", svcErr.Message)
	}
}

func TestDeleteSubscription_PersistFailureDoesNotMutateRuntimeState(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = closer.Close()
	})

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	sub := subscription.NewSubscription("sub-1", "sub", "https://example.com/sub", true, false)
	subMgr.Register(sub)

	subModel := model.Subscription{
		ID:                        sub.ID,
		Name:                      sub.Name(),
		URL:                       sub.URL(),
		UpdateIntervalNs:          int64(30 * time.Second),
		Enabled:                   sub.Enabled(),
		Ephemeral:                 sub.Ephemeral(),
		EphemeralNodeEvictDelayNs: sub.EphemeralNodeEvictDelayNs(),
		CreatedAtNs:               time.Now().Add(-time.Minute).UnixNano(),
		UpdatedAtNs:               time.Now().UnixNano(),
	}
	if err := engine.UpsertSubscription(subModel); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	raw := []byte(`{"type":"ss","server":"1.1.1.1","port":443,"tag":"s1"}`)
	hash := node.HashFromRawOptions(raw)
	pool.AddNodeFromSub(hash, raw, sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"tag-a"}})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
	}

	// Force DB write failure; DeleteSubscription must not mutate runtime state.
	_ = closer.Close()

	err = cp.DeleteSubscription(sub.ID)
	if err == nil {
		t.Fatal("expected delete subscription error after db close")
	}

	if got := subMgr.Lookup(sub.ID); got == nil {
		t.Fatal("subscription should remain registered on persist failure")
	}

	if _, ok := pool.GetEntry(hash); !ok {
		t.Fatal("node should remain in pool on persist failure")
	}
}

func TestGetSubscription_NodeCountExcludesEvictedManagedNodes(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	subA := subscription.NewSubscription("sub-a", "sub-a", "https://example.com/a", true, false)
	subB := subscription.NewSubscription("sub-b", "sub-b", "https://example.com/b", true, false)
	subMgr.Register(subA)
	subMgr.Register(subB)

	// Active node owned by subA.
	activeRaw := []byte(`{"type":"ss","server":"1.1.1.1","port":443}`)
	activeHash := node.HashFromRawOptions(activeRaw)
	pool.AddNodeFromSub(activeHash, activeRaw, subA.ID)
	subA.ManagedNodes().StoreNode(activeHash, subscription.ManagedNode{Tags: []string{"active"}})
	activeEntry, ok := pool.GetEntry(activeHash)
	if !ok {
		t.Fatal("active entry missing")
	}
	activeOutbound := testutil.NewNoopOutbound()
	activeEntry.Outbound.Store(&activeOutbound)
	pool.RecordResult(activeHash, true)

	// Shared node is marked evicted in subA but still healthy in pool via subB.
	sharedRaw := []byte(`{"type":"ss","server":"2.2.2.2","port":443}`)
	sharedHash := node.HashFromRawOptions(sharedRaw)
	pool.AddNodeFromSub(sharedHash, sharedRaw, subA.ID)
	pool.AddNodeFromSub(sharedHash, sharedRaw, subB.ID)
	subA.ManagedNodes().StoreNode(sharedHash, subscription.ManagedNode{Tags: []string{"shared-a"}})
	subB.ManagedNodes().StoreNode(sharedHash, subscription.ManagedNode{Tags: []string{"shared-b"}})
	sharedEntry, ok := pool.GetEntry(sharedHash)
	if !ok {
		t.Fatal("shared entry missing")
	}
	sharedOutbound := testutil.NewNoopOutbound()
	sharedEntry.Outbound.Store(&sharedOutbound)
	pool.RecordResult(sharedHash, true)

	evictedNode, ok := subA.ManagedNodes().LoadNode(sharedHash)
	if !ok {
		t.Fatal("subA shared managed node missing")
	}
	evictedNode.Evicted = true
	subA.ManagedNodes().StoreNode(sharedHash, evictedNode)
	pool.RemoveNodeFromSub(sharedHash, subA.ID)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
	}

	resp, err := cp.GetSubscription(subA.ID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if resp.NodeCount != 1 {
		t.Fatalf("node_count = %d, want 1", resp.NodeCount)
	}
	if resp.HealthyNodeCount != 1 {
		t.Fatalf("healthy_node_count = %d, want 1", resp.HealthyNodeCount)
	}
}

func TestGetSubscription_HealthyNodeCount_ExcludesDisabledSubscriptionNodes(t *testing.T) {
	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
	})

	sub := subscription.NewSubscription("sub-disabled", "sub-disabled", "https://example.com/sub", false, false)
	subMgr.Register(sub)

	raw := []byte(`{"type":"ss","server":"1.1.1.1","port":443}`)
	hash := node.HashFromRawOptions(raw)
	pool.AddNodeFromSub(hash, raw, sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"tag-a"}})

	entry, ok := pool.GetEntry(hash)
	if !ok {
		t.Fatal("entry missing")
	}
	outbound := testutil.NewNoopOutbound()
	entry.Outbound.Store(&outbound)
	pool.RecordResult(hash, true)

	cp := &ControlPlaneService{
		Pool:   pool,
		SubMgr: subMgr,
	}

	resp, err := cp.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if resp.NodeCount != 1 {
		t.Fatalf("node_count = %d, want 1", resp.NodeCount)
	}
	if resp.HealthyNodeCount != 0 {
		t.Fatalf("healthy_node_count = %d, want 0", resp.HealthyNodeCount)
	}
}

// --- ClashFingerprintPolicy ---

func mustRegisterSub(t *testing.T, cp *ControlPlaneService, sub *subscription.Subscription) {
	t.Helper()
	now := time.Now().UnixNano()
	ms := model.Subscription{
		ID:                        sub.ID,
		Name:                      sub.Name(),
		URL:                       sub.URL(),
		UpdateIntervalNs:          sub.UpdateIntervalNs(),
		Enabled:                   sub.Enabled(),
		Ephemeral:                 sub.Ephemeral(),
		IncrementalAliveNodes:     sub.IncrementalAliveNodes(),
		EphemeralNodeEvictDelayNs: sub.EphemeralNodeEvictDelayNs(),
		ClashFingerprintPolicy:    sub.ClashFingerprintPolicy().String(),
		CreatedAtNs:               now,
		UpdatedAtNs:               now,
	}
	if err := cp.Engine.UpsertSubscription(ms); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	cp.SubMgr.Register(sub)
}

func TestCreateSubscription_ClashFingerprintPolicy_DefaultIsReject(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
	}

	name := "default-policy-sub"
	resp, err := cp.CreateSubscription(CreateSubscriptionRequest{
		Name: &name,
		URL:  strPtr("https://example.com/default-policy"),
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if resp.ClashFingerprintPolicy != "reject" {
		t.Fatalf("clash_fingerprint_policy: got %q, want %q", resp.ClashFingerprintPolicy, "reject")
	}

	// Verify in-memory runtime.
	sub := subMgr.Lookup(resp.ID)
	if sub == nil {
		t.Fatal("runtime subscription not found")
	}
	if sub.ClashFingerprintPolicy().String() != "reject" {
		t.Fatalf("runtime policy: got %s, want reject", sub.ClashFingerprintPolicy())
	}
}

func TestCreateSubscription_ClashFingerprintPolicy_EachValidValue(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
	}

	for _, tc := range []struct {
		value string
		want  subscription.ClashFingerprintPolicy
	}{
		{value: "drop_safe", want: subscription.ClashFingerprintDropSafe},
		{value: "drop_always", want: subscription.ClashFingerprintDropAlways},
	} {
		t.Run(tc.value, func(t *testing.T) {
			name := "sub-" + tc.value
			resp, err := cp.CreateSubscription(CreateSubscriptionRequest{
				Name:                   &name,
				URL:                    strPtr("https://example.com/" + tc.value),
				ClashFingerprintPolicy: &tc.value,
			})
			if err != nil {
				t.Fatalf("CreateSubscription: %v", err)
			}
			if resp.ClashFingerprintPolicy != tc.value {
				t.Fatalf("clash_fingerprint_policy: got %q, want %q", resp.ClashFingerprintPolicy, tc.value)
			}

			sub := subMgr.Lookup(resp.ID)
			if sub == nil {
				t.Fatal("runtime subscription not found")
			}
			if sub.ClashFingerprintPolicy() != tc.want {
				t.Fatalf("runtime policy: got %v, want %v", sub.ClashFingerprintPolicy(), tc.want)
			}
		})
	}
}

func TestCreateSubscription_ClashFingerprintPolicy_RejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
	}

	invalidValues := []string{"unknown", "", "DROP_ALWAYS", "drop_always "}
	for _, v := range invalidValues {
		t.Run(v, func(t *testing.T) {
			name := "sub-invalid-" + v
			_, err := cp.CreateSubscription(CreateSubscriptionRequest{
				Name:                   &name,
				URL:                    strPtr("https://example.com/invalid"),
				ClashFingerprintPolicy: &v,
			})
			if err == nil {
				t.Fatal("expected error for invalid policy")
			}
			var svcErr *ServiceError
			if !errors.As(err, &svcErr) {
				t.Fatalf("expected ServiceError, got %T: %v", err, err)
			}
			if svcErr.Code != "INVALID_ARGUMENT" {
				t.Fatalf("code: got %q, want INVALID_ARGUMENT", svcErr.Code)
			}
			if !strings.Contains(svcErr.Message, "clash_fingerprint_policy") {
				t.Fatalf("message missing field name: %q", svcErr.Message)
			}
		})
	}
}

func TestUpdateSubscription_ClashFingerprintPolicy_ChangesAndPersists(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	fetchCalled := make(chan struct{}, 1)
	scheduler := topology.NewSubscriptionScheduler(topology.SchedulerConfig{
		SubManager: subMgr,
		Pool:       pool,
		Fetcher: func(url string) ([]byte, error) {
			select {
			case fetchCalled <- struct{}{}:
			default:
			}
			return []byte("proxies:"), nil
		},
	})

	cp := &ControlPlaneService{
		Engine:    engine,
		Pool:      pool,
		SubMgr:    subMgr,
		Scheduler: scheduler,
	}

	name := "policy-update-sub"
	createResp, err := cp.CreateSubscription(CreateSubscriptionRequest{
		Name: &name,
		URL:  strPtr("https://example.com/policy-update"),
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	subID := createResp.ID

	// Update to drop_safe. This should trigger a refresh (fetchCount increments).
	patch := json.RawMessage(`{"clash_fingerprint_policy": "drop_safe"}`)
	resp, err := cp.UpdateSubscription(subID, patch)
	if err != nil {
		t.Fatalf("UpdateSubscription: %v", err)
	}
	if resp.ClashFingerprintPolicy != "drop_safe" {
		t.Fatalf("response policy: got %q, want %q", resp.ClashFingerprintPolicy, "drop_safe")
	}

	// Verify in-memory runtime.
	sub := subMgr.Lookup(subID)
	if sub == nil {
		t.Fatal("runtime subscription not found")
	}
	if sub.ClashFingerprintPolicy().String() != "drop_safe" {
		t.Fatalf("runtime policy: got %s, want drop_safe", sub.ClashFingerprintPolicy())
	}

	// Verify persisted.
	subs, err := engine.ListSubscriptions()
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	var found bool
	for _, ms := range subs {
		if ms.ID == subID {
			found = true
			if ms.ClashFingerprintPolicy != "drop_safe" {
				t.Fatalf("persisted policy: got %q, want %q", ms.ClashFingerprintPolicy, "drop_safe")
			}
			break
		}
	}
	if !found {
		t.Fatal("subscription not found in persistence")
	}

	// Verify the asynchronous refresh was triggered (scheduler called fetcher).
	select {
	case <-fetchCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for scheduler UpdateSubscription")
	}
}

func TestUpdateSubscription_ClashFingerprintPolicy_NoChangeDoesNotTriggerRefresh(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	fetchCount := 0
	scheduler := topology.NewSubscriptionScheduler(topology.SchedulerConfig{
		SubManager: subMgr,
		Pool:       pool,
		Fetcher: func(url string) ([]byte, error) {
			fetchCount++
			return []byte("proxies:"), nil
		},
	})

	cp := &ControlPlaneService{
		Engine:    engine,
		Pool:      pool,
		SubMgr:    subMgr,
		Scheduler: scheduler,
	}

	name := "no-change-sub"
	createResp, err := cp.CreateSubscription(CreateSubscriptionRequest{
		Name:                   &name,
		URL:                    strPtr("https://example.com/no-change"),
		ClashFingerprintPolicy: strPtr("drop_safe"),
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	subID := createResp.ID

	// Reset fetch count after create triggers refresh.
	createFetchCount := fetchCount

	// Patch with same policy value — should NOT trigger refresh.
	patch := json.RawMessage(`{"clash_fingerprint_policy": "drop_safe"}`)
	_, err = cp.UpdateSubscription(subID, patch)
	if err != nil {
		t.Fatalf("UpdateSubscription: %v", err)
	}
	if fetchCount != createFetchCount {
		t.Fatal("expected no fetch when policy unchanged")
	}
}

func TestUpdateSubscription_ClashFingerprintPolicy_RejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
	}

	name := "invalid-patch-sub"
	createResp, err := cp.CreateSubscription(CreateSubscriptionRequest{
		Name: &name,
		URL:  strPtr("https://example.com/invalid-patch"),
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	subID := createResp.ID

	_, err = cp.UpdateSubscription(subID, json.RawMessage(`{"clash_fingerprint_policy": "bogus"}`))
	if err == nil {
		t.Fatal("expected error for invalid policy")
	}
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Fatalf("code: got %q, want INVALID_ARGUMENT", svcErr.Code)
	}
}

func TestBootstrap_ClashFingerprintPolicy_RestoresFromPersistence(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	now := time.Now().UnixNano()

	// Persist a subscription with drop_always policy.
	ms := model.Subscription{
		ID:                        "sub-bootstrap-1",
		Name:                      "BootstrapSub",
		URL:                       "https://example.com/bootstrap",
		UpdateIntervalNs:          int64(5 * time.Minute),
		Enabled:                   true,
		Ephemeral:                 false,
		EphemeralNodeEvictDelayNs: int64(72 * time.Hour),
		ClashFingerprintPolicy:    "drop_always",
		CreatedAtNs:               now,
		UpdatedAtNs:               now,
	}
	if err := engine.UpsertSubscription(ms); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	// Persist a subscription with empty policy (legacy).
	ms2 := model.Subscription{
		ID:                        "sub-bootstrap-2",
		Name:                      "BootstrapLegacy",
		URL:                       "https://example.com/legacy",
		UpdateIntervalNs:          int64(5 * time.Minute),
		Enabled:                   true,
		Ephemeral:                 false,
		EphemeralNodeEvictDelayNs: int64(72 * time.Hour),
		ClashFingerprintPolicy:    "",
		CreatedAtNs:               now,
		UpdatedAtNs:               now,
	}
	if err := engine.UpsertSubscription(ms2); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	// Persist a subscription with invalid policy (legacy).
	ms3 := model.Subscription{
		ID:                        "sub-bootstrap-3",
		Name:                      "BootstrapInvalid",
		URL:                       "https://example.com/invalid",
		UpdateIntervalNs:          int64(5 * time.Minute),
		Enabled:                   true,
		Ephemeral:                 false,
		EphemeralNodeEvictDelayNs: int64(72 * time.Hour),
		ClashFingerprintPolicy:    "unknown_value",
		CreatedAtNs:               now,
		UpdatedAtNs:               now,
	}
	if err := engine.UpsertSubscription(ms3); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	// Bootstrap (simulate what main.go does).
	dbSubs, err := engine.ListSubscriptions()
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}

	subMgr := topology.NewSubscriptionManager()
	for _, subms := range dbSubs {
		sub := subscription.NewSubscription(subms.ID, subms.Name, subms.URL, subms.Enabled, subms.Ephemeral)
		sub.SetFetchConfig(subms.URL, subms.UpdateIntervalNs)
		sub.SetSourceType(subms.SourceType)
		sub.SetContent(subms.Content)
		sub.SetIncrementalAliveNodes(subms.IncrementalAliveNodes)
		sub.SetEphemeralNodeEvictDelayNs(subms.EphemeralNodeEvictDelayNs)
		sub.SetClashFingerprintPolicy(subscription.ParseClashFingerprintPolicy(subms.ClashFingerprintPolicy))
		sub.CreatedAtNs = subms.CreatedAtNs
		sub.UpdatedAtNs = subms.UpdatedAtNs
		subMgr.Register(sub)
	}

	// Verify sub 1: drop_always.
	sub1 := subMgr.Lookup("sub-bootstrap-1")
	if sub1 == nil {
		t.Fatal("sub-bootstrap-1 not found")
	}
	if sub1.ClashFingerprintPolicy().String() != "drop_always" {
		t.Fatalf("bootstrap sub1 policy: got %s, want drop_always", sub1.ClashFingerprintPolicy())
	}

	// Verify sub 2: empty → reject.
	sub2 := subMgr.Lookup("sub-bootstrap-2")
	if sub2 == nil {
		t.Fatal("sub-bootstrap-2 not found")
	}
	if sub2.ClashFingerprintPolicy() != subscription.ClashFingerprintReject {
		t.Fatalf("bootstrap sub2 policy: got %v, want reject", sub2.ClashFingerprintPolicy())
	}

	// Verify sub 3: invalid → reject.
	sub3 := subMgr.Lookup("sub-bootstrap-3")
	if sub3 == nil {
		t.Fatal("sub-bootstrap-3 not found")
	}
	if sub3.ClashFingerprintPolicy() != subscription.ClashFingerprintReject {
		t.Fatalf("bootstrap sub3 policy: got %v, want reject", sub3.ClashFingerprintPolicy())
	}
}

// --- end ClashFingerprintPolicy ---

func TestListPlatforms_FailsFastOnCorruptPersistedFiltersJSON(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cacheDir := filepath.Join(dir, "cache")

	engine, closer, err := state.PersistenceBootstrap(stateDir, cacheDir)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	now := time.Now().UnixNano()
	platformRow := model.Platform{
		ID:                     "plat-1",
		Name:                   "broken-platform",
		StickyTTLNs:            int64(time.Hour),
		RegexFilters:           []string{`^ok$`},
		RegionFilters:          []string{"us"},
		ReverseProxyMissAction: "TREAT_AS_EMPTY",
		AllocationPolicy:       "BALANCED",
		UpdatedAtNs:            now,
	}
	if err := engine.UpsertPlatform(platformRow); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}

	db, err := state.OpenDB(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatalf("OpenDB(state.db): %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE platforms SET regex_filters_json = ? WHERE id = ?`,
		`{"bad":"shape"}`,
		platformRow.ID,
	); err != nil {
		t.Fatalf("corrupt platform row: %v", err)
	}

	cp := &ControlPlaneService{Engine: engine}
	_, err = cp.ListPlatforms()
	if err == nil {
		t.Fatal("expected ListPlatforms to fail on corrupt filters")
	}
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		t.Fatalf("expected ServiceError, got %T (%v)", err, err)
	}
	if serviceErr.Code != "INTERNAL" {
		t.Fatalf("service error code = %q, want INTERNAL", serviceErr.Code)
	}
	if serviceErr.Err == nil || !strings.Contains(serviceErr.Err.Error(), "decode platform") {
		t.Fatalf("unexpected wrapped service error: %v", serviceErr.Err)
	}
}

func TestGetPlatform_FailsFastOnCorruptPersistedFiltersJSON(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cacheDir := filepath.Join(dir, "cache")

	engine, closer, err := state.PersistenceBootstrap(stateDir, cacheDir)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	now := time.Now().UnixNano()
	platformRow := model.Platform{
		ID:                     "plat-1",
		Name:                   "broken-platform",
		StickyTTLNs:            int64(time.Hour),
		RegexFilters:           []string{`^ok$`},
		RegionFilters:          []string{"us"},
		ReverseProxyMissAction: "TREAT_AS_EMPTY",
		AllocationPolicy:       "BALANCED",
		UpdatedAtNs:            now,
	}
	if err := engine.UpsertPlatform(platformRow); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}

	db, err := state.OpenDB(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatalf("OpenDB(state.db): %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE platforms SET regex_filters_json = ? WHERE id = ?`,
		`{"bad":"shape"}`,
		platformRow.ID,
	); err != nil {
		t.Fatalf("corrupt platform row: %v", err)
	}

	cp := &ControlPlaneService{Engine: engine}
	_, err = cp.GetPlatform(platformRow.ID)
	if err == nil {
		t.Fatal("expected GetPlatform to fail on corrupt filters")
	}
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		t.Fatalf("expected ServiceError, got %T (%v)", err, err)
	}
	if serviceErr.Code != "INTERNAL" {
		t.Fatalf("service error code = %q, want INTERNAL", serviceErr.Code)
	}
	if serviceErr.Err == nil || !strings.Contains(serviceErr.Err.Error(), "decode platform") {
		t.Fatalf("unexpected wrapped service error: %v", serviceErr.Err)
	}
}

func TestDeletePlatform_DoesNotDecodeCorruptPersistedFiltersJSON(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cacheDir := filepath.Join(dir, "cache")

	engine, closer, err := state.PersistenceBootstrap(stateDir, cacheDir)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	platformRow := model.Platform{
		ID:                     "plat-delete-corrupt",
		Name:                   "delete-corrupt",
		StickyTTLNs:            int64(time.Hour),
		RegexFilters:           []string{`^ok$`},
		RegionFilters:          []string{"us"},
		ReverseProxyMissAction: "TREAT_AS_EMPTY",
		AllocationPolicy:       "BALANCED",
		UpdatedAtNs:            time.Now().UnixNano(),
	}
	if err := engine.UpsertPlatform(platformRow); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}
	db, err := state.OpenDB(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatalf("OpenDB(state.db): %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE platforms SET regex_filters_json = ? WHERE id = ?`,
		`{"bad":"shape"}`,
		platformRow.ID,
	); err != nil {
		t.Fatalf("corrupt platform row: %v", err)
	}

	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              nil,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})
	pool.RegisterPlatform(platform.NewConfiguredPlatform(
		platformRow.ID,
		platformRow.Name,
		nil,
		nil,
		nil,
		nil,
		platformRow.StickyTTLNs,
		platformRow.ReverseProxyMissAction,
		string(platform.ReverseProxyEmptyAccountBehaviorAccountHeaderRule),
		"",
		platformRow.AllocationPolicy,
		true,
	))

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
	}

	if err := cp.DeletePlatform(platformRow.ID); err != nil {
		t.Fatalf("DeletePlatform: %v", err)
	}

	_, err = engine.GetPlatform(platformRow.ID)
	if !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("GetPlatform after delete err = %v, want ErrNotFound", err)
	}
	if _, ok := pool.GetPlatform(platformRow.ID); ok {
		t.Fatalf("platform %s should be removed from pool", platformRow.ID)
	}
}

func TestResetPlatformToDefault_SupportsBuiltInDefaultPlatform(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cacheDir := filepath.Join(dir, "cache")

	engine, closer, err := state.PersistenceBootstrap(stateDir, cacheDir)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	defaultRow := model.Platform{
		ID:                     platform.DefaultPlatformID,
		Name:                   platform.DefaultPlatformName,
		StickyTTLNs:            int64(2 * time.Hour),
		RegexFilters:           []string{`^legacy-`},
		RegionFilters:          []string{"us"},
		ReverseProxyMissAction: string(platform.ReverseProxyMissActionTreatAsEmpty),
		AllocationPolicy:       string(platform.AllocationPolicyBalanced),
		UpdatedAtNs:            time.Now().UnixNano(),
	}
	if err := engine.UpsertPlatform(defaultRow); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}

	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              nil,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})
	pool.RegisterPlatform(platform.NewConfiguredPlatform(
		defaultRow.ID,
		defaultRow.Name,
		nil,
		nil,
		nil,
		nil,
		defaultRow.StickyTTLNs,
		defaultRow.ReverseProxyMissAction,
		string(platform.ReverseProxyEmptyAccountBehaviorAccountHeaderRule),
		"",
		defaultRow.AllocationPolicy,
		true,
	))

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              45 * time.Minute,
			DefaultPlatformRegexFilters:           []string{"^prod-"},
			DefaultPlatformRegionFilters:          []string{"jp"},
			DefaultPlatformReverseProxyMissAction: string(platform.ReverseProxyMissActionReject),
			DefaultPlatformAllocationPolicy:       string(platform.AllocationPolicyPreferIdleIP),
		},
	}

	resp, err := cp.ResetPlatformToDefault(platform.DefaultPlatformID)
	if err != nil {
		t.Fatalf("ResetPlatformToDefault: %v", err)
	}
	if resp.ID != platform.DefaultPlatformID {
		t.Fatalf("response id = %q, want %q", resp.ID, platform.DefaultPlatformID)
	}
	if resp.Name != platform.DefaultPlatformName {
		t.Fatalf("response name = %q, want %q", resp.Name, platform.DefaultPlatformName)
	}
	if resp.StickyTTL != (45 * time.Minute).String() {
		t.Fatalf("response sticky_ttl = %q, want %q", resp.StickyTTL, (45 * time.Minute).String())
	}
	if !reflect.DeepEqual(resp.RegexFilters, []string{"^prod-"}) {
		t.Fatalf("response regex_filters = %v, want %v", resp.RegexFilters, []string{"^prod-"})
	}
	if !reflect.DeepEqual(resp.RegionFilters, []string{"jp"}) {
		t.Fatalf("response region_filters = %v, want %v", resp.RegionFilters, []string{"jp"})
	}
	if resp.ReverseProxyMissAction != string(platform.ReverseProxyMissActionReject) {
		t.Fatalf("response reverse_proxy_miss_action = %q, want %q", resp.ReverseProxyMissAction, platform.ReverseProxyMissActionReject)
	}
	if resp.AllocationPolicy != string(platform.AllocationPolicyPreferIdleIP) {
		t.Fatalf("response allocation_policy = %q, want %q", resp.AllocationPolicy, platform.AllocationPolicyPreferIdleIP)
	}

	stored, err := engine.GetPlatform(platform.DefaultPlatformID)
	if err != nil {
		t.Fatalf("GetPlatform: %v", err)
	}
	if stored.Name != platform.DefaultPlatformName {
		t.Fatalf("stored name = %q, want %q", stored.Name, platform.DefaultPlatformName)
	}
	if stored.StickyTTLNs != int64(45*time.Minute) {
		t.Fatalf("stored sticky_ttl_ns = %d, want %d", stored.StickyTTLNs, int64(45*time.Minute))
	}
	if !reflect.DeepEqual(stored.RegexFilters, []string{"^prod-"}) {
		t.Fatalf("stored regex_filters = %v, want %v", stored.RegexFilters, []string{"^prod-"})
	}
	if !reflect.DeepEqual(stored.RegionFilters, []string{"jp"}) {
		t.Fatalf("stored region_filters = %v, want %v", stored.RegionFilters, []string{"jp"})
	}
	if stored.ReverseProxyMissAction != string(platform.ReverseProxyMissActionReject) {
		t.Fatalf("stored reverse_proxy_miss_action = %q, want %q", stored.ReverseProxyMissAction, platform.ReverseProxyMissActionReject)
	}
	if stored.AllocationPolicy != string(platform.AllocationPolicyPreferIdleIP) {
		t.Fatalf("stored allocation_policy = %q, want %q", stored.AllocationPolicy, platform.AllocationPolicyPreferIdleIP)
	}

	plat, ok := pool.GetPlatform(platform.DefaultPlatformID)
	if !ok {
		t.Fatalf("platform %s should remain in pool", platform.DefaultPlatformID)
	}
	if plat.Name != platform.DefaultPlatformName {
		t.Fatalf("pool platform name = %q, want %q", plat.Name, platform.DefaultPlatformName)
	}
	if plat.StickyTTLNs != int64(45*time.Minute) {
		t.Fatalf("pool sticky_ttl_ns = %d, want %d", plat.StickyTTLNs, int64(45*time.Minute))
	}
	if len(plat.RegexFilters) != 1 || plat.RegexFilters[0].String() != "^prod-" {
		t.Fatalf("pool regex_filters = %v, want [%q]", plat.RegexFilters, "^prod-")
	}
	if !reflect.DeepEqual(plat.RegionFilters, []string{"jp"}) {
		t.Fatalf("pool region_filters = %v, want %v", plat.RegionFilters, []string{"jp"})
	}
	if plat.ReverseProxyMissAction != string(platform.ReverseProxyMissActionReject) {
		t.Fatalf("pool reverse_proxy_miss_action = %q, want %q", plat.ReverseProxyMissAction, platform.ReverseProxyMissActionReject)
	}
	if plat.AllocationPolicy != platform.AllocationPolicyPreferIdleIP {
		t.Fatalf("pool allocation_policy = %q, want %q", plat.AllocationPolicy, platform.AllocationPolicyPreferIdleIP)
	}
}

func TestResetPlatformToDefault_DoesNotDecodeCorruptPersistedFiltersJSON(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cacheDir := filepath.Join(dir, "cache")

	engine, closer, err := state.PersistenceBootstrap(stateDir, cacheDir)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	platformRow := model.Platform{
		ID:                     "plat-reset-corrupt",
		Name:                   "reset-corrupt",
		StickyTTLNs:            int64(time.Hour),
		RegexFilters:           []string{`^ok$`},
		RegionFilters:          []string{"us"},
		ReverseProxyMissAction: "TREAT_AS_EMPTY",
		AllocationPolicy:       "BALANCED",
		UpdatedAtNs:            time.Now().UnixNano(),
	}
	if err := engine.UpsertPlatform(platformRow); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}
	db, err := state.OpenDB(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatalf("OpenDB(state.db): %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE platforms SET regex_filters_json = ? WHERE id = ?`,
		`{"bad":"shape"}`,
		platformRow.ID,
	); err != nil {
		t.Fatalf("corrupt platform row: %v", err)
	}

	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              nil,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})
	pool.RegisterPlatform(platform.NewConfiguredPlatform(
		platformRow.ID,
		platformRow.Name,
		nil,
		nil,
		nil,
		nil,
		platformRow.StickyTTLNs,
		platformRow.ReverseProxyMissAction,
		string(platform.ReverseProxyEmptyAccountBehaviorAccountHeaderRule),
		"",
		platformRow.AllocationPolicy,
		true,
	))

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              45 * time.Minute,
			DefaultPlatformRegexFilters:           []string{"^prod-"},
			DefaultPlatformRegionFilters:          []string{"jp"},
			DefaultPlatformReverseProxyMissAction: "REJECT",
			DefaultPlatformAllocationPolicy:       "PREFER_IDLE_IP",
		},
	}

	resp, err := cp.ResetPlatformToDefault(platformRow.ID)
	if err != nil {
		t.Fatalf("ResetPlatformToDefault: %v", err)
	}
	if resp.Name != platformRow.Name {
		t.Fatalf("response name = %q, want %q", resp.Name, platformRow.Name)
	}
	if resp.StickyTTL != (45 * time.Minute).String() {
		t.Fatalf("response sticky_ttl = %q, want %q", resp.StickyTTL, (45 * time.Minute).String())
	}
	if !reflect.DeepEqual(resp.RegexFilters, []string{"^prod-"}) {
		t.Fatalf("response regex_filters = %v, want %v", resp.RegexFilters, []string{"^prod-"})
	}
	if !reflect.DeepEqual(resp.RegionFilters, []string{"jp"}) {
		t.Fatalf("response region_filters = %v, want %v", resp.RegionFilters, []string{"jp"})
	}
	if resp.ReverseProxyMissAction != "REJECT" {
		t.Fatalf("response reverse_proxy_miss_action = %q, want REJECT", resp.ReverseProxyMissAction)
	}
	if resp.AllocationPolicy != "PREFER_IDLE_IP" {
		t.Fatalf("response allocation_policy = %q, want PREFER_IDLE_IP", resp.AllocationPolicy)
	}

	stored, err := engine.GetPlatform(platformRow.ID)
	if err != nil {
		t.Fatalf("GetPlatform: %v", err)
	}
	storedResp := platformToResponse(*stored)
	if !reflect.DeepEqual(storedResp.RegexFilters, []string{"^prod-"}) {
		t.Fatalf("stored regex_filters = %v, want %v", storedResp.RegexFilters, []string{"^prod-"})
	}
	if !reflect.DeepEqual(storedResp.RegionFilters, []string{"jp"}) {
		t.Fatalf("stored region_filters = %v, want %v", storedResp.RegionFilters, []string{"jp"})
	}
}

func TestResetPlatformToDefault_InvalidPersistedPlatformNameReturnsInvalidArgument(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cacheDir := filepath.Join(dir, "cache")

	engine, closer, err := state.PersistenceBootstrap(stateDir, cacheDir)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	platformRow := model.Platform{
		ID:                     "plat-reset-invalid-name",
		Name:                   "valid-name",
		StickyTTLNs:            int64(time.Hour),
		RegexFilters:           []string{},
		RegionFilters:          []string{},
		ReverseProxyMissAction: "TREAT_AS_EMPTY",
		AllocationPolicy:       "BALANCED",
		UpdatedAtNs:            time.Now().UnixNano(),
	}
	if err := engine.UpsertPlatform(platformRow); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}

	db, err := state.OpenDB(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatalf("OpenDB(state.db): %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE platforms SET name = ? WHERE id = ?`, "bad:name", platformRow.ID); err != nil {
		t.Fatalf("corrupt platform name row: %v", err)
	}

	cp := &ControlPlaneService{
		Engine: engine,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              45 * time.Minute,
			DefaultPlatformRegexFilters:           []string{"^prod-"},
			DefaultPlatformRegionFilters:          []string{"jp"},
			DefaultPlatformReverseProxyMissAction: "REJECT",
			DefaultPlatformAllocationPolicy:       "PREFER_IDLE_IP",
		},
	}

	_, err = cp.ResetPlatformToDefault(platformRow.ID)
	if err == nil {
		t.Fatal("expected ResetPlatformToDefault to fail for invalid persisted platform name")
	}
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T: %v", err, err)
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Fatalf("service error code = %q, want %q", svcErr.Code, "INVALID_ARGUMENT")
	}
	if !strings.Contains(svcErr.Message, "name:") {
		t.Fatalf("service error message = %q, expected to mention name", svcErr.Message)
	}
}

func TestListAccountHeaderRules_FailsFastOnCorruptPersistedHeadersColumn(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cacheDir := filepath.Join(dir, "cache")

	engine, closer, err := state.PersistenceBootstrap(stateDir, cacheDir)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	rule := model.AccountHeaderRule{
		URLPrefix:   "api.example.com/v1",
		Headers:     []string{"Authorization"},
		UpdatedAtNs: time.Now().UnixNano(),
	}
	if _, err := engine.UpsertAccountHeaderRuleWithCreated(rule); err != nil {
		t.Fatalf("UpsertAccountHeaderRuleWithCreated: %v", err)
	}

	db, err := state.OpenDB(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatalf("OpenDB(state.db): %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE account_header_rules SET headers_json = ? WHERE url_prefix = ?`,
		`{"bad":"shape"}`,
		rule.URLPrefix,
	); err != nil {
		t.Fatalf("corrupt account_header_rules row: %v", err)
	}

	cp := &ControlPlaneService{Engine: engine}
	_, err = cp.ListAccountHeaderRules()
	if err == nil {
		t.Fatal("expected ListAccountHeaderRules to fail on corrupt headers_json")
	}
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		t.Fatalf("expected ServiceError, got %T (%v)", err, err)
	}
	if serviceErr.Code != "INTERNAL" {
		t.Fatalf("service error code = %q, want INTERNAL", serviceErr.Code)
	}
	if serviceErr.Err == nil || !strings.Contains(serviceErr.Err.Error(), "decode account header rule") {
		t.Fatalf("unexpected wrapped service error: %v", serviceErr.Err)
	}
}

// --- Platform CRUD protocol filter tests ---

func TestCreatePlatform_WithProtocolFilters(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              30 * time.Minute,
			DefaultPlatformRegexFilters:           []string{},
			DefaultPlatformRegionFilters:          []string{},
			DefaultPlatformReverseProxyMissAction: "TREAT_AS_EMPTY",
			DefaultPlatformAllocationPolicy:       "BALANCED",
		},
	}

	name := "proto-cfg"
	created, err := cp.CreatePlatform(CreatePlatformRequest{
		Name:                   &name,
		ProtocolFilters:        []string{"ss", "vmess"}, // aliases should be canonicalized
		ExcludeProtocolFilters: []string{"hysteria2"},
	})
	if err != nil {
		t.Fatalf("CreatePlatform: %v", err)
	}

	// Response should have canonical values.
	if !reflect.DeepEqual(created.ProtocolFilters, []string{"shadowsocks", "vmess"}) {
		t.Fatalf("response protocol_filters = %v, want [shadowsocks vmess]", created.ProtocolFilters)
	}
	if !reflect.DeepEqual(created.ExcludeProtocolFilters, []string{"hysteria2"}) {
		t.Fatalf("response exclude_protocol_filters = %v, want [hysteria2]", created.ExcludeProtocolFilters)
	}

	// Persisted model should also be canonical.
	stored, err := engine.GetPlatform(created.ID)
	if err != nil {
		t.Fatalf("GetPlatform: %v", err)
	}
	if !reflect.DeepEqual(stored.ProtocolFilters, []string{"shadowsocks", "vmess"}) {
		t.Fatalf("stored protocol_filters = %v, want [shadowsocks vmess]", stored.ProtocolFilters)
	}
	if !reflect.DeepEqual(stored.ExcludeProtocolFilters, []string{"hysteria2"}) {
		t.Fatalf("stored exclude_protocol_filters = %v, want [hysteria2]", stored.ExcludeProtocolFilters)
	}

	// Runtime platform should also be canonical.
	plat, ok := pool.GetPlatform(created.ID)
	if !ok {
		t.Fatalf("platform %s not found in pool", created.ID)
	}
	if !reflect.DeepEqual(plat.ProtocolFilters, []string{"shadowsocks", "vmess"}) {
		t.Fatalf("runtime protocol_filters = %v, want [shadowsocks vmess]", plat.ProtocolFilters)
	}
	if !reflect.DeepEqual(plat.ExcludeProtocolFilters, []string{"hysteria2"}) {
		t.Fatalf("runtime exclude_protocol_filters = %v, want [hysteria2]", plat.ExcludeProtocolFilters)
	}
}

func TestCreatePlatform_ProtocolFiltersDedup(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              30 * time.Minute,
			DefaultPlatformRegexFilters:           []string{},
			DefaultPlatformRegionFilters:          []string{},
			DefaultPlatformReverseProxyMissAction: "TREAT_AS_EMPTY",
			DefaultPlatformAllocationPolicy:       "BALANCED",
		},
	}

	name := "proto-dedup"
	created, err := cp.CreatePlatform(CreatePlatformRequest{
		Name:            &name,
		ProtocolFilters: []string{"ss", "shadowsocks", "SS"}, // all normalise to shadowsocks
	})
	if err != nil {
		t.Fatalf("CreatePlatform: %v", err)
	}

	if len(created.ProtocolFilters) != 1 || created.ProtocolFilters[0] != "shadowsocks" {
		t.Fatalf("expected deduplicated [shadowsocks], got %v", created.ProtocolFilters)
	}
}

func TestUpdatePlatform_PatchProtocolFiltersCanonicalizes(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              30 * time.Minute,
			DefaultPlatformRegexFilters:           []string{},
			DefaultPlatformRegionFilters:          []string{},
			DefaultPlatformReverseProxyMissAction: "TREAT_AS_EMPTY",
			DefaultPlatformAllocationPolicy:       "BALANCED",
		},
	}

	name := "patch-proto"
	created, err := cp.CreatePlatform(CreatePlatformRequest{Name: &name})
	if err != nil {
		t.Fatalf("CreatePlatform: %v", err)
	}

	// PATCH protocol_filters with aliases.
	updated, err := cp.UpdatePlatform(created.ID, []byte(`{"protocol_filters":["hy2","ss"]}`))
	if err != nil {
		t.Fatalf("UpdatePlatform: %v", err)
	}

	if !reflect.DeepEqual(updated.ProtocolFilters, []string{"hysteria2", "shadowsocks"}) {
		t.Fatalf("patched protocol_filters = %v, want [hysteria2 shadowsocks]", updated.ProtocolFilters)
	}

	// PATCH exclude_protocol_filters with alias.
	updated2, err := cp.UpdatePlatform(created.ID, []byte(`{"exclude_protocol_filters":["vmess1"]}`))
	if err != nil {
		t.Fatalf("UpdatePlatform (exclude): %v", err)
	}
	if !reflect.DeepEqual(updated2.ExcludeProtocolFilters, []string{"vmess"}) {
		t.Fatalf("patched exclude_protocol_filters = %v, want [vmess]", updated2.ExcludeProtocolFilters)
	}

	// PATCH protocol_filters to empty (clear).
	cleared, err := cp.UpdatePlatform(created.ID, []byte(`{"protocol_filters":[]}`))
	if err != nil {
		t.Fatalf("UpdatePlatform (clear): %v", err)
	}
	if len(cleared.ProtocolFilters) != 0 {
		t.Fatalf("expected empty protocol_filters, got %v", cleared.ProtocolFilters)
	}
}

func TestUpdatePlatform_PatchQualityFiltersCanClear(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              30 * time.Minute,
			DefaultPlatformRegexFilters:           []string{},
			DefaultPlatformRegionFilters:          []string{},
			DefaultPlatformReverseProxyMissAction: "TREAT_AS_EMPTY",
			DefaultPlatformAllocationPolicy:       "BALANCED",
		},
	}

	name := "patch-quality"
	cf := true
	created, err := cp.CreatePlatform(CreatePlatformRequest{
		Name:                         &name,
		QualityGrade:                 "A",
		QualityMinScore:              80,
		QualityCloudflareChallenged:  &cf,
		QualityCheckedSinceNs:        123456789,
		QualityProfile:               "openai",
	})
	if err != nil {
		t.Fatalf("CreatePlatform: %v", err)
	}
	if created.QualityCloudflareChallenged == nil || !*created.QualityCloudflareChallenged {
		t.Fatalf("created quality_cloudflare_challenged = %v, want true", created.QualityCloudflareChallenged)
	}

	cleared, err := cp.UpdatePlatform(created.ID, []byte(`{
		"quality_grade":"",
		"quality_min_score":0,
		"quality_cloudflare_challenged":null,
		"quality_checked_since_ns":0,
		"quality_profile":""
	}`))
	if err != nil {
		t.Fatalf("UpdatePlatform clear quality filters: %v", err)
	}
	if cleared.QualityGrade != "" {
		t.Fatalf("quality_grade = %q, want empty", cleared.QualityGrade)
	}
	if cleared.QualityMinScore != 0 {
		t.Fatalf("quality_min_score = %v, want 0", cleared.QualityMinScore)
	}
	if cleared.QualityCloudflareChallenged != nil {
		t.Fatalf("quality_cloudflare_challenged = %v, want nil", cleared.QualityCloudflareChallenged)
	}
	if cleared.QualityCheckedSinceNs != 0 {
		t.Fatalf("quality_checked_since_ns = %v, want 0", cleared.QualityCheckedSinceNs)
	}
	if cleared.QualityProfile != "" {
		t.Fatalf("quality_profile = %q, want empty", cleared.QualityProfile)
	}
}

func TestCreatePlatform_RejectsInvalidProtocolFilter(t *testing.T) {
	dir := t.TempDir()
	engine, closer, err := state.PersistenceBootstrap(
		filepath.Join(dir, "state"),
		filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	subMgr := topology.NewSubscriptionManager()
	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		SubLookup:              subMgr.Lookup,
		GeoLookup:              func(netip.Addr) string { return "us" },
		MaxLatencyTableEntries: 16,
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return 10 * time.Minute },
	})

	cp := &ControlPlaneService{
		Engine: engine,
		Pool:   pool,
		SubMgr: subMgr,
		EnvCfg: &config.EnvConfig{
			DefaultPlatformStickyTTL:              30 * time.Minute,
			DefaultPlatformRegexFilters:           []string{},
			DefaultPlatformRegionFilters:          []string{},
			DefaultPlatformReverseProxyMissAction: "TREAT_AS_EMPTY",
			DefaultPlatformAllocationPolicy:       "BALANCED",
		},
	}

	name := "bad-proto"
	_, err = cp.CreatePlatform(CreatePlatformRequest{
		Name:            &name,
		ProtocolFilters: []string{"bogus_protocol"},
	})
	if err == nil {
		t.Fatal("expected error for invalid protocol filter")
	}
	var svcErr *ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected ServiceError, got %T", err)
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Fatalf("expected INVALID_ARGUMENT, got %s", svcErr.Code)
	}
	if !strings.Contains(svcErr.Message, "protocol_filters[0]") {
		t.Fatalf("error message = %q, expected to mention protocol_filters[0]", svcErr.Message)
	}
}

// TestDeriveCloudflareStatus verifies the CF status derivation.
func TestDeriveCloudflareStatus(t *testing.T) {
	tests := []struct {
		name     string
		quality  *model.NodeQuality
		want     string
	}{
		{
			name: "challenged",
			quality: &model.NodeQuality{
				ServiceReachable:     true,
				CloudflareChallenged: true,
			},
			want: "challenged",
		},
		{
			name: "clean",
			quality: &model.NodeQuality{
				ServiceReachable:     true,
				CloudflareChallenged: false,
			},
			want: "clean",
		},
		{
			name: "ng_service_unreachable",
			quality: &model.NodeQuality{
				ServiceReachable:     false,
				CloudflareChallenged: false,
			},
			want: "ng",
		},
		{
			name: "ng_unreachable_despite_challenge_flag",
			quality: &model.NodeQuality{
				ServiceReachable:     false,
				CloudflareChallenged: false,
			},
			want: "ng",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveCloudflareStatus(tt.quality)
			if got != tt.want {
				t.Fatalf("deriveCloudflareStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestValidateQualityGrade verifies quality grade validation.
func TestValidateQualityGrade(t *testing.T) {
	if err := validateQualityGrade(""); err != nil {
		t.Fatalf("empty should be valid: %v", err)
	}
	for _, g := range []string{"A", "B", "C", "D", "F"} {
		if err := validateQualityGrade(g); err != nil {
			t.Fatalf("grade %q should be valid: %v", g, err)
		}
	}
	if err := validateQualityGrade("E"); err == nil {
		t.Fatal("grade E should be invalid")
	}
	if err := validateQualityGrade("G"); err == nil {
		t.Fatal("grade G should be invalid")
	}
	if err := validateQualityGrade("AA"); err == nil {
		t.Fatal("grade AA should be invalid")
	}
}

// TestValidateQualityMinScore verifies quality min score validation.
func TestValidateQualityMinScore(t *testing.T) {
	if err := validateQualityMinScore(0); err != nil {
		t.Fatalf("score 0 should be valid: %v", err)
	}
	if err := validateQualityMinScore(50); err != nil {
		t.Fatalf("score 50 should be valid: %v", err)
	}
	if err := validateQualityMinScore(100); err != nil {
		t.Fatalf("score 100 should be valid: %v", err)
	}
	if err := validateQualityMinScore(-1); err == nil {
		t.Fatal("score -1 should be invalid")
	}
	if err := validateQualityMinScore(101); err == nil {
		t.Fatal("score 101 should be invalid")
	}
}

// TestValidateQualityCheckedSinceNs verifies checked-since validation.
func TestValidateQualityCheckedSinceNs(t *testing.T) {
	if err := validateQualityCheckedSinceNs(0); err != nil {
		t.Fatalf("0 should be valid: %v", err)
	}
	if err := validateQualityCheckedSinceNs(1000); err != nil {
		t.Fatalf("1000 should be valid: %v", err)
	}
	if err := validateQualityCheckedSinceNs(-1); err == nil {
		t.Fatal("-1 should be invalid")
	}
}

// TestValidateQualityProfile verifies quality profile validation.
func TestValidateQualityProfile(t *testing.T) {
	if err := validateQualityProfile(""); err != nil {
		t.Fatalf("empty should be valid: %v", err)
	}
	if err := validateQualityProfile("generic"); err != nil {
		t.Fatalf("generic should be valid: %v", err)
	}
	if err := validateQualityProfile("nonexistent"); err == nil {
		t.Fatal("nonexistent profile should be invalid")
	}
}
