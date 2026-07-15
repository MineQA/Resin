import type { CloudflareStatusToken } from "../../lib/cloudflareStatus";

export type PlatformMissAction = "TREAT_AS_EMPTY" | "REJECT";
export type PlatformEmptyAccountBehavior = "RANDOM" | "FIXED_HEADER" | "ACCOUNT_HEADER_RULE";
export type PlatformAllocationPolicy = "BALANCED" | "PREFER_LOW_LATENCY" | "PREFER_IDLE_IP";
export type QualityGradeFilter = "A" | "B" | "C" | "D" | "F";
export type QualityCloudflareFilter = "any" | "challenged" | "clean";
export type QualityProfile = "generic" | "openai" | "grok" | "gemini" | "claude";

export type Platform = {
  id: string;
  name: string;
  sticky_ttl: string;
  regex_filters: string[];
  region_filters: string[];
  protocol_filters: string[];
  exclude_protocol_filters: string[];
  routable_node_count: number;
  reverse_proxy_miss_action: PlatformMissAction;
  reverse_proxy_empty_account_behavior: PlatformEmptyAccountBehavior;
  reverse_proxy_fixed_account_header: string;
  allocation_policy: PlatformAllocationPolicy;
  passive_circuit_breaker_disabled: boolean;
  quality_grade: string;
  quality_min_score: number;
  quality_cloudflare_challenged: boolean | null;
  /** Detailed CF status filter array (repeated query keys, OR within values). */
  quality_cloudflare_statuses: CloudflareStatusToken[];
  quality_checked_since_ns: number;
  quality_profile: string;
  updated_at: string;
};

export type PageResponse<T> = {
  items: T[];
  total: number;
  limit: number;
  offset: number;
};

export type PlatformCreateInput = {
  name: string;
  sticky_ttl?: string;
  regex_filters?: string[];
  region_filters?: string[];
  protocol_filters?: string[];
  exclude_protocol_filters?: string[];
  reverse_proxy_miss_action?: PlatformMissAction;
  reverse_proxy_empty_account_behavior?: PlatformEmptyAccountBehavior;
  reverse_proxy_fixed_account_header?: string;
  allocation_policy?: PlatformAllocationPolicy;
  passive_circuit_breaker_disabled?: boolean;
  quality_grade?: string;
  quality_min_score?: number;
  quality_cloudflare_challenged?: boolean | null;
  quality_cloudflare_statuses?: CloudflareStatusToken[];
  quality_checked_since_ns?: number;
  quality_profile?: string;
};

export type PlatformUpdateInput = {
  name?: string;
  sticky_ttl?: string;
  regex_filters?: string[];
  region_filters?: string[];
  protocol_filters?: string[];
  exclude_protocol_filters?: string[];
  reverse_proxy_miss_action?: PlatformMissAction;
  reverse_proxy_empty_account_behavior?: PlatformEmptyAccountBehavior;
  reverse_proxy_fixed_account_header?: string;
  allocation_policy?: PlatformAllocationPolicy;
  passive_circuit_breaker_disabled?: boolean;
  quality_grade?: string;
  quality_min_score?: number;
  quality_cloudflare_challenged?: boolean | null;
  quality_cloudflare_statuses?: CloudflareStatusToken[];
  quality_checked_since_ns?: number;
  quality_profile?: string;
};

export type PlatformLease = {
  platform_id: string;
  account: string;
  node_hash: string;
  node_tag: string;
  egress_ip: string;
  expiry: string;
  last_accessed: string;
};

export type PlatformLeaseSortBy = "account" | "expiry" | "last_accessed";
export type SortOrder = "asc" | "desc";

export type ListPlatformLeasesInput = {
  limit?: number;
  offset?: number;
  account?: string;
  fuzzy?: boolean;
  sort_by?: PlatformLeaseSortBy;
  sort_order?: SortOrder;
};
