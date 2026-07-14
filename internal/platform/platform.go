package platform

import (
	"net/netip"
	"regexp"
	"sync"

	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/node"
)

// builtInQualityProfiles lists known built-in quality profiles.
var builtInQualityProfiles = map[string]bool{
	"generic": true,
	"openai":  true,
	"grok":    true,
	"gemini":  true,
	"claude":  true,
}

// DefaultPlatformID is the well-known UUID of the built-in Default platform.
const DefaultPlatformID = "00000000-0000-0000-0000-000000000000"

// DefaultPlatformName is the built-in platform name.
const DefaultPlatformName = "Default"

// GeoLookupFunc resolves an IP address to a lowercase ISO country code.
type GeoLookupFunc func(netip.Addr) string

// PoolRangeFunc iterates all nodes in the global pool.
type PoolRangeFunc func(fn func(node.Hash, *node.NodeEntry) bool)

// GetEntryFunc retrieves a node entry from the global pool by hash.
type GetEntryFunc func(node.Hash) (*node.NodeEntry, bool)

// Platform represents a routing platform with its filtered routable view.
type Platform struct {
	ID   string
	Name string

	// Filter configuration.
	RegexFilters           []*regexp.Regexp
	RegionFilters          []string // lowercase ISO codes, supports negation "!xx"
	ProtocolFilters        []string // canonical protocol names for include filtering
	ExcludeProtocolFilters []string // canonical protocol names for exclude filtering

	// Quality filter configuration.
	// Empty/zero/nil values mean "no filter".
	QualityGrade                string  // empty = no filter, else A/B/C/D/F
	QualityMinScore             float64 // 0 = no filter, else 0..100
	QualityCloudflareChallenged *bool   // nil = no filter, true/false = explicit match
	QualityCheckedSinceNs       int64   // 0 = no filter, else nanoseconds timestamp
	QualityProfile              string  // empty = no filter, else built-in profile name

	// Other config fields.
	StickyTTLNs                      int64
	ReverseProxyMissAction           string
	ReverseProxyEmptyAccountBehavior string
	ReverseProxyFixedAccountHeader   string
	ReverseProxyFixedAccountHeaders  []string
	AllocationPolicy                 AllocationPolicy
	PassiveCircuitBreakerDisabled    bool

	// Routable view & its lock.
	// viewMu serializes both FullRebuild and NotifyDirty.
	view   *RoutableView
	viewMu sync.Mutex
}

// NewPlatform creates a Platform with an empty routable view.
func NewPlatform(id, name string, regexFilters []*regexp.Regexp, regionFilters []string) *Platform {
	return &Platform{
		ID:            id,
		Name:          name,
		RegexFilters:  regexFilters,
		RegionFilters: regionFilters,
		view:          NewRoutableView(),
	}
}

// View returns the platform's routable view as a read-only interface.
// External callers cannot Add/Remove/Clear — only FullRebuild and NotifyDirty can mutate.
func (p *Platform) View() ReadOnlyView {
	return p.view
}

// FullRebuild clears the routable view and re-evaluates all nodes from the pool.
// Acquires viewMu — any concurrent NotifyDirty calls block until rebuild completes.
func (p *Platform) FullRebuild(
	poolRange PoolRangeFunc,
	subLookup node.SubLookupFunc,
	geoLookup GeoLookupFunc,
) {
	p.viewMu.Lock()
	defer p.viewMu.Unlock()

	p.view.Clear()
	poolRange(func(h node.Hash, entry *node.NodeEntry) bool {
		if p.evaluateNode(entry, subLookup, geoLookup) {
			p.view.Add(h)
		}
		return true
	})
}

// NotifyDirty re-evaluates a single node and adds/removes it from the view.
// Acquires viewMu — serialized with FullRebuild.
func (p *Platform) NotifyDirty(
	h node.Hash,
	getEntry GetEntryFunc,
	subLookup node.SubLookupFunc,
	geoLookup GeoLookupFunc,
) {
	p.viewMu.Lock()
	defer p.viewMu.Unlock()

	entry, ok := getEntry(h)
	if !ok {
		// Node was deleted from pool.
		p.view.Remove(h)
		return
	}

	if p.evaluateNode(entry, subLookup, geoLookup) {
		p.view.Add(h)
	} else {
		p.view.Remove(h)
	}
}

// evaluateNode checks all filter conditions for platform routability.
func (p *Platform) evaluateNode(
	entry *node.NodeEntry,
	subLookup node.SubLookupFunc,
	geoLookup GeoLookupFunc,
) bool {
	// 0. Disabled nodes are never routable.
	if entry.IsDisabledBySubscriptions(subLookup) {
		return false
	}

	// 1. Healthy for routing (outbound ready + circuit not open).
	if !entry.IsHealthy() {
		return false
	}

	// 2. Tag regex match.
	if !entry.MatchRegexs(p.RegexFilters, subLookup) {
		return false
	}

	// 3. Protocol filter (include/exclude).
	if !p.matchProtocolFilters(entry) {
		return false
	}

	// 4. Egress IP must be known.
	egressIP := entry.GetEgressIP()
	if !egressIP.IsValid() {
		return false
	}

	// 5. Region filter (when configured).
	if len(p.RegionFilters) > 0 {
		region := entry.GetRegion(geoLookup)
		if !MatchRegionFilter(region, p.RegionFilters) {
			return false
		}
	}

	// 6. Has at least one latency record.
	if !entry.HasLatency() {
		return false
	}

	// 7. Quality filters.
	if !p.matchQualityFilters(entry) {
		return false
	}

	return true
}

// matchProtocolFilters checks whether the node's protocol satisfies the
// platform's include/exclude protocol filter rules.
//
//   - When ProtocolFilters is non-empty, the node's protocol must be in the set.
//   - When ExcludeProtocolFilters is non-empty, the node's protocol must NOT be in the set.
//   - Unknown/unparseable protocols fail any active protocol filter.
func (p *Platform) matchProtocolFilters(entry *node.NodeEntry) bool {
	if len(p.ProtocolFilters) == 0 && len(p.ExcludeProtocolFilters) == 0 {
		return true
	}
	canonical := node.RawOptionsProtocol(entry.RawOptions)
	if canonical == "" {
		return false
	}
	if len(p.ProtocolFilters) > 0 {
		found := false
		for _, f := range p.ProtocolFilters {
			if f == canonical {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(p.ExcludeProtocolFilters) > 0 {
		for _, f := range p.ExcludeProtocolFilters {
			if f == canonical {
				return false
			}
		}
	}
	return true
}

// matchQualityFilters checks the node's quality result against the platform's
// quality filter configuration. Any active quality filter rejects nodes with
// no quality result (nil). When all quality filters are empty/zero/nil, every
// node passes regardless of quality state.
func (p *Platform) matchQualityFilters(entry *node.NodeEntry) bool {
	if p.QualityGrade == "" && p.QualityMinScore == 0 &&
		p.QualityCloudflareChallenged == nil && p.QualityCheckedSinceNs == 0 &&
		p.QualityProfile == "" {
		return true // no quality filters active
	}

	q := entry.GetQuality()
	if q == nil {
		return false // any active quality filter rejects missing quality
	}

	// Grade filter: empty means no filter, otherwise A/B/C/D/F.
	if p.QualityGrade != "" && q.Grade != p.QualityGrade {
		return false
	}

	// Min score filter: 0 means no filter, otherwise 0..100.
	if p.QualityMinScore > 0 && q.Score < p.QualityMinScore {
		return false
	}

	// Cloudflare challenged filter: nil means no filter, true/false = explicit match.
	if p.QualityCloudflareChallenged != nil && q.CloudflareChallenged != *p.QualityCloudflareChallenged {
		return false
	}

	// Checked since filter: 0 means no filter, otherwise nanoseconds timestamp.
	if p.QualityCheckedSinceNs > 0 && q.LastCheckedNs < p.QualityCheckedSinceNs {
		return false
	}

	// Profile filter: empty means no filter, otherwise must match exactly.
	if p.QualityProfile != "" && q.Profile != p.QualityProfile {
		return false
	}

	return true
}

// IsBuiltInQualityProfile returns true if the given profile name matches a
// known built-in quality profile.
func IsBuiltInQualityProfile(profile string) bool {
	return builtInQualityProfiles[profile]
}

// MatchRegionFilter applies include/exclude region filters.
// Positive entries (xx) build an include set; negative entries (!xx) build an exclude set.
// Unknown regions never match when region filters are configured.
// Final result is: region known AND (include empty OR region in include) AND (region not in exclude).
func MatchRegionFilter(region string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	if region == "" {
		return false
	}

	included := false
	hasInclude := false

	for _, filter := range filters {
		if len(filter) > 0 && filter[0] == '!' {
			if region == filter[1:] {
				return false
			}
			continue
		}
		hasInclude = true
		if region == filter {
			included = true
		}
	}

	if hasInclude && !included {
		return false
	}
	return true
}
