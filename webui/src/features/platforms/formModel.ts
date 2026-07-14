import { z } from "zod";
import { normalizeProtocolList } from "../../lib/protocolOptions";
import { allocationPolicies, emptyAccountBehaviors, missActions, qualityCloudflareFilterOptions } from "./constants";
import { parseHeaderLines, parseLinesToList } from "./formParsers";
import type { Platform, PlatformCreateInput, PlatformUpdateInput } from "./types";

const platformNameForbiddenChars = ".:|/\\@?#%~";
const platformNameForbiddenSpacing = " \t\r\n";
const platformNameReserved = "api";

function containsAny(source: string, chars: string): boolean {
  for (const ch of chars) {
    if (source.includes(ch)) {
      return true;
    }
  }
  return false;
}

export const platformNameRuleHint = "平台名不能包含 .:|/\\@?#%~、空格、Tab、换行、回车，也不能为保留字。";

const NS_PER_MS = 1_000_000;

/** Convert a unix-nanosecond value (0 = no filter) to a datetime-local input value. */
export function nsToDatetimeLocal(ns: number): string {
  if (!ns || ns <= 0) return "";
  const ms = Math.floor(ns / NS_PER_MS);
  if (!Number.isFinite(ms)) return "";
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Convert a datetime-local input value to unix-nanoseconds. Empty returns 0. */
export function datetimeLocalToNs(localValue: string): number {
  if (!localValue) return 0;
  const d = new Date(localValue);
  if (Number.isNaN(d.getTime())) return 0;
  return Math.floor(d.getTime() * NS_PER_MS);
}

export const platformFormSchema = z.object({
  name: z.string().trim()
    .min(1, "平台名称不能为空")
    .refine((value) => !containsAny(value, platformNameForbiddenChars), {
      message: "平台名称不能包含字符 .:|/\\@?#%~",
    })
    .refine((value) => !containsAny(value, platformNameForbiddenSpacing), {
      message: "平台名称不能包含空格、Tab、换行、回车",
    })
    .refine((value) => value.toLowerCase() !== platformNameReserved, {
      message: "平台名称不能为保留字",
    }),
  sticky_ttl: z.string().optional(),
  regex_filters_text: z.string().optional(),
  region_filters_text: z.string().optional(),
  protocol_filters: z.array(z.string()),
  exclude_protocol_filters: z.array(z.string()),
  reverse_proxy_miss_action: z.enum(missActions),
  reverse_proxy_empty_account_behavior: z.enum(emptyAccountBehaviors),
  reverse_proxy_fixed_account_header: z.string().optional(),
  allocation_policy: z.enum(allocationPolicies),
  passive_circuit_breaker_disabled: z.boolean(),
  quality_grade: z.string().optional(),
  quality_min_score_text: z.string().optional(),
  quality_cloudflare_filter: z.enum(qualityCloudflareFilterOptions),
  quality_checked_since_text: z.string().optional(),
  quality_profile: z.string().optional(),
}).superRefine((value, ctx) => {
  if (
    value.reverse_proxy_empty_account_behavior === "FIXED_HEADER" &&
    parseHeaderLines(value.reverse_proxy_fixed_account_header).length === 0
  ) {
    ctx.addIssue({
      code: "custom",
      path: ["reverse_proxy_fixed_account_header"],
      message: "用于提取 Account 的 Headers 不能为空",
    });
  }
});

export type PlatformFormValues = z.infer<typeof platformFormSchema>;

export const defaultPlatformFormValues: PlatformFormValues = {
  name: "",
  sticky_ttl: "",
  regex_filters_text: "",
  region_filters_text: "",
  protocol_filters: [],
  exclude_protocol_filters: [],
  reverse_proxy_miss_action: "TREAT_AS_EMPTY",
  reverse_proxy_empty_account_behavior: "RANDOM",
  reverse_proxy_fixed_account_header: "Authorization",
  allocation_policy: "BALANCED",
  passive_circuit_breaker_disabled: false,
  quality_grade: "",
  quality_min_score_text: "",
  quality_cloudflare_filter: "any",
  quality_checked_since_text: "",
  quality_profile: "",
};

export function platformToFormValues(platform: Platform): PlatformFormValues {
  const regexFilters = Array.isArray(platform.regex_filters) ? platform.regex_filters : [];
  const regionFilters = Array.isArray(platform.region_filters) ? platform.region_filters : [];

  // Map nullable bool to tri-state select value.
  let cfFilter: "any" | "challenged" | "clean" = "any";
  if (platform.quality_cloudflare_challenged === true) {
    cfFilter = "challenged";
  } else if (platform.quality_cloudflare_challenged === false) {
    cfFilter = "clean";
  }

  return {
    name: platform.name,
    sticky_ttl: platform.sticky_ttl,
    regex_filters_text: regexFilters.join("\n"),
    region_filters_text: regionFilters.join("\n"),
    protocol_filters: normalizeProtocolList(platform.protocol_filters),
    exclude_protocol_filters: normalizeProtocolList(platform.exclude_protocol_filters),
    reverse_proxy_miss_action: platform.reverse_proxy_miss_action,
    reverse_proxy_empty_account_behavior: platform.reverse_proxy_empty_account_behavior,
    reverse_proxy_fixed_account_header: platform.reverse_proxy_fixed_account_header,
    allocation_policy: platform.allocation_policy,
    passive_circuit_breaker_disabled: platform.passive_circuit_breaker_disabled,
    quality_grade: platform.quality_grade || "",
    quality_min_score_text: platform.quality_min_score > 0 ? String(platform.quality_min_score) : "",
    quality_cloudflare_filter: cfFilter,
    quality_checked_since_text: nsToDatetimeLocal(platform.quality_checked_since_ns),
    quality_profile: platform.quality_profile || "",
  };
}

function toPlatformPayloadBase(values: PlatformFormValues) {
  // Parse min score: empty or invalid => 0 (no filter).
  const minScoreRaw = (values.quality_min_score_text || "").trim();
  let quality_min_score = 0;
  if (minScoreRaw) {
    const parsed = Number(minScoreRaw);
    if (Number.isFinite(parsed)) {
      quality_min_score = Math.max(0, Math.min(100, Math.round(parsed)));
    }
  }

  // Map tri-state select back to nullable bool.
  let quality_cloudflare_challenged: boolean | null = null;
  if (values.quality_cloudflare_filter === "challenged") {
    quality_cloudflare_challenged = true;
  } else if (values.quality_cloudflare_filter === "clean") {
    quality_cloudflare_challenged = false;
  }

  return {
    name: values.name.trim(),
    regex_filters: parseLinesToList(values.regex_filters_text),
    region_filters: parseLinesToList(values.region_filters_text, (value) => value.toLowerCase()),
    protocol_filters: normalizeProtocolList(values.protocol_filters),
    exclude_protocol_filters: normalizeProtocolList(values.exclude_protocol_filters),
    reverse_proxy_miss_action: values.reverse_proxy_miss_action,
    reverse_proxy_empty_account_behavior: values.reverse_proxy_empty_account_behavior,
    reverse_proxy_fixed_account_header: parseHeaderLines(values.reverse_proxy_fixed_account_header).join("\n"),
    allocation_policy: values.allocation_policy,
    passive_circuit_breaker_disabled: values.passive_circuit_breaker_disabled,
    quality_grade: (values.quality_grade || "").trim(),
    quality_min_score,
    quality_cloudflare_challenged,
    quality_checked_since_ns: datetimeLocalToNs(values.quality_checked_since_text || ""),
    quality_profile: (values.quality_profile || "").trim(),
  };
}

export function toPlatformCreateInput(values: PlatformFormValues): PlatformCreateInput {
  return {
    ...toPlatformPayloadBase(values),
    sticky_ttl: values.sticky_ttl?.trim() || undefined,
  };
}

export function toPlatformUpdateInput(values: PlatformFormValues): PlatformUpdateInput {
  return {
    ...toPlatformPayloadBase(values),
    sticky_ttl: values.sticky_ttl?.trim() || "",
  };
}
