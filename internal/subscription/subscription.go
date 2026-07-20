// Package subscription provides subscription types and parsing logic.
package subscription

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/puzpuzpuz/xsync/v4"
)

const defaultEphemeralNodeEvictDelayNs = int64(72 * time.Hour)

const (
	// SourceTypeRemote pulls subscription content over HTTP(S) from URL.
	SourceTypeRemote = "remote"
	// SourceTypeLocal reads subscription content from local text content.
	SourceTypeLocal = "local"
)

const (
	// UpdateModeReplace replaces current subscription nodes with refreshed content.
	UpdateModeReplace = false
	// UpdateModeIncrementalAlive keeps existing non-evicted nodes and merges refreshed content.
	UpdateModeIncrementalAlive = true
)

// Subscription update mode constants.
const (
	UpdateModeInterval = "interval"
	UpdateModeDaily    = "daily"
)

// ManagedNode represents one hash entry in subscription managed nodes.
type ManagedNode struct {
	Tags    []string
	Evicted bool
}

// ManagedNodes wraps hash->ManagedNode map.
//
// Maintenance rule:
//   - StoreNode makes a defensive copy of input Tags.
//   - LoadNode/RangeNodes return direct references to stored tag slices.
//   - Callers must treat returned Tags as read-only and must not mutate them.
//   - If mutation is needed, make an explicit copy first.
type ManagedNodes struct {
	m *xsync.Map[node.Hash, ManagedNode]
}

// NewManagedNodes creates an empty managed-node view.
func NewManagedNodes() *ManagedNodes {
	return &ManagedNodes{m: xsync.NewMap[node.Hash, ManagedNode]()}
}

// Size returns the count of hash entries (including evicted entries).
func (mn *ManagedNodes) Size() int {
	if mn == nil || mn.m == nil {
		return 0
	}
	return mn.m.Size()
}

// LoadNode loads the full managed-node state for a hash.
// Tags are returned as-is (no copy); treat them as read-only.
func (mn *ManagedNodes) LoadNode(h node.Hash) (ManagedNode, bool) {
	if mn == nil || mn.m == nil {
		return ManagedNode{}, false
	}
	n, ok := mn.m.Load(h)
	if !ok {
		return ManagedNode{}, false
	}
	return n, true
}

// StoreNode stores the full managed-node state for a hash.
// Tags are defensively copied on store.
func (mn *ManagedNodes) StoreNode(h node.Hash, n ManagedNode) {
	if mn == nil || mn.m == nil {
		return
	}
	mn.m.Store(h, ManagedNode{
		Tags:    cloneTags(n.Tags),
		Evicted: n.Evicted,
	})
}

// Delete deletes a hash entry.
func (mn *ManagedNodes) Delete(h node.Hash) {
	if mn == nil || mn.m == nil {
		return
	}
	mn.m.Delete(h)
}

// RangeNodes iterates hash->ManagedNode entries.
// ManagedNode.Tags is provided as-is (no copy); treat it as read-only.
func (mn *ManagedNodes) RangeNodes(fn func(node.Hash, ManagedNode) bool) {
	if mn == nil || mn.m == nil || fn == nil {
		return
	}
	mn.m.Range(fn)
}

// Subscription represents a subscription's runtime state.
// It has two synchronization layers:
//   - mu protects mutable config fields
//     (url/updateInterval/name/enabled/ephemeral/ephemeralNodeEvictDelayNs).
//   - opMu serializes high-level operations (update/rename/eviction/delete)
//     on the same subscription instance.
//
// Lock-order rule (must be preserved to avoid deadlocks):
//   - If both locks are needed in one flow, always acquire opMu before mu.
//   - Never call WithOpLock from code that already holds mu.
type Subscription struct {
	// Immutable after creation.
	ID string

	// Operation-level lock for serializing multi-step workflows.
	opMu sync.Mutex

	// Mutable fields guarded by mu.
	mu         sync.RWMutex
	url        string
	sourceType string
	content    string
	// updateIntervalNs is the configured subscription refresh interval.
	updateIntervalNs      int64
	name                  string
	enabled               bool
	ephemeral             bool
	incrementalAliveNodes bool
	// ephemeralNodeEvictDelayNs is the per-subscription eviction delay for
	// circuit-broken nodes when Ephemeral is enabled.
	ephemeralNodeEvictDelayNs int64
	// clashFingerprintPolicy controls Clash certificate fingerprint handling
	// during subscription parsing. Default is ClashFingerprintReject.
	clashFingerprintPolicy ClashFingerprintPolicy

	// updateMode is the subscription refresh schedule mode: "interval" or "daily".
	updateMode string
	// updateTime is the daily scheduled time in "HH:mm" format (ignored in interval mode).
	updateTime string
	// updateTimezone is the IANA timezone for daily scheduling (ignored in interval mode).
	updateTimezone string

	// Persistence timestamps (written under mu or single-writer context).
	CreatedAtNs int64
	UpdatedAtNs int64

	// Runtime-only fields (NOT persisted). Atomic for lock-free reads
	// from the scheduler's due-check loop.
	LastCheckedNs atomic.Int64
	LastUpdatedNs atomic.Int64
	LastError     atomic.Pointer[string]

	// Scheduler sequencing for stale-attempt guards.
	attemptSeq     atomic.Int64
	lastAppliedSeq atomic.Int64

	// managedNodes is the subscription's node view: Hash → ManagedNode.
	// Swapped atomically on subscription update.
	managedNodes atomic.Pointer[ManagedNodes]

	// configVersion is incremented whenever refresh-input-related config changes
	// (URL, source_type, content, update_interval, clash_fingerprint_policy).
	// Mode/time/timezone do NOT bump it — they only affect scheduling, not
	// refresh input. Scheduler uses configVersion for stale-guard.
	configVersion atomic.Int64
}

// NewSubscription creates a Subscription with an empty ManagedNodes map.
func NewSubscription(id, name, url string, enabled, ephemeral bool) *Subscription {
	s := &Subscription{
		ID:                        id,
		url:                       url,
		sourceType:                SourceTypeRemote,
		updateMode:                UpdateModeInterval,
		updateTime:                "",
		updateTimezone:            "",
		name:                      name,
		enabled:                   enabled,
		ephemeral:                 ephemeral,
		incrementalAliveNodes:     UpdateModeReplace,
		ephemeralNodeEvictDelayNs: defaultEphemeralNodeEvictDelayNs,
		clashFingerprintPolicy:    ClashFingerprintReject,
	}
	empty := NewManagedNodes()
	s.managedNodes.Store(empty)
	emptyErr := ""
	s.LastError.Store(&emptyErr)
	s.configVersion.Store(1)
	return s
}

// SetLastError atomically sets the last error string.
func (s *Subscription) SetLastError(err string) { s.LastError.Store(&err) }

// GetLastError atomically loads the last error string.
func (s *Subscription) GetLastError() string { return *s.LastError.Load() }

// NextAttemptSeq returns a strictly increasing sequence for refresh attempts.
func (s *Subscription) NextAttemptSeq() int64 { return s.attemptSeq.Add(1) }

// LastAppliedSeq returns the latest applied refresh attempt sequence.
func (s *Subscription) LastAppliedSeq() int64 { return s.lastAppliedSeq.Load() }

// MarkAppliedAttempt records the latest applied refresh attempt sequence.
func (s *Subscription) MarkAppliedAttempt(seq int64) { s.lastAppliedSeq.Store(seq) }

// WithOpLock runs fn under the subscription operation lock.
func (s *Subscription) WithOpLock(fn func()) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	fn()
}

// URL returns the subscription source URL (thread-safe).
func (s *Subscription) URL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.url
}

// SourceType returns the subscription source type (thread-safe).
func (s *Subscription) SourceType() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeSourceType(s.sourceType)
}

// Content returns the local subscription content (thread-safe).
func (s *Subscription) Content() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.content
}

// ConfigVersion returns the scheduler input config version.
func (s *Subscription) ConfigVersion() int64 {
	return s.configVersion.Load()
}

// UpdateIntervalNs returns the configured update interval in nanoseconds (thread-safe).
func (s *Subscription) UpdateIntervalNs() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updateIntervalNs
}

// SetFetchConfig updates URL and update interval together atomically under lock.
func (s *Subscription) SetFetchConfig(url string, updateIntervalNs int64) {
	s.mu.Lock()
	changed := s.url != url || s.updateIntervalNs != updateIntervalNs
	s.url = url
	s.updateIntervalNs = updateIntervalNs
	if changed {
		s.configVersion.Add(1)
	}
	s.mu.Unlock()
}

// SetSourceType updates subscription source type (thread-safe).
func (s *Subscription) SetSourceType(sourceType string) {
	sourceType = normalizeSourceType(sourceType)
	s.mu.Lock()
	if s.sourceType != sourceType {
		s.sourceType = sourceType
		s.configVersion.Add(1)
	}
	s.mu.Unlock()
}

// SetContent updates local subscription content (thread-safe).
func (s *Subscription) SetContent(content string) {
	s.mu.Lock()
	if s.content != content {
		s.content = content
		s.configVersion.Add(1)
	}
	s.mu.Unlock()
}

// Name returns the subscription name (thread-safe).
func (s *Subscription) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

// SetName updates the subscription name (thread-safe).
func (s *Subscription) SetName(name string) {
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
}

// Enabled returns whether the subscription is enabled (thread-safe).
func (s *Subscription) Enabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// SetEnabled updates the enabled flag (thread-safe).
func (s *Subscription) SetEnabled(v bool) {
	s.mu.Lock()
	s.enabled = v
	s.mu.Unlock()
}

// Ephemeral returns whether the subscription is ephemeral (thread-safe).
func (s *Subscription) Ephemeral() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ephemeral
}

// SetEphemeral updates the ephemeral flag (thread-safe).
func (s *Subscription) SetEphemeral(v bool) {
	s.mu.Lock()
	s.ephemeral = v
	s.mu.Unlock()
}

// IncrementalAliveNodes returns whether refresh keeps existing non-evicted nodes (thread-safe).
func (s *Subscription) IncrementalAliveNodes() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.incrementalAliveNodes
}

// SetIncrementalAliveNodes updates the refresh merge mode (thread-safe).
func (s *Subscription) SetIncrementalAliveNodes(v bool) {
	s.mu.Lock()
	s.incrementalAliveNodes = v
	s.mu.Unlock()
}

// EphemeralNodeEvictDelayNs returns the per-subscription eviction delay in nanoseconds.
func (s *Subscription) EphemeralNodeEvictDelayNs() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ephemeralNodeEvictDelayNs
}

// SetEphemeralNodeEvictDelayNs updates the per-subscription eviction delay.
func (s *Subscription) SetEphemeralNodeEvictDelayNs(v int64) {
	s.mu.Lock()
	s.ephemeralNodeEvictDelayNs = v
	s.mu.Unlock()
}

// ClashFingerprintPolicy returns the clash fingerprint policy (thread-safe).
func (s *Subscription) ClashFingerprintPolicy() ClashFingerprintPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clashFingerprintPolicy
}

// SetClashFingerprintPolicy updates the clash fingerprint policy (thread-safe).
// Increments configVersion when the value changes so in-flight stale parses
// are rejected and a new refresh is triggered downstream.
func (s *Subscription) SetClashFingerprintPolicy(v ClashFingerprintPolicy) {
	s.mu.Lock()
	if s.clashFingerprintPolicy != v {
		s.clashFingerprintPolicy = v
		s.configVersion.Add(1)
	}
	s.mu.Unlock()
}

// UpdateMode returns the subscription update mode (thread-safe).
func (s *Subscription) UpdateMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updateMode
}

// SetUpdateMode updates the subscription update mode (thread-safe).
// Does NOT bump configVersion because schedule mode does not affect refresh
// input (URL, content, fingerprint policy) and must not invalidate an
// in-flight refresh attempt. This is consistent with SetUpdateTime and
// SetUpdateTimezone.
func (s *Subscription) SetUpdateMode(v string) {
	s.mu.Lock()
	s.updateMode = v
	s.mu.Unlock()
}

// UpdateTime returns the daily scheduled update time "HH:mm" (thread-safe).
func (s *Subscription) UpdateTime() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updateTime
}

// SetUpdateTime sets the daily scheduled update time (thread-safe).
func (s *Subscription) SetUpdateTime(v string) {
	s.mu.Lock()
	s.updateTime = v
	s.mu.Unlock()
}

// UpdateTimezone returns the IANA timezone for daily scheduling (thread-safe).
func (s *Subscription) UpdateTimezone() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updateTimezone
}

// SetUpdateTimezone sets the IANA timezone for daily scheduling (thread-safe).
func (s *Subscription) SetUpdateTimezone(v string) {
	s.mu.Lock()
	s.updateTimezone = v
	s.mu.Unlock()
}

// ManagedNodes returns the current node view via atomic load.
func (s *Subscription) ManagedNodes() *ManagedNodes {
	return s.managedNodes.Load()
}

// SwapManagedNodes atomically replaces the managed nodes view.
func (s *Subscription) SwapManagedNodes(m *ManagedNodes) {
	s.managedNodes.Store(m)
}

// DiffHashes computes the hash diff between old and new managed-nodes maps.
// Returns slices of added, kept, and removed hashes.
func DiffHashes(
	oldMap, newMap *ManagedNodes,
) (added, kept, removed []node.Hash) {
	// Hashes only in new → added. Hashes in both → kept.
	newMap.RangeNodes(func(h node.Hash, _ ManagedNode) bool {
		if _, ok := oldMap.LoadNode(h); ok {
			kept = append(kept, h)
		} else {
			added = append(added, h)
		}
		return true
	})

	// Hashes only in old → removed.
	oldMap.RangeNodes(func(h node.Hash, _ ManagedNode) bool {
		if _, ok := newMap.LoadNode(h); !ok {
			removed = append(removed, h)
		}
		return true
	})

	return added, kept, removed
}

// IsSubscriptionDue determines whether a subscription is due for refresh
// at the given time. For interval mode it checks lastChecked + interval <= now.
// For daily mode it checks whether lastChecked < most recent scheduled time <= now.
// This is a pure function for deterministic testing — inject now instead of time.Now().
func IsSubscriptionDue(lastCheckedNs int64, now time.Time, updateMode string, updateIntervalNs int64, updateTime, updateTimezone string) bool {
	if updateMode == UpdateModeDaily {
		if updateTime == "" || updateTimezone == "" {
			return false // no valid daily config — never due
		}
		loc, err := time.LoadLocation(updateTimezone)
		if err != nil {
			return false // invalid timezone — never due
		}
		h, m, err := ParseHHMM(updateTime)
		if err != nil {
			return false // invalid time — never due
		}

		nowInLoc := now.In(loc)
		todayScheduled := time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), h, m, 0, 0, loc)

		if todayScheduled.After(nowInLoc) {
			// Today's scheduled time hasn't arrived yet.
			// For never-checked subs (lastChecked=0), wait for the scheduled time.
			// Otherwise, use yesterday's scheduled time as the most recent moment.
			if lastCheckedNs == 0 {
				return false // not yet due — wait for today's scheduled time
			}
			yesterdayScheduled := todayScheduled.AddDate(0, 0, -1)
			lastChecked := time.Unix(0, lastCheckedNs)
			return lastChecked.Before(yesterdayScheduled)
		}

		// Today's scheduled time has arrived or is now.
		mostRecentScheduled := todayScheduled
		lastChecked := time.Unix(0, lastCheckedNs)
		return lastChecked.Before(mostRecentScheduled) && !mostRecentScheduled.After(nowInLoc)
	}

	// Interval mode: last + interval - lookahead <= now.
	const schedulerLookahead = 15 * time.Second
	return lastCheckedNs+updateIntervalNs-int64(schedulerLookahead) <= now.UnixNano()
}

// ParseHHMM parses a "HH:mm" string into hour and minute components.
func ParseHHMM(s string) (hour, min int, err error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, fmt.Errorf("invalid time format %q, expected HH:mm", s)
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid time %q, expected HH:mm", s)
	}
	return h, m, nil
}

func normalizeSourceType(sourceType string) string {
	switch sourceType {
	case SourceTypeLocal:
		return SourceTypeLocal
	default:
		return SourceTypeRemote
	}
}

func cloneTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	cp := make([]string, len(tags))
	copy(cp, tags)
	return cp
}
