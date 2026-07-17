import { apiRequest } from "../../lib/api-client";
import {
  BALANCED_SCORING_POLICY,
  normalizeCFStatus,
  type CloudflareStatusToken,
  type ScoringPolicy,
} from "../../lib/cloudflareStatus";
import type {
  ACL4SSRPreviewRequest,
  ACL4SSRPreviewResponse,
  CreatedExportToken,
  EnvConfig,
  ExportToken,
  ProxyCheckProfile,
  RuleProfileCreateBody,
  RuleProfileDetail,
  RuleProfilePatchBody,
  RuleProfileSummary,
  RuntimeConfig,
  RuntimeConfigPatch,
} from "./types";

const path = "/api/v1/system/config";

const PROXY_CHECK_PROFILES: ProxyCheckProfile[] = ["generic", "openai", "grok", "gemini", "claude"];

const DEFAULT_CONFIG: RuntimeConfig = {
  request_log_enabled: true,
  reverse_proxy_log_detail_enabled: false,
  reverse_proxy_log_req_headers_max_bytes: 0,
  reverse_proxy_log_req_body_max_bytes: 0,
  reverse_proxy_log_resp_headers_max_bytes: 0,
  reverse_proxy_log_resp_body_max_bytes: 0,
  max_consecutive_failures: 0,
  max_latency_test_interval: "",
  max_authority_latency_test_interval: "",
  max_egress_test_interval: "",
  latency_test_url: "",
  latency_authorities: [],
  p2c_latency_window: "",
  latency_decay_window: "",
  cache_flush_interval: "",
  cache_flush_dirty_threshold: 0,
  proxy_check_enabled: false,
  proxy_check_interval: "30m",
  proxy_check_profile: "generic",
  proxy_check_service_reachability: true,
  proxy_check_api_reachability: false,
  proxy_check_cloudflare_detection: true,
  proxy_check_multi_round: false,
  proxy_check_rounds: 1,
  proxy_check_trigger_on_new_node: false,
  proxy_check_scoring: null,
};

function asNumber(raw: unknown, fallback: number): number {
  const value = Number(raw);
  if (!Number.isFinite(value)) {
    return fallback;
  }
  return value;
}

function asString(raw: unknown, fallback: string): string {
  if (typeof raw !== "string") {
    return fallback;
  }
  return raw;
}

function asBool(raw: unknown, fallback: boolean): boolean {
  if (typeof raw !== "boolean") {
    return fallback;
  }
  return raw;
}

function asProfile(raw: unknown, fallback: ProxyCheckProfile): ProxyCheckProfile {
  if (typeof raw === "string" && (PROXY_CHECK_PROFILES as string[]).includes(raw)) {
    return raw as ProxyCheckProfile;
  }
  return fallback;
}

function clampInt(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) {
    return min;
  }
  const rounded = Math.round(value);
  if (rounded < min) return min;
  if (rounded > max) return max;
  return rounded;
}

// ---------------------------------------------------------------------------
// Scoring policy normalization
// ---------------------------------------------------------------------------

function asIntNumber(raw: unknown, fallback: number): number {
  const value = Number(raw);
  if (!Number.isFinite(value)) {
    return fallback;
  }
  return value;
}

function normalizeStatusScores(
  raw: Record<string, unknown> | null | undefined,
): ScoringPolicy["cloudflare"]["status_scores"] {
  const balanced = BALANCED_SCORING_POLICY.cloudflare.status_scores;
  const result = { ...balanced };
  if (!raw || typeof raw !== "object") {
    return result;
  }
  for (const key of Object.keys(result) as CloudflareStatusToken[]) {
    const val = raw[key];
    if (val === null) {
      result[key] = null;
    } else if (typeof val === "number" && Number.isFinite(val)) {
      const rounded = Math.round(val);
      result[key] = Math.max(0, Math.min(100, rounded));
    }
  }
  return result;
}

function normalizeGradeCaps(
  raw: Record<string, unknown> | null | undefined,
): ScoringPolicy["cloudflare"]["grade_caps"] {
  const accepted: string[] = ["A", "B", "C", "D", "F"];
  const result: ScoringPolicy["cloudflare"]["grade_caps"] = {};
  if (!raw || typeof raw !== "object") {
    return result;
  }
  for (const [token, grade] of Object.entries(raw)) {
    if (typeof grade === "string" && accepted.includes(grade)) {
      result[normalizeCFStatus(token) as CloudflareStatusToken] = grade as ScoringPolicy["cloudflare"]["grade_caps"][CloudflareStatusToken];
    }
  }
  return result;
}

function normalizeLatencyBands(
  raw: unknown,
): ScoringPolicy["latency"]["bands"] {
  if (!Array.isArray(raw)) {
    return BALANCED_SCORING_POLICY.latency.bands.map((b) => ({ ...b }));
  }
  return raw.map((item) => {
    const band = item as Record<string, unknown>;
    return {
      max_ms: band.max_ms === null ? null : asIntNumber(band.max_ms, 0),
      score: asIntNumber(band.score, 0),
    };
  });
}

function normalizeCVBands(
  raw: unknown,
): ScoringPolicy["stability"]["cv_bands"] {
  if (!Array.isArray(raw)) {
    return BALANCED_SCORING_POLICY.stability.cv_bands.map((b) => ({ ...b }));
  }
  return raw.map((item) => {
    const band = item as Record<string, unknown>;
    return {
      max_percent: band.max_percent === null ? null : asIntNumber(band.max_percent, 0),
      score: asIntNumber(band.score, 0),
    };
  });
}

function normalizeDimensionCaps(
  raw: unknown,
): ScoringPolicy["dimension_caps"] {
  const result: ScoringPolicy["dimension_caps"] = {
    service: null,
    api: null,
    cloudflare: null,
    stability: null,
    latency: null,
  };
  if (!raw || typeof raw !== "object") {
    return result;
  }
  const record = raw as Record<string, unknown>;
  const acceptedGrades = ["A", "B", "C", "D", "F"];
  for (const key of ["service", "api", "cloudflare", "stability", "latency"] as const) {
    const cap = record[key];
    if (cap && typeof cap === "object") {
      const obj = cap as Record<string, unknown>;
      const belowScore = asIntNumber(obj.below_score, 0);
      const gradeCap = typeof obj.grade_cap === "string" && acceptedGrades.includes(obj.grade_cap)
        ? (obj.grade_cap as "A" | "B" | "C" | "D" | "F")
        : "F";
      result[key] = { below_score: belowScore, grade_cap: gradeCap };
    } else {
      result[key] = null;
    }
  }
  return result;
}

function normalizeScoringPolicy(raw: unknown): ScoringPolicy | null {
  if (!raw || typeof raw !== "object") {
    return null;
  }
  const obj = raw as Record<string, unknown>;
  const version = asIntNumber(obj.version, 0);
  if (version !== 1) {
    return null;
  }
  const weightsRaw = (obj.weights ?? {}) as Record<string, unknown>;
  const weights = {
    service: asIntNumber(weightsRaw.service, 0),
    api: asIntNumber(weightsRaw.api, 0),
    cloudflare: asIntNumber(weightsRaw.cloudflare, 0),
    stability: asIntNumber(weightsRaw.stability, 0),
    latency: asIntNumber(weightsRaw.latency, 0),
  };
  const thresholdsRaw = (obj.grade_thresholds ?? {}) as Record<string, unknown>;
  const grade_thresholds = {
    A: asIntNumber(thresholdsRaw.A, 90),
    B: asIntNumber(thresholdsRaw.B, 75),
    C: asIntNumber(thresholdsRaw.C, 60),
    D: asIntNumber(thresholdsRaw.D, 40),
  };
  const cfRaw = (obj.cloudflare ?? {}) as Record<string, unknown>;
  const policy = typeof cfRaw.policy === "string" ? cfRaw.policy : "score_and_grade";
  const cloudflare = {
    policy: (["observe_only", "score", "grade", "score_and_grade"].includes(policy) ? policy : "score_and_grade") as ScoringPolicy["cloudflare"]["policy"],
    target_url: typeof cfRaw.target_url === "string" ? cfRaw.target_url : "",
    status_scores: normalizeStatusScores(cfRaw.status_scores as Record<string, unknown> | null),
    grade_caps: normalizeGradeCaps(cfRaw.grade_caps as Record<string, unknown> | null),
  };
  const latencyRaw = (obj.latency ?? {}) as Record<string, unknown>;
  const latency = {
    bands: normalizeLatencyBands(latencyRaw.bands),
  };
  const stabilityRaw = (obj.stability ?? {}) as Record<string, unknown>;
  const stability = {
    cv_bands: normalizeCVBands(stabilityRaw.cv_bands),
  };
  const dimension_caps = normalizeDimensionCaps(obj.dimension_caps);
  return {
    version,
    weights,
    grade_thresholds,
    cloudflare,
    latency,
    stability,
    dimension_caps,
  };
}

function normalizeRuntimeConfig(raw: Partial<RuntimeConfig> | null | undefined): RuntimeConfig {
  if (!raw) {
    return DEFAULT_CONFIG;
  }

  return {
    request_log_enabled: Boolean(raw.request_log_enabled),
    reverse_proxy_log_detail_enabled: Boolean(raw.reverse_proxy_log_detail_enabled),
    reverse_proxy_log_req_headers_max_bytes: asNumber(
      raw.reverse_proxy_log_req_headers_max_bytes,
      DEFAULT_CONFIG.reverse_proxy_log_req_headers_max_bytes,
    ),
    reverse_proxy_log_req_body_max_bytes: asNumber(
      raw.reverse_proxy_log_req_body_max_bytes,
      DEFAULT_CONFIG.reverse_proxy_log_req_body_max_bytes,
    ),
    reverse_proxy_log_resp_headers_max_bytes: asNumber(
      raw.reverse_proxy_log_resp_headers_max_bytes,
      DEFAULT_CONFIG.reverse_proxy_log_resp_headers_max_bytes,
    ),
    reverse_proxy_log_resp_body_max_bytes: asNumber(
      raw.reverse_proxy_log_resp_body_max_bytes,
      DEFAULT_CONFIG.reverse_proxy_log_resp_body_max_bytes,
    ),
    max_consecutive_failures: asNumber(raw.max_consecutive_failures, DEFAULT_CONFIG.max_consecutive_failures),
    max_latency_test_interval: asString(raw.max_latency_test_interval, DEFAULT_CONFIG.max_latency_test_interval),
    max_authority_latency_test_interval: asString(
      raw.max_authority_latency_test_interval,
      DEFAULT_CONFIG.max_authority_latency_test_interval,
    ),
    max_egress_test_interval: asString(raw.max_egress_test_interval, DEFAULT_CONFIG.max_egress_test_interval),
    latency_test_url: asString(raw.latency_test_url, DEFAULT_CONFIG.latency_test_url),
    latency_authorities: Array.isArray(raw.latency_authorities)
      ? raw.latency_authorities.filter((item): item is string => typeof item === "string")
      : DEFAULT_CONFIG.latency_authorities,
    p2c_latency_window: asString(raw.p2c_latency_window, DEFAULT_CONFIG.p2c_latency_window),
    latency_decay_window: asString(raw.latency_decay_window, DEFAULT_CONFIG.latency_decay_window),
    cache_flush_interval: asString(raw.cache_flush_interval, DEFAULT_CONFIG.cache_flush_interval),
    cache_flush_dirty_threshold: asNumber(
      raw.cache_flush_dirty_threshold,
      DEFAULT_CONFIG.cache_flush_dirty_threshold,
    ),
    proxy_check_enabled: asBool(raw.proxy_check_enabled, DEFAULT_CONFIG.proxy_check_enabled),
    proxy_check_interval: asString(raw.proxy_check_interval, DEFAULT_CONFIG.proxy_check_interval),
    proxy_check_profile: asProfile(raw.proxy_check_profile, DEFAULT_CONFIG.proxy_check_profile),
    proxy_check_service_reachability: asBool(
      raw.proxy_check_service_reachability,
      DEFAULT_CONFIG.proxy_check_service_reachability,
    ),
    proxy_check_api_reachability: asBool(
      raw.proxy_check_api_reachability,
      DEFAULT_CONFIG.proxy_check_api_reachability,
    ),
    proxy_check_cloudflare_detection: asBool(
      raw.proxy_check_cloudflare_detection,
      DEFAULT_CONFIG.proxy_check_cloudflare_detection,
    ),
    proxy_check_multi_round: asBool(raw.proxy_check_multi_round, DEFAULT_CONFIG.proxy_check_multi_round),
    proxy_check_rounds: clampInt(asNumber(raw.proxy_check_rounds, DEFAULT_CONFIG.proxy_check_rounds), 1, 3),
    proxy_check_trigger_on_new_node: asBool(
      raw.proxy_check_trigger_on_new_node,
      DEFAULT_CONFIG.proxy_check_trigger_on_new_node,
    ),
    proxy_check_scoring: normalizeScoringPolicy(raw.proxy_check_scoring),
  };
}

export async function getSystemConfig(): Promise<RuntimeConfig> {
  const data = await apiRequest<RuntimeConfig>(path);
  return normalizeRuntimeConfig(data);
}

export async function getDefaultSystemConfig(): Promise<RuntimeConfig> {
  const data = await apiRequest<RuntimeConfig>(path + "/default");
  return normalizeRuntimeConfig(data);
}

export async function patchSystemConfig(patch: RuntimeConfigPatch): Promise<RuntimeConfig> {
  const data = await apiRequest<RuntimeConfig>(path, {
    method: "PATCH",
    body: patch,
  });
  return normalizeRuntimeConfig(data);
}

export async function getEnvConfig(): Promise<EnvConfig> {
  return await apiRequest<EnvConfig>(path + "/env");
}

export async function listExportTokens(): Promise<ExportToken[]> {
  return await apiRequest<ExportToken[]>("/api/v1/export-tokens");
}

export async function createExportToken(name: string): Promise<CreatedExportToken> {
  return await apiRequest<CreatedExportToken>("/api/v1/export-tokens", {
    method: "POST",
    body: { name },
  });
}

export async function deleteExportToken(id: string): Promise<void> {
  await apiRequest<void>(`/api/v1/export-tokens/${id}`, {
    method: "DELETE",
  });
}

// ---------------------------------------------------------------------------
// Rule Profiles
// ---------------------------------------------------------------------------

const ruleProfilePath = "/api/v1/rule-profiles";

export async function listRuleProfiles(enabled?: boolean): Promise<RuleProfileSummary[]> {
  const url = enabled === undefined ? ruleProfilePath : `${ruleProfilePath}?enabled=${String(enabled)}`;
  return await apiRequest<RuleProfileSummary[]>(url);
}

export async function getRuleProfile(id: string): Promise<RuleProfileDetail> {
  return await apiRequest<RuleProfileDetail>(`${ruleProfilePath}/${id}`);
}

export async function createRuleProfile(body: RuleProfileCreateBody): Promise<RuleProfileDetail> {
  return await apiRequest<RuleProfileDetail>(ruleProfilePath, {
    method: "POST",
    body,
  });
}

export async function updateRuleProfile(id: string, body: RuleProfilePatchBody): Promise<RuleProfileDetail> {
  return await apiRequest<RuleProfileDetail>(`${ruleProfilePath}/${id}`, {
    method: "PATCH",
    body,
  });
}

export async function deleteRuleProfile(id: string): Promise<void> {
  await apiRequest<void>(`${ruleProfilePath}/${id}`, {
    method: "DELETE",
  });
}

/** Dry-run ACL4SSR → Mihomo YAML conversion. Never persists a Rule Profile. */
export async function previewACL4SSR(body: ACL4SSRPreviewRequest): Promise<ACL4SSRPreviewResponse> {
  // Send exactly one field; omit the other so JSON matches the server contract.
  const payload: Record<string, string> =
    body.source_id !== undefined && body.source_id !== ""
      ? { source_id: body.source_id }
      : { ini_content: body.ini_content ?? "" };
  return await apiRequest<ACL4SSRPreviewResponse>(`${ruleProfilePath}/acl4ssr/preview`, {
    method: "POST",
    body: payload,
  });
}

export async function triggerAllQualityProbes(): Promise<{
  candidate_count: number;
  coalesced: boolean;
}> {
  return apiRequest<{ candidate_count: number; coalesced: boolean }>(
    "/api/v1/proxy-check/actions/trigger-all",
    { method: "POST" },
  );
}
