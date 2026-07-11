import { apiRequest } from "../../lib/api-client";
import type {
  EgressProbeResult,
  LatencyProbeResult,
  NodeListQuery,
  NodePoolExportResponse,
  NodeSummary,
  PageResponse,
} from "./types";

const basePath = "/api/v1/nodes";
const listBasePath = "/api/v1/node-pool/nodes";

type ApiNodeSummary = Omit<NodeSummary, "tags"> & {
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
};

function normalizeNode(raw: ApiNodeSummary): NodeSummary {
  const { reference_latency_ms, ...rest } = raw;
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
