package platform

import (
	"net/netip"
	"regexp"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/testutil"
)

// makeFullyRoutableEntry creates a NodeEntry that passes all 5 filter conditions.
func makeFullyRoutableEntry(hash node.Hash, subIDs ...string) *node.NodeEntry {
	return makeFullyRoutableEntryWithRawOptions(hash, nil, subIDs...)
}

func makeFullyRoutableEntryWithRawOptions(hash node.Hash, rawOptions []byte, subIDs ...string) *node.NodeEntry {
	e := node.NewNodeEntry(hash, rawOptions, time.Now(), 16)
	for _, id := range subIDs {
		e.AddSubscriptionID(id)
	}
	// Set all conditions to pass.
	e.LatencyTable.LoadEntry("example.com", node.DomainLatencyStats{
		Ewma:        100 * time.Millisecond,
		LastUpdated: time.Now(),
	})
	ob := testutil.NewNoopOutbound()
	e.Outbound.Store(&ob)
	e.SetEgressIP(netip.MustParseAddr("1.2.3.4"))
	return e
}

func alwaysLookup(subID string, hash node.Hash) (string, bool, []string, bool) {
	return "TestSub", true, []string{"us-node", "fast"}, true
}

func usGeoLookup(addr netip.Addr) string { return "us" }

func TestPlatform_EvaluateNode_AllPass(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil) // no filters
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 1 {
		t.Fatalf("expected 1 routable node, got %d", p.View().Size())
	}
}

func TestPlatform_EvaluateNode_CircuitOpen(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")
	entry.CircuitOpenSince.Store(time.Now().UnixNano()) // circuit open

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("circuit-broken node should not be routable")
	}
}

func TestPlatform_EvaluateNode_NoLatency(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	h := makeHash(`{"type":"ss"}`)
	// Create entry without latency table (maxLatencyTableEntries=0).
	entry := node.NewNodeEntry(h, nil, time.Now(), 0)
	entry.AddSubscriptionID("sub1")
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)
	entry.SetEgressIP(netip.MustParseAddr("1.2.3.4"))

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("node without latency should not be routable")
	}
}

func TestPlatform_EvaluateNode_NoOutbound(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")
	entry.Outbound.Store(nil) // no outbound

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("node without outbound should not be routable")
	}
}

func TestPlatform_EvaluateNode_NoEgressIP(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil) // no region filters
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")
	entry.SetEgressIP(netip.Addr{}) // egress unknown

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("node without egress IP should not be routable")
	}
}

func TestPlatform_EvaluateNode_RegexFilter(t *testing.T) {
	regexes := []*regexp.Regexp{regexp.MustCompile("us")}
	p := NewPlatform("p1", "Test", regexes, nil)
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	// Lookup returns "TestSub/us-node" which matches "us".
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 1 {
		t.Fatal("node matching regex should be routable")
	}

	// Now with a "jp" filter — should NOT match.
	p2 := NewPlatform("p2", "Test", []*regexp.Regexp{regexp.MustCompile("^jp")}, nil)
	p2.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p2.View().Size() != 0 {
		t.Fatal("node not matching regex should not be routable")
	}
}

func TestPlatform_EvaluateNode_RegionFilter(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, []string{"us"})
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 1 {
		t.Fatal("node in allowed region should be routable")
	}

	// Region filter "jp" — node has US egress, should fail.
	p2 := NewPlatform("p2", "Test", nil, []string{"jp"})
	p2.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p2.View().Size() != 0 {
		t.Fatal("node not in allowed region should not be routable")
	}
}

func TestPlatform_EvaluateNode_RegionFilter_NoEgressIP(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, []string{"us"})
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")
	// Don't set egress IP — clear it.
	entry.SetEgressIP(netip.Addr{})

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("node without egress IP should not be routable")
	}
}

func TestPlatform_EvaluateNode_RegionFilter_PrefersStoredRegion(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, []string{"jp"})
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")
	entry.SetEgressRegion("jp")

	geoCalled := false
	geoLookup := func(netip.Addr) string {
		geoCalled = true
		return "us"
	}

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, geoLookup)

	if p.View().Size() != 1 {
		t.Fatal("stored region should be used before GeoIP fallback")
	}
	if geoCalled {
		t.Fatal("GeoIP lookup should be skipped when stored region exists")
	}
}

func TestPlatform_EvaluateNode_RegionFilter_ExcludeOnlyUnknownRegion(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, []string{"!hk"})
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	geoLookup := func(netip.Addr) string { return "" }
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, geoLookup)

	if p.View().Size() != 0 {
		t.Fatal("node with unknown region should not be routable when region filters are configured")
	}
}

func TestMatchRegionFilter(t *testing.T) {
	tests := []struct {
		name    string
		filters []string
		region  string
		want    bool
	}{
		{
			name:    "include only match",
			filters: []string{"hk", "us"},
			region:  "hk",
			want:    true,
		},
		{
			name:    "include only miss",
			filters: []string{"hk", "us"},
			region:  "jp",
			want:    false,
		},
		{
			name:    "exclude only",
			filters: []string{"!hk"},
			region:  "us",
			want:    true,
		},
		{
			name:    "exclude only blocked",
			filters: []string{"!hk"},
			region:  "hk",
			want:    false,
		},
		{
			name:    "exclude only unknown region",
			filters: []string{"!hk"},
			region:  "",
			want:    false,
		},
		{
			name:    "mixed include and exclude allows expected",
			filters: []string{"hk", "!us"},
			region:  "hk",
			want:    true,
		},
		{
			name:    "mixed include and exclude blocks excluded",
			filters: []string{"hk", "!us"},
			region:  "us",
			want:    false,
		},
		{
			name:    "mixed include and same exclude blocks",
			filters: []string{"hk", "!hk"},
			region:  "hk",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchRegionFilter(tt.region, tt.filters); got != tt.want {
				t.Fatalf("MatchRegionFilter(%q, %v) = %v, want %v", tt.region, tt.filters, got, tt.want)
			}
		})
	}
}

func TestPlatform_NotifyDirty_AddRemove(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	entryStore := map[node.Hash]*node.NodeEntry{h: entry}
	getEntry := func(hash node.Hash) (*node.NodeEntry, bool) {
		e, ok := entryStore[hash]
		return e, ok
	}

	// Initially empty — add via NotifyDirty.
	p.NotifyDirty(h, getEntry, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("NotifyDirty should add passing node")
	}

	// Circuit-break → NotifyDirty removes.
	entry.CircuitOpenSince.Store(time.Now().UnixNano())
	p.NotifyDirty(h, getEntry, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("NotifyDirty should remove circuit-broken node")
	}

	// Recover → NotifyDirty re-adds.
	entry.CircuitOpenSince.Store(0)
	p.NotifyDirty(h, getEntry, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("NotifyDirty should re-add recovered node")
	}

	// Delete from pool → NotifyDirty removes.
	delete(entryStore, h)
	p.NotifyDirty(h, getEntry, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("NotifyDirty should remove deleted node")
	}
}

func TestPlatform_NotifyDirty_QualityStatusChange(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.QualityCloudflareStatuses = []string{"clean"}
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	entryStore := map[node.Hash]*node.NodeEntry{h: entry}
	getEntry := func(hash node.Hash) (*node.NodeEntry, bool) {
		e, ok := entryStore[hash]
		return e, ok
	}

	// No quality -> reject.
	p.NotifyDirty(h, getEntry, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("NotifyDirty should reject node with no quality")
	}

	// Set quality with matching status -> add.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareStatus: "clean", Profile: "generic",
	})
	p.NotifyDirty(h, getEntry, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("NotifyDirty should add node with matching status")
	}

	// Change quality to non-matching status -> remove.
	entry.SetQuality(&model.NodeQuality{
		Grade: "B", Score: 70, ServiceReachable: true,
		CloudflareStatus: "block", Profile: "generic",
	})
	p.NotifyDirty(h, getEntry, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("NotifyDirty should remove node with non-matching status")
	}

	// Change back to matching -> re-add.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareStatus: "clean", Profile: "generic",
	})
	p.NotifyDirty(h, getEntry, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("NotifyDirty should re-add node with matching status")
	}
}

func TestPlatform_FullRebuild_ClearsOld(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	h1 := makeHash(`{"type":"ss","n":1}`)
	h2 := makeHash(`{"type":"ss","n":2}`)
	e1 := makeFullyRoutableEntry(h1, "sub1")
	e2 := makeFullyRoutableEntry(h2, "sub1")

	// First rebuild with 2 nodes.
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h1, e1)
		fn(h2, e2)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 2 {
		t.Fatalf("expected 2, got %d", p.View().Size())
	}

	// Second rebuild with only 1 node — old entries cleared.
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h1, e1)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 1 {
		t.Fatalf("expected 1 after rebuild, got %d", p.View().Size())
	}
	if p.View().Contains(h2) {
		t.Fatal("h2 should have been removed by rebuild")
	}
}

func TestPlatform_EvaluateNode_ProtocolFilterInclude(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.ProtocolFilters = []string{"shadowsocks"}
	rawOptions := []byte(`{"type":"ss"}`)
	h := makeHash(string(rawOptions))
	entry := makeFullyRoutableEntryWithRawOptions(h, rawOptions, "sub1")

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 1 {
		t.Fatal("shadowsocks node should be routable with protocol include filter")
	}
}

func TestPlatform_EvaluateNode_ProtocolFilterIncludeReject(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.ProtocolFilters = []string{"vmess"}
	rawOptions := []byte(`{"type":"ss"}`)
	h := makeHash(string(rawOptions))
	entry := makeFullyRoutableEntryWithRawOptions(h, rawOptions, "sub1")

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("shadowsocks node should be rejected by vmess-only include filter")
	}
}

func TestPlatform_EvaluateNode_ProtocolFilterExclude(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.ExcludeProtocolFilters = []string{"shadowsocks"}
	rawOptions := []byte(`{"type":"ss"}`)
	h := makeHash(string(rawOptions))
	entry := makeFullyRoutableEntryWithRawOptions(h, rawOptions, "sub1")

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("shadowsocks node should be excluded by protocol exclude filter")
	}
}

func TestPlatform_EvaluateNode_ProtocolFilterExcludePassesOthers(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.ExcludeProtocolFilters = []string{"vmess"}
	rawOptions := []byte(`{"type":"ss"}`)
	h := makeHash(string(rawOptions))
	entry := makeFullyRoutableEntryWithRawOptions(h, rawOptions, "sub1")

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 1 {
		t.Fatal("shadowsocks node should not be excluded by vmess exclude filter")
	}
}

func TestPlatform_EvaluateNode_ProtocolFilterIncludeAndExclude(t *testing.T) {
	// Include "shadowsocks" but exclude "ss" (same canonical) — exclude wins.
	p := NewPlatform("p1", "Test", nil, nil)
	p.ProtocolFilters = []string{"shadowsocks"}
	p.ExcludeProtocolFilters = []string{"shadowsocks"}
	rawOptions := []byte(`{"type":"ss"}`)
	h := makeHash(string(rawOptions))
	entry := makeFullyRoutableEntryWithRawOptions(h, rawOptions, "sub1")

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("exclude should win over include for the same protocol")
	}
}

func TestPlatform_EvaluateNode_UnknownProtocolIncludeRejected(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.ProtocolFilters = []string{"shadowsocks"}
	rawOptions := []byte(`{"type":"unknown_proto"}`)
	h := makeHash(string(rawOptions))
	entry := makeFullyRoutableEntryWithRawOptions(h, rawOptions, "sub1")

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("unknown protocol node should be rejected by include-only filter")
	}
}

// TestPlatform_EvaluateNode_QualityGradeFilter verifies grade-based quality filtering.
func TestPlatform_EvaluateNode_QualityGradeFilter(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.QualityGrade = "A"
	p.QualityMinScore = 0 // no min score filter
	p.QualityCloudflareChallenged = nil
	p.QualityCheckedSinceNs = 0
	p.QualityProfile = ""

	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	// Node with no quality should be rejected when any quality filter is active.
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node without quality should be rejected by quality grade filter")
	}

	// Set quality with grade B - should still fail.
	qB := &model.NodeQuality{
		Grade:            "B",
		Score:            90,
		ServiceReachable: true,
		Profile:          "generic",
	}
	entry.SetQuality(qB)
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node with grade B should be rejected by grade A filter")
	}

	// Set quality with grade A - should pass.
	qA := &model.NodeQuality{
		Grade:            "A",
		Score:            95,
		ServiceReachable: true,
		Profile:          "generic",
	}
	entry.SetQuality(qA)
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("node with grade A should pass grade A filter")
	}
}

// TestPlatform_EvaluateNode_QualityMinScoreFilter verifies min score filtering.
func TestPlatform_EvaluateNode_QualityMinScoreFilter(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.QualityMinScore = 80.0

	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	// No quality -> reject.
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node without quality should be rejected by min score filter")
	}

	// Score 70 -> reject.
	entry.SetQuality(&model.NodeQuality{
		Grade: "B", Score: 70, ServiceReachable: true, Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node with score 70 should be rejected by min score 80 filter")
	}

	// Score 90 -> pass.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true, Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("node with score 90 should pass min score 80 filter")
	}
}

// TestPlatform_EvaluateNode_QualityCloudflareStatusesFilter verifies detailed
// CF status filter: OR within selected statuses, empty→unchecked normalization,
// missing quality rejection, and intersection with existing bool filter.
func TestPlatform_EvaluateNode_QualityCloudflareStatusesFilter(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.QualityCloudflareStatuses = []string{"clean", "not_detected"}

	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	// No quality -> reject.
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node without quality should be rejected by CF status filter")
	}

	// Status "block" not in {clean, not_detected} -> reject.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareStatus: "block", Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node with block status should not match clean/not_detected filter")
	}

	// Status "clean" in filter -> pass.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareStatus: "clean", Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("node with clean status should match clean/not_detected filter")
	}

	// Empty legacy status normalizes to "unchecked" -> reject (not in filter).
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareStatus: "", Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node with empty legacy status should normalize to unchecked and not match clean/not_detected")
	}

	// Single status filter with "unchecked" should match empty status.
	p2 := NewPlatform("p2", "Test2", nil, nil)
	p2.QualityCloudflareStatuses = []string{"unchecked"}
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareStatus: "", Profile: "generic", // empty -> unchecked
	})
	p2.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p2.View().Size() != 1 {
		t.Fatal("node with empty status should match unchecked filter")
	}

	// Intersection with CloudflareChallenged filter.
	trueVal := true
	p3 := NewPlatform("p3", "Test3", nil, nil)
	p3.QualityCloudflareStatuses = []string{"block", "js_challenge"}
	p3.QualityCloudflareChallenged = &trueVal // must be challenged

	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareStatus: "block", CloudflareChallenged: true, Profile: "generic",
	})
	p3.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p3.View().Size() != 1 {
		t.Fatal("node with block status and challenged=true should pass intersection filter")
	}

	// Same but clean (not in status filter) -> reject.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareStatus: "clean", CloudflareChallenged: true, Profile: "generic",
	})
	p3.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p3.View().Size() != 0 {
		t.Fatal("node with clean status should be rejected by block/js_challenge filter")
	}
}

// TestPlatform_EvaluateNode_QualityCloudflareChallengedFilter verifies CF filter.
func TestPlatform_EvaluateNode_QualityCloudflareChallengedFilter(t *testing.T) {
	trueVal := true
	p := NewPlatform("p1", "Test", nil, nil)
	p.QualityCloudflareChallenged = &trueVal

	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	// No quality -> reject.
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node without quality should be rejected by CF challenged filter")
	}

	// Not challenged -> reject.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareChallenged: false, Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("non-challenged node should be rejected by CF=true filter")
	}

	// Challenged -> pass.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		CloudflareChallenged: true, Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("challenged node should pass CF=true filter")
	}
}

// TestPlatform_EvaluateNode_QualityCheckedSinceNsFilter verifies checked-since filter.
func TestPlatform_EvaluateNode_QualityCheckedSinceNsFilter(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.QualityCheckedSinceNs = 5000

	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	// No quality -> reject.
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node without quality should be rejected by checked-since filter")
	}

	// LastChecked before 5000 -> reject.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		LastCheckedNs: 3000, Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node with LastCheckedNs=3000 should be rejected by checked-since=5000")
	}

	// LastChecked after 5000 -> pass.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true,
		LastCheckedNs: 6000, Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("node with LastCheckedNs=6000 should pass checked-since=5000 filter")
	}
}

// TestPlatform_EvaluateNode_QualityProfileFilter verifies quality profile filter.
func TestPlatform_EvaluateNode_QualityProfileFilter(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.QualityProfile = "openai"

	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	// No quality -> reject.
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node without quality should be rejected by profile filter")
	}

	// Wrong profile -> reject.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true, Profile: "generic",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 0 {
		t.Fatal("node with generic profile should be rejected by openai filter")
	}

	// Correct profile -> pass.
	entry.SetQuality(&model.NodeQuality{
		Grade: "A", Score: 90, ServiceReachable: true, Profile: "openai",
	})
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("node with openai profile should pass openai filter")
	}
}

// TestPlatform_EvaluateNode_QualityFilters_NoFilter_IgnoresQuality verifies that
// when all quality filters are empty, nodes pass regardless of quality state.
func TestPlatform_EvaluateNode_QualityFilters_NoFilter_IgnoresQuality(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil) // all quality filters at zero values
	h := makeHash(`{"type":"ss"}`)
	entry := makeFullyRoutableEntry(h, "sub1")

	// Node without quality should still be routable when no quality filters.
	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)
	if p.View().Size() != 1 {
		t.Fatal("node without quality should be routable when no quality filters are active")
	}
}

func TestPlatform_EvaluateNode_UnknownProtocolExcludeOnlyRejected(t *testing.T) {
	p := NewPlatform("p1", "Test", nil, nil)
	p.ExcludeProtocolFilters = []string{"vmess"}
	rawOptions := []byte(`{"type":"unknown_proto"}`)
	h := makeHash(string(rawOptions))
	entry := makeFullyRoutableEntryWithRawOptions(h, rawOptions, "sub1")

	p.FullRebuild(func(fn func(node.Hash, *node.NodeEntry) bool) {
		fn(h, entry)
	}, alwaysLookup, usGeoLookup)

	if p.View().Size() != 0 {
		t.Fatal("unknown protocol node should be rejected when any protocol filter is active")
	}
}
