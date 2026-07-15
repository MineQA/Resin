package probe

import (
	"encoding/json"
	"time"

	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/node"
)

// ---------------------------------------------------------------------------
// QualityProbeConfig groups the hot-reloadable quality probe settings.
// Using one grouped config avoids adding many individual closures to ProbeConfig.
// ---------------------------------------------------------------------------

// QualityProbeConfig configures the active quality (proxy-check) probe loop.
// All fields are closures for hot-reload from RuntimeConfig.
type QualityProbeConfig struct {
	// Enabled controls whether the periodic quality scan loop runs.
	// New/re-enabled node event checks are controlled independently by
	// TriggerOnNewNode.
	Enabled func() bool

	// Interval is the minimum time between quality checks for a given node.
	Interval func() time.Duration

	// Profile returns the target profile name (e.g. "generic", "openai").
	Profile func() string

	// Opts returns the ProxyCheckOptions to use for quality checks.
	Opts func() ProxyCheckOptions

	// ScoringPolicy returns the canonical nested scoring policy (Phase 3B1).
	// When nil, legacy flat-option scoring behavior is used. When non-nil,
	// the new weighted scoring engine is used with custom CF target_url support.
	ScoringPolicy func() *ScoringPolicy

	// TriggerOnNewNode controls whether newly added or re-enabled nodes
	// automatically trigger an immediate quality check.
	TriggerOnNewNode func() bool
}

// ---------------------------------------------------------------------------
// TriggerImmediateQualityProbe
// ---------------------------------------------------------------------------

// TriggerImmediateQualityProbe enqueues an async quality probe for a node.
// Caller returns immediately. The task competes for queue capacity with
// egress and latency probes — this coupling is intentional because all
// probe types share the same worker pool.
func (m *ProbeManager) TriggerImmediateQualityProbe(hash node.Hash) {
	m.enqueueProbe(hash, probeTaskKindQuality, probePriorityNormal)
}

// TriggerImmediateQualityProbeForce enqueues a forced async quality probe
// that executes independently of ProxyCheckEnabled. The force bit allows
// the task to run even when periodic quality scanning is disabled, gated
// only by TriggerOnNewNode at execution time.
func (m *ProbeManager) TriggerImmediateQualityProbeForce(hash node.Hash) {
	m.enqueueProbe(hash, probeTaskKindQualityForce, probePriorityNormal)
}

func (m *ProbeManager) qualityEnabled() bool {
	return m.qualityCfg != nil && m.qualityCfg.Enabled != nil && m.qualityCfg.Enabled()
}

// triggerOnNewNodeEnabled mirrors qualityEnabled() for the TriggerOnNewNode
// gate. Returns true only when QualityCfg is non-nil, the TriggerOnNewNode
// closure is non-nil, and the closure returns true.
func (m *ProbeManager) triggerOnNewNodeEnabled() bool {
	return m.qualityCfg != nil && m.qualityCfg.TriggerOnNewNode != nil && m.qualityCfg.TriggerOnNewNode()
}

func (m *ProbeManager) currentQualityInterval() time.Duration {
	if m.qualityCfg == nil || m.qualityCfg.Interval == nil {
		return 30 * time.Minute
	}
	interval := m.qualityCfg.Interval()
	if interval <= 0 {
		return 30 * time.Minute
	}
	return interval
}

func (m *ProbeManager) currentQualityProfile() string {
	if m.qualityCfg == nil || m.qualityCfg.Profile == nil {
		return "generic"
	}
	return m.qualityCfg.Profile()
}

func (m *ProbeManager) currentQualityOptions() ProxyCheckOptions {
	if m.qualityCfg == nil || m.qualityCfg.Opts == nil {
		return DefaultOptions()
	}
	return m.qualityCfg.Opts()
}

// ---------------------------------------------------------------------------
// scanQuality
// ---------------------------------------------------------------------------

// scanQuality iterates all pool nodes and enqueues quality probes for
// those that are due for re-check.
//
// scanQuality is started as a goroutine from ProbeManager.Start() and
// follows the same scanloop cadence as scanEgress/scanLatency.
func (m *ProbeManager) scanQuality() {
	if !m.qualityEnabled() {
		return
	}

	now := time.Now()
	interval := m.currentQualityInterval()
	lookahead := 15 * time.Second
	profile := m.currentQualityProfile()
	subLookup := m.pool.MakeSubLookup()

	m.pool.Range(func(h node.Hash, entry *node.NodeEntry) bool {
		select {
		case <-m.stopCh:
			return false
		default:
		}

		if entry.IsDisabledBySubscriptions(subLookup) {
			return true // disabled node -> skip periodic probe
		}

		if entry.Outbound.Load() == nil {
			return true // skip nil outbound
		}

		// Determine if the node is due for a quality check.
		q := entry.GetQuality()
		due := false

		if q == nil {
			// Never checked -> due immediately.
			due = true
		} else {
			// Profile changed -> due immediately.
			if q.Profile != profile {
				due = true
			} else {
				// Check LastCheckedNs against configured interval.
				lastChecked := q.LastCheckedNs
				if lastChecked > 0 {
					nextDue := time.Unix(0, lastChecked).Add(interval).Add(-lookahead)
					if now.Before(nextDue) {
						due = false
					} else {
						due = true
					}
				} else {
					due = true
				}
			}
		}

		if due {
			m.enqueueProbe(h, probeTaskKindQuality, probePriorityNormal)
		}

		return true
	})
}

// ---------------------------------------------------------------------------
// performQualityCheck
// ---------------------------------------------------------------------------

// performQualityCheck executes a quality (proxy-check) probe against a node.
//
// When force is true, the method gates on TriggerOnNewNode instead of
// qualityEnabled(), allowing event-triggered checks to run even when the
// periodic quality scan is disabled. The force flag is set by
// probeTaskKindQualityForce tasks.
//
// IMPORTANT: This method must NOT call pool.RecordResult and must NOT affect
// failure count, circuit breaker, or routing/routability health. Quality
// probes check target-service reachability (e.g. OpenAI API) and are
// independent from node health tracking.
//
// This shares the queue/worker pool with egress and latency probes.
// When enabled aggressively (low interval, many nodes), quality checks
// can compete for worker capacity with health probes.
func (m *ProbeManager) performQualityCheck(hash node.Hash, entry *node.NodeEntry, force bool) {
	if m.fetcher == nil {
		return
	}
	if force {
		if !m.triggerOnNewNodeEnabled() {
			return
		}
	} else {
		if !m.qualityEnabled() {
			return
		}
	}
	if entry.Outbound.Load() == nil {
		return
	}

	// Fire the shared probe event callback for metrics parity.
	if m.onProbeEvent != nil {
		m.onProbeEvent("quality")
	}

	profile := LookupProfile(m.currentQualityProfile())
	opts := m.currentQualityOptions()

	// Inject scoring policy from runtime config (Phase 3B1).
	if m.qualityCfg != nil && m.qualityCfg.ScoringPolicy != nil {
		policy := m.qualityCfg.ScoringPolicy()
		opts.ScoringPolicy = policy
	}

	m.performNodeQualityCheck(hash, profile, opts)
}

// performNodeQualityCheck executes CheckProxySync, maps to NodeQuality,
// and records it. Shared by single-node checks and the sweep. Callers
// handle config resolution, gating, events, and eligibility. Errors
// still write back quality via ProxyScoreToNodeQuality.
func (m *ProbeManager) performNodeQualityCheck(hash node.Hash, profile TargetProfile, opts ProxyCheckOptions) (*ProxyScore, error) {
	score, err := m.CheckProxySync(hash, profile, opts)
	nq := ProxyScoreToNodeQuality(profile.Name, score, err)
	m.pool.RecordNodeQuality(hash, nq)
	return score, err
}

// ---------------------------------------------------------------------------
// performQualitySweep
// ---------------------------------------------------------------------------

// performQualitySweep iterates all pool nodes and performs a synchronous
// quality probe on each eligible (non-disabled, outbound-ready) node using
// the current runtime profile, options, and scoring policy fixed at sweep
// start. It does NOT call RecordResult and does NOT affect circuit breaker
// or routing health.
//
// The sweep is executed as a single task (probeTaskKindQualitySweep with
// node.Zero key). Each node is checked independently and stopCh is checked
// between nodes to allow graceful shutdown mid-sweep.
//
// Design decisions:
//   - Profile/options are captured once at sweep start so a mid-sweep config
//     change does not produce inconsistent results across nodes.
//   - Each node fires onProbeEvent("quality") for metrics parity.
//   - The sweep is NOT gated by qualityEnabled() or triggerOnNewNodeEnabled().
//   - Disabled subscription nodes and nil outbound are skipped silently.
//   - No RecordResult is called (does not affect health/failure tracking).
func (m *ProbeManager) performQualitySweep() {
	if m.fetcher == nil {
		return
	}

	// Fix runtime config at sweep start.
	profile := LookupProfile(m.currentQualityProfile())
	opts := m.currentQualityOptions()

	// Inject scoring policy.
	if m.qualityCfg != nil && m.qualityCfg.ScoringPolicy != nil {
		policy := m.qualityCfg.ScoringPolicy()
		opts.ScoringPolicy = policy
	}

	subLookup := m.pool.MakeSubLookup()

	m.pool.Range(func(h node.Hash, entry *node.NodeEntry) bool {
		// Check stop signal before each node.
		select {
		case <-m.stopCh:
			return false
		default:
		}

		if entry.IsDisabledBySubscriptions(subLookup) {
			return true
		}
		if entry.Outbound.Load() == nil {
			return true
		}

		// Fire probe event for metrics parity.
		if m.onProbeEvent != nil {
			m.onProbeEvent("quality")
		}

		m.performNodeQualityCheck(h, profile, opts)
		return true
	})
}

// ---------------------------------------------------------------------------
// ProxyScoreToNodeQuality
// ---------------------------------------------------------------------------

// ProxyScoreToNodeQuality converts a ProxyScore (from CheckProxy) into a
// model.NodeQuality suitable for storage via RecordNodeQuality.
//
// When score is nil (check could not be performed), a quality entry with
// the profile, last error, and default aggregate values is returned so the
// node's quality state reflects the attempt even on total failure.
// RecordNodeQuality sets NodeHash and LastCheckedNs internally.
//
// Phase 3B2:
//   - Stores aggregate canonical CloudflareStatus as string.
//   - Derives compatibility CloudflareChallenged and CloudflareChallengeType
//     from challenge statuses only (js_challenge, captcha_challenge, block,
//     challenge); false/empty type otherwise.
//   - When ScoringBreakdown != nil, serializes a COMPACT breakdown JSON
//     (version, effective weights, sub-scores, unavailable dims, applied caps,
//     grade_from_score, final_grade, terminal_reason) and sets
//     ScoringPolicyVersion. Legacy/nil breakdown produces version 0 and empty
//     breakdown string.
func ProxyScoreToNodeQuality(profile string, score *ProxyScore, err error) *model.NodeQuality {
	nq := &model.NodeQuality{
		Profile: profile,
	}
	if err != nil {
		nq.LastError = err.Error()
	}
	if score != nil {
		nq.Grade = score.Grade
		nq.Score = score.Score
		nq.Unstable = score.Unstable
		nq.ServiceReachable = score.ServiceReachable
		nq.APIReachable = score.APIReachable
		nq.AvgLatencyMs = score.AvgLatencyMs

		// Store aggregate canonical CloudflareStatus.
		nq.CloudflareStatus = string(score.CloudflareStatus)

		// Derive compatibility fields from challenge statuses only.
		if isChallengeStatus(score.CloudflareStatus) {
			nq.CloudflareChallenged = true
			nq.CloudflareChallengeType = string(score.CloudflareStatus)
		} else {
			nq.CloudflareChallenged = false
			nq.CloudflareChallengeType = ""
		}

		// Serialize compact breakdown when available (Phase 3B1 scoring path).
		if score.ScoringBreakdown != nil {
			nq.ScoringPolicyVersion = score.ScoringBreakdown.Version
			nq.ScoreBreakdown = serializeCompactBreakdown(score.ScoringBreakdown)
		} else {
			nq.ScoringPolicyVersion = 0
			nq.ScoreBreakdown = ""
		}

		// If no top-level error but round results have transport errors,
		// propagate the first as LastError (Phase 3A: LastError on failure).
		if nq.LastError == "" {
			for _, r := range score.RoundResults {
				if r.Error != "" {
					nq.LastError = r.Error
					break
				}
			}
		}
	} else {
		// No score means total failure — record grade F / score 0.
		nq.Grade = "F"
		nq.Score = 0
		if nq.LastError == "" {
			nq.LastError = "proxy check failed"
		}
	}
	return nq
}

// serializeCompactBreakdown produces a compact JSON representation of the
// scoring breakdown suitable for persistence. It includes only:
// version, effective_weights, sub_scores, unavailable_dimensions, applied_caps,
// grade_from_score, final_grade, terminal_reason.
// It does NOT include response bodies, headers, round results, raw policy, or
// redundant Grade field.
// Marshal errors are silently handled — an empty string is returned so the
// quality recording is not blocked.
func serializeCompactBreakdown(sr *ScoringResult) string {
	if sr == nil {
		return ""
	}
	type compactBreakdown struct {
		Version          int                       `json:"version"`
		EffectiveWeights map[string]int            `json:"effective_weights"`
		SubScores        map[string]*SubScoreEntry `json:"sub_scores"`
		UnavailableDims  []string                  `json:"unavailable_dimensions"`
		AppliedCaps      []CapApplication          `json:"applied_caps"`
		GradeFromScore   string                    `json:"grade_from_score"`
		FinalGrade       string                    `json:"final_grade"`
		TerminalReason   string                    `json:"terminal_reason,omitempty"`
	}
	cb := compactBreakdown{
		Version:          sr.Version,
		EffectiveWeights: sr.EffectiveWeights,
		SubScores:        sr.SubScores,
		UnavailableDims:  sr.UnavailableDims,
		AppliedCaps:      sr.AppliedCaps,
		GradeFromScore:   sr.GradeFromScore,
		FinalGrade:       sr.FinalGrade,
		TerminalReason:   sr.TerminalReason,
	}
	b, err := json.Marshal(cb)
	if err != nil {
		return ""
	}
	return string(b)
}
