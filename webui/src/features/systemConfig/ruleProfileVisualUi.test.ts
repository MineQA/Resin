import { describe, expect, it } from "vitest";
import {
  extractAllQuoted,
  extractLeadingCount,
  extractQuoted,
  localizeVisualIssue,
} from "./ruleProfileVisualI18n";
import {
  applyPlanContextMatches,
  canDeleteModeledItem,
  canMoveItemPastRawAnchors,
  captureApplyPlanContext,
  type ApplyPlanContext,
} from "./ruleProfileVisualUi";
import type { VisualDraft } from "./ruleProfileVisualModel";

function stubDraft(fingerprint: string): VisualDraft {
  return {
    sourceFingerprint: fingerprint,
    groups: [],
    providers: [],
    rules: [],
    blockedSections: [],
  };
}

describe("canDeleteModeledItem", () => {
  const items = [
    { id: "g0", kind: "modeled" },
    { id: "g1", kind: "modeled" },
    { id: "g2", kind: "raw" },
    { id: "g3", kind: "modeled" },
  ];

  it("allows deleting modeled items after the last raw", () => {
    expect(canDeleteModeledItem(items, "g3")).toBe(true);
  });

  it("blocks deleting modeled items before a later raw", () => {
    expect(canDeleteModeledItem(items, "g0")).toBe(false);
    expect(canDeleteModeledItem(items, "g1")).toBe(false);
  });

  it("never allows deleting raw items", () => {
    expect(canDeleteModeledItem(items, "g2")).toBe(false);
  });

  it("allows delete when no raw exists after the item", () => {
    const onlyModeled = [
      { id: "a", kind: "modeled" },
      { id: "b", kind: "modeled" },
    ];
    expect(canDeleteModeledItem(onlyModeled, "a")).toBe(true);
    expect(canDeleteModeledItem(onlyModeled, "b")).toBe(true);
  });

  it("returns false for unknown id", () => {
    expect(canDeleteModeledItem(items, "missing")).toBe(false);
  });
});

describe("applyPlanContextMatches", () => {
  const draft = stubDraft("fp1");
  const yaml = "proxies: []\nrules:\n  - MATCH,DIRECT\n";

  it("matches the exact open-time snapshot", () => {
    const snap = captureApplyPlanContext(draft, yaml, false);
    expect(applyPlanContextMatches(snap, draft, yaml, false)).toBe(true);
  });

  it("rejects different draft object identity", () => {
    const snap = captureApplyPlanContext(draft, yaml, false);
    const other = stubDraft("fp1");
    expect(applyPlanContextMatches(snap, other, yaml, false)).toBe(false);
  });

  it("rejects YAML drift", () => {
    const snap = captureApplyPlanContext(draft, yaml, false);
    expect(applyPlanContextMatches(snap, draft, yaml + "\n# x", false)).toBe(false);
  });

  it("rejects stale=true", () => {
    const snap = captureApplyPlanContext(draft, yaml, false);
    expect(applyPlanContextMatches(snap, draft, yaml, true)).toBe(false);
  });

  it("rejects null snapshot or draft", () => {
    const snap: ApplyPlanContext | null = null;
    expect(applyPlanContextMatches(snap, draft, yaml, false)).toBe(false);
    expect(applyPlanContextMatches(captureApplyPlanContext(draft, yaml, false), null, yaml, false)).toBe(false);
  });
});

describe("canMoveItemPastRawAnchors", () => {
  const items = [
    { id: "a", kind: "modeled", originalIndex: 0 },
    { id: "b", kind: "raw", originalIndex: 1 },
    { id: "c", kind: "modeled", originalIndex: 2 },
  ];

  it("blocks moving modeled across raw", () => {
    expect(canMoveItemPastRawAnchors(items, 0, 1)).toBe(false);
    expect(canMoveItemPastRawAnchors(items, 2, -1)).toBe(false);
  });

  it("blocks moving raw itself", () => {
    expect(canMoveItemPastRawAnchors(items, 1, 1)).toBe(false);
    expect(canMoveItemPastRawAnchors(items, 1, -1)).toBe(false);
  });
});

describe("localizeVisualIssue", () => {
  const t = (key: string, options?: Record<string, unknown>) => {
    if (!options) {
      return key;
    }
    return key.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(options[name] ?? ""));
  };

  it("maps EMPTY_GROUP_NAME", () => {
    expect(localizeVisualIssue(t, { code: "EMPTY_GROUP_NAME", message: "Group name must not be empty" }))
      .toBe("组名称不能为空");
  });

  it("maps DUPLICATE_GROUP_NAME with quoted name", () => {
    expect(
      localizeVisualIssue(t, {
        code: "DUPLICATE_GROUP_NAME",
        message: 'Duplicate group name: "PROXY"',
      }),
    ).toBe("重复的组名称：PROXY");
  });

  it("maps RAW_GROUPS_PRESERVED with count", () => {
    expect(
      localizeVisualIssue(t, {
        code: "RAW_GROUPS_PRESERVED",
        message: "2 raw group(s) preserved (read-only in visual mode)",
      }),
    ).toBe("2 个原始组将保留（可视化只读）");
  });

  it("maps GROUP_MEMBER_STALE_REFERENCE with two quotes", () => {
    expect(
      localizeVisualIssue(t, {
        code: "GROUP_MEMBER_STALE_REFERENCE",
        message: 'Group "AUTO" proxies list references "Missing" which is not a known group',
      }),
    ).toBe("组「AUTO」的 proxies 引用了未知成员「Missing」");
  });

  it("falls back for unknown codes with detail", () => {
    expect(
      localizeVisualIssue(t, {
        code: "FUTURE_CODE",
        message: "Something new happened",
      }),
    ).toBe("（FUTURE_CODE）Something new happened");
  });

  it("extract helpers", () => {
    expect(extractQuoted('x "ab" y')).toBe("ab");
    expect(extractAllQuoted('a "one" b "two"')).toEqual(["one", "two"]);
    expect(extractLeadingCount("12 raw items")).toBe(12);
    expect(extractLeadingCount("nope")).toBeNull();
  });
});
