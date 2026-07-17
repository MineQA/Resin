/**
 * ruleProfileVisualFidelity.ts — Fidelity reports and apply planning.
 *
 * `planRuleProfileVisualApply(baseYaml, draft)` is the combined entry point
 * that returns a VisualApplyPlan merging validation + fidelity report.
 * The plan is operation-aware: it detects renames, deletions, additions,
 * reorders, stale references, and comment risks.
 */

import { isMap, isScalar, isSeq, parseDocument, type YAMLMap, type YAMLSeq } from "yaml";
import {
  type VisualDraft,
  type FidelityIssue,
  type FidelityReport,
  type VisualApplyPlan,
  type ModeledGroup,
  type GroupItem,
  type ProviderItem,
  type RuleItem,
} from "./ruleProfileVisualModel";
import { computeFingerprint } from "./ruleProfileVisualModel";
import { parseRuleProfileVisualDraft } from "./ruleProfileVisualParse";
import { validateRuleProfileVisualDraft } from "./ruleProfileVisualValidate";

// ─── Public API — combined plan ──────────────────────────────────────────────

/**
 * Combine validation + fidelity into a single apply plan.
 * This is the recommended entry point before apply: it checks everything
 * and returns a plan that apply() can honor.
 */
export function planRuleProfileVisualApply(
  baseYaml: string,
  draft: VisualDraft,
): VisualApplyPlan {
  // 1. Validate the draft
  const validation = validateRuleProfileVisualDraft(draft);

  // 2. Build fidelity report
  const fidelity = buildOperationFidelityReport(baseYaml, draft);

  // 3. Detect operations (changes from base to draft)
  const ops = detectOperations(baseYaml, draft);

  // 4. Determine canApply: no blockers, no validation errors
  const canApply = !fidelity.hasBlocker && validation.valid;

  // 5. Compute stats
  const stats = {
    groupsChanged: ops.groupsChanged,
    providersChanged: ops.providersChanged,
    rulesChanged: ops.rulesChanged,
    operations: ops.operations,
  };

  return { canApply, fidelity, validation, stats };
}

// ─── Operation detection ─────────────────────────────────────────────────────

interface DetectedOps {
  groupsChanged: number;
  providersChanged: number;
  rulesChanged: number;
  operations: string[];
}

function detectOperations(baseYaml: string, draft: VisualDraft): DetectedOps {
  const ops = new Set<string>();
  let groupsChanged = 0;
  let providersChanged = 0;
  let rulesChanged = 0;

  const baseResult = parseRuleProfileVisualDraft(baseYaml);
  if (!baseResult.ok || !baseResult.draft) {
    return { groupsChanged: 0, providersChanged: 0, rulesChanged: 0, operations: [] };
  }

  const base = baseResult.draft;

  // Groups: detect rename, edit, add, delete
  const baseGroupMap = new Map<number, GroupItem>();
  for (const g of base.groups) baseGroupMap.set(g.originalIndex, g);
  const draftGroupMap = new Map<number, GroupItem>();
  for (const g of draft.groups) draftGroupMap.set(g.originalIndex, g);

  for (const [idx, bg] of baseGroupMap) {
    const dg = draftGroupMap.get(idx);
    if (!dg) {
      // Deleted
      ops.add("delete");
      groupsChanged++;
    } else if (bg.kind === "modeled" && dg.kind === "modeled") {
      if (bg.name !== dg.name) {
        ops.add("rename");
        groupsChanged++;
      }
      if (fieldsChanged(bg, dg)) {
        ops.add("edit");
        groupsChanged++;
      }
    }
  }
  for (const [idx] of draftGroupMap) {
    if (!baseGroupMap.has(idx)) {
      ops.add("add");
      groupsChanged++;
    }
  }
  // Reorder detection
  if (base.groups.length === draft.groups.length) {
    const baseOrder = base.groups.map((g) => g.originalIndex);
    const draftOrder = draft.groups.map((g) => g.originalIndex);
    if (!arraysEqual(baseOrder, draftOrder)) {
      ops.add("reorder");
    }
  }

  // Providers: detect rename, edit, add, delete
  const baseProvMap = new Map<number, ProviderItem>();
  for (const p of base.providers) baseProvMap.set(p.originalIndex, p);
  const draftProvMap = new Map<number, ProviderItem>();
  for (const p of draft.providers) draftProvMap.set(p.originalIndex, p);

  for (const [idx, bp] of baseProvMap) {
    const dp = draftProvMap.get(idx);
    if (!dp) {
      ops.add("delete");
      providersChanged++;
    } else if (bp.kind === "modeled" && dp.kind === "modeled") {
      if (bp.key !== dp.key) {
        ops.add("rename");
        providersChanged++;
      }
      if (bp.url !== dp.url || bp.interval !== dp.interval) {
        ops.add("edit");
        providersChanged++;
      }
    }
  }
  for (const [idx] of draftProvMap) {
    if (!baseProvMap.has(idx)) {
      ops.add("add");
      providersChanged++;
    }
  }
  if (base.providers.length === draft.providers.length) {
    const baseOrder = base.providers.map((p) => p.originalIndex);
    const draftOrder = draft.providers.map((p) => p.originalIndex);
    if (!arraysEqual(baseOrder, draftOrder)) {
      ops.add("reorder");
    }
  }

  // Rules: detect edit, add, delete, reorder
  const baseRuleMap = new Map<number, RuleItem>();
  for (const r of base.rules) baseRuleMap.set(r.originalIndex, r);
  const draftRuleMap = new Map<number, RuleItem>();
  for (const r of draft.rules) draftRuleMap.set(r.originalIndex, r);

  for (const [idx, br] of baseRuleMap) {
    const dr = draftRuleMap.get(idx);
    if (!dr) {
      ops.add("delete");
      rulesChanged++;
    } else if (br.kind === "modeled" && dr.kind === "modeled") {
      if (br.provider !== dr.provider || br.geoipCode !== dr.geoipCode ||
          br.policy !== dr.policy || br.noResolve !== dr.noResolve) {
        ops.add("edit");
        rulesChanged++;
      }
    }
  }
  for (const [idx] of draftRuleMap) {
    if (!baseRuleMap.has(idx)) {
      ops.add("add");
      rulesChanged++;
    }
  }
  if (base.rules.length === draft.rules.length) {
    const baseOrder = base.rules.map((r) => r.originalIndex);
    const draftOrder = draft.rules.map((r) => r.originalIndex);
    if (!arraysEqual(baseOrder, draftOrder)) {
      ops.add("reorder");
    }
  }

  return {
    groupsChanged,
    providersChanged,
    rulesChanged,
    operations: [...ops],
  };
}

function fieldsChanged(a: ModeledGroup, b: ModeledGroup): boolean {
  return (
    a.type !== b.type ||
    a.includeAllProxies !== b.includeAllProxies ||
    a.filter !== b.filter ||
    a.url !== b.url ||
    a.interval !== b.interval ||
    a.timeout !== b.timeout ||
    a.tolerance !== b.tolerance ||
    JSON.stringify(a.proxies) !== JSON.stringify(b.proxies)
  );
}

// ─── Operation-aware fidelity report ─────────────────────────────────────────

function buildOperationFidelityReport(baseYaml: string, draft: VisualDraft): FidelityReport {
  const issues: FidelityIssue[] = [];

  // 1. Parse check
  let doc;
  try {
    doc = parseDocument(baseYaml, { prettyErrors: true, uniqueKeys: false });
  } catch {
    return blocker(issues, "general", "Base YAML cannot be parsed; Apply disabled", "BASE_PARSE_ERROR");
  }
  if (doc.errors.length > 0) {
    return blocker(issues, "general", "Base YAML has parse errors; Apply disabled", "BASE_PARSE_ERRORS");
  }

  // 2. Fingerprint check
  const baseFp = computeFingerprint(baseYaml);
  if (baseFp !== draft.sourceFingerprint) {
    return blocker(issues, "general", "Visual draft is stale; base YAML has changed", "STALE_DRAFT");
  }

  const root = doc.contents;
  if (!root || !isMap(root)) {
    return blocker(issues, "general", "Root is not a mapping", "ROOT_NOT_MAP");
  }
  const rootMap = root as YAMLMap;

  // 3. Blocked sections
  for (const sec of draft.blockedSections) {
    const label = sectionLabel(sec);
    issues.push({
      severity: "blocker",
      section: sec,
      message: `${label} section is blocked (anchors/aliases or wrong type) and cannot be visually edited`,
      code: "SECTION_BLOCKED",
    });
  }

  // 4. Get base draft for comparison
  const baseResult = parseRuleProfileVisualDraft(baseYaml);
  const baseDraft = baseResult.ok ? baseResult.draft : null;

  // 5. Detect and report operations
  if (baseDraft) {
    reportGroupChanges(baseDraft, draft, rootMap, issues);
    reportProviderChanges(baseDraft, draft, rootMap, issues);
    reportRuleChanges(baseDraft, draft, rootMap, issues);
  }

  // 6. Unknown top-level keys
  const knownTopLevel = new Set([
    "proxies", "proxy-groups", "rule-providers", "rules", "proxy-providers",
  ]);
  for (const pair of rootMap.items) {
    const key = isScalar(pair.key) ? String(pair.key) : null;
    if (key && !knownTopLevel.has(key)) {
      issues.push({
        severity: "info",
        section: "general",
        message: `Unknown top-level key "${key}" will be preserved as-is`,
        code: "UNKNOWN_TOP_LEVEL_KEY",
      });
    }
  }

  // 7. Raw items info
  const rawGroupCount = draft.groups.filter((g) => g.kind === "raw").length;
  const rawProviderCount = draft.providers.filter((p) => p.kind === "raw").length;
  const rawRuleCount = draft.rules.filter((r) => r.kind === "raw").length;
  if (rawGroupCount > 0) addInfo(issues, "groups", `${rawGroupCount} raw group(s) preserved (read-only in visual mode)`, "RAW_GROUPS_PRESERVED");
  if (rawProviderCount > 0) addInfo(issues, "providers", `${rawProviderCount} raw provider(s) preserved (read-only in visual mode)`, "RAW_PROVIDERS_PRESERVED");
  if (rawRuleCount > 0) addInfo(issues, "rules", `${rawRuleCount} raw rule(s) preserved (read-only in visual mode)`, "RAW_RULES_PRESERVED");

  // 8. Unknown modeled fields info
  for (const group of draft.groups) {
    if (group.kind === "modeled" && group.unknownKeyCount > 0) {
      addInfo(issues, "groups", `Group "${group.name}" has ${group.unknownKeyCount} unknown field(s) preserved`, "GROUP_UNKNOWN_FIELDS", group.id);
    }
  }
  for (const prov of draft.providers) {
    if (prov.kind === "modeled" && prov.unknownKeyCount > 0) {
      addInfo(issues, "providers", `Provider "${prov.key}" has ${prov.unknownKeyCount} unknown field(s) preserved`, "PROVIDER_UNKNOWN_FIELDS", prov.id);
    }
  }

  // 9. Proxy-providers / proxies pass-through
  const proxyProvidersNode = rootMap.get("proxy-providers", true);
  if (proxyProvidersNode != null) {
    addInfo(issues, "general", "proxy-providers is preserved as-is (YAML-only, not visually editable)", "PROXY_PROVIDERS_YAML_ONLY");
  }
  const proxiesNode = rootMap.get("proxies", true);
  if (proxiesNode != null) {
    addInfo(issues, "general", "proxies top-level list is preserved as-is (YAML-only)", "PROXIES_YAML_ONLY");
  }

  // 10. Section and item comment risk
  checkCommentRisks(rootMap, draft, issues, baseDraft);

  return {
    issues,
    hasBlocker: issues.some((i) => i.severity === "blocker"),
  };
}

// ─── Section-specific change reporters ───────────────────────────────────────

function reportGroupChanges(
  base: VisualDraft,
  draft: VisualDraft,
  rootMap: YAMLMap,
  issues: FidelityIssue[],
): void {
  const baseGroupMap = new Map<number, GroupItem>();
  for (const g of base.groups) baseGroupMap.set(g.originalIndex, g);

  // Detect deletions and renames
  for (const dg of draft.groups) {
    if (dg.kind !== "modeled") continue;
    const bg = baseGroupMap.get(dg.originalIndex);
    if (!bg || bg.kind !== "modeled") continue;

    if (bg.name !== dg.name) {
      // Rename detected — warn about stale references
      addWarning(issues, "groups", dg.id,
        `Group renamed from "${bg.originalName ?? bg.name}" to "${dg.name}". Rules or group members referencing the old name will not be auto-updated.`,
        "GROUP_RENAMED_STALE_REFS");
    }
  }

  // Detect modeled group deletion — if commented, warn
  for (const bg of base.groups) {
    const stillPresent = draft.groups.some((dg) => dg.originalIndex === bg.originalIndex);
    if (!stillPresent && bg.kind === "modeled") {
      // Check if the deleted item had comments
      const hasComment = itemHasCommentInSection(rootMap, "proxy-groups", bg.originalIndex);
      if (hasComment) {
        addWarning(issues, "groups", bg.id,
          `Modeled group with comments is being deleted. Comments will be lost.`,
          "DELETE_COMMENTED_ITEM");
      }
    }
  }
}

function reportProviderChanges(
  base: VisualDraft,
  draft: VisualDraft,
  rootMap: YAMLMap,
  issues: FidelityIssue[],
): void {
  const baseProvMap = new Map<number, ProviderItem>();
  for (const p of base.providers) baseProvMap.set(p.originalIndex, p);

  for (const dp of draft.providers) {
    if (dp.kind !== "modeled") continue;
    const bp = baseProvMap.get(dp.originalIndex);
    if (!bp || bp.kind !== "modeled") continue;

    if (bp.key !== dp.key) {
      addWarning(issues, "providers", dp.id,
        `Provider renamed from "${bp.originalKey ?? bp.key}" to "${dp.key}". RULE-SET rules referencing the old key will not be auto-updated.`,
        "PROVIDER_RENAMED_STALE_REFS");
    }
  }

  for (const bp of base.providers) {
    const stillPresent = draft.providers.some((dp) => dp.originalIndex === bp.originalIndex);
    if (!stillPresent && bp.kind === "modeled") {
      const hasComment = itemHasCommentInSection(rootMap, "rule-providers", bp.originalIndex);
      if (hasComment) {
        addWarning(issues, "providers", bp.id,
          "Modeled provider with comments is being deleted. Comments will be lost.",
          "DELETE_COMMENTED_ITEM");
      }
    }
  }
}

function reportRuleChanges(
  base: VisualDraft,
  draft: VisualDraft,
  rootMap: YAMLMap,
  issues: FidelityIssue[],
): void {
  const baseRuleMap = new Map<number, RuleItem>();
  for (const r of base.rules) baseRuleMap.set(r.originalIndex, r);

  // Reorder detection among modeled rules
  const baseModeledOrder = base.rules.filter((r) => r.kind === "modeled").map((r) => r.originalIndex);
  const draftModeledOrder = draft.rules.filter((r) => r.kind === "modeled").map((r) => r.originalIndex);
  if (!arraysEqual(baseModeledOrder, draftModeledOrder) && baseModeledOrder.length > 0) {
    addInfo(issues, "rules",
      "Rules have been reordered. Comments associated with each rule node will be preserved with their node.",
      "RULES_REORDERED_COMMENTS");
  }

  // Detect deletion of modeled rules — if commented, warn
  for (const br of base.rules) {
    if (br.kind !== "modeled") continue;
    const stillPresent = draft.rules.some((dr) => dr.originalIndex === br.originalIndex);
    if (!stillPresent) {
      const hasComment = itemHasCommentInSection(rootMap, "rules", br.originalIndex);
      if (hasComment) {
        addWarning(issues, "rules", br.id,
          "Modeled rule with comments is being deleted. Comments will be lost.",
          "DELETE_COMMENTED_ITEM");
      }
    }
  }
}

// ─── Comment risk checks ─────────────────────────────────────────────────────

function checkCommentRisks(
  rootMap: YAMLMap,
  draft: VisualDraft,
  issues: FidelityIssue[],
  baseDraft: VisualDraft | null,
): void {
  // Check section-key comments
  for (const key of ["proxy-groups", "rule-providers", "rules"]) {
    const pair = rootMap.items.find((p) => {
      const k = isScalar(p.key) ? String(p.key) : null;
      return k === key;
    });
    if (pair && pair.key && typeof pair.key === "object" && "commentBefore" in (pair.key as Record<string, unknown>)) {
      const comment = (pair.key as Record<string, unknown>).commentBefore;
      if (comment) {
        addInfo(issues, keyToSection(key),
          `${key} key has a preceding comment; will be preserved with its node`,
          "SECTION_KEY_COMMENT");
      }
    }
  }

  // Check for comment risk on modeled items (only if reorder detected)
  // REORDER_COMMENT_RISK is a warning — inter-item comment association may shift
  const baseOrder = baseDraft?.groups.map((g) => g.originalIndex) ?? [];
  const draftOrder = draft.groups.map((g) => g.originalIndex);
  if (!arraysEqual(baseOrder, draftOrder) && hasAnyComment(rootMap, "proxy-groups")) {
    addWarning(issues, "groups", undefined,
      "Groups have been reordered; comments are preserved with their original nodes but inter-item comment association may shift",
      "REORDER_COMMENT_RISK");
  }

  const baseRuleOrder = baseDraft?.rules.map((r) => r.originalIndex) ?? [];
  const draftRuleOrder = draft.rules.map((r) => r.originalIndex);
  if (!arraysEqual(baseRuleOrder, draftRuleOrder) && hasAnyComment(rootMap, "rules")) {
    addWarning(issues, "rules", undefined,
      "Rules have been reordered; comments are preserved with their original nodes but inter-item comment association may shift",
      "REORDER_COMMENT_RISK");
  }
}

function hasAnyComment(rootMap: YAMLMap, sectionKey: string): boolean {
  const node = rootMap.get(sectionKey, true);
  if (!isSeq(node)) return false;
  const seq = node as YAMLSeq;
  for (const item of seq.items) {
    if (item && typeof item === "object" && "commentBefore" in (item as Record<string, unknown>) && (item as Record<string, unknown>).commentBefore) {
      return true;
    }
  }
  return false;
}

function itemHasCommentInSection(rootMap: YAMLMap, sectionKey: string, index: number): boolean {
  const node = rootMap.get(sectionKey, true);
  if (!isSeq(node)) return false;
  const seq = node as YAMLSeq;
  // Section-level comment (between key and first item) is associated with index 0
  if (index === 0 && seq.commentBefore) return true;
  if (index < 0 || index >= seq.items.length) return false;
  const item = seq.items[index];
  return !!(item && typeof item === "object" && "commentBefore" in (item as Record<string, unknown>) && (item as Record<string, unknown>).commentBefore);
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function sectionLabel(sec: string): string {
  switch (sec) {
    case "groups": return "proxy-groups";
    case "providers": return "rule-providers";
    case "rules": return "rules";
    default: return sec;
  }
}

function keyToSection(key: string): "groups" | "providers" | "rules" | "general" {
  if (key === "proxy-groups") return "groups";
  if (key === "rule-providers") return "providers";
  if (key === "rules") return "rules";
  return "general";
}

function arraysEqual(a: number[], b: number[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

function blocker(issues: FidelityIssue[], section: string, message: string, code: string): FidelityReport {
  issues.push({ severity: "blocker", section: section as FidelityIssue["section"], message, code });
  return { issues, hasBlocker: true };
}

function addInfo(issues: FidelityIssue[], section: string, message: string, code: string, itemId?: string): void {
  issues.push({ severity: "info", section: section as FidelityIssue["section"], itemId, message, code });
}

function addWarning(issues: FidelityIssue[], section: string, itemId: string | undefined, message: string, code: string): void {
  issues.push({ severity: "warning", section: section as FidelityIssue["section"], itemId, message, code });
}

// Keep original export for backward compat (forwarding to new plan API)
export function buildFidelityReport(baseYaml: string, draft: VisualDraft): FidelityReport {
  return planRuleProfileVisualApply(baseYaml, draft).fidelity;
}
