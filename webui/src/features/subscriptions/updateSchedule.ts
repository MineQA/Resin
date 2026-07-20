import type { SubscriptionUpdateMode } from "./types";

export const DEFAULT_DAILY_UPDATE_TIME = "04:00";
export const LOCAL_SOURCE_UPDATE_INTERVAL = "12h";

const HHMM_PATTERN = /^([01]\d|2[0-3]):([0-5]\d)$/;

export function resolveBrowserTimezone(): string {
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (typeof tz === "string" && tz.trim()) {
      return tz.trim();
    }
  } catch {
    // ignore
  }
  return "UTC";
}

/** Missing or unknown update_mode is treated as interval for legacy rows. */
export function normalizeUpdateMode(value: unknown): SubscriptionUpdateMode {
  if (value === "daily") {
    return "daily";
  }
  return "interval";
}

/** Normalize browser time inputs (HH:mm or HH:mm:ss) toward strict HH:mm. */
export function normalizeUpdateTime(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed) {
    return "";
  }

  const match = /^(\d{1,2}):(\d{2})(?::\d{2})?$/.exec(trimmed);
  if (!match) {
    return trimmed;
  }

  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (!Number.isInteger(hour) || !Number.isInteger(minute) || hour < 0 || hour > 23 || minute < 0 || minute > 59) {
    return trimmed;
  }

  return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}

export function isValidUpdateTime(raw: string): boolean {
  return HHMM_PATTERN.test(normalizeUpdateTime(raw));
}

export type UpdateScheduleFormValues = {
  source_type: "remote" | "local";
  update_mode: SubscriptionUpdateMode;
  update_interval: string;
  update_time: string;
  update_timezone: string;
};

export type UpdateSchedulePayload = {
  update_mode: SubscriptionUpdateMode;
  update_interval: string;
  update_time?: string;
  update_timezone?: string;
};

/**
 * Build API update-schedule fields.
 * Local sources always use interval mode (daily is not meaningful for local text).
 * Retained alternate-mode fields may be included when non-empty so switching back is seamless.
 */
export function buildUpdateSchedulePayload(values: UpdateScheduleFormValues): UpdateSchedulePayload {
  const updateInterval =
    values.source_type === "local"
      ? LOCAL_SOURCE_UPDATE_INTERVAL
      : values.update_interval.trim();

  if (values.source_type === "local") {
    return {
      update_mode: "interval",
      update_interval: updateInterval,
    };
  }

  const mode = values.update_mode === "daily" ? "daily" : "interval";
  const updateTime = normalizeUpdateTime(values.update_time);
  const updateTimezone = values.update_timezone.trim();

  if (mode === "daily") {
    return {
      update_mode: "daily",
      update_interval: updateInterval || LOCAL_SOURCE_UPDATE_INTERVAL,
      update_time: updateTime,
      update_timezone: updateTimezone,
    };
  }

  const payload: UpdateSchedulePayload = {
    update_mode: "interval",
    update_interval: updateInterval,
  };
  if (updateTime) {
    payload.update_time = updateTime;
  }
  if (updateTimezone) {
    payload.update_timezone = updateTimezone;
  }
  return payload;
}

export type FormatUpdatePlanInput = {
  source_type: "remote" | "local";
  update_mode: SubscriptionUpdateMode;
  update_interval: string;
  update_time: string;
  update_timezone: string;
};

/**
 * Human-readable schedule for list cells.
 * `formatInterval` should already localize Go durations (e.g. formatGoDuration).
 * `t` maps Chinese keys to the active locale.
 */
export function formatUpdatePlan(
  input: FormatUpdatePlanInput,
  formatInterval: (raw: string) => string,
  t: (text: string, options?: Record<string, unknown>) => string,
): string {
  const mode =
    input.source_type === "local" ? "interval" : normalizeUpdateMode(input.update_mode);

  if (mode === "daily") {
    const time = normalizeUpdateTime(input.update_time) || DEFAULT_DAILY_UPDATE_TIME;
    const timezone = input.update_timezone.trim() || "UTC";
    return t("每天 {{time}} · {{timezone}}", { time, timezone });
  }

  const intervalLabel = tidyIntervalLabel(formatInterval(input.update_interval || LOCAL_SOURCE_UPDATE_INTERVAL));
  return t("每 {{interval}}", { interval: intervalLabel });
}

/** Drop trailing zero units from localized duration labels (e.g. "12 小时 0 分钟" → "12 小时"). */
function tidyIntervalLabel(label: string): string {
  return label
    .replace(/\s+0\s*分钟$/, "")
    .replace(/\s+0m$/, "")
    .replace(/\s+0\s*秒$/, "")
    .replace(/\s+0s$/, "")
    .trim();
}
