import type {
  ScoreBreakdown,
} from "../../lib/cloudflareStatus";

/** A built-in proxy source returned by GET /api/v1/proxy-sources. */
export type ProxySource = {
  id: string;
  name: string;
  url: string;
};

/** A single proxy candidate for display and selection. */
export type ProxyCandidate = {
  id: string;
  proxy: string;
  source?: string;
  selected?: boolean;
};

/** Options sent with a proxy-check task (mirrors backend ProbeCheckOptions). */
export type ProxyCheckOptions = {
  service_reachability?: boolean;
  api_reachability?: boolean;
  cloudflare_detection?: boolean;
  protocol_discovery?: boolean;
  multi_round?: boolean;
  rounds?: number;
  ip_info_enrichment?: boolean;
};

/** Preset names mapped to ProxyCheckOptions. */
export type ProxyCheckPreset = "quick" | "standard" | "deep";

/** ProxyScore as returned by the backend.
 * Most fields have no JSON tags so they marshal as PascalCase (Go default).
 * `cloudflare_status` and `scoring_breakdown` have explicit snake_case JSON tags.
 */
export type ProxyRoundResult = {
  Latency: number;
  ServiceReachable: boolean;
  APIReachable: boolean;
  CloudflareChallenged: boolean;
  CloudflareChallengeType: string;
  /** Canonical 8-value CF status (snake_case JSON tag from backend). */
  cloudflare_status?: string;
  Error: string;
};

export type ProxyScore = {
  Grade: string;
  Score: number;
  Unstable: boolean;
  ServiceReachable: boolean;
  APIReachable: boolean;
  CloudflareChallenged: boolean;
  CloudflareChallengeType: string;
  /** Canonical 8-value aggregate CF status (snake_case JSON tag from backend). */
  cloudflare_status?: string;
  /** Compact scoring breakdown from the weighted engine (snake_case JSON tag, omitempty). */
  scoring_breakdown?: ScoreBreakdown | null;
  AvgLatencyMs: number;
  RoundResults?: ProxyRoundResult[];
};

/** A single result item in a batch check response. */
export type ProxyCheckResultItem = {
  proxy?: string;
  hash?: string;
  score?: ProxyScore | null;
  error?: string;
};

/** Aggregated batch result. */
export type ProxyCheckBatchResult = {
  results: ProxyCheckResultItem[];
  total: number;
  done: number;
  failed: number;
  summary?: string;
};

/** Task lifecycle status. */
export type ProxyCheckTaskStatus =
  | "pending"
  | "running"
  | "completed"
  | "completed_with_errors"
  | "failed";

/** A batch proxy-check task. */
export type ProxyCheckTask = {
  id: string;
  status: ProxyCheckTaskStatus;
  created_at: string;
  completed_at?: string | null;
  request: ProxyCheckBatchRequest;
  result?: ProxyCheckBatchResult | null;
  error?: string;
  total: number;
  done: number;
  failed: number;
};

/** Request body for POST /api/v1/proxy-check/tasks. */
export type ProxyCheckBatchRequest = {
  proxies?: string[];
  profile?: string;
  options?: ProxyCheckOptions;
};

/** Response from POST /api/v1/proxy-check/tasks (task snapshot). */
export type ProxyCheckTaskResponse = ProxyCheckTask;

/** Request body for POST /api/v1/proxy-sources/fetch. */
export type FetchRequest = {
  source: string;
  limit?: number;
};

/** A fetched proxy candidate from the backend. */
export type SourceProxyCandidate = {
  proxy: string;
  source_id: string;
};

/** Result item for one fetched source. */
export type SourceFetchResult = {
  source_id: string;
  source_name: string;
  total_extracted: number;
  returned_count: number;
  candidates?: SourceProxyCandidate[];
  error?: string;
};

/** Response from POST /api/v1/proxy-sources/fetch. */
export type FetchResponse = {
  results: SourceFetchResult[];
  total_sources: number;
};

/** Response from GET /api/v1/proxy-sources. */
export type SourceListResponse = ProxySource[];

/** Request body for POST /api/v1/proxy-check/import. */
export type ImportRequest = {
  proxies: string[];
  confirm_checked: true;
};

/** Response from POST /api/v1/proxy-check/import. */
export type ImportResponse = {
  imported_count: number;
  skipped_count: number;
  subscription_id: string;
  node_hashes?: string[];
  errors?: string[];
};
