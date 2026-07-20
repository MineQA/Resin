import { describe, expect, it } from "vitest";
import {
  buildUpdateSchedulePayload,
  DEFAULT_DAILY_UPDATE_TIME,
  formatUpdatePlan,
  isValidUpdateTime,
  LOCAL_SOURCE_UPDATE_INTERVAL,
  normalizeUpdateMode,
  normalizeUpdateTime,
} from "./updateSchedule";

describe("normalizeUpdateMode", () => {
  it("defaults missing and unknown values to interval", () => {
    expect(normalizeUpdateMode(undefined)).toBe("interval");
    expect(normalizeUpdateMode(null)).toBe("interval");
    expect(normalizeUpdateMode("")).toBe("interval");
    expect(normalizeUpdateMode("INTERVAL")).toBe("interval");
    expect(normalizeUpdateMode("weekly")).toBe("interval");
  });

  it("accepts daily", () => {
    expect(normalizeUpdateMode("daily")).toBe("daily");
  });
});

describe("normalizeUpdateTime / isValidUpdateTime", () => {
  it("normalizes HH:mm and HH:mm:ss", () => {
    expect(normalizeUpdateTime("4:05")).toBe("04:05");
    expect(normalizeUpdateTime("04:05")).toBe("04:05");
    expect(normalizeUpdateTime("04:05:30")).toBe("04:05");
    expect(normalizeUpdateTime(" 23:59 ")).toBe("23:59");
  });

  it("validates strict HH:mm after normalize", () => {
    expect(isValidUpdateTime("04:00")).toBe(true);
    expect(isValidUpdateTime("4:00")).toBe(true);
    expect(isValidUpdateTime("24:00")).toBe(false);
    expect(isValidUpdateTime("12:60")).toBe(false);
    expect(isValidUpdateTime("")).toBe(false);
    expect(isValidUpdateTime("4am")).toBe(false);
  });
});

describe("buildUpdateSchedulePayload", () => {
  it("forces interval for local sources without daily fields", () => {
    expect(
      buildUpdateSchedulePayload({
        source_type: "local",
        update_mode: "daily",
        update_interval: "6h",
        update_time: "08:30",
        update_timezone: "Asia/Shanghai",
      }),
    ).toEqual({
      update_mode: "interval",
      update_interval: LOCAL_SOURCE_UPDATE_INTERVAL,
    });
  });

  it("builds interval payload and keeps optional daily fields when present", () => {
    expect(
      buildUpdateSchedulePayload({
        source_type: "remote",
        update_mode: "interval",
        update_interval: "12h",
        update_time: "08:30",
        update_timezone: "Asia/Shanghai",
      }),
    ).toEqual({
      update_mode: "interval",
      update_interval: "12h",
      update_time: "08:30",
      update_timezone: "Asia/Shanghai",
    });
  });

  it("builds daily payload with required fields and retained interval", () => {
    expect(
      buildUpdateSchedulePayload({
        source_type: "remote",
        update_mode: "daily",
        update_interval: "6h",
        update_time: "8:30",
        update_timezone: " Asia/Shanghai ",
      }),
    ).toEqual({
      update_mode: "daily",
      update_interval: "6h",
      update_time: "08:30",
      update_timezone: "Asia/Shanghai",
    });
  });
});

describe("formatUpdatePlan", () => {
  const t = (text: string, options?: Record<string, unknown>) => {
    if (!options) {
      return text;
    }
    return text.replace(/\{\{(\w+)\}\}/g, (_, key: string) => String(options[key] ?? ""));
  };
  const formatInterval = (raw: string) => {
    if (raw === "12h") {
      return "12 小时 0 分钟";
    }
    return raw;
  };

  it("formats interval and daily plans", () => {
    expect(
      formatUpdatePlan(
        {
          source_type: "remote",
          update_mode: "interval",
          update_interval: "12h",
          update_time: "",
          update_timezone: "",
        },
        formatInterval,
        t,
      ),
    ).toBe("每 12 小时");

    expect(
      formatUpdatePlan(
        {
          source_type: "remote",
          update_mode: "daily",
          update_interval: "12h",
          update_time: "08:30",
          update_timezone: "Asia/Shanghai",
        },
        formatInterval,
        t,
      ),
    ).toBe("每天 08:30 · Asia/Shanghai");
  });

  it("forces local sources to interval display", () => {
    expect(
      formatUpdatePlan(
        {
          source_type: "local",
          update_mode: "daily",
          update_interval: "12h",
          update_time: "08:30",
          update_timezone: "Asia/Shanghai",
        },
        formatInterval,
        t,
      ),
    ).toBe("每 12 小时");
  });

  it("falls back daily defaults when fields are empty", () => {
    expect(
      formatUpdatePlan(
        {
          source_type: "remote",
          update_mode: "daily",
          update_interval: "",
          update_time: "",
          update_timezone: "",
        },
        formatInterval,
        t,
      ),
    ).toBe(`每天 ${DEFAULT_DAILY_UPDATE_TIME} · UTC`);
  });
});
