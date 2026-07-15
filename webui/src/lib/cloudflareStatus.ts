// Shared constants and helpers for the 8 canonical Cloudflare observation
// statuses. These mirror the backend `internal/cloudflare` package tokens and
// the persisted `quality_cloudflare_status` field. Keeping them in one leaf
// module avoids cross-feature coupling while preventing token drift.

export const CF_STATUS_JS_CHALLENGE = "js_challenge";
export const CF_STATUS_CAPTCHA_CHALLENGE = "captcha_challenge";
export const CF_STATUS_BLOCK = "block";
export const CF_STATUS_CHALLENGE = "challenge";
export const CF_STATUS_NG = "ng";
export const CF_STATUS_CLEAN = "clean";
export const CF_STATUS_NOT_DETECTED = "not_detected";
export const CF_STATUS_UNCHECKED = "unchecked";

/** All eight canonical status tokens in display order (not severity order). */
export const CF_STATUS_TOKENS = [
  CF_STATUS_CLEAN,
  CF_STATUS_NOT_DETECTED,
  CF_STATUS_JS_CHALLENGE,
  CF_STATUS_CAPTCHA_CHALLENGE,
  CF_STATUS_CHALLENGE,
  CF_STATUS_BLOCK,
  CF_STATUS_NG,
  CF_STATUS_UNCHECKED,
] as const;

export type CloudflareStatusToken = (typeof CF_STATUS_TOKENS)[number];

/** Challenge statuses — these are the only statuses that affect score/grade. */
export const CF_CHALLENGE_STATUSES: ReadonlySet<CloudflareStatusToken> = new Set([
  CF_STATUS_JS_CHALLENGE,
  CF_STATUS_CAPTCHA_CHALLENGE,
  CF_STATUS_BLOCK,
  CF_STATUS_CHALLENGE,
]);

/** Statuses permitted to have a null (unavailable) score. */
export const CF_NULLABLE_SCORE_STATUSES: ReadonlySet<CloudflareStatusToken> = new Set([
  CF_STATUS_NG,
  CF_STATUS_UNCHECKED,
]);

const VALID_TOKENS: ReadonlySet<string> = new Set(CF_STATUS_TOKENS);

/** Normalize an arbitrary backend value to a canonical token, defaulting to unchecked. */
export function normalizeCFStatus(raw: string | null | undefined): CloudflareStatusToken {
  if (typeof raw === "string" && VALID_TOKENS.has(raw)) {
    return raw as CloudflareStatusToken;
  }
  return CF_STATUS_UNCHECKED;
}

/** True for the four challenge statuses (js/captcha/block/generic challenge). */
export function isChallengeStatus(status: string | null | undefined): boolean {
  return typeof status === "string" && CF_CHALLENGE_STATUSES.has(status as CloudflareStatusToken);
}

/** Deduplicate and normalize a list of status strings to canonical tokens. */
export function normalizeCFStatusSet(raw: string[] | null | undefined): CloudflareStatusToken[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  const seen = new Set<CloudflareStatusToken>();
  const result: CloudflareStatusToken[] = [];
  for (const item of raw) {
    if (typeof item !== "string") {
      continue;
    }
    const token = normalizeCFStatus(item);
    if (token === CF_STATUS_UNCHECKED && item !== CF_STATUS_UNCHECKED) {
      // Unknown tokens are dropped; only an explicit "unchecked" survives.
      continue;
    }
    if (!seen.has(token)) {
      seen.add(token);
      result.push(token);
    }
  }
  return result;
}

// ---------------------------------------------------------------------------
// Scoring policy types (mirror internal/config/scoring_policy.go canonical v1)
// ---------------------------------------------------------------------------

export type ScoringPolicyMode =
  | "observe_only"
  | "score"
  | "grade"
  | "score_and_grade";

export const CF_POLICY_MODES: ScoringPolicyMode[] = [
  "observe_only",
  "score",
  "grade",
  "score_and_grade",
];

export type Grade = "A" | "B" | "C" | "D" | "F";

export type DimensionKey = "service" | "api" | "cloudflare" | "stability" | "latency";

export const DIMENSION_KEYS: DimensionKey[] = [
  "service",
  "api",
  "cloudflare",
  "stability",
  "latency",
];

export type DimensionCap = {
  below_score: number;
  grade_cap: Grade;
};

export type LatencyBand = {
  max_ms: number | null;
  score: number;
};

export type CVBand = {
  max_percent: number | null;
  score: number;
};

export type ScoringPolicy = {
  version: number;
  weights: Record<DimensionKey, number>;
  grade_thresholds: Record<"A" | "B" | "C" | "D", number>;
  cloudflare: {
    policy: ScoringPolicyMode;
    target_url: string;
    status_scores: Record<CloudflareStatusToken, number | null>;
    grade_caps: Partial<Record<CloudflareStatusToken, Grade>>;
  };
  latency: {
    bands: LatencyBand[];
  };
  stability: {
    cv_bands: CVBand[];
  };
  dimension_caps: Record<DimensionKey, DimensionCap | null>;
};

/**
 * SubScoreEntry mirrors the backend `probe.SubScoreEntry` JSON shape.
 * `value` is omitted/null when unavailable; `unavailable` is true when the
 * dimension was excluded from the weighted calculation.
 */
export type SubScoreEntry = {
  value?: number | null;
  unavailable?: boolean;
};

/**
 * CapApplication mirrors the backend `probe.CapApplication` JSON shape.
 * Recorded when a grade cap was applied (dimension or CF status reason).
 */
export type CapApplication = {
  dimension: string;
  reason: string;
  cap: string;
};

export type ScoreBreakdown = {
  version: number;
  effective_weights?: Record<string, number> | null;
  sub_scores?: Record<string, SubScoreEntry | null> | null;
  unavailable_dimensions?: string[] | null;
  applied_caps?: CapApplication[] | null;
  grade_from_score?: Grade | null;
  final_grade?: Grade | null;
  terminal_reason?: string | null;
};

// ---------------------------------------------------------------------------
// Balanced preset (exact values from the locked contract)
// ---------------------------------------------------------------------------

export const BALANCED_SCORING_POLICY: ScoringPolicy = {
  version: 1,
  weights: {
    service: 40,
    api: 15,
    cloudflare: 20,
    stability: 10,
    latency: 15,
  },
  grade_thresholds: {
    A: 90,
    B: 75,
    C: 60,
    D: 40,
  },
  cloudflare: {
    policy: "score_and_grade",
    target_url: "",
    status_scores: {
      clean: 100,
      not_detected: 100,
      js_challenge: 40,
      captcha_challenge: 20,
      challenge: 10,
      block: 0,
      ng: null,
      unchecked: null,
    },
    grade_caps: {
      js_challenge: "D",
      captcha_challenge: "D",
      challenge: "D",
      block: "F",
    },
  },
  latency: {
    bands: [
      { max_ms: 300, score: 100 },
      { max_ms: 800, score: 80 },
      { max_ms: 1500, score: 60 },
      { max_ms: 3000, score: 30 },
      { max_ms: null, score: 0 },
    ],
  },
  stability: {
    cv_bands: [
      { max_percent: 5, score: 100 },
      { max_percent: 15, score: 80 },
      { max_percent: 30, score: 60 },
      { max_percent: 50, score: 30 },
      { max_percent: null, score: 0 },
    ],
  },
  dimension_caps: {
    service: null,
    api: null,
    cloudflare: null,
    stability: null,
    latency: null,
  },
};

// ---------------------------------------------------------------------------
// Deep clone helper (avoids shared references when editing a draft)
// ---------------------------------------------------------------------------

export function cloneScoringPolicy(policy: ScoringPolicy): ScoringPolicy {
  return {
    version: policy.version,
    weights: { ...policy.weights },
    grade_thresholds: { ...policy.grade_thresholds },
    cloudflare: {
      policy: policy.cloudflare.policy,
      target_url: policy.cloudflare.target_url,
      status_scores: { ...policy.cloudflare.status_scores },
      grade_caps: { ...policy.cloudflare.grade_caps },
    },
    latency: {
      bands: policy.latency.bands.map((band) => ({ ...band })),
    },
    stability: {
      cv_bands: policy.stability.cv_bands.map((band) => ({ ...band })),
    },
    dimension_caps: {
      service: policy.dimension_caps.service ? { ...policy.dimension_caps.service } : null,
      api: policy.dimension_caps.api ? { ...policy.dimension_caps.api } : null,
      cloudflare: policy.dimension_caps.cloudflare ? { ...policy.dimension_caps.cloudflare } : null,
      stability: policy.dimension_caps.stability ? { ...policy.dimension_caps.stability } : null,
      latency: policy.dimension_caps.latency ? { ...policy.dimension_caps.latency } : null,
    },
  };
}

// ---------------------------------------------------------------------------
// Legacy compatibility: derive an effective policy from legacy boolean/flags
// ---------------------------------------------------------------------------

export type LegacyNormalizationInput = {
  proxy_check_cloudflare_detection?: boolean | null;
  proxy_check_service_reachability?: boolean | null;
  proxy_check_api_reachability?: boolean | null;
  proxy_check_multi_round?: boolean | null;
};

/**
 * Produce a canonical v1 policy from legacy flat flags when the backend returns
 * null/missing `proxy_check_scoring`. The derived policy matches current
 * behavior without lying: CF observation always runs; the policy only controls
 * score/grade impact. Disabled dimensions get zero weight.
 */
export function deriveEffectivePolicy(legacy: LegacyNormalizationInput): ScoringPolicy {
  const policy = cloneScoringPolicy(BALANCED_SCORING_POLICY);

  // CF policy mode from legacy boolean.
  const cfDetection =
    typeof legacy.proxy_check_cloudflare_detection === "boolean"
      ? legacy.proxy_check_cloudflare_detection
      : true;
  policy.cloudflare.policy = cfDetection ? "score_and_grade" : "observe_only";

  // Zero out disabled dimensions.
  if (legacy.proxy_check_service_reachability === false) {
    policy.weights.service = 0;
  }
  if (legacy.proxy_check_api_reachability === false) {
    policy.weights.api = 0;
  }
  // Stability requires multi-round.
  if (legacy.proxy_check_multi_round === false) {
    policy.weights.stability = 0;
  }

  return policy;
}

/**
 * Compare a policy to the balanced preset by JSON value. Used to decide whether
 * the UI shows "平衡（推荐）" or "自定义".
 */
export function isBalancedPreset(policy: ScoringPolicy): boolean {
  return JSON.stringify(policy) === JSON.stringify(BALANCED_SCORING_POLICY);
}

// ---------------------------------------------------------------------------
// Client-side validation (server remains authoritative)
// ---------------------------------------------------------------------------

export type ScoringPolicyValidation = {
  ok: boolean;
  errors: string[];
};

export function validateScoringPolicy(policy: ScoringPolicy): ScoringPolicyValidation {
  const errors: string[] = [];

  if (policy.version !== 1) {
    errors.push("version");
  }

  // Weights
  let positiveWeights = 0;
  for (const key of DIMENSION_KEYS) {
    const w = policy.weights[key];
    if (typeof w !== "number" || !Number.isInteger(w) || w < 0 || w > 100) {
      errors.push(`weights.${key}`);
    } else if (w > 0) {
      positiveWeights += 1;
    }
  }
  if (positiveWeights === 0) {
    errors.push("weights.none_positive");
  }

  // Grade thresholds
  const { A, B, C, D } = policy.grade_thresholds;
  const thresholds = [A, B, C, D];
  for (const t of thresholds) {
    if (typeof t !== "number" || !Number.isInteger(t) || t < 0 || t > 100) {
      errors.push("grade_thresholds");
      break;
    }
  }
  if (!(A > B && B > C && C > D)) {
    errors.push("grade_thresholds.order");
  }

  // CF status scores
  const statusScores = policy.cloudflare.status_scores;
  for (const token of CF_STATUS_TOKENS) {
    const val = statusScores[token];
    if (val === null) {
      if (!CF_NULLABLE_SCORE_STATUSES.has(token)) {
        errors.push(`cloudflare.status_scores.${token}.null`);
      }
    } else if (typeof val !== "number" || !Number.isInteger(val) || val < 0 || val > 100) {
      errors.push(`cloudflare.status_scores.${token}`);
    }
  }

  // CF policy mode
  if (!CF_POLICY_MODES.includes(policy.cloudflare.policy)) {
    errors.push("cloudflare.policy");
  }

  // Latency bands
  const bands = policy.latency.bands;
  if (!Array.isArray(bands) || bands.length === 0) {
    errors.push("latency.bands.empty");
  } else {
    let prevMax = -Infinity;
    let openFound = false;
    for (let i = 0; i < bands.length; i += 1) {
      const band = bands[i];
      if (typeof band.score !== "number" || !Number.isInteger(band.score) || band.score < 0 || band.score > 100) {
        errors.push(`latency.bands[${i}].score`);
      }
      if (band.max_ms === null) {
        if (i !== bands.length - 1) {
          errors.push(`latency.bands[${i}].open_not_last`);
        }
        openFound = true;
      } else {
        if (typeof band.max_ms !== "number" || !Number.isFinite(band.max_ms) || band.max_ms < 0) {
          errors.push(`latency.bands[${i}].max_ms`);
        } else if (band.max_ms <= prevMax) {
          errors.push(`latency.bands[${i}].order`);
        }
        prevMax = band.max_ms;
      }
    }
    if (!openFound) {
      errors.push("latency.bands.missing_open");
    }
  }

  // CV bands
  const cvBands = policy.stability.cv_bands;
  if (!Array.isArray(cvBands) || cvBands.length === 0) {
    errors.push("stability.cv_bands.empty");
  } else {
    let prevMax = -Infinity;
    let openFound = false;
    for (let i = 0; i < cvBands.length; i += 1) {
      const band = cvBands[i];
      if (typeof band.score !== "number" || !Number.isInteger(band.score) || band.score < 0 || band.score > 100) {
        errors.push(`stability.cv_bands[${i}].score`);
      }
      if (band.max_percent === null) {
        if (i !== cvBands.length - 1) {
          errors.push(`stability.cv_bands[${i}].open_not_last`);
        }
        openFound = true;
      } else {
        if (typeof band.max_percent !== "number" || !Number.isFinite(band.max_percent) || band.max_percent < 0) {
          errors.push(`stability.cv_bands[${i}].max_percent`);
        } else if (band.max_percent <= prevMax) {
          errors.push(`stability.cv_bands[${i}].order`);
        }
        prevMax = band.max_percent;
      }
    }
    if (!openFound) {
      errors.push("stability.cv_bands.missing_open");
    }
  }

  // Dimension caps
  for (const key of DIMENSION_KEYS) {
    const cap = policy.dimension_caps[key];
    if (cap !== null) {
      if (
        typeof cap.below_score !== "number" ||
        !Number.isInteger(cap.below_score) ||
        cap.below_score < 0 ||
        cap.below_score > 100
      ) {
        errors.push(`dimension_caps.${key}.below_score`);
      }
      if (!["A", "B", "C", "D", "F"].includes(cap.grade_cap)) {
        errors.push(`dimension_caps.${key}.grade_cap`);
      }
    }
  }

  return { ok: errors.length === 0, errors };
}

// ---------------------------------------------------------------------------
// Custom target URL best-effort safety (client-side mirror)
// ---------------------------------------------------------------------------

export function isValidCustomTargetURL(url: string): boolean {
  if (!url) {
    return true; // empty means reuse profile ServiceURL
  }
  try {
    const u = new URL(url);
    // Scheme must be exactly https: — not http, not any other protocol.
    if (u.protocol !== "https:") {
      return false;
    }
    if (u.username || u.password) {
      return false;
    }
    if (u.hash) {
      return false;
    }
    const host = u.hostname.toLowerCase();
    if (!host) {
      return false;
    }
    if (host === "localhost" || host.endsWith(".localhost")) {
      return false;
    }
    // Reject obvious IP literals in loopback/private ranges
    if (/^\d+\.\d+\.\d+\.\d+$/.test(host)) {
      const parts = host.split(".").map(Number);
      if (parts[0] === 10) return false;
      if (parts[0] === 127) return false;
      if (parts[0] === 169 && parts[1] === 254) return false;
      if (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) return false;
      if (parts[0] === 192 && parts[1] === 168) return false;
      if (parts[0] === 100 && parts[1] === 64) return false;
    }
    // IPv6 loopback/private
    if (host === "::1" || host === "[::1]") return false;
    if (host.startsWith("fc") || host.startsWith("fd")) return false;
    if (host.startsWith("fe80")) return false;
    return true;
  } catch {
    return false;
  }
}

// ---------------------------------------------------------------------------
// Display helpers (moved from ScoreBreakdown per react-refresh isolation)
// ---------------------------------------------------------------------------

/** Map a grade letter to a badge variant. */
export function gradeBadgeVariant(grade: string | null | undefined): "success" | "info" | "warning" | "danger" | "neutral" {
  switch (grade) {
    case "A":
      return "success";
    case "B":
      return "info";
    case "C":
      return "warning";
    case "D":
    case "F":
      return "danger";
    default:
      return "neutral";
  }
}

/** Map a CF status token to a badge variant. */
export function cfStatusBadgeVariant(status: CloudflareStatusToken): "success" | "info" | "warning" | "danger" | "neutral" | "muted" {
  switch (status) {
    case "clean":
    case "not_detected":
      return "success";
    case "js_challenge":
    case "captcha_challenge":
    case "challenge":
      return "warning";
    case "block":
    case "ng":
      return "danger";
    case "unchecked":
    default:
      return "muted";
  }
}

/** Human-readable label for a CF status token. */
export function cfStatusLabel(status: CloudflareStatusToken): string {
  switch (status) {
    case "clean":
      return "CF 干净";
    case "not_detected":
      return "未发现 CF";
    case "js_challenge":
      return "JS 挑战";
    case "captcha_challenge":
      return "验证码挑战";
    case "challenge":
      return "CF 挑战";
    case "block":
      return "CF 封锁";
    case "ng":
      return "不可达";
    case "unchecked":
    default:
      return "未检测";
  }
}

/** Longer description for a CF status token. */
export function cfStatusDescription(status: CloudflareStatusToken): string {
  switch (status) {
    case "clean":
      return "响应带有 Cloudflare 特征且未被挑战";
    case "not_detected":
      return "响应成功但未观察到 Cloudflare 特征";
    case "js_challenge":
      return "Cloudflare JavaScript 挑战页";
    case "captcha_challenge":
      return "Cloudflare 验证码挑战页";
    case "challenge":
      return "Cloudflare 通用挑战（cf-mitigated: challenge）";
    case "block":
      return "Cloudflare 直接封锁（1020 等）";
    case "ng":
      return "观测请求失败，不计入 CF 评分";
    case "unchecked":
    default:
      return "历史记录尚未在新评分引擎下刷新";
  }
}

/** Human-readable dimension label. */
export function dimensionLabel(key: DimensionKey): string {
  switch (key) {
    case "service":
      return "服务可达";
    case "api":
      return "API 可达";
    case "cloudflare":
      return "Cloudflare";
    case "stability":
      return "稳定性";
    case "latency":
      return "延迟";
  }
}

/** Human-readable label for a CF policy mode. */
export function cfPolicyModeLabel(mode: string): string {
  switch (mode) {
    case "observe_only":
      return "仅观测";
    case "score":
      return "影响评分";
    case "grade":
      return "影响等级";
    case "score_and_grade":
      return "影响评分与等级";
    default:
      return mode;
  }
}

/** Description text for a CF policy mode. */
export function cfPolicyModeDescription(mode: string): string {
  switch (mode) {
    case "observe_only":
      return "始终观测并记录 Cloudflare 状态，但不影响分数和等级";
    case "score":
      return "观测结果按状态分计入总分，但不改变等级封顶";
    case "grade":
      return "观测结果按挑战类型封顶等级，但不计入总分";
    case "score_and_grade":
      return "观测结果同时计入总分并按挑战类型封顶等级";
    default:
      return mode;
  }
}

// ---------------------------------------------------------------------------
// Contradiction detection between legacy challenged filter and detailed status
// ---------------------------------------------------------------------------

/**
 * Detect contradiction between legacy `quality_cloudflare_challenged` and the
 * detailed multi-select status filter.
 *
 * Correct semantics:
 * - legacy challenged=true  → contradiction only when selected has ZERO challenge statuses
 * - legacy challenged=false → contradiction only when selected has ZERO non-challenge statuses
 * - Mixed set (contains both challenge and non-challenge) is NOT a contradiction
 *   because the intersection with the legacy filter is still non-empty.
 */
/**
 * Normalize legacy-challenged value: accepts both node vocabulary
 * (`"true"`/`"false"`/`"any"`) and platform vocabulary
 * (`"challenged"`/`"clean"`/`"any"`).
 */
function normalizeLegacyChallenged(raw: string): "any" | "true" | "false" {
  if (raw === "true" || raw === "challenged") return "true";
  if (raw === "false" || raw === "clean") return "false";
  return "any";
}

export function isCFContradiction(
  selected: CloudflareStatusToken[],
  legacyChallenged: string,
): boolean {
  const norm = normalizeLegacyChallenged(legacyChallenged);
  if (selected.length === 0 || norm === "any") {
    return false;
  }
  const hasChallenge = selected.some((tok) => isChallengeStatus(tok));
  const hasNonChallenge = selected.some((tok) => !isChallengeStatus(tok));
  // Mixed set → no contradiction (intersection non-empty).
  if (hasChallenge && hasNonChallenge) {
    return false;
  }
  if (norm === "true") {
    // Need at least one challenge status; having only non-challenge → contradiction.
    return !hasChallenge;
  }
  // norm === "false"
  // Need at least one non-challenge status; having only challenge → contradiction.
  return !hasNonChallenge;
}
