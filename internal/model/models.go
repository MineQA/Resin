// Package model defines domain structs shared across the persistence layer.
package model

import "encoding/json"

// Platform represents a routing platform.
type Platform struct {
	ID                               string `json:"id"`
	Name                             string `json:"name"`
	StickyTTLNs                      int64  `json:"sticky_ttl_ns"`
	RegexFilters                     []string
	RegionFilters                    []string
	ProtocolFilters                  []string
	ExcludeProtocolFilters           []string
	ReverseProxyMissAction           string   `json:"reverse_proxy_miss_action"`
	ReverseProxyEmptyAccountBehavior string   `json:"reverse_proxy_empty_account_behavior"`
	ReverseProxyFixedAccountHeader   string   `json:"reverse_proxy_fixed_account_header"`
	AllocationPolicy                 string   `json:"allocation_policy"`
	PassiveCircuitBreakerDisabled    bool     `json:"passive_circuit_breaker_disabled"`
	QualityGrade                     string   `json:"quality_grade"`
	QualityMinScore                  float64  `json:"quality_min_score"`
	QualityCloudflareChallenged      *bool    `json:"quality_cloudflare_challenged,omitempty"`
	QualityCloudflareStatuses        []string `json:"quality_cloudflare_statuses,omitempty"`
	QualityCheckedSinceNs            int64    `json:"quality_checked_since_ns"`
	QualityProfile                   string   `json:"quality_profile"`
	UpdatedAtNs                      int64    `json:"updated_at_ns"`
}

// Subscription represents a node subscription source.
type Subscription struct {
	ID                        string `json:"id"`
	Name                      string `json:"name"`
	SourceType                string `json:"source_type"`
	URL                       string `json:"url"`
	Content                   string `json:"content"`
	UpdateIntervalNs          int64  `json:"update_interval_ns"`
	Enabled                   bool   `json:"enabled"`
	Ephemeral                 bool   `json:"ephemeral"`
	IncrementalAliveNodes     bool   `json:"incremental_alive_nodes"`
	EphemeralNodeEvictDelayNs int64  `json:"ephemeral_node_evict_delay_ns"`
	ClashFingerprintPolicy    string `json:"clash_fingerprint_policy"`
	CreatedAtNs               int64  `json:"created_at_ns"`
	UpdatedAtNs               int64  `json:"updated_at_ns"`
}

// AccountHeaderRule defines header extraction rules for reverse proxy account matching.
type AccountHeaderRule struct {
	URLPrefix   string `json:"url_prefix"`
	Headers     []string
	UpdatedAtNs int64 `json:"updated_at_ns"`
}

// NodeStatic holds the immutable portion of a node's data.
type NodeStatic struct {
	Hash        string          `json:"hash"`
	RawOptions  json.RawMessage `json:"raw_options_json"`
	CreatedAtNs int64           `json:"created_at_ns"`
}

// NodeDynamic holds the mutable runtime state of a node.
type NodeDynamic struct {
	Hash                               string `json:"hash"`
	FailureCount                       int    `json:"failure_count"`
	CircuitOpenSince                   int64  `json:"circuit_open_since"`
	EgressIP                           string `json:"egress_ip"`
	EgressRegion                       string `json:"egress_region"`
	EgressUpdatedAtNs                  int64  `json:"egress_updated_at_ns"`
	LastLatencyProbeAttemptNs          int64  `json:"last_latency_probe_attempt_ns"`
	LastAuthorityLatencyProbeAttemptNs int64  `json:"last_authority_latency_probe_attempt_ns"`
	LastEgressUpdateAttemptNs          int64  `json:"last_egress_update_attempt_ns"`
}

// NodeLatency holds per-domain latency statistics for a node.
type NodeLatency struct {
	NodeHash      string `json:"node_hash"`
	Domain        string `json:"domain"`
	EwmaNs        int64  `json:"ewma_ns"`
	LastUpdatedNs int64  `json:"last_updated_ns"`
}

// NodeLatencyKey is the composite primary key for node_latency.
type NodeLatencyKey struct {
	NodeHash string
	Domain   string
}

// NodeQualityKey is the composite primary key for node_quality.
type NodeQualityKey struct {
	NodeHash string
	Profile  string
}

// NodeQuality holds quality check aggregate results for a node+profile.
// This stores the aggregate summary only, not individual round results.
type NodeQuality struct {
	NodeHash                string  `json:"node_hash"`
	Profile                 string  `json:"profile"`
	Grade                   string  `json:"grade"`
	Score                   float64 `json:"score"`
	Unstable                bool    `json:"unstable"`
	ServiceReachable        bool    `json:"service_reachable"`
	APIReachable            bool    `json:"api_reachable"`
	CloudflareChallenged    bool    `json:"cloudflare_challenged"`
	CloudflareChallengeType string  `json:"cloudflare_challenge_type"`
	AvgLatencyMs            float64 `json:"avg_latency_ms"`
	LastCheckedNs           int64   `json:"last_checked_ns"`
	LastError               string  `json:"last_error"`
	// CloudflareStatus is the canonical aggregate CF observation outcome.
	// Empty string means legacy/unchecked (no breakdown available).
	CloudflareStatus string `json:"cloudflare_status"`
	// ScoringPolicyVersion records the version of the scoring policy that
	// produced this result. 0 means legacy (before Phase 3B scoring).
	ScoringPolicyVersion int `json:"scoring_policy_version"`
	// ScoreBreakdown is compact JSON of the scoring breakdown (sub-scores,
	// effective weights, caps, etc.). Empty string means no breakdown
	// (legacy/nil ScoringBreakdown).
	ScoreBreakdown string `json:"score_breakdown"`
}

// Lease represents a sticky routing lease.
type Lease struct {
	PlatformID     string `json:"platform_id"`
	Account        string `json:"account"`
	NodeHash       string `json:"node_hash"`
	EgressIP       string `json:"egress_ip"`
	CreatedAtNs    int64  `json:"created_at_ns"`
	ExpiryNs       int64  `json:"expiry_ns"`
	LastAccessedNs int64  `json:"last_accessed_ns"`
}

// LeaseKey is the composite primary key for leases.
type LeaseKey struct {
	PlatformID string
	Account    string
}

// SubscriptionNode links a subscription to a node with tags.
type SubscriptionNode struct {
	SubscriptionID string `json:"subscription_id"`
	NodeHash       string `json:"node_hash"`
	Tags           []string
	Evicted        bool `json:"evicted"`
}

// SubscriptionNodeKey is the composite primary key for subscription_nodes.
type SubscriptionNodeKey struct {
	SubscriptionID string
	NodeHash       string
}

// ExportToken represents an API token used for exporting node-pool data.
type ExportToken struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	TokenHash    string `json:"-"`
	TokenPrefix  string `json:"token_prefix"`
	Enabled      bool   `json:"enabled"`
	CreatedAtNs  int64  `json:"created_at_ns"`
	LastUsedAtNs int64  `json:"last_used_at_ns"`
}
