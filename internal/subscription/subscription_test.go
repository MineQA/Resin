package subscription

import (
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
)

func TestNewSubscription(t *testing.T) {
	s := NewSubscription("id1", "MySub", "https://example.com", true, false)
	if s.ID != "id1" {
		t.Fatalf("expected id1, got %s", s.ID)
	}
	if s.Name() != "MySub" {
		t.Fatalf("expected MySub, got %s", s.Name())
	}
	if !s.Enabled() {
		t.Fatal("expected enabled")
	}
	if s.Ephemeral() {
		t.Fatal("expected not ephemeral")
	}
	if got, want := s.EphemeralNodeEvictDelayNs(), int64(72*time.Hour); got != want {
		t.Fatalf("expected default ephemeral evict delay ns %d, got %d", want, got)
	}
	if s.ManagedNodes() == nil {
		t.Fatal("ManagedNodes should not be nil")
	}
	if got := s.SourceType(); got != SourceTypeRemote {
		t.Fatalf("expected default source type %q, got %q", SourceTypeRemote, got)
	}
}

func TestSubscription_NameThreadSafe(t *testing.T) {
	s := NewSubscription("id1", "original", "url", true, false)
	s.SetName("updated")
	if s.Name() != "updated" {
		t.Fatalf("expected updated, got %s", s.Name())
	}
}

func TestSubscription_EphemeralNodeEvictDelayThreadSafe(t *testing.T) {
	s := NewSubscription("id1", "sub", "url", true, true)
	s.SetEphemeralNodeEvictDelayNs(int64(10 * time.Minute))
	if got, want := s.EphemeralNodeEvictDelayNs(), int64(10*time.Minute); got != want {
		t.Fatalf("expected %d, got %d", want, got)
	}
}

func TestSubscription_SwapManagedNodes(t *testing.T) {
	s := NewSubscription("id1", "sub", "url", true, false)

	h1 := node.HashFromRawOptions([]byte(`{"type":"ss","server":"1.1.1.1"}`))
	h2 := node.HashFromRawOptions([]byte(`{"type":"ss","server":"2.2.2.2"}`))

	newMap := NewManagedNodes()
	newMap.StoreNode(h1, ManagedNode{Tags: []string{"tag-a"}})
	newMap.StoreNode(h2, ManagedNode{Tags: []string{"tag-b"}})
	s.SwapManagedNodes(newMap)

	loaded := s.ManagedNodes()
	managed, ok := loaded.LoadNode(h1)
	tags := managed.Tags
	if !ok || len(tags) != 1 || tags[0] != "tag-a" {
		t.Fatalf("unexpected tag for h1: ok=%v, tags=%v", ok, tags)
	}
}

func TestManagedNodes_LoadNodeStoreNode(t *testing.T) {
	m := NewManagedNodes()
	h := node.HashFromRawOptions([]byte(`{"type":"ss","server":"1.1.1.1"}`))

	m.StoreNode(h, ManagedNode{
		Tags:    []string{"tag-a", "tag-b"},
		Evicted: true,
	})

	got, ok := m.LoadNode(h)
	if !ok {
		t.Fatal("expected hash to exist")
	}
	if !got.Evicted {
		t.Fatal("expected Evicted=true")
	}
	if len(got.Tags) != 2 || got.Tags[0] != "tag-a" || got.Tags[1] != "tag-b" {
		t.Fatalf("unexpected tags: %+v", got.Tags)
	}
}

func TestManagedNodes_StoreNodeCopiesInputTags(t *testing.T) {
	m := NewManagedNodes()
	h := node.HashFromRawOptions([]byte(`{"type":"ss","server":"8.8.8.8"}`))
	input := []string{"tag-a", "tag-b"}

	m.StoreNode(h, ManagedNode{Tags: input})
	input[0] = "mutated"
	input[1] = "changed"

	got, ok := m.LoadNode(h)
	if !ok {
		t.Fatal("expected hash to exist")
	}
	if len(got.Tags) != 2 || got.Tags[0] != "tag-a" || got.Tags[1] != "tag-b" {
		t.Fatalf("stored tags should not be affected by caller mutation: %+v", got.Tags)
	}
}

func TestSubscription_SourceTypeAndContent(t *testing.T) {
	s := NewSubscription("id1", "sub", "url", true, false)
	v0 := s.ConfigVersion()

	s.SetSourceType(SourceTypeLocal)
	s.SetContent("vmess://example")
	if got := s.SourceType(); got != SourceTypeLocal {
		t.Fatalf("expected source type %q, got %q", SourceTypeLocal, got)
	}
	if got := s.Content(); got != "vmess://example" {
		t.Fatalf("unexpected content: %q", got)
	}
	if s.ConfigVersion() <= v0 {
		t.Fatalf("expected config version to increase: old=%d new=%d", v0, s.ConfigVersion())
	}
}

func TestSubscription_SetClashFingerprintPolicy_IncrementsConfigVersion(t *testing.T) {
	s := NewSubscription("id1", "sub", "url", true, false)
	v0 := s.ConfigVersion()

	// Changing policy bumps configVersion.
	s.SetClashFingerprintPolicy(ClashFingerprintDropSafe)
	if s.ClashFingerprintPolicy() != ClashFingerprintDropSafe {
		t.Fatalf("expected drop_safe, got %v", s.ClashFingerprintPolicy())
	}
	if s.ConfigVersion() <= v0 {
		t.Fatalf("expected config version to increase: old=%d new=%d", v0, s.ConfigVersion())
	}

	v1 := s.ConfigVersion()

	// Setting the same value does NOT bump configVersion.
	s.SetClashFingerprintPolicy(ClashFingerprintDropSafe)
	if s.ConfigVersion() != v1 {
		t.Fatalf("expected config version unchanged: got %d, want %d", s.ConfigVersion(), v1)
	}

	// Default is reject.
	s2 := NewSubscription("id2", "sub2", "url", true, false)
	if s2.ClashFingerprintPolicy() != ClashFingerprintReject {
		t.Fatalf("default clash fingerprint policy: got %v, want reject", s2.ClashFingerprintPolicy())
	}
}

func TestDiffHashes(t *testing.T) {
	h1 := node.HashFromRawOptions([]byte(`{"type":"ss","server":"1.1.1.1"}`))
	h2 := node.HashFromRawOptions([]byte(`{"type":"ss","server":"2.2.2.2"}`))
	h3 := node.HashFromRawOptions([]byte(`{"type":"ss","server":"3.3.3.3"}`))

	oldMap := NewManagedNodes()
	oldMap.StoreNode(h1, ManagedNode{Tags: []string{"a"}})
	oldMap.StoreNode(h2, ManagedNode{Tags: []string{"b"}})

	newMap := NewManagedNodes()
	newMap.StoreNode(h2, ManagedNode{Tags: []string{"b"}})
	newMap.StoreNode(h3, ManagedNode{Tags: []string{"c"}})

	added, kept, removed := DiffHashes(oldMap, newMap)

	if len(added) != 1 || added[0] != h3 {
		t.Fatalf("expected h3 added, got %v", added)
	}
	if len(kept) != 1 || kept[0] != h2 {
		t.Fatalf("expected h2 kept, got %v", kept)
	}
	if len(removed) != 1 || removed[0] != h1 {
		t.Fatalf("expected h1 removed, got %v", removed)
	}
}

// ---------------------------------------------------------------------------
// Test: UpdateMode accessors
// ---------------------------------------------------------------------------

func TestSubscription_UpdateModeDefaults(t *testing.T) {
	s := NewSubscription("id1", "sub", "url", true, false)
	if got := s.UpdateMode(); got != UpdateModeInterval {
		t.Fatalf("expected default update_mode %q, got %q", UpdateModeInterval, got)
	}
	if got := s.UpdateTime(); got != "" {
		t.Fatalf("expected default update_time empty, got %q", got)
	}
	if got := s.UpdateTimezone(); got != "" {
		t.Fatalf("expected default update_timezone empty, got %q", got)
	}
}

func TestSubscription_SetUpdateMode(t *testing.T) {
	s := NewSubscription("id1", "sub", "url", true, false)
	v0 := s.ConfigVersion()

	// Changing mode should NOT bump configVersion — mode only affects
	// scheduling, not refresh input.
	s.SetUpdateMode(UpdateModeDaily)
	if got := s.UpdateMode(); got != UpdateModeDaily {
		t.Fatalf("expected update_mode %q, got %q", UpdateModeDaily, got)
	}
	if s.ConfigVersion() != v0 {
		t.Fatalf("expected config version unchanged after mode change: old=%d new=%d", v0, s.ConfigVersion())
	}

	// Setting the same mode also does not bump configVersion.
	s.SetUpdateMode(UpdateModeDaily)
	if s.ConfigVersion() != v0 {
		t.Fatalf("expected config version unchanged after no-op set: got %d, want %d", s.ConfigVersion(), v0)
	}

	s.SetUpdateTime("10:30")
	if got := s.UpdateTime(); got != "10:30" {
		t.Fatalf("expected update_time %q, got %q", "10:30", got)
	}
	s.SetUpdateTimezone("Asia/Shanghai")
	if got := s.UpdateTimezone(); got != "Asia/Shanghai" {
		t.Fatalf("expected update_timezone %q, got %q", "Asia/Shanghai", got)
	}

	// Verify config version still unchanged after all schedule field changes.
	if s.ConfigVersion() != v0 {
		t.Fatalf("expected config version unchanged after all schedule setters: got %d, want %d", s.ConfigVersion(), v0)
	}
}

// ---------------------------------------------------------------------------
// Test: ParseHHMM
// ---------------------------------------------------------------------------

func TestParseHHMM_Valid(t *testing.T) {
	tests := []struct {
		input    string
		wantH, wantM int
	}{
		{"00:00", 0, 0},
		{"08:00", 8, 0},
		{"12:30", 12, 30},
		{"23:59", 23, 59},
	}
	for _, tt := range tests {
		h, m, err := ParseHHMM(tt.input)
		if err != nil {
			t.Fatalf("ParseHHMM(%q): unexpected error: %v", tt.input, err)
		}
		if h != tt.wantH || m != tt.wantM {
			t.Fatalf("ParseHHMM(%q): got %02d:%02d, want %02d:%02d", tt.input, h, m, tt.wantH, tt.wantM)
		}
	}
}

func TestParseHHMM_Invalid(t *testing.T) {
	tests := []string{
		"", "0:00", "00:0", "24:00", "00:60", "abc", "12:34:56", " 08:00",
	}
	for _, input := range tests {
		_, _, err := ParseHHMM(input)
		if err == nil {
			t.Fatalf("ParseHHMM(%q): expected error", input)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: IsSubscriptionDue — pure function, inject now
// ---------------------------------------------------------------------------

func TestIsSubscriptionDue_Interval_Positive(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	// lastChecked = now - interval + 16s → beyond lookahead (15s), NOT due
	lastChecked := now.UnixNano() - int64(5*time.Minute) + int64(16*time.Second)
	due := IsSubscriptionDue(lastChecked, now, UpdateModeInterval, int64(5*time.Minute), "", "")
	if due {
		t.Fatal("expected NOT due when lastChecked is recent (beyond lookahead)")
	}

	// lastChecked = now - interval - 1s → within lookahead => due
	lastChecked = now.UnixNano() - int64(5*time.Minute) - int64(1*time.Second)
	due = IsSubscriptionDue(lastChecked, now, UpdateModeInterval, int64(5*time.Minute), "", "")
	if !due {
		t.Fatal("expected due when lastChecked is older than interval")
	}
}

func TestIsSubscriptionDue_Interval_ZeroLastChecked(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	// never checked → due
	due := IsSubscriptionDue(0, now, UpdateModeInterval, int64(5*time.Minute), "", "")
	if !due {
		t.Fatal("expected due when lastChecked is 0 (never checked)")
	}
}

func TestIsSubscriptionDue_Daily_OnTime(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	// Scheduled at 10:00 CST. now = 10:00:05 CST → due.
	scheduled := time.Date(2025, 1, 15, 10, 0, 0, 0, loc)
	now := scheduled.Add(5 * time.Second)
	lastChecked := scheduled.Add(-1 * time.Hour) // last checked at 09:00

	due := IsSubscriptionDue(lastChecked.UnixNano(), now, UpdateModeDaily, 0, "10:00", "Asia/Shanghai")
	if !due {
		t.Fatal("expected due at scheduled time")
	}
}

func TestIsSubscriptionDue_Daily_NotYet(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	// Scheduled at 10:00 CST. now = 09:00 CST → NOT due.
	now := time.Date(2025, 1, 15, 9, 0, 0, 0, loc)
	lastChecked := time.Date(2025, 1, 14, 10, 0, 0, 0, loc) // checked yesterday at 10:00

	due := IsSubscriptionDue(lastChecked.UnixNano(), now, UpdateModeDaily, 0, "10:00", "Asia/Shanghai")
	if due {
		t.Fatal("expected NOT due before today's scheduled time")
	}
}

func TestIsSubscriptionDue_Daily_CrossDay(t *testing.T) {
	// Scheduled at 10:00 UTC. now = next day 10:01.
	scheduledToday := time.Date(2025, 1, 16, 10, 0, 0, 0, time.UTC)
	now := scheduledToday.Add(1 * time.Minute)
	lastChecked := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC) // checked yesterday at 10:00

	due := IsSubscriptionDue(lastChecked.UnixNano(), now, UpdateModeDaily, 0, "10:00", "UTC")
	if !due {
		t.Fatal("expected due after cross-day boundary")
	}
}

func TestIsSubscriptionDue_Daily_ManualBeforeScheduled(t *testing.T) {
	// Scheduled at 10:00, manual refresh at 09:00 → don't cancel today's 10:00.
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Date(2025, 1, 15, 9, 0, 0, 0, loc)
	lastChecked := now // just refreshed manually

	// Before manual refresh, the most recent scheduled moment is yesterday 10:00.
	// lastChecked (09:00 today) > yesterday 10:00 → NOT due for yesterday.
	yesterdayScheduled := time.Date(2025, 1, 14, 10, 0, 0, 0, loc)
	if lastChecked.After(yesterdayScheduled) {
		due := IsSubscriptionDue(lastChecked.UnixNano(), now, UpdateModeDaily, 0, "10:00", "America/New_York")
		if due {
			t.Fatal("expected NOT due after manual refresh and before today's scheduled time")
		}
	}

	// Now advance to 10:01 — today's scheduled moment has arrived.
	nowLater := time.Date(2025, 1, 15, 10, 1, 0, 0, loc)
	due := IsSubscriptionDue(lastChecked.UnixNano(), nowLater, UpdateModeDaily, 0, "10:00", "America/New_York")
	if !due {
		t.Fatal("expected due when today's scheduled time arrives after manual refresh")
	}
}

func TestIsSubscriptionDue_Daily_ManualAfterScheduled(t *testing.T) {
	// Scheduled at 10:00, manual refresh at 11:00 → avoid duplicate.
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Date(2025, 1, 15, 11, 0, 0, 0, loc)
	lastChecked := now // just refreshed manually at 11:00

	// mostRecentScheduled = today 10:00, lastChecked (11:00) > today 10:00 → NOT due.
	due := IsSubscriptionDue(lastChecked.UnixNano(), now, UpdateModeDaily, 0, "10:00", "America/New_York")
	if due {
		t.Fatal("expected NOT due when manual refresh already happened after today's scheduled time")
	}
}

func TestIsSubscriptionDue_Daily_ServiceRecovery(t *testing.T) {
	// Service was down for 2 days. Scheduled at 08:00 UTC.
	// now = Jan 17 08:30 (after today's scheduled time).
	scheduled := time.Date(2025, 1, 17, 8, 0, 0, 0, time.UTC)
	now := scheduled.Add(30 * time.Minute)
	lastChecked := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC) // last checked Jan 15 08:00

	// mostRecentScheduled = today Jan 17 08:00.
	// lastChecked (Jan 15 08:00) < Jan 17 08:00 → due (catch-up).
	due := IsSubscriptionDue(lastChecked.UnixNano(), now, UpdateModeDaily, 0, "08:00", "UTC")
	if !due {
		t.Fatal("expected due when service missed scheduled times (catch-up)")
	}

	// After update, lastChecked = now. Immediately check again → NOT due.
	due = IsSubscriptionDue(now.UnixNano(), now, UpdateModeDaily, 0, "08:00", "UTC")
	if due {
		t.Fatal("expected NOT due immediately after catch-up")
	}
}

func TestIsSubscriptionDue_Daily_UpdateFailureStillUpdatesLastChecked(t *testing.T) {
	// Scheduled at 08:00. Update at 08:30 fails, lastChecked = 08:30.
	// Check at 09:00 → mostRecentScheduled is still today 08:00,
	// lastChecked (08:30) > 08:00 → NOT due (avoids infinite retry on same day).
	loc, _ := time.LoadLocation("Europe/London")
	lastChecked := time.Date(2025, 1, 15, 8, 30, 0, 0, loc) // update failed
	now := time.Date(2025, 1, 15, 9, 0, 0, 0, loc)

	due := IsSubscriptionDue(lastChecked.UnixNano(), now, UpdateModeDaily, 0, "08:00", "Europe/London")
	if due {
		t.Fatal("expected NOT due on same day after failed update (lastChecked updated)")
	}

	// Next day at 08:01 → due again.
	nextDay := time.Date(2025, 1, 16, 8, 1, 0, 0, loc)
	due = IsSubscriptionDue(lastChecked.UnixNano(), nextDay, UpdateModeDaily, 0, "08:00", "Europe/London")
	if !due {
		t.Fatal("expected due on next day after failed update")
	}
}

func TestIsSubscriptionDue_Daily_EmptyConfigNeverDue(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	// No update_time set → never due
	if IsSubscriptionDue(0, now, UpdateModeDaily, 0, "", "UTC") {
		t.Fatal("expected NOT due when update_time is empty")
	}
	// No update_timezone set → never due
	if IsSubscriptionDue(0, now, UpdateModeDaily, 0, "10:00", "") {
		t.Fatal("expected NOT due when update_timezone is empty")
	}
}

func TestIsSubscriptionDue_Daily_InvalidTimezoneNeverDue(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	if IsSubscriptionDue(0, now, UpdateModeDaily, 0, "10:00", "Bogus/Zone") {
		t.Fatal("expected NOT due when timezone is invalid")
	}
}

func TestIsSubscriptionDue_Daily_InvalidTimeNeverDue(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	if IsSubscriptionDue(0, now, UpdateModeDaily, 0, "25:00", "UTC") {
		t.Fatal("expected NOT due when time is invalid")
	}
}

func TestIsSubscriptionDue_Daily_Unchecked(t *testing.T) {
	// Never checked (lastChecked = 0) but today's scheduled time already passed → due.
	loc, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Date(2025, 1, 15, 15, 0, 0, 0, loc) // 15:00 JST
	due := IsSubscriptionDue(0, now, UpdateModeDaily, 0, "10:00", "Asia/Tokyo")
	if !due {
		t.Fatal("expected due for unchecked daily sub when today's scheduled time has passed")
	}
}

func TestIsSubscriptionDue_Daily_UncheckedBeforeScheduled(t *testing.T) {
	// Never checked, but scheduled time hasn't arrived yet → NOT due.
	loc, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Date(2025, 1, 15, 8, 0, 0, 0, loc) // 08:00 JST, scheduled at 10:00
	due := IsSubscriptionDue(0, now, UpdateModeDaily, 0, "10:00", "Asia/Tokyo")
	if due {
		t.Fatal("expected NOT due for unchecked daily sub when scheduled time hasn't arrived yet")
	}
}

// ---------------------------------------------------------------------------
// Test: IsSubscriptionDue — interval edge cases
// ---------------------------------------------------------------------------

func TestIsSubscriptionDue_Interval_NegativeUpdateInterval(t *testing.T) {
	// Should not crash; treat as always due.
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	due := IsSubscriptionDue(now.UnixNano(), now, UpdateModeInterval, -1, "", "")
	if !due {
		t.Fatal("expected due with negative interval (past)")
	}
}

func TestIsSubscriptionDue_Daily_MidnightCrossing(t *testing.T) {
	// Scheduled at 23:00 UTC, check at 00:30 UTC next day.
	now := time.Date(2025, 1, 16, 0, 30, 0, 0, time.UTC)
	lastChecked := time.Date(2025, 1, 15, 23, 0, 0, 0, time.UTC) // auto-updated at scheduled time

	// mostRecentScheduled = today Jan 16 23:00 is in the future → use yesterday Jan 15 23:00.
	// lastChecked (Jan 15 23:00) == yesterday's 23:00 → NOT due.
	due := IsSubscriptionDue(lastChecked.UnixNano(), now, UpdateModeDaily, 0, "23:00", "UTC")
	if due {
		t.Fatal("expected NOT due at midnight crossing when last update was yesterday's scheduled time")
	}
}

func TestIsSubscriptionDue_Daily_DifferentTimezone(t *testing.T) {
	// Subscription configured for Asia/Shanghai (UTC+8) at 08:00 CST.
	// Server is in UTC. Current server time = 00:01 UTC = 08:01 CST → due.
	locSH, _ := time.LoadLocation("Asia/Shanghai")
	nowUTC := time.Date(2025, 1, 15, 0, 1, 0, 0, time.UTC) // 08:01 CST

	// lastChecked more than a day ago in CST
	lastChecked := time.Date(2025, 1, 14, 8, 0, 0, 0, locSH) // yesterday 08:00 CST
	due := IsSubscriptionDue(lastChecked.UnixNano(), nowUTC, UpdateModeDaily, 0, "08:00", "Asia/Shanghai")
	if !due {
		t.Fatal("expected due when server UTC time corresponds to scheduled CST time")
	}
}

func TestDiffHashes_Empty(t *testing.T) {
	empty := NewManagedNodes()
	h1 := node.HashFromRawOptions([]byte(`{"type":"ss"}`))

	full := NewManagedNodes()
	full.StoreNode(h1, ManagedNode{Tags: []string{"t"}})

	// All new.
	added, kept, removed := DiffHashes(empty, full)
	if len(added) != 1 || len(kept) != 0 || len(removed) != 0 {
		t.Fatalf("empty→full: added=%d kept=%d removed=%d", len(added), len(kept), len(removed))
	}

	// All removed.
	added, kept, removed = DiffHashes(full, empty)
	if len(added) != 0 || len(kept) != 0 || len(removed) != 1 {
		t.Fatalf("full→empty: added=%d kept=%d removed=%d", len(added), len(kept), len(removed))
	}
}
