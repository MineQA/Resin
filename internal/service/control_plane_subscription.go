package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/state"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/topology"
)

// ------------------------------------------------------------------
// Subscription
// ------------------------------------------------------------------

// SubscriptionResponse is the API response for a subscription.
type SubscriptionResponse struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	SourceType              string `json:"source_type"`
	URL                     string `json:"url"`
	Content                 string `json:"content"`
	UpdateInterval          string `json:"update_interval"`
	UpdateMode              string `json:"update_mode"`
	UpdateTime              string `json:"update_time"`
	UpdateTimezone          string `json:"update_timezone"`
	NodeCount               int    `json:"node_count"`
	HealthyNodeCount        int    `json:"healthy_node_count"`
	Ephemeral               bool   `json:"ephemeral"`
	IncrementalAliveNodes   bool   `json:"incremental_alive_nodes"`
	EphemeralNodeEvictDelay string `json:"ephemeral_node_evict_delay"`
	ClashFingerprintPolicy  string `json:"clash_fingerprint_policy"`
	Enabled                 bool   `json:"enabled"`
	CreatedAt               string `json:"created_at"`
	LastChecked             string `json:"last_checked,omitempty"`
	LastUpdated             string `json:"last_updated,omitempty"`
	LastError               string `json:"last_error,omitempty"`
}

func (s *ControlPlaneService) subToResponse(sub *subscription.Subscription) SubscriptionResponse {
	nodeCount := 0
	healthyNodeCount := 0
	var isHealthyAndEnabled func(*node.NodeEntry) bool
	if sub.Enabled() && s != nil && s.Pool != nil {
		isHealthyAndEnabled = s.Pool.MakeHealthyAndEnabledEvaluator()
	}
	if managed := sub.ManagedNodes(); managed != nil {
		managed.RangeNodes(func(h node.Hash, n subscription.ManagedNode) bool {
			if n.Evicted {
				return true
			}
			nodeCount++
			if isHealthyAndEnabled != nil {
				entry, ok := s.Pool.GetEntry(h)
				if ok && isHealthyAndEnabled(entry) {
					healthyNodeCount++
				}
			}
			return true
		})
	}

	resp := SubscriptionResponse{
		ID:                      sub.ID,
		Name:                    sub.Name(),
		SourceType:              sub.SourceType(),
		URL:                     sub.URL(),
		Content:                 sub.Content(),
		UpdateInterval:          time.Duration(sub.UpdateIntervalNs()).String(),
		UpdateMode:              sub.UpdateMode(),
		UpdateTime:              sub.UpdateTime(),
		UpdateTimezone:          sub.UpdateTimezone(),
		NodeCount:               nodeCount,
		HealthyNodeCount:        healthyNodeCount,
		Ephemeral:               sub.Ephemeral(),
		IncrementalAliveNodes:   sub.IncrementalAliveNodes(),
		EphemeralNodeEvictDelay: time.Duration(sub.EphemeralNodeEvictDelayNs()).String(),
		ClashFingerprintPolicy:  sub.ClashFingerprintPolicy().String(),
		Enabled:                 sub.Enabled(),
		CreatedAt:               time.Unix(0, sub.CreatedAtNs).UTC().Format(time.RFC3339Nano),
	}
	if lc := sub.LastCheckedNs.Load(); lc > 0 {
		resp.LastChecked = time.Unix(0, lc).UTC().Format(time.RFC3339Nano)
	}
	if lu := sub.LastUpdatedNs.Load(); lu > 0 {
		resp.LastUpdated = time.Unix(0, lu).UTC().Format(time.RFC3339Nano)
	}
	resp.LastError = sub.GetLastError()
	return resp
}

// ListSubscriptions returns all subscriptions, optionally filtered by enabled.
func (s *ControlPlaneService) ListSubscriptions(enabled *bool) ([]SubscriptionResponse, error) {
	var result []SubscriptionResponse
	s.SubMgr.Range(func(id string, sub *subscription.Subscription) bool {
		if enabled != nil && sub.Enabled() != *enabled {
			return true
		}
		result = append(result, s.subToResponse(sub))
		return true
	})
	if result == nil {
		result = []SubscriptionResponse{}
	}
	return result, nil
}

// GetSubscription returns a single subscription by ID.
func (s *ControlPlaneService) GetSubscription(id string) (*SubscriptionResponse, error) {
	sub := s.SubMgr.Lookup(id)
	if sub == nil {
		return nil, notFound("subscription not found")
	}
	r := s.subToResponse(sub)
	return &r, nil
}

// CreateSubscriptionRequest holds create subscription parameters.
type CreateSubscriptionRequest struct {
	Name                    *string `json:"name"`
	SourceType              *string `json:"source_type"`
	URL                     *string `json:"url"`
	Content                 *string `json:"content"`
	UpdateInterval          *string `json:"update_interval"`
	UpdateMode              *string `json:"update_mode"`
	UpdateTime              *string `json:"update_time"`
	UpdateTimezone          *string `json:"update_timezone"`
	Enabled                 *bool   `json:"enabled"`
	Ephemeral               *bool   `json:"ephemeral"`
	IncrementalAliveNodes   *bool   `json:"incremental_alive_nodes"`
	EphemeralNodeEvictDelay *string `json:"ephemeral_node_evict_delay"`
	ClashFingerprintPolicy  *string `json:"clash_fingerprint_policy"`
}

const minSubscriptionUpdateInterval = 30 * time.Second
const defaultSubscriptionEphemeralNodeEvictDelay = 72 * time.Hour

func validateClashFingerprintPolicy(raw string) (subscription.ClashFingerprintPolicy, *ServiceError) {
	switch raw {
	case "reject", "drop_safe", "drop_always":
		return subscription.ParseClashFingerprintPolicy(raw), nil
	default:
		return 0, invalidArg("clash_fingerprint_policy: must be one of reject, drop_safe, drop_always")
	}
}

func parseUpdateMode(raw *string) (string, *ServiceError) {
	if raw == nil {
		return subscription.UpdateModeInterval, nil
	}
	v := strings.ToLower(strings.TrimSpace(*raw))
	switch v {
	case subscription.UpdateModeInterval, subscription.UpdateModeDaily:
		return v, nil
	default:
		return "", invalidArg("update_mode: must be \"interval\" or \"daily\"")
	}
}

func validateUpdateTime(raw string) (string, *ServiceError) {
	if _, _, err := subscription.ParseHHMM(raw); err != nil {
		return "", invalidArg("update_time: " + err.Error())
	}
	return raw, nil
}

func validateUpdateTimezone(raw string) (string, *ServiceError) {
	if _, err := time.LoadLocation(raw); err != nil {
		return "", invalidArg("update_timezone: invalid IANA timezone: " + err.Error())
	}
	return raw, nil
}

func parseSubscriptionSourceType(raw *string) (string, *ServiceError) {
	if raw == nil {
		return subscription.SourceTypeRemote, nil
	}
	value := strings.ToLower(strings.TrimSpace(*raw))
	switch value {
	case subscription.SourceTypeRemote, subscription.SourceTypeLocal:
		return value, nil
	default:
		return "", invalidArg("source_type: must be remote or local")
	}
}

// CreateSubscription creates a new subscription.
func (s *ControlPlaneService) CreateSubscription(req CreateSubscriptionRequest) (*SubscriptionResponse, error) {
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		return nil, invalidArg("name is required")
	}
	name := strings.TrimSpace(*req.Name)

	sourceType, verr := parseSubscriptionSourceType(req.SourceType)
	if verr != nil {
		return nil, verr
	}

	subURL := ""
	content := ""
	switch sourceType {
	case subscription.SourceTypeRemote:
		if req.URL == nil || strings.TrimSpace(*req.URL) == "" {
			return nil, invalidArg("url is required for remote subscription")
		}
		subURL = strings.TrimSpace(*req.URL)
		if _, verr := parseHTTPAbsoluteURL("url", subURL); verr != nil {
			return nil, verr
		}
		if req.Content != nil && strings.TrimSpace(*req.Content) != "" {
			return nil, invalidArg("content is not allowed for remote subscription")
		}
	case subscription.SourceTypeLocal:
		if req.Content == nil || strings.TrimSpace(*req.Content) == "" {
			return nil, invalidArg("content is required for local subscription")
		}
		content = *req.Content
		if req.URL != nil && strings.TrimSpace(*req.URL) != "" {
			return nil, invalidArg("url is not allowed for local subscription")
		}
	default:
		return nil, invalidArg("source_type: must be remote or local")
	}

	updateMode, verr := parseUpdateMode(req.UpdateMode)
	if verr != nil {
		return nil, verr
	}

	updateTime := ""
	if req.UpdateTime != nil {
		updateTime = strings.TrimSpace(*req.UpdateTime)
	}
	updateTimezone := ""
	if req.UpdateTimezone != nil {
		updateTimezone = strings.TrimSpace(*req.UpdateTimezone)
	}

	updateInterval := 5 * time.Minute
	// update_interval is hard-validated only in interval mode. In daily mode a
	// retained value may still be stored for seamless mode switching (must still
	// satisfy the persistence floor of >=30s); unparseable or below-min values
	// fall back to the default and are ignored by the scheduler.
	if req.UpdateInterval != nil {
		d, err := time.ParseDuration(*req.UpdateInterval)
		if err != nil {
			if updateMode == subscription.UpdateModeInterval {
				return nil, invalidArg("update_interval: " + err.Error())
			}
			// daily mode: keep default when retained interval is unparseable
		} else if d < minSubscriptionUpdateInterval {
			if updateMode == subscription.UpdateModeInterval {
				return nil, invalidArg("update_interval: must be >= 30s")
			}
			// daily mode: keep default when retained interval is below persistence floor
		} else {
			updateInterval = d
		}
	}

	// Validate daily-only fields when mode is daily.
	if updateMode == subscription.UpdateModeDaily {
		if sourceType == subscription.SourceTypeLocal {
			return nil, invalidArg("update_mode: local subscriptions cannot use \"daily\" mode; use \"interval\" instead")
		}
		if updateTime == "" {
			return nil, invalidArg("update_time is required when update_mode is \"daily\"")
		}
		if _, verr := validateUpdateTime(updateTime); verr != nil {
			return nil, verr
		}
		if updateTimezone == "" {
			return nil, invalidArg("update_timezone is required when update_mode is \"daily\"")
		}
		if _, verr := validateUpdateTimezone(updateTimezone); verr != nil {
			return nil, verr
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	ephemeral := false
	if req.Ephemeral != nil {
		ephemeral = *req.Ephemeral
	}
	incrementalAliveNodes := false
	if req.IncrementalAliveNodes != nil {
		incrementalAliveNodes = *req.IncrementalAliveNodes
	}
	ephemeralNodeEvictDelay := defaultSubscriptionEphemeralNodeEvictDelay
	if req.EphemeralNodeEvictDelay != nil {
		d, err := time.ParseDuration(*req.EphemeralNodeEvictDelay)
		if err != nil {
			return nil, invalidArg("ephemeral_node_evict_delay: " + err.Error())
		}
		if d < 0 {
			return nil, invalidArg("ephemeral_node_evict_delay: must be non-negative")
		}
		ephemeralNodeEvictDelay = d
	}

	clashFingerprintPolicy := subscription.ClashFingerprintReject
	if req.ClashFingerprintPolicy != nil {
		v, verr := validateClashFingerprintPolicy(*req.ClashFingerprintPolicy)
		if verr != nil {
			return nil, verr
		}
		clashFingerprintPolicy = v
	}

	id := uuid.New().String()
	now := time.Now().UnixNano()

	ms := model.Subscription{
		ID:                        id,
		Name:                      name,
		SourceType:                sourceType,
		URL:                       subURL,
		Content:                   content,
		UpdateIntervalNs:          int64(updateInterval),
		UpdateMode:                updateMode,
		UpdateTime:                updateTime,
		UpdateTimezone:            updateTimezone,
		Enabled:                   enabled,
		Ephemeral:                 ephemeral,
		IncrementalAliveNodes:     incrementalAliveNodes,
		EphemeralNodeEvictDelayNs: int64(ephemeralNodeEvictDelay),
		ClashFingerprintPolicy:    clashFingerprintPolicy.String(),
		CreatedAtNs:               now,
		UpdatedAtNs:               now,
	}
	if err := s.Engine.UpsertSubscription(ms); err != nil {
		return nil, internal("persist subscription", err)
	}

	sub := subscription.NewSubscription(id, name, subURL, enabled, ephemeral)
	sub.SetFetchConfig(subURL, int64(updateInterval))
	sub.SetSourceType(sourceType)
	sub.SetContent(content)
	sub.SetUpdateMode(updateMode)
	sub.SetUpdateTime(updateTime)
	sub.SetUpdateTimezone(updateTimezone)
	sub.SetIncrementalAliveNodes(incrementalAliveNodes)
	sub.SetEphemeralNodeEvictDelayNs(int64(ephemeralNodeEvictDelay))
	sub.SetClashFingerprintPolicy(clashFingerprintPolicy)
	sub.CreatedAtNs = now
	sub.UpdatedAtNs = now
	s.SubMgr.Register(sub)

	r := s.subToResponse(sub)
	return &r, nil
}

// UpdateSubscription applies a constrained partial patch to a subscription.
// This is not RFC 7396 JSON Merge Patch: patch must be a non-empty object and
// null values are rejected.
func (s *ControlPlaneService) UpdateSubscription(id string, patchJSON json.RawMessage) (*SubscriptionResponse, error) {
	patch, verr := parseMergePatch(patchJSON)
	if verr != nil {
		return nil, verr
	}
	if err := patch.validateFields(subscriptionPatchAllowedFields, func(key string) string {
		return fmt.Sprintf("field %q is read-only or unknown", key)
	}); err != nil {
		return nil, err
	}

	sub := s.SubMgr.Lookup(id)
	if sub == nil {
		return nil, notFound("subscription not found")
	}

	// Track what changed for side-effects.
	nameChanged := false
	enabledChanged := false
	urlChanged := false
	contentChanged := false
	clashFingerprintPolicyChanged := false
	sourceType := sub.SourceType()

	newName := sub.Name()
	if nameStr, ok, err := patch.optionalNonEmptyString("name"); err != nil {
		return nil, err
	} else if ok {
		newName = nameStr
		if newName != sub.Name() {
			nameChanged = true
		}
	}

	newURL := sub.URL()
	if urlStr, ok, err := patch.optionalString("url"); err != nil {
		return nil, err
	} else if ok {
		if sourceType != subscription.SourceTypeRemote {
			return nil, invalidArg("url: field is not allowed for local subscription")
		}
		if _, verr := parseHTTPAbsoluteURL("url", urlStr); verr != nil {
			return nil, verr
		}
		newURL = urlStr
		if newURL != sub.URL() {
			urlChanged = true
		}
	}

	newContent := sub.Content()
	if contentStr, ok, err := patch.optionalString("content"); err != nil {
		return nil, err
	} else if ok {
		if sourceType != subscription.SourceTypeLocal {
			return nil, invalidArg("content: field is not allowed for remote subscription")
		}
		if strings.TrimSpace(contentStr) == "" {
			return nil, invalidArg("content: must be a non-empty string")
		}
		newContent = contentStr
		if newContent != sub.Content() {
			contentChanged = true
		}
	}

	newMode := sub.UpdateMode()
	if modeStr, ok, err := patch.optionalString("update_mode"); err != nil {
		return nil, err
	} else if ok {
		parsed, verr := parseUpdateMode(&modeStr)
		if verr != nil {
			return nil, verr
		}
		newMode = parsed
	}

	newUpdateTime := sub.UpdateTime()
	if t, ok, err := patch.optionalString("update_time"); err != nil {
		return nil, err
	} else if ok {
		// Only validate update_time format when mode is (or will be) daily;
		// interval mode retains the value as-is for seamless mode switching.
		if newMode == subscription.UpdateModeDaily {
			if _, verr := validateUpdateTime(t); verr != nil {
				return nil, verr
			}
		}
		newUpdateTime = t
	}

	newUpdateTimezone := sub.UpdateTimezone()
	if tz, ok, err := patch.optionalString("update_timezone"); err != nil {
		return nil, err
	} else if ok {
		// Only validate update_timezone format when mode is (or will be) daily;
		// interval mode retains the value as-is for seamless mode switching.
		if newMode == subscription.UpdateModeDaily {
			if _, verr := validateUpdateTimezone(tz); verr != nil {
				return nil, verr
			}
		}
		newUpdateTimezone = tz
	}

	// Validate daily fields consistency when mode is daily.
	// Re-validate final retained values (not only patch-present fields) so switching
	// to daily with previously-ignored invalid time/tz fails cleanly.
	if newMode == subscription.UpdateModeDaily {
		if sourceType == subscription.SourceTypeLocal {
			return nil, invalidArg("update_mode: local subscriptions cannot use \"daily\" mode; use \"interval\" instead")
		}
		if newUpdateTime == "" {
			return nil, invalidArg("update_time is required when update_mode is \"daily\"")
		}
		if _, verr := validateUpdateTime(newUpdateTime); verr != nil {
			return nil, verr
		}
		if newUpdateTimezone == "" {
			return nil, invalidArg("update_timezone is required when update_mode is \"daily\"")
		}
		if _, verr := validateUpdateTimezone(newUpdateTimezone); verr != nil {
			return nil, verr
		}
	}

	newInterval := sub.UpdateIntervalNs()
	// update_interval is hard-validated only in interval mode. In daily mode a
	// retained value may still be stored for seamless mode switching (must still
	// satisfy the persistence floor of >=30s); unparseable or below-min values
	// keep the previous interval and are ignored by the scheduler.
	if d, ok, err := patch.optionalDurationString("update_interval"); err != nil {
		if newMode == subscription.UpdateModeInterval {
			return nil, err
		}
		// daily mode: keep previous interval when retained value is unparseable
	} else if ok {
		if d < minSubscriptionUpdateInterval {
			if newMode == subscription.UpdateModeInterval {
				return nil, invalidArg("update_interval: must be >= 30s")
			}
			// daily mode: keep previous interval when retained value is below floor
		} else {
			newInterval = int64(d)
		}
	}

	newEnabled := sub.Enabled()
	if b, ok, err := patch.optionalBool("enabled"); err != nil {
		return nil, err
	} else if ok {
		if b != newEnabled {
			enabledChanged = true
		}
		newEnabled = b
	}

	newEphemeral := sub.Ephemeral()
	if b, ok, err := patch.optionalBool("ephemeral"); err != nil {
		return nil, err
	} else if ok {
		newEphemeral = b
	}

	newIncrementalAliveNodes := sub.IncrementalAliveNodes()
	if b, ok, err := patch.optionalBool("incremental_alive_nodes"); err != nil {
		return nil, err
	} else if ok {
		newIncrementalAliveNodes = b
	}

	newEphemeralNodeEvictDelay := sub.EphemeralNodeEvictDelayNs()
	if d, ok, err := patch.optionalDurationString("ephemeral_node_evict_delay"); err != nil {
		return nil, err
	} else if ok {
		if d < 0 {
			return nil, invalidArg("ephemeral_node_evict_delay: must be non-negative")
		}
		newEphemeralNodeEvictDelay = int64(d)
	}

	newClashFingerprintPolicy := sub.ClashFingerprintPolicy()
	if v, ok, err := patch.optionalString("clash_fingerprint_policy"); err != nil {
		return nil, err
	} else if ok {
		validated, verr := validateClashFingerprintPolicy(v)
		if verr != nil {
			return nil, verr
		}
		newClashFingerprintPolicy = validated
		if newClashFingerprintPolicy != sub.ClashFingerprintPolicy() {
			clashFingerprintPolicyChanged = true
		}
	}

	now := time.Now().UnixNano()
	ms := model.Subscription{
		ID:                        id,
		Name:                      newName,
		SourceType:                sourceType,
		URL:                       newURL,
		Content:                   newContent,
		UpdateIntervalNs:          newInterval,
		UpdateMode:                newMode,
		UpdateTime:                newUpdateTime,
		UpdateTimezone:            newUpdateTimezone,
		Enabled:                   newEnabled,
		Ephemeral:                 newEphemeral,
		IncrementalAliveNodes:     newIncrementalAliveNodes,
		EphemeralNodeEvictDelayNs: newEphemeralNodeEvictDelay,
		ClashFingerprintPolicy:    newClashFingerprintPolicy.String(),
		LastCheckedNs:             sub.LastCheckedNs.Load(),
		CreatedAtNs:               sub.CreatedAtNs,
		UpdatedAtNs:               now,
	}
	if err := s.Engine.UpsertSubscription(ms); err != nil {
		return nil, internal("persist subscription", err)
	}

	// Apply side-effects via scheduler.
	sub.SetFetchConfig(newURL, newInterval)
	sub.SetContent(newContent)
	sub.SetEphemeral(newEphemeral)
	sub.SetIncrementalAliveNodes(newIncrementalAliveNodes)
	sub.SetEphemeralNodeEvictDelayNs(newEphemeralNodeEvictDelay)
	sub.SetClashFingerprintPolicy(newClashFingerprintPolicy)
	sub.SetUpdateMode(newMode)
	sub.SetUpdateTime(newUpdateTime)
	sub.SetUpdateTimezone(newUpdateTimezone)
	sub.UpdatedAtNs = now

	if nameChanged {
		s.Scheduler.RenameSubscription(sub, newName)
	}
	if enabledChanged {
		s.Scheduler.SetSubscriptionEnabled(sub, newEnabled)
	}
	if urlChanged || contentChanged || clashFingerprintPolicyChanged {
		go s.Scheduler.UpdateSubscription(sub)
	}

	r := s.subToResponse(sub)
	return &r, nil
}

// DeleteSubscription deletes a subscription and evicts its nodes.
func (s *ControlPlaneService) DeleteSubscription(id string) error {
	sub := s.SubMgr.Lookup(id)
	if sub == nil {
		return notFound("subscription not found")
	}

	var (
		managedHashes []node.Hash
		deleteErr     error
	)

	// Keep delete atomic across persistence + in-memory runtime state:
	// if DB delete fails, do not mutate runtime subscription/node state.
	sub.WithOpLock(func() {
		// Re-check under lock in case another goroutine removed it between
		// the initial Lookup and lock acquisition.
		lockedSub := s.SubMgr.Lookup(id)
		if lockedSub == nil {
			deleteErr = notFound("subscription not found")
			return
		}

		lockedSub.ManagedNodes().RangeNodes(func(h node.Hash, _ subscription.ManagedNode) bool {
			managedHashes = append(managedHashes, h)
			return true
		})

		if err := s.Engine.DeleteSubscription(id); err != nil {
			if errors.Is(err, state.ErrNotFound) {
				deleteErr = notFound("subscription not found")
			} else {
				deleteErr = internal("delete subscription", err)
			}
			return
		}

		// Persist succeeded; now apply in-memory cleanup.
		for _, h := range managedHashes {
			s.Pool.RemoveNodeFromSub(h, id)
		}
		s.SubMgr.Unregister(id)
	})

	return deleteErr
}

// RefreshSubscription triggers an immediate subscription refresh (blocks).
func (s *ControlPlaneService) RefreshSubscription(id string) error {
	sub := s.SubMgr.Lookup(id)
	if sub == nil {
		return notFound("subscription not found")
	}
	s.Scheduler.UpdateSubscription(sub)
	return nil
}

// CleanupSubscriptionCircuitOpenNodes removes problematic nodes from a subscription.
// It marks nodes as evicted (while keeping managed hashes) for nodes currently
// circuit-open, and nodes with no outbound while carrying a non-empty last error.
func (s *ControlPlaneService) CleanupSubscriptionCircuitOpenNodes(id string) (int, error) {
	return s.cleanupSubscriptionCircuitOpenNodesWithHook(id, nil)
}

// cleanupSubscriptionCircuitOpenNodesWithHook performs cleanup with an optional
// hook between first scan and second confirmation scan. The hook is only used
// by tests to simulate TOCTOU recovery.
func (s *ControlPlaneService) cleanupSubscriptionCircuitOpenNodesWithHook(
	id string,
	betweenScans func(),
) (int, error) {
	sub := s.SubMgr.Lookup(id)
	if sub == nil {
		return 0, notFound("subscription not found")
	}

	var (
		cleanedCount int
		evicted      []node.Hash
		cleanupErr   error
	)

	sub.WithOpLock(func() {
		// Re-check under lock in case another goroutine deleted the subscription
		// between lookup and lock acquisition.
		lockedSub := s.SubMgr.Lookup(id)
		if lockedSub == nil {
			cleanupErr = notFound("subscription not found")
			return
		}

		cleanedCount, evicted = topology.CleanupSubscriptionNodesWithConfirmNoLock(
			lockedSub,
			s.Pool,
			shouldCleanupSubscriptionNode,
			betweenScans,
		)
	})
	if cleanupErr != nil {
		return 0, cleanupErr
	}

	if s.Engine != nil {
		for _, h := range evicted {
			s.Engine.MarkSubscriptionNode(id, h.Hex())
		}
	}

	return cleanedCount, nil
}

func shouldCleanupSubscriptionNode(entry *node.NodeEntry) bool {
	if entry == nil {
		return false
	}
	return entry.IsCircuitOpen() || (!entry.HasOutbound() && entry.GetLastError() != "")
}
