/**
 * UI-only helpers for Rule Profile visual editor.
 * Does not reimplement AST apply/parse semantics.
 */

import type {
  GroupItem,
  ModeledGroup,
  ModeledProvider,
  ModeledRule,
  ProviderItem,
  RuleItem,
  VisualDraft,
} from "./ruleProfileVisualModel";

/** Snapshot of the context an open Apply plan was created against. */
export type ApplyPlanContext = {
  /** Draft object identity at open time (reference equality). */
  draft: VisualDraft;
  sourceFingerprint: string;
  templateYAML: string;
  stale: boolean;
};

/** Capture context for an open plan. Requires a non-stale draft. */
export function captureApplyPlanContext(
  draft: VisualDraft,
  templateYAML: string,
  stale: boolean,
): ApplyPlanContext {
  return {
    draft,
    sourceFingerprint: draft.sourceFingerprint,
    templateYAML,
    stale,
  };
}

/** True when an open plan still matches the live editor context. */
export function applyPlanContextMatches(
  snapshot: ApplyPlanContext | null,
  draft: VisualDraft | null,
  templateYAML: string,
  stale: boolean,
): boolean {
  if (!snapshot || !draft) {
    return false;
  }
  return (
    snapshot.draft === draft
    && snapshot.sourceFingerprint === draft.sourceFingerprint
    && snapshot.templateYAML === templateYAML
    && snapshot.stale === stale
    && stale === false
  );
}

let newItemSeq = 0;

function nextClientId(prefix: string): string {
  newItemSeq += 1;
  return `${prefix}-new-${newItemSeq}`;
}

/** Whether swapping index with index+dir keeps every raw item at its originalIndex. */
export function canMoveItemPastRawAnchors<T extends { kind: string; originalIndex: number }>(
  items: T[],
  index: number,
  dir: -1 | 1,
): boolean {
  const item = items[index];
  if (!item || item.kind === "raw") {
    return false;
  }
  const target = index + dir;
  if (target < 0 || target >= items.length) {
    return false;
  }
  const next = items.slice();
  const a = next[index];
  const b = next[target];
  if (!a || !b) {
    return false;
  }
  next[index] = b;
  next[target] = a;
  for (let i = 0; i < next.length; i += 1) {
    const row = next[i];
    if (row && row.kind === "raw" && row.originalIndex !== i) {
      return false;
    }
  }
  return true;
}

export function moveItemInList<T>(items: T[], index: number, dir: -1 | 1): T[] {
  const target = index + dir;
  if (target < 0 || target >= items.length) {
    return items;
  }
  const next = items.slice();
  const a = next[index];
  const b = next[target];
  if (!a || !b) {
    return items;
  }
  next[index] = b;
  next[target] = a;
  return next;
}

/** MATCH terminal index if last rule is modeled MATCH; otherwise -1. */
export function terminalMatchIndex(rules: RuleItem[]): number {
  if (rules.length === 0) {
    return -1;
  }
  const last = rules[rules.length - 1];
  if (last && last.kind === "modeled" && last.ruleType === "MATCH") {
    return rules.length - 1;
  }
  return -1;
}

export function canMoveRule(rules: RuleItem[], index: number, dir: -1 | 1): boolean {
  const item = rules[index];
  if (!item || item.kind === "raw") {
    return false;
  }
  if (item.kind === "modeled" && item.ruleType === "MATCH") {
    return false;
  }
  const matchIdx = terminalMatchIndex(rules);
  if (matchIdx >= 0 && dir === 1 && index + 1 === matchIdx) {
    // Would swap non-MATCH with terminal MATCH → MATCH not last.
    return false;
  }
  if (matchIdx >= 0 && dir === -1 && index === matchIdx) {
    return false;
  }
  return canMoveItemPastRawAnchors(rules, index, dir);
}

export function createSelectGroup(): ModeledGroup {
  return {
    kind: "modeled",
    id: nextClientId("g"),
    originalIndex: -1,
    originalName: null,
    name: "",
    type: "select",
    proxies: [],
    includeAllProxies: false,
    filter: null,
    url: null,
    interval: null,
    timeout: null,
    tolerance: null,
    unknownKeyCount: 0,
  };
}

export function createUrlTestGroup(): ModeledGroup {
  return {
    kind: "modeled",
    id: nextClientId("g"),
    originalIndex: -1,
    originalName: null,
    name: "",
    type: "url-test",
    proxies: null,
    includeAllProxies: true,
    filter: null,
    url: "https://www.gstatic.com/generate_204",
    interval: 300,
    timeout: null,
    tolerance: null,
    unknownKeyCount: 0,
  };
}

export function createHttpProvider(): ModeledProvider {
  return {
    kind: "modeled",
    id: nextClientId("p"),
    originalIndex: -1,
    originalKey: null,
    key: "",
    url: "https://",
    interval: 86400,
    unknownKeyCount: 0,
  };
}

export function createRuleSetRule(policy = "DIRECT"): ModeledRule {
  return {
    kind: "modeled",
    id: nextClientId("r"),
    originalIndex: -1,
    ruleType: "RULE-SET",
    provider: "",
    policy,
    noResolve: false,
    rawText: "",
  };
}

export function createGeoipRule(policy = "DIRECT"): ModeledRule {
  return {
    kind: "modeled",
    id: nextClientId("r"),
    originalIndex: -1,
    ruleType: "GEOIP",
    geoipCode: "CN",
    policy,
    noResolve: true,
    rawText: "",
  };
}

export function createMatchRule(policy = "DIRECT"): ModeledRule {
  return {
    kind: "modeled",
    id: nextClientId("r"),
    originalIndex: -1,
    ruleType: "MATCH",
    policy,
    noResolve: false,
    rawText: "",
  };
}

/** Insert rule before terminal MATCH when present; otherwise append. */
export function insertRuleBeforeMatch(rules: RuleItem[], rule: RuleItem): RuleItem[] {
  const matchIdx = terminalMatchIndex(rules);
  if (matchIdx >= 0) {
    const next = rules.slice();
    next.splice(matchIdx, 0, rule);
    return next;
  }
  return [...rules, rule];
}

export function policyOptionsFromDraft(draft: VisualDraft): string[] {
  const names = new Set<string>(["DIRECT", "REJECT"]);
  for (const g of draft.groups) {
    if (g.kind === "modeled" && g.name.trim()) {
      names.add(g.name.trim());
    } else if (g.kind === "raw" && g.label.trim()) {
      names.add(g.label.trim());
    }
  }
  return Array.from(names);
}

export function providerKeyOptionsFromDraft(draft: VisualDraft): string[] {
  const keys: string[] = [];
  for (const p of draft.providers) {
    if (p.kind === "modeled" && p.key.trim()) {
      keys.push(p.key.trim());
    } else if (p.kind === "raw" && p.label.trim()) {
      keys.push(p.label.trim());
    }
  }
  return keys;
}

export function countRawItems(draft: VisualDraft): number {
  return (
    draft.groups.filter((g) => g.kind === "raw").length
    + draft.providers.filter((p) => p.kind === "raw").length
    + draft.rules.filter((r) => r.kind === "raw").length
  );
}

export function updateGroupAt(groups: GroupItem[], id: string, patch: Partial<ModeledGroup>): GroupItem[] {
  return groups.map((g) => {
    if (g.id !== id || g.kind !== "modeled") {
      return g;
    }
    return { ...g, ...patch, kind: "modeled" };
  });
}

export function updateProviderAt(
  providers: ProviderItem[],
  id: string,
  patch: Partial<ModeledProvider>,
): ProviderItem[] {
  return providers.map((p) => {
    if (p.id !== id || p.kind !== "modeled") {
      return p;
    }
    return { ...p, ...patch, kind: "modeled" };
  });
}

export function updateRuleAt(rules: RuleItem[], id: string, patch: Partial<ModeledRule>): RuleItem[] {
  return rules.map((r) => {
    if (r.id !== id || r.kind !== "modeled") {
      return r;
    }
    return { ...r, ...patch, kind: "modeled" };
  });
}

export function removeModeledById<T extends { id: string; kind: string }>(items: T[], id: string): T[] {
  return items.filter((item) => !(item.id === id && item.kind === "modeled"));
}

/**
 * Deleting a modeled item is safe only when it does not shift any later raw item's
 * absolute position (core requires raw items to stay at their originalIndex).
 * Safe when: item is modeled AND no raw item exists at a higher list index.
 */
export function canDeleteModeledItem(
  items: Array<{ id: string; kind: string }>,
  id: string,
): boolean {
  const index = items.findIndex((item) => item.id === id);
  if (index < 0) {
    return false;
  }
  const item = items[index];
  if (!item || item.kind !== "modeled") {
    return false;
  }
  for (let i = index + 1; i < items.length; i += 1) {
    if (items[i]?.kind === "raw") {
      return false;
    }
  }
  return true;
}

export function parseOptionalNumber(raw: string): number | null {
  const trimmed = raw.trim();
  if (!trimmed) {
    return null;
  }
  const n = Number(trimmed);
  if (!Number.isFinite(n)) {
    return null;
  }
  return n;
}

export function membersToLines(proxies: string[] | null): string {
  if (!proxies || proxies.length === 0) {
    return "";
  }
  return proxies.join("\n");
}

export function linesToMembers(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}
