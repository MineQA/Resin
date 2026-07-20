import { apiRequest } from "../../lib/api-client";
import {
  CLASH_FINGERPRINT_POLICY_DEFAULT,
  type ClashFingerprintPolicy,
  type PageResponse,
  type Subscription,
  type SubscriptionCreateInput,
  type SubscriptionUpdateInput,
} from "./types";
import { normalizeUpdateMode } from "./updateSchedule";

const basePath = "/api/v1/subscriptions";

const CLASH_FINGERPRINT_POLICY_VALUES: ReadonlySet<ClashFingerprintPolicy> = new Set([
  "reject",
  "drop_safe",
  "drop_always",
]);

function normalizeClashFingerprintPolicy(value: unknown): ClashFingerprintPolicy {
  if (typeof value === "string" && CLASH_FINGERPRINT_POLICY_VALUES.has(value as ClashFingerprintPolicy)) {
    return value as ClashFingerprintPolicy;
  }
  return CLASH_FINGERPRINT_POLICY_DEFAULT;
}

type ApiSubscription = Omit<
  Subscription,
  "last_checked" | "last_updated" | "last_error" | "update_mode" | "update_time" | "update_timezone"
> & {
  source_type?: "remote" | "local";
  content?: string;
  clash_fingerprint_policy?: ClashFingerprintPolicy;
  update_mode?: string | null;
  update_time?: string | null;
  update_timezone?: string | null;
  last_checked?: string | null;
  last_updated?: string | null;
  last_error?: string | null;
};

function normalizeSubscription(raw: ApiSubscription): Subscription {
  return {
    ...raw,
    source_type: raw.source_type ?? "remote",
    content: raw.content ?? "",
    clash_fingerprint_policy: normalizeClashFingerprintPolicy(raw.clash_fingerprint_policy),
    update_mode: normalizeUpdateMode(raw.update_mode),
    update_time: typeof raw.update_time === "string" ? raw.update_time : "",
    update_timezone: typeof raw.update_timezone === "string" ? raw.update_timezone : "",
    last_checked: raw.last_checked || "",
    last_updated: raw.last_updated || "",
    last_error: raw.last_error || "",
  };
}

function normalizeSubscriptionPage(raw: PageResponse<ApiSubscription>): PageResponse<Subscription> {
  return {
    ...raw,
    items: raw.items.map(normalizeSubscription),
  };
}

export type ListSubscriptionsInput = {
  enabled?: boolean;
  limit?: number;
  offset?: number;
  keyword?: string;
};

export async function listSubscriptions(input: ListSubscriptionsInput = {}): Promise<PageResponse<Subscription>> {
  const query = new URLSearchParams({
    limit: String(input.limit ?? 50),
    offset: String(input.offset ?? 0),
    sort_by: "created_at",
    sort_order: "desc",
  });

  if (input.enabled !== undefined) {
    query.set("enabled", String(input.enabled));
  }
  const keyword = input.keyword?.trim();
  if (keyword) {
    query.set("keyword", keyword);
  }

  const data = await apiRequest<PageResponse<ApiSubscription>>(`${basePath}?${query.toString()}`);
  return normalizeSubscriptionPage(data);
}

export async function createSubscription(input: SubscriptionCreateInput): Promise<Subscription> {
  const data = await apiRequest<ApiSubscription>(basePath, {
    method: "POST",
    body: input,
  });
  return normalizeSubscription(data);
}

export async function updateSubscription(id: string, input: SubscriptionUpdateInput): Promise<Subscription> {
  const data = await apiRequest<ApiSubscription>(`${basePath}/${id}`, {
    method: "PATCH",
    body: input,
  });
  return normalizeSubscription(data);
}

export async function deleteSubscription(id: string): Promise<void> {
  await apiRequest<void>(`${basePath}/${id}`, {
    method: "DELETE",
  });
}

export async function refreshSubscription(id: string): Promise<void> {
  await apiRequest<{ status: "ok" }>(`${basePath}/${id}/actions/refresh`, {
    method: "POST",
  });
}

export async function cleanupSubscriptionCircuitOpenNodes(id: string): Promise<number> {
  const data = await apiRequest<{ cleaned_count: number }>(`${basePath}/${id}/actions/cleanup-circuit-open-nodes`, {
    method: "POST",
  });
  return data.cleaned_count;
}
