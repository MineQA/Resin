package probe

import (
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
	// Enabled controls whether the quality scan loop runs and whether
	// new/re-enabled nodes trigger a quality check.
	Enabled func() bool

	// Interval is the minimum time between quality checks for a given node.
	Interval func() time.Duration

	// Profile returns the target profile name (e.g. "generic", "openai").
	Profile func() string

	// Opts returns the ProxyCheckOptions to use for quality checks.
	Opts func() ProxyCheckOptions

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

func (m *ProbeManager) qualityEnabled() bool {
	return m.qualityCfg != nil && m.qualityCfg.Enabled != nil && m.qualityCfg.Enabled()
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
// IMPORTANT: This method must NOT call pool.RecordResult and must NOT affect
// failure count, circuit breaker, or routing/routability health. Quality
// probes check target-service reachability (e.g. OpenAI API) and are
// independent from node health tracking.
//
// This shares the queue/worker pool with egress and latency probes.
// When enabled aggressively (low interval, many nodes), quality checks
// can compete for worker capacity with health probes.
func (m *ProbeManager) performQualityCheck(hash node.Hash, entry *node.NodeEntry) {
	if m.fetcher == nil {
		return
	}
	if !m.qualityEnabled() {
		return
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

	score, err := CheckProxy(m.fetcher, hash, profile, opts)

	// Map ProxyScore to model.NodeQuality and record via pool.
	nq := ProxyScoreToNodeQuality(profile.Name, score, err)
	m.pool.RecordNodeQuality(hash, nq)
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
		nq.CloudflareChallenged = score.CloudflareChallenged
		nq.CloudflareChallengeType = score.CloudflareChallengeType
		nq.AvgLatencyMs = score.AvgLatencyMs
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
