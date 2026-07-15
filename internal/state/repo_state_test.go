package state

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/platform"
)

// helper: create a state.db in a temp dir, init DDL, return StateRepo + cleanup.
func newTestStateRepo(t *testing.T) *StateRepo {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateStateDB(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return newStateRepo(db)
}

func TestMigrateStateDB_UpgradesLegacyPlatformsColumns(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Simulate a legacy platforms schema without newly added columns.
	_, err = db.Exec(`
		CREATE TABLE platforms (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			sticky_ttl_ns INTEGER NOT NULL,
			regex_filters_json TEXT NOT NULL DEFAULT '[]',
			region_filters_json TEXT NOT NULL DEFAULT '[]',
			reverse_proxy_miss_action TEXT NOT NULL DEFAULT 'RANDOM',
			allocation_policy TEXT NOT NULL DEFAULT 'BALANCED',
			updated_at_ns INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create legacy platforms table: %v", err)
	}

	if err := MigrateStateDB(db); err != nil {
		t.Fatalf("MigrateStateDB: %v", err)
	}

	if ok, err := hasTableColumn(db, "platforms", "reverse_proxy_empty_account_behavior"); err != nil || !ok {
		t.Fatalf("expected migrated column reverse_proxy_empty_account_behavior, ok=%v err=%v", ok, err)
	}
	if ok, err := hasTableColumn(db, "platforms", "reverse_proxy_fixed_account_header"); err != nil || !ok {
		t.Fatalf("expected migrated column reverse_proxy_fixed_account_header, ok=%v err=%v", ok, err)
	}
	if ok, err := hasTableColumn(db, "platforms", "passive_circuit_breaker_disabled"); err != nil || !ok {
		t.Fatalf("expected migrated column passive_circuit_breaker_disabled, ok=%v err=%v", ok, err)
	}
}

func TestMigrateStateDB_LegacyBaselineAdvancesToLatest(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE platforms (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			sticky_ttl_ns INTEGER NOT NULL,
			regex_filters_json TEXT NOT NULL DEFAULT '[]',
			region_filters_json TEXT NOT NULL DEFAULT '[]',
			reverse_proxy_miss_action TEXT NOT NULL DEFAULT 'RANDOM',
			reverse_proxy_empty_account_behavior TEXT NOT NULL DEFAULT 'RANDOM',
			reverse_proxy_fixed_account_header TEXT NOT NULL DEFAULT '',
			allocation_policy TEXT NOT NULL DEFAULT 'BALANCED',
			updated_at_ns INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create legacy latest-like platforms table: %v", err)
	}

	if err := MigrateStateDB(db); err != nil {
		t.Fatalf("MigrateStateDB: %v", err)
	}

	var version int
	var dirty bool
	err = db.QueryRow("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if dirty {
		t.Fatalf("schema_migrations dirty=true")
	}
	if version != stateVersionAddQualityCloudflareStatuses {
		t.Fatalf("schema_migrations version: got %d, want %d", version, stateVersionAddQualityCloudflareStatuses)
	}
	if ok, err := hasTableColumn(db, "subscriptions", "incremental_alive_nodes"); err != nil || !ok {
		t.Fatalf("expected migrated column subscriptions.incremental_alive_nodes, ok=%v err=%v", ok, err)
	}
	if ok, err := hasTableColumn(db, "platforms", "passive_circuit_breaker_disabled"); err != nil || !ok {
		t.Fatalf("expected migrated column platforms.passive_circuit_breaker_disabled, ok=%v err=%v", ok, err)
	}
	if ok, err := hasTableColumn(db, "subscriptions", "clash_fingerprint_policy"); err != nil || !ok {
		t.Fatalf("expected migrated column subscriptions.clash_fingerprint_policy, ok=%v err=%v", ok, err)
	}
}

func TestMigrateStateDB_LegacyV8AdvancesToLatest(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Simulate a database at migration 000008 (has protocol_filters_json,
	// exclude_protocol_filters_json) but WITHOUT 000009's clash_fingerprint_policy
	// and WITHOUT a schema_migrations table — this is the legacy v8 shape.
	_, err = db.Exec(`
		CREATE TABLE platforms (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			sticky_ttl_ns INTEGER NOT NULL,
			regex_filters_json TEXT NOT NULL DEFAULT '[]',
			region_filters_json TEXT NOT NULL DEFAULT '[]',
			reverse_proxy_miss_action TEXT NOT NULL DEFAULT 'RANDOM',
			reverse_proxy_empty_account_behavior TEXT NOT NULL DEFAULT 'RANDOM',
			reverse_proxy_fixed_account_header TEXT NOT NULL DEFAULT '',
			passive_circuit_breaker_disabled INTEGER NOT NULL DEFAULT 0,
			protocol_filters_json TEXT NOT NULL DEFAULT '[]',
			exclude_protocol_filters_json TEXT NOT NULL DEFAULT '[]',
			allocation_policy TEXT NOT NULL DEFAULT 'BALANCED',
			updated_at_ns INTEGER NOT NULL
		);
		CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			source_type TEXT NOT NULL DEFAULT 'remote',
			url TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			update_interval_ns INTEGER NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			ephemeral INTEGER NOT NULL DEFAULT 0,
			ephemeral_node_evict_delay_ns INTEGER NOT NULL,
			incremental_alive_nodes INTEGER NOT NULL DEFAULT 0,
			created_at_ns INTEGER NOT NULL,
			updated_at_ns INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create legacy v8 tables: %v", err)
	}

	if err := MigrateStateDB(db); err != nil {
		t.Fatalf("MigrateStateDB: %v", err)
	}

	// Must end at the latest state migration version.
	var version int
	var dirty bool
	err = db.QueryRow("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if dirty {
		t.Fatalf("schema_migrations dirty=true")
	}
	if version != stateVersionAddQualityCloudflareStatuses {
		t.Fatalf("schema_migrations version: got %d, want %d", version, stateVersionAddQualityCloudflareStatuses)
	}

	// v8 columns must still be present.
	if ok, err := hasTableColumn(db, "platforms", "protocol_filters_json"); err != nil || !ok {
		t.Fatalf("expected column platforms.protocol_filters_json, ok=%v err=%v", ok, err)
	}
	if ok, err := hasTableColumn(db, "platforms", "exclude_protocol_filters_json"); err != nil || !ok {
		t.Fatalf("expected column platforms.exclude_protocol_filters_json, ok=%v err=%v", ok, err)
	}

	// v9 column must now exist.
	if ok, err := hasTableColumn(db, "subscriptions", "clash_fingerprint_policy"); err != nil || !ok {
		t.Fatalf("expected migrated column subscriptions.clash_fingerprint_policy, ok=%v err=%v", ok, err)
	}
}

func TestMigrateStateDB_AddsIncrementalAliveNodesToLegacySubscriptions(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE platforms (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			sticky_ttl_ns INTEGER NOT NULL,
			regex_filters_json TEXT NOT NULL DEFAULT '[]',
			region_filters_json TEXT NOT NULL DEFAULT '[]',
			reverse_proxy_miss_action TEXT NOT NULL DEFAULT 'RANDOM',
			reverse_proxy_empty_account_behavior TEXT NOT NULL DEFAULT 'RANDOM',
			reverse_proxy_fixed_account_header TEXT NOT NULL DEFAULT '',
			allocation_policy TEXT NOT NULL DEFAULT 'BALANCED',
			updated_at_ns INTEGER NOT NULL
		);

		CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			source_type TEXT NOT NULL DEFAULT 'remote',
			url TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			update_interval_ns INTEGER NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			ephemeral INTEGER NOT NULL DEFAULT 0,
			ephemeral_node_evict_delay_ns INTEGER NOT NULL,
			created_at_ns INTEGER NOT NULL,
			updated_at_ns INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create legacy platforms and subscriptions tables: %v", err)
	}

	if err := MigrateStateDB(db); err != nil {
		t.Fatalf("MigrateStateDB: %v", err)
	}

	if ok, err := hasTableColumn(db, "subscriptions", "incremental_alive_nodes"); err != nil || !ok {
		t.Fatalf("expected migrated column subscriptions.incremental_alive_nodes, ok=%v err=%v", ok, err)
	}

	var version int
	var dirty bool
	if err := db.QueryRow("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if dirty {
		t.Fatalf("schema_migrations dirty=true")
	}
	if version != stateVersionAddQualityCloudflareStatuses {
		t.Fatalf("schema_migrations version: got %d, want %d", version, stateVersionAddQualityCloudflareStatuses)
	}
	if ok, err := hasTableColumn(db, "platforms", "passive_circuit_breaker_disabled"); err != nil || !ok {
		t.Fatalf("expected migrated column platforms.passive_circuit_breaker_disabled, ok=%v err=%v", ok, err)
	}
	if ok, err := hasTableColumn(db, "subscriptions", "clash_fingerprint_policy"); err != nil || !ok {
		t.Fatalf("expected migrated column subscriptions.clash_fingerprint_policy, ok=%v err=%v", ok, err)
	}
}

func TestMigrateStateDB_NormalizesLegacyRandomMissAction(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE platforms (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			sticky_ttl_ns INTEGER NOT NULL,
			regex_filters_json TEXT NOT NULL DEFAULT '[]',
			region_filters_json TEXT NOT NULL DEFAULT '[]',
			reverse_proxy_miss_action TEXT NOT NULL DEFAULT 'RANDOM',
			reverse_proxy_empty_account_behavior TEXT NOT NULL DEFAULT 'RANDOM',
			reverse_proxy_fixed_account_header TEXT NOT NULL DEFAULT '',
			allocation_policy TEXT NOT NULL DEFAULT 'BALANCED',
			updated_at_ns INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create legacy latest-like platforms table: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO platforms (
			id, name, sticky_ttl_ns, regex_filters_json, region_filters_json,
			reverse_proxy_miss_action, reverse_proxy_empty_account_behavior,
			reverse_proxy_fixed_account_header, allocation_policy, updated_at_ns
		)
		VALUES
			('p-random', 'LegacyRandom', 1, '[]', '[]', 'RANDOM', 'RANDOM', '', 'BALANCED', 1),
			('p-reject', 'LegacyReject', 1, '[]', '[]', 'REJECT', 'RANDOM', '', 'BALANCED', 1)
	`)
	if err != nil {
		t.Fatalf("seed legacy platforms: %v", err)
	}

	if err := MigrateStateDB(db); err != nil {
		t.Fatalf("MigrateStateDB: %v", err)
	}

	var randomMissAction string
	if err := db.QueryRow(`SELECT reverse_proxy_miss_action FROM platforms WHERE id='p-random'`).Scan(&randomMissAction); err != nil {
		t.Fatalf("query random miss action: %v", err)
	}
	if randomMissAction != "TREAT_AS_EMPTY" {
		t.Fatalf("random miss action: got %q, want %q", randomMissAction, "TREAT_AS_EMPTY")
	}

	var rejectMissAction string
	if err := db.QueryRow(`SELECT reverse_proxy_miss_action FROM platforms WHERE id='p-reject'`).Scan(&rejectMissAction); err != nil {
		t.Fatalf("query reject miss action: %v", err)
	}
	if rejectMissAction != "REJECT" {
		t.Fatalf("reject miss action: got %q, want %q", rejectMissAction, "REJECT")
	}

	var version int
	var dirty bool
	if err := db.QueryRow("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if dirty {
		t.Fatalf("schema_migrations dirty=true")
	}
	if version != stateVersionAddQualityCloudflareStatuses {
		t.Fatalf("schema_migrations version: got %d, want %d", version, stateVersionAddQualityCloudflareStatuses)
	}
	if ok, err := hasTableColumn(db, "subscriptions", "incremental_alive_nodes"); err != nil || !ok {
		t.Fatalf("expected migrated column subscriptions.incremental_alive_nodes, ok=%v err=%v", ok, err)
	}
	if ok, err := hasTableColumn(db, "platforms", "passive_circuit_breaker_disabled"); err != nil || !ok {
		t.Fatalf("expected migrated column platforms.passive_circuit_breaker_disabled, ok=%v err=%v", ok, err)
	}
	if ok, err := hasTableColumn(db, "subscriptions", "clash_fingerprint_policy"); err != nil || !ok {
		t.Fatalf("expected migrated column subscriptions.clash_fingerprint_policy, ok=%v err=%v", ok, err)
	}
}

// --- system_config ---

func TestStateRepo_SystemConfig_RoundTrip(t *testing.T) {
	repo := newTestStateRepo(t)

	// Initially empty.
	cfg, ver, err := repo.GetSystemConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil || ver != 0 {
		t.Fatalf("expected nil config and version 0, got %v, %d", cfg, ver)
	}

	// Save.
	c := config.NewDefaultRuntimeConfig()
	c.MaxConsecutiveFailures = 7
	now := time.Now().UnixNano()
	if err := repo.SaveSystemConfig(c, 1, now); err != nil {
		t.Fatal(err)
	}

	// Read back.
	cfg, ver, err = repo.GetSystemConfig()
	if err != nil {
		t.Fatal(err)
	}
	if ver != 1 {
		t.Fatalf("expected version 1, got %d", ver)
	}
	if cfg.MaxConsecutiveFailures != 7 {
		t.Fatalf("expected max_consecutive_failures 7, got %d", cfg.MaxConsecutiveFailures)
	}

	// Upsert (idempotent, bump version).
	c.MaxConsecutiveFailures = 11
	if err := repo.SaveSystemConfig(c, 2, now+1); err != nil {
		t.Fatal(err)
	}
	cfg, ver, err = repo.GetSystemConfig()
	if err != nil {
		t.Fatal(err)
	}
	if ver != 2 || cfg.MaxConsecutiveFailures != 11 {
		t.Fatalf("expected version 2 + max_consecutive_failures 11, got %d + %d", ver, cfg.MaxConsecutiveFailures)
	}
}

// --- platforms ---

func TestStateRepo_Platforms_CRUD(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	p := model.Platform{
		ID: "plat-1", Name: "Default", StickyTTLNs: 1000,
		RegexFilters: []string{}, RegionFilters: []string{},
		ReverseProxyMissAction: "TREAT_AS_EMPTY", AllocationPolicy: "BALANCED",
		PassiveCircuitBreakerDisabled: true,
		UpdatedAtNs:                   now,
	}
	if err := repo.UpsertPlatform(p); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetPlatform("plat-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Default" {
		t.Fatalf("unexpected get result: %+v", got)
	}
	if got.ReverseProxyEmptyAccountBehavior != "RANDOM" {
		t.Fatalf(
			"unexpected reverse_proxy_empty_account_behavior: got %q, want %q",
			got.ReverseProxyEmptyAccountBehavior,
			"RANDOM",
		)
	}
	if !got.PassiveCircuitBreakerDisabled {
		t.Fatal("expected passive_circuit_breaker_disabled to round-trip true")
	}

	// List.
	list, err := repo.ListPlatforms()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "Default" {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Idempotent upsert (update same ID).
	p.Name = "Default-Renamed"
	p.PassiveCircuitBreakerDisabled = false
	if err := repo.UpsertPlatform(p); err != nil {
		t.Fatal(err)
	}
	list, err = repo.ListPlatforms()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "Default-Renamed" {
		t.Fatalf("expected renamed platform, got %+v", list)
	}
	if list[0].PassiveCircuitBreakerDisabled {
		t.Fatal("expected passive_circuit_breaker_disabled to update to false")
	}

	// Delete.
	if err := repo.DeletePlatform("plat-1"); err != nil {
		t.Fatal(err)
	}
	list, err = repo.ListPlatforms()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list after delete, got %+v", list)
	}
	if _, err := repo.GetPlatform("plat-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestStateRepo_Platform_ValidationFixedHeaderBehavior(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	base := model.Platform{
		ID: "plat-fixed-header", Name: "FixedHeader", StickyTTLNs: 1000,
		RegexFilters: []string{}, RegionFilters: []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "FIXED_HEADER",
		AllocationPolicy:                 "BALANCED",
		UpdatedAtNs:                      now,
	}

	if err := repo.UpsertPlatform(base); err == nil {
		t.Fatal("expected error when fixed-header behavior has empty header")
	}

	base.ReverseProxyFixedAccountHeader = "x-account-id\nauthorization\nX-Account-Id"
	if err := repo.UpsertPlatform(base); err != nil {
		t.Fatalf("expected fixed-header behavior to accept valid header, got %v", err)
	}

	got, err := repo.GetPlatform(base.ID)
	if err != nil {
		t.Fatalf("GetPlatform: %v", err)
	}
	if got.ReverseProxyFixedAccountHeader != "X-Account-Id\nAuthorization" {
		t.Fatalf(
			"fixed header canonicalization mismatch: got %q, want %q",
			got.ReverseProxyFixedAccountHeader,
			"X-Account-Id\nAuthorization",
		)
	}
}

func TestStateRepo_Platform_NameUniqueViolation(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	p1 := model.Platform{
		ID: "plat-1", Name: "SameName", StickyTTLNs: 1000,
		RegexFilters: []string{}, RegionFilters: []string{},
		ReverseProxyMissAction: "TREAT_AS_EMPTY", AllocationPolicy: "BALANCED",
		UpdatedAtNs: now,
	}
	if err := repo.UpsertPlatform(p1); err != nil {
		t.Fatal(err)
	}

	// Different ID, same name → should fail with ErrConflict.
	p2 := p1
	p2.ID = "plat-2"
	err := repo.UpsertPlatform(p2)
	if err == nil {
		t.Fatal("expected ErrConflict for same name with different ID")
	}
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Original should still exist untouched.
	list, _ := repo.ListPlatforms()
	if len(list) != 1 || list[0].ID != "plat-1" {
		t.Fatalf("expected original plat-1 to survive, got %+v", list)
	}
}

func TestStateRepo_Platform_ValidationRejectsInvalidRegex(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	base := model.Platform{
		ID: "plat-1", Name: "Test", StickyTTLNs: 1000,
		RegexFilters: []string{}, RegionFilters: []string{},
		ReverseProxyMissAction: "TREAT_AS_EMPTY", AllocationPolicy: "BALANCED",
		UpdatedAtNs: now,
	}

	// Uncompilable regex.
	bad := base
	bad.RegexFilters = []string{"(unclosed"}
	if err := repo.UpsertPlatform(bad); err == nil {
		t.Fatal("expected error for uncompilable regex")
	}

	// Invalid region_filters.
	bad = base
	bad.RegionFilters = []string{""}
	if err := repo.UpsertPlatform(bad); err == nil {
		t.Fatal("expected error for invalid region_filters")
	}

	// Valid config should still succeed.
	base.RegexFilters = []string{"^ss$", "vmess"}
	base.RegionFilters = []string{"us", "jp"}
	if err := repo.UpsertPlatform(base); err != nil {
		t.Fatalf("valid platform rejected: %v", err)
	}

	// DB should have exactly 1 platform.
	list, _ := repo.ListPlatforms()
	if len(list) != 1 {
		t.Fatalf("expected 1 platform, got %d", len(list))
	}
}

func TestStateRepo_Platform_ValidationRejectsInvalidName(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	tests := []string{
		"bad:name",
		"api",
	}
	for i, name := range tests {
		bad := model.Platform{
			ID:                     fmt.Sprintf("plat-%d", i+1),
			Name:                   name,
			StickyTTLNs:            1000,
			RegexFilters:           []string{},
			RegionFilters:          []string{},
			ReverseProxyMissAction: "TREAT_AS_EMPTY",
			AllocationPolicy:       "BALANCED",
			UpdatedAtNs:            now,
		}
		if err := repo.UpsertPlatform(bad); err == nil {
			t.Fatalf("expected error for invalid platform name %q", name)
		}
	}
}

// --- subscriptions ---

func TestStateRepo_Subscriptions_CRUD(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	s := model.Subscription{
		ID: "sub-1", Name: "MySub", URL: "https://example.com/sub",
		UpdateIntervalNs: int64(30 * time.Second), Enabled: true,
		Ephemeral: false, EphemeralNodeEvictDelayNs: int64(72 * time.Hour), CreatedAtNs: now, UpdatedAtNs: now,
		ClashFingerprintPolicy: "reject",
	}
	if err := repo.UpsertSubscription(s); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].URL != "https://example.com/sub" {
		t.Fatalf("unexpected list: %+v", list)
	}
	if list[0].ClashFingerprintPolicy != "reject" {
		t.Fatalf("expected default clash_fingerprint_policy reject, got %q", list[0].ClashFingerprintPolicy)
	}

	// Update.
	s.URL = "https://example.com/sub-v2"
	s.ClashFingerprintPolicy = "drop_safe"
	if err := repo.UpsertSubscription(s); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.ListSubscriptions()
	if list[0].URL != "https://example.com/sub-v2" {
		t.Fatalf("expected updated URL, got %s", list[0].URL)
	}
	if list[0].ClashFingerprintPolicy != "drop_safe" {
		t.Fatalf("expected clash_fingerprint_policy drop_safe, got %q", list[0].ClashFingerprintPolicy)
	}

	// Delete.
	if err := repo.DeleteSubscription("sub-1"); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.ListSubscriptions()
	if len(list) != 0 {
		t.Fatal("expected empty after delete")
	}
}

func TestStateRepo_Subscription_CreatedAtNsPreserved(t *testing.T) {
	repo := newTestStateRepo(t)
	originalCreatedAt := int64(1000000)

	s := model.Subscription{
		ID: "sub-1", Name: "MySub", URL: "https://example.com",
		UpdateIntervalNs: int64(30 * time.Second), Enabled: true,
		Ephemeral: false, EphemeralNodeEvictDelayNs: int64(72 * time.Hour),
		CreatedAtNs:            originalCreatedAt,
		UpdatedAtNs:            originalCreatedAt,
		ClashFingerprintPolicy: "reject",
	}
	if err := repo.UpsertSubscription(s); err != nil {
		t.Fatal(err)
	}

	// Upsert again with a DIFFERENT created_at_ns — it should be ignored.
	s.CreatedAtNs = int64(9999999)
	s.URL = "https://example.com/v2"
	s.UpdatedAtNs = int64(2000000)
	if err := repo.UpsertSubscription(s); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(list))
	}
	if list[0].CreatedAtNs != originalCreatedAt {
		t.Fatalf("created_at_ns was overwritten: expected %d, got %d", originalCreatedAt, list[0].CreatedAtNs)
	}
	if list[0].URL != "https://example.com/v2" {
		t.Fatalf("URL should have been updated, got %s", list[0].URL)
	}
	if list[0].UpdatedAtNs != int64(2000000) {
		t.Fatalf("updated_at_ns should have been updated, got %d", list[0].UpdatedAtNs)
	}
	if list[0].ClashFingerprintPolicy != "reject" {
		t.Fatalf("expected clash_fingerprint_policy reject, got %q", list[0].ClashFingerprintPolicy)
	}
}

func TestStateRepo_Subscription_LocalSourcePersists(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	s := model.Subscription{
		ID:                        "sub-local",
		Name:                      "LocalSub",
		SourceType:                "local",
		URL:                       "",
		Content:                   "vmess://example",
		UpdateIntervalNs:          int64(time.Hour),
		Enabled:                   true,
		Ephemeral:                 false,
		EphemeralNodeEvictDelayNs: int64(72 * time.Hour),
		CreatedAtNs:               now,
		UpdatedAtNs:               now,
		ClashFingerprintPolicy:    "drop_always",
	}
	if err := repo.UpsertSubscription(s); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(list))
	}
	if list[0].SourceType != "local" {
		t.Fatalf("source_type: got %q, want %q", list[0].SourceType, "local")
	}
	if list[0].Content != "vmess://example" {
		t.Fatalf("content: got %q", list[0].Content)
	}
	if list[0].ClashFingerprintPolicy != "drop_always" {
		t.Fatalf("clash_fingerprint_policy: got %q, want %q", list[0].ClashFingerprintPolicy, "drop_always")
	}
}

// --- account_header_rules ---

func TestStateRepo_AccountHeaderRules_CRUD(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	r := model.AccountHeaderRule{
		URLPrefix: "api.example.com/v1", Headers: []string{"Authorization"}, UpdatedAtNs: now,
	}
	if _, err := repo.UpsertAccountHeaderRuleWithCreated(r); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListAccountHeaderRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || len(list[0].Headers) != 1 || list[0].Headers[0] != "Authorization" {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Update.
	r.Headers = []string{"x-api-key"}
	if _, err := repo.UpsertAccountHeaderRuleWithCreated(r); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.ListAccountHeaderRules()
	if len(list[0].Headers) != 1 || list[0].Headers[0] != "x-api-key" {
		t.Fatalf("expected updated headers, got %v", list[0].Headers)
	}

	// Delete.
	if err := repo.DeleteAccountHeaderRule("api.example.com/v1"); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.ListAccountHeaderRules()
	if len(list) != 0 {
		t.Fatal("expected empty after delete")
	}
}

func TestStateRepo_AccountHeaderRules_UpsertCreatedFlag(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	r := model.AccountHeaderRule{
		URLPrefix:   "api.example.com/v1",
		Headers:     []string{"Authorization"},
		UpdatedAtNs: now,
	}
	created, err := repo.UpsertAccountHeaderRuleWithCreated(r)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected first upsert to report created=true")
	}

	r.Headers = []string{"x-api-key"}
	r.UpdatedAtNs = now + 1
	created, err = repo.UpsertAccountHeaderRuleWithCreated(r)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected second upsert to report created=false")
	}
}

func TestStateRepo_EnsureAccountHeaderRule_InsertsOnlyWhenMissing(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	created, err := repo.EnsureAccountHeaderRule(model.AccountHeaderRule{
		URLPrefix:   "*",
		Headers:     []string{"Authorization", "x-api-key"},
		UpdatedAtNs: now,
	})
	if err != nil {
		t.Fatalf("EnsureAccountHeaderRule first call: %v", err)
	}
	if !created {
		t.Fatal("expected first ensure call to create row")
	}

	created, err = repo.EnsureAccountHeaderRule(model.AccountHeaderRule{
		URLPrefix:   "*",
		Headers:     []string{"X-Should-Not-Overwrite"},
		UpdatedAtNs: now + 1,
	})
	if err != nil {
		t.Fatalf("EnsureAccountHeaderRule second call: %v", err)
	}
	if created {
		t.Fatal("expected second ensure call to skip existing row")
	}

	list, err := repo.ListAccountHeaderRules()
	if err != nil {
		t.Fatalf("ListAccountHeaderRules: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one rule, got %d", len(list))
	}
	if list[0].URLPrefix != "*" {
		t.Fatalf("url_prefix = %q, want %q", list[0].URLPrefix, "*")
	}
	if !reflect.DeepEqual(list[0].Headers, []string{"Authorization", "x-api-key"}) {
		t.Fatalf("headers = %v, want %v", list[0].Headers, []string{"Authorization", "x-api-key"})
	}
}

// --- concurrent writes ---

func TestStateRepo_ConcurrentWrites(t *testing.T) {
	repo := newTestStateRepo(t)
	now := time.Now().UnixNano()

	// Run 20 concurrent platform upserts on different IDs.
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			p := model.Platform{
				ID: "plat-" + itoa(i), Name: "Platform-" + itoa(i),
				StickyTTLNs: 1000, RegexFilters: []string{}, RegionFilters: []string{},
				ReverseProxyMissAction: "TREAT_AS_EMPTY", AllocationPolicy: "BALANCED",
				UpdatedAtNs: now,
			}
			errs <- repo.UpsertPlatform(p)
		}(i)
	}

	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent upsert failed: %v", err)
		}
	}

	list, _ := repo.ListPlatforms()
	if len(list) != 20 {
		t.Fatalf("expected 20 platforms, got %d", len(list))
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

// TestMigrateStateDB_AddsQualityFilterColumns verifies that migration 000010
// creates the quality filter columns on platforms.
func TestMigrateStateDB_AddsQualityFilterColumns(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := MigrateStateDB(db); err != nil {
		t.Fatalf("MigrateStateDB: %v", err)
	}

	for _, col := range []string{"quality_grade", "quality_min_score", "quality_cloudflare_challenged", "quality_checked_since_ns", "quality_profile"} {
		if ok, err := hasTableColumn(db, "platforms", col); err != nil || !ok {
			t.Fatalf("expected column %s, ok=%v err=%v", col, ok, err)
		}
	}
}

// TestPlatformCRUD_WithQualityFilters verifies round-trip of quality filter
// fields including nullable Cloudflare challenged with nil/true/false values.
func TestPlatformCRUD_WithQualityFilters(t *testing.T) {
	repo := newTestStateRepo(t)

	// Create platform with nil CF challenged (no filter).
	p1 := model.Platform{
		ID:                               "plat-q-1",
		Name:                             "Quality-Platform-1",
		StickyTTLNs:                      30000000000,
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "RANDOM",
		AllocationPolicy:                 "BALANCED",
		UpdatedAtNs:                      time.Now().UnixNano(),
		QualityGrade:                     "A",
		QualityMinScore:                  80.0,
		QualityCloudflareChallenged:      nil,
		QualityCheckedSinceNs:            1000000,
		QualityProfile:                   "generic",
	}
	if err := repo.UpsertPlatform(p1); err != nil {
		t.Fatalf("UpsertPlatform: %v", err)
	}
	got1, err := repo.GetPlatform("plat-q-1")
	if err != nil {
		t.Fatalf("GetPlatform: %v", err)
	}
	if got1.QualityGrade != "A" {
		t.Fatalf("QualityGrade = %q, want A", got1.QualityGrade)
	}
	if got1.QualityMinScore != 80.0 {
		t.Fatalf("QualityMinScore = %f, want 80.0", got1.QualityMinScore)
	}
	if got1.QualityCloudflareChallenged != nil {
		t.Fatal("QualityCloudflareChallenged should be nil")
	}
	if got1.QualityCheckedSinceNs != 1000000 {
		t.Fatalf("QualityCheckedSinceNs = %d, want 1000000", got1.QualityCheckedSinceNs)
	}
	if got1.QualityProfile != "generic" {
		t.Fatalf("QualityProfile = %q, want generic", got1.QualityProfile)
	}

	// Update with true CF challenged.
	trueVal := true
	p2 := p1
	p2.QualityCloudflareChallenged = &trueVal
	p2.UpdatedAtNs = time.Now().UnixNano()
	if err := repo.UpsertPlatform(p2); err != nil {
		t.Fatalf("UpsertPlatform (true): %v", err)
	}
	got2, err := repo.GetPlatform("plat-q-1")
	if err != nil {
		t.Fatalf("GetPlatform after true: %v", err)
	}
	if got2.QualityCloudflareChallenged == nil || *got2.QualityCloudflareChallenged != true {
		t.Fatal("QualityCloudflareChallenged should be true")
	}

	// Update with false CF challenged.
	falseVal := false
	p3 := p1
	p3.QualityCloudflareChallenged = &falseVal
	p3.UpdatedAtNs = time.Now().UnixNano()
	if err := repo.UpsertPlatform(p3); err != nil {
		t.Fatalf("UpsertPlatform (false): %v", err)
	}
	got3, err := repo.GetPlatform("plat-q-1")
	if err != nil {
		t.Fatalf("GetPlatform after false: %v", err)
	}
	if got3.QualityCloudflareChallenged == nil || *got3.QualityCloudflareChallenged != false {
		t.Fatal("QualityCloudflareChallenged should be false")
	}

	// Verify ListPlatforms also includes quality fields.
	list, err := repo.ListPlatforms()
	if err != nil {
		t.Fatalf("ListPlatforms: %v", err)
	}
	var found bool
	for _, p := range list {
		if p.ID == "plat-q-1" {
			found = true
			if p.QualityGrade != "A" {
				t.Fatalf("ListPlatforms QualityGrade = %q", p.QualityGrade)
			}
		}
	}
	if !found {
		t.Fatal("platform not found in ListPlatforms")
	}
}

// TestPlatformDefaultPlatform_QualityFiltersZero verifies the Default platform
// has zero-value quality filters (no filter).
func TestPlatformDefaultPlatform_QualityFiltersZero(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := MigrateStateDB(db); err != nil {
		t.Fatalf("MigrateStateDB: %v", err)
	}
	repo := newStateRepo(db)

	// Create a Default-like platform with no quality filters.
	p := model.Platform{
		ID:                               platform.DefaultPlatformID,
		Name:                             platform.DefaultPlatformName,
		StickyTTLNs:                      30000000000,
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "RANDOM",
		AllocationPolicy:                 "BALANCED",
		UpdatedAtNs:                      time.Now().UnixNano(),
	}
	if err := repo.UpsertPlatform(p); err != nil {
		t.Fatalf("UpsertPlatform Default: %v", err)
	}
	got, err := repo.GetPlatform(platform.DefaultPlatformID)
	if err != nil {
		t.Fatalf("GetPlatform Default: %v", err)
	}
	if got.QualityGrade != "" {
		t.Fatalf("Default QualityGrade should be empty, got %q", got.QualityGrade)
	}
	if got.QualityMinScore != 0 {
		t.Fatalf("Default QualityMinScore should be 0, got %f", got.QualityMinScore)
	}
	if got.QualityCloudflareChallenged != nil {
		t.Fatal("Default QualityCloudflareChallenged should be nil")
	}
	if got.QualityCheckedSinceNs != 0 {
		t.Fatalf("Default QualityCheckedSinceNs should be 0, got %d", got.QualityCheckedSinceNs)
	}
	if got.QualityProfile != "" {
		t.Fatalf("Default QualityProfile should be empty, got %q", got.QualityProfile)
	}
}

// TestMigrateStateDB_AddsQualityCFStatusesColumn verifies that migration 000011
// creates the quality_cloudflare_statuses_json column on platforms.
func TestMigrateStateDB_AddsQualityCFStatusesColumn(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := MigrateStateDB(db); err != nil {
		t.Fatalf("MigrateStateDB: %v", err)
	}

	if ok, err := hasTableColumn(db, "platforms", "quality_cloudflare_statuses_json"); err != nil || !ok {
		t.Fatalf("expected column quality_cloudflare_statuses_json, ok=%v err=%v", ok, err)
	}
}

// TestPlatformCRUD_WithQualityCFStatuses verifies round-trip of quality CF
// statuses field with empty, nil, and populated values.
func TestPlatformCRUD_WithQualityCFStatuses(t *testing.T) {
	repo := newTestStateRepo(t)

	// Create platform with empty statuses.
	p1 := model.Platform{
		ID:                               "plat-cf-1",
		Name:                             "CF-Statuses-1",
		StickyTTLNs:                      30000000000,
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "RANDOM",
		AllocationPolicy:                 "BALANCED",
		UpdatedAtNs:                      time.Now().UnixNano(),
		QualityCloudflareStatuses:        []string{},
	}
	if err := repo.UpsertPlatform(p1); err != nil {
		t.Fatalf("UpsertPlatform with empty statuses: %v", err)
	}
	got1, err := repo.GetPlatform("plat-cf-1")
	if err != nil {
		t.Fatalf("GetPlatform: %v", err)
	}
	if len(got1.QualityCloudflareStatuses) != 0 {
		t.Fatalf("QualityCloudflareStatuses should be empty, got %v", got1.QualityCloudflareStatuses)
	}

	// Create platform with populated statuses.
	p2 := model.Platform{
		ID:                               "plat-cf-2",
		Name:                             "CF-Statuses-2",
		StickyTTLNs:                      30000000000,
		RegexFilters:                     []string{},
		RegionFilters:                    []string{},
		ReverseProxyMissAction:           "TREAT_AS_EMPTY",
		ReverseProxyEmptyAccountBehavior: "RANDOM",
		AllocationPolicy:                 "BALANCED",
		UpdatedAtNs:                      time.Now().UnixNano(),
		QualityCloudflareStatuses:        []string{"clean", "not_detected", "block"},
	}
	if err := repo.UpsertPlatform(p2); err != nil {
		t.Fatalf("UpsertPlatform with statuses: %v", err)
	}
	got2, err := repo.GetPlatform("plat-cf-2")
	if err != nil {
		t.Fatalf("GetPlatform: %v", err)
	}
	if len(got2.QualityCloudflareStatuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d: %v", len(got2.QualityCloudflareStatuses), got2.QualityCloudflareStatuses)
	}
	if got2.QualityCloudflareStatuses[0] != "block" || got2.QualityCloudflareStatuses[1] != "clean" || got2.QualityCloudflareStatuses[2] != "not_detected" {
		t.Fatalf("statuses mismatch: got %v", got2.QualityCloudflareStatuses)
	}

	// List includes statuses.
	all, err := repo.ListPlatforms()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range all {
		if p.ID == "plat-cf-2" {
			found = true
			if len(p.QualityCloudflareStatuses) != 3 {
				t.Fatalf("ListPlatforms: expected 3 statuses, got %d", len(p.QualityCloudflareStatuses))
			}
		}
	}
	if !found {
		t.Fatal("ListPlatforms did not return plat-cf-2")
	}

	// Update - clear statuses.
	p1.QualityCloudflareStatuses = []string{"ng"}
	p1.UpdatedAtNs = time.Now().UnixNano()
	if err := repo.UpsertPlatform(p1); err != nil {
		t.Fatalf("UpsertPlatform update statuses: %v", err)
	}
	got1b, err := repo.GetPlatform("plat-cf-1")
	if err != nil {
		t.Fatalf("GetPlatform after update: %v", err)
	}
	if len(got1b.QualityCloudflareStatuses) != 1 || got1b.QualityCloudflareStatuses[0] != "ng" {
		t.Fatalf("after update, expected [ng], got %v", got1b.QualityCloudflareStatuses)
	}

	p1.QualityCloudflareStatuses = []string{"bogus"}
	if err := repo.UpsertPlatform(p1); err == nil {
		t.Fatal("expected invalid quality cloudflare status to be rejected")
	}
}

// TestMigrateStateDB_000011DownDropsCFStatusesColumn verifies rollback of
// migration 000011.
func TestMigrateStateDB_000011DownDropsCFStatusesColumn(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := MigrateStateDB(db); err != nil {
		t.Fatal(err)
	}
	downSQL, err := migrationsFS.ReadFile("migrations/state/000011_platforms_add_quality_cloudflare_statuses.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(downSQL)); err != nil {
		t.Fatalf("apply state migration 000011 down: %v", err)
	}
	if ok, err := hasTableColumn(db, "platforms", "quality_cloudflare_statuses_json"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("quality_cloudflare_statuses_json still present after migration down")
	}
}
