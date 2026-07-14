import { apiRequest } from "../../lib/api-client";
import type {
  CloudflareStatus,
  EgressProbeResult,
  LatencyProbeResult,
  NodeQuality,
  NodeListQuery,
  NodePoolExportResponse,
  NodeSummary,
  PageResponse,
} from "./types";

const basePath = "/api/v1/nodes";
const listBasePath = "/api/v1/node-pool/nodes";

type ApiNodeQuality = {
  quality_profile?: string | null;
  quality_grade?: string | null;
  quality_score?: number | null;
  quality_unstable?: boolean | null;
  quality_service_reachable?: boolean | null;
  quality_api_reachable?: boolean | null;
  quality_cloudflare_challenged?: boolean | null;
  quality_cloudflare_challenge_type?: string | null;
  quality_cloudflare_status?: string | null;
  quality_avg_latency_ms?: number | null;
  quality_last_checked?: string | null;
  quality_last_error?: string | null;
};

type ApiNodeSummary = Omit<NodeSummary, "tags" | "quality"> & {
  tags?: NodeSummary["tags"] | null;
  enabled?: boolean | null;
  display_tag?: string | null;
  protocol?: string | null;
  last_error?: string | null;
  circuit_open_since?: string | null;
  egress_ip?: string | null;
  reference_latency_ms?: number | null;
  region?: string | null;
  last_egress_update?: string | null;
  last_latency_probe_attempt?: string | null;
  last_authority_latency_probe_attempt?: string | null;
  last_egress_update_attempt?: string | null;
  quality?: ApiNodeQuality | null;
};

function asStringOrEmpty(raw: unknown): string {
  return typeof raw === "string" ? raw : "";
}

function normalizeCloudflareStatus(raw: string | null | undefined): CloudflareStatus | undefined {
  if (raw === "challenged" || raw === "clean" || raw === "ng") {
    return raw;
  }
  return undefined;
}

function normalizeQuality(raw: ApiNodeQuality | null | undefined): NodeQuality | undefined {
  if (!raw) {
    return undefined;
  }
  if (!raw.quality_grade && raw.quality_grade !== "") {
    // Missing grade means no quality record; backend omits the whole object.
    return undefined;
  }
  const grade = raw.quality_grade || "";
  if (!grade) {
    return undefined;
  }
  return {
    quality_profile: raw.quality_profile || undefined,
    quality_grade: grade,
    quality_score: typeof raw.quality_score === "number" ? raw.quality_score : 0,
    quality_unstable: Boolean(raw.quality_unstable),
    quality_service_reachable: Boolean(raw.quality_service_reachable),
    quality_api_reachable: Boolean(raw.quality_api_reachable),
    quality_cloudflare_challenged: Boolean(raw.quality_cloudflare_challenged),
    quality_cloudflare_challenge_type: raw.quality_cloudflare_challenge_type || undefined,
    quality_cloudflare_status: normalizeCloudflareStatus(raw.quality_cloudflare_status),
    quality_avg_latency_ms: typeof raw.quality_avg_latency_ms === "number" ? raw.quality_avg_latency_ms : undefined,
    quality_last_checked: raw.quality_last_checked || undefined,
    quality_last_error: raw.quality_last_error || undefined,
  };
}

function normalizeNode(raw: ApiNodeSummary): NodeSummary {
  const { reference_latency_ms, quality, ...rest } = raw;
  const normalized: NodeSummary = {
    ...rest,
    enabled: raw.enabled !== false,
    display_tag: raw.display_tag || "",
    protocol: raw.protocol || "",
    tags: Array.isArray(raw.tags) ? raw.tags : [],
    last_error: raw.last_error || "",
    circuit_open_since: raw.circuit_open_since || "",
    egress_ip: raw.egress_ip || "",
    region: raw.region || "",
    last_egress_update: raw.last_egress_update || "",
    last_latency_probe_attempt: raw.last_latency_probe_attempt || "",
    last_authority_latency_probe_attempt: raw.last_authority_latency_probe_attempt || "",
    last_egress_update_attempt: raw.last_egress_update_attempt || "",
  };

  // Backend uses `omitempty`; field missing means "no reference latency".
  if (typeof reference_latency_ms === "number") {
    normalized.reference_latency_ms = reference_latency_ms;
  }

  const normalizedQuality = normalizeQuality(quality);
  if (normalizedQuality) {
    normalized.quality = normalizedQuality;
  }

  // Ensure no stray empty-string fields leak from spread.
  if (!normalized.display_tag) normalized.display_tag = asStringOrEmpty(raw.display_tag);
  if (!normalized.protocol) normalized.protocol = asStringOrEmpty(raw.protocol);

  return normalized;
}

export async function listNodes(filters: NodeListQuery): Promise<PageResponse<NodeSummary>> {
  const query = buildNodeListSearchParams(filters);
  const data = await apiRequest<PageResponse<ApiNodeSummary>>(`${listBasePath}?${query.toString()}`);
  return {
    ...data,
    items: data.items.map(normalizeNode),
  };
}

export function buildNodeListSearchParams(filters: NodeListQuery): URLSearchParams {
  const query = new URLSearchParams({
    limit: String(filters.limit ?? 50),
    offset: String(filters.offset ?? 0),
    sort_by: filters.sort_by || "tag",
    sort_order: filters.sort_order || "asc",
  });

  const appendIfNotEmpty = (key: string, value?: string) => {
    if (!value) {
      return;
    }
    const trimmed = value.trim();
    if (!trimmed) {
      return;
    }
    query.set(key, trimmed);
  };

  appendIfNotEmpty("platform_id", filters.platform_id);
  appendIfNotEmpty("subscription_id", filters.subscription_id);
  appendIfNotEmpty("tag_keyword", filters.tag_keyword);
  appendIfNotEmpty("region", filters.region?.toLowerCase());
  appendIfNotEmpty("egress_ip", filters.egress_ip);
  appendIfNotEmpty("probed_since", filters.probed_since);
  appendIfNotEmpty("protocol", filters.protocol);
  appendIfNotEmpty("exclude_protocol", filters.exclude_protocol);

  if (filters.circuit_open !== undefined) {
    query.set("circuit_open", String(filters.circuit_open));
  }
  if (filters.has_outbound !== undefined) {
    query.set("has_outbound", String(filters.has_outbound));
  }
  if (filters.enabled !== undefined) {
    query.set("enabled", String(filters.enabled));
  }
  if (filters.routable !== undefined) {
    query.set("routable", String(filters.routable));
  }

  // Quality filters
  appendIfNotEmpty("quality_grade", filters.quality_grade);
  appendIfNotEmpty("quality_profile", filters.quality_profile);
  appendIfNotEmpty("quality_checked_since", filters.quality_checked_since);
  if (filters.quality_min_score !== undefined) {
    query.set("quality_min_score", String(filters.quality_min_score));
  }
  if (filters.quality_cloudflare_challenged !== undefined) {
    query.set("quality_cloudflare_challenged", String(filters.quality_cloudflare_challenged));
  }

  return query;
}

export async function getNode(hash: string): Promise<NodeSummary> {
  const data = await apiRequest<ApiNodeSummary>(`${basePath}/${hash}`);
  return normalizeNode(data);
}

export async function probeEgress(hash: string): Promise<EgressProbeResult> {
  return apiRequest<EgressProbeResult>(`${basePath}/${hash}/actions/probe-egress`, {
    method: "POST",
  });
}

export async function probeLatency(hash: string): Promise<LatencyProbeResult> {
  return apiRequest<LatencyProbeResult>(`${basePath}/${hash}/actions/probe-latency`, {
    method: "POST",
  });
}

export function buildNodePoolExportURL(filters: NodeListQuery, exportToken: string, format: string = "clash"): string {
  const query = buildNodeListSearchParams({
    ...filters,
    sort_by: undefined,
    sort_order: undefined,
    limit: filters.limit ?? 100000,
    offset: filters.offset ?? 0,
  });
  query.set("format", format);
  query.set("export_token", exportToken);
  return `/api/v1/node-pool/export?${query.toString()}`;
}

export async function exportNodePool(filters: NodeListQuery, exportToken: string): Promise<NodePoolExportResponse> {
  return apiRequest<NodePoolExportResponse>(buildNodePoolExportURL(filters, exportToken, "sing-box"), { auth: false });
}

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL?.trim() ?? "";

function apiURL(path: string): string {
  if (path.startsWith("http://") || path.startsWith("https://")) {
    return path;
  }
  return `${API_BASE_URL}${path}`;
}

export async function exportNodePoolText(filters: NodeListQuery, exportToken: string, format: string = "clash"): Promise<string> {
  const url = buildNodePoolExportURL(filters, exportToken, format);
  const res = await fetch(apiURL(url));
  if (!res.ok) {
    throw new Error(`export failed: ${res.status} ${res.statusText}`);
  }
  return res.text();
}
