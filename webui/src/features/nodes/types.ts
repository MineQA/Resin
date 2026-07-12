export type NodeTag = {
  subscription_id: string;
  subscription_name: string;
  tag: string;
};

export type NodeQuality = {
  quality_profile?: string;
  quality_grade: string;
  quality_score: number;
  quality_unstable: boolean;
  quality_service_reachable: boolean;
  quality_api_reachable: boolean;
  quality_cloudflare_challenged: boolean;
  quality_cloudflare_challenge_type?: string;
  quality_avg_latency_ms?: number;
  quality_last_checked?: string;
  quality_last_error?: string;
};

export type NodeSummary = {
  node_hash: string;
  created_at: string;
  protocol?: string;
  enabled: boolean;
  display_tag?: string;
  has_outbound: boolean;
  last_error?: string;
  circuit_open_since?: string;
  failure_count: number;
  egress_ip?: string;
  reference_latency_ms?: number;
  region?: string;
  last_egress_update?: string;
  last_latency_probe_attempt?: string;
  last_authority_latency_probe_attempt?: string;
  last_egress_update_attempt?: string;
  tags: NodeTag[];
  quality?: NodeQuality;
};

export type PageResponse<T> = {
  items: T[];
  total: number;
  limit: number;
  offset: number;
  unique_egress_ips: number;
  unique_healthy_egress_ips: number;
};

export type NodeSortBy =
  | "tag"
  | "created_at"
  | "failure_count"
  | "region"
  | "quality_score"
  | "quality_last_checked";
export type SortOrder = "asc" | "desc";

export type QualityGradeFilter = "A" | "B" | "C" | "D" | "F";

export type NodeListFilters = {
  platform_id?: string;
  subscription_id?: string;
  tag_keyword?: string;
  region?: string;
  egress_ip?: string;
  probed_since?: string;
  enabled?: boolean;
  circuit_open?: boolean;
  has_outbound?: boolean;
  routable?: boolean;
  protocol?: string;
  exclude_protocol?: string;
  quality_grade?: QualityGradeFilter;
  quality_min_score?: number;
  quality_cloudflare_challenged?: boolean;
  quality_checked_since?: string;
  quality_profile?: string;
};

export type NodeListQuery = NodeListFilters & {
  sort_by?: NodeSortBy;
  sort_order?: SortOrder;
  limit?: number;
  offset?: number;
};

export type EgressProbeResult = {
  egress_ip: string;
  region?: string;
  latency_ewma_ms: number;
};

export type LatencyProbeResult = {
  latency_ewma_ms: number;
};

export type NodePoolExportResponse = {
  outbounds: unknown[];
};
