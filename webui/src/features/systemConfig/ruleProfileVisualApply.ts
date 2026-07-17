/**
 * ruleProfileVisualApply.ts — Apply a VisualDraft to base YAML via AST operations.
 *
 * Uses yaml@2.9 parseDocument + clone + in-place YAMLMap/YAMLSeq manipulation.
 * Only modifies proxy-groups, rule-providers, rules top-level pairs.
 *
 * Key guarantees:
 * - Blocked sections refuse to apply.
 * - Missing sections are created as empty seq/map via doc.createPair.
 * - Provider key rename preserves pair position, comments, and unknown value fields.
 * - Modeled groups independently preserve both proxies and include-all-proxies.
 * - Unknown pairs on modeled items are preserved during edit.
 * - Raw items are immutable: count and order are enforced against base.
 * - Semantically no-op drafts return the original YAML byte-for-byte.
 * - After editing, output is deterministic (same input + same edits = same output).
 * - Stringified with lineWidth: 0.
 */

import {
  isMap,
  isScalar,
  isSeq,
  parseDocument,
  type Document,
  type Pair,
  type Scalar,
  type YAMLMap,
  type YAMLSeq,
} from "yaml";
import {
  computeFingerprint,
  draftsEqual,
  type GroupItem,
  type ModeledGroup,
  type ModeledProvider,
  type ModeledRule,
  type ProviderItem,
  type RuleItem,
  type VisualApplyPlan,
  type VisualApplyResult,
  type VisualDraft,
  type VisualError,
} from "./ruleProfileVisualModel";
import { parseRuleProfileVisualDraft } from "./ruleProfileVisualParse";

// ─── Public API ──────────────────────────────────────────────────────────────

export function applyRuleProfileVisualDraft(
  baseYaml: string,
  draft: VisualDraft,
  plan?: VisualApplyPlan,
): VisualApplyResult {
  const errors: VisualError[] = [];

  // 1. Always check fingerprint regardless of plan
  const baseFp = computeFingerprint(baseYaml);
  if (baseFp !== draft.sourceFingerprint) {
    return {
      ok: false,
      yaml: null,
      errors: [{
        message: "Visual draft is stale; base YAML has changed. Reload from YAML first.",
        code: "STALE_DRAFT",
      }],
    };
  }

  // 2. Check plan if provided
  if (plan) {
    if (!plan.canApply) {
      return {
        ok: false,
        yaml: null,
        errors: [{ message: "Apply plan indicates this draft cannot be applied", code: "PLAN_REJECTED" }],
      };
    }
  }

  // 3. Check blocked sections — refuse if any section is blocked
  if (draft.blockedSections.length > 0) {
    const sectionNames = draft.blockedSections.join(", ");
    return {
      ok: false,
      yaml: null,
      errors: [{
        message: `Cannot apply: blocked section(s): ${sectionNames}. Resolve blockers first.`,
        code: "BLOCKED_SECTIONS",
      }],
    };
  }

  // 4. Parse base
  let doc: Document;
  try {
    doc = parseDocument(baseYaml, { prettyErrors: true, uniqueKeys: false }) as Document;
  } catch (err) {
    return {
      ok: false,
      yaml: null,
      errors: [{ message: String(err), code: "PARSE_ERROR" }],
    };
  }

  if (doc.errors.length > 0) {
    return {
      ok: false,
      yaml: null,
      errors: doc.errors.map((e) => ({
        message: e.message,
        code: "YAML_PARSE_ERROR",
      })),
    };
  }

  const root = doc.contents;
  if (!root || !isMap(root)) {
    return {
      ok: false,
      yaml: null,
      errors: [{ message: "Root is not a mapping", code: "ROOT_NOT_MAP" }],
    };
  }

  // 5. No-op detection: reparse base and compare semantically
  const baseDraftResult = parseRuleProfileVisualDraft(baseYaml);
  if (baseDraftResult.ok && baseDraftResult.draft) {
    if (draftsEqual(draft, baseDraftResult.draft)) {
      return { ok: true, yaml: baseYaml, errors: [] };
    }
  }

  // 6. Clone document for mutation
  const cloned = doc.clone();

  // 7. Raw item enforcement: check positions and integrity against base parse
  if (!baseDraftResult.ok || !baseDraftResult.draft) {
    return {
      ok: false,
      yaml: null,
      errors: [{ message: "Cannot enforce raw item integrity: base draft parse failed", code: "BASE_PARSE_ERROR" }],
    };
  }
  const rawEnforcement = checkRawItemIntegrity(baseDraftResult.draft, draft);
  if (!rawEnforcement.ok) {
    return {
      ok: false,
      yaml: null,
      errors: rawEnforcement.errors,
    };
  }

  // 8. Apply each section
  try {
    applyGroups(cloned, draft.groups);
    applyProviders(cloned, draft.providers);
    applyRules(cloned, draft.rules);
  } catch (err) {
    return {
      ok: false,
      yaml: null,
      errors: [...errors, { message: String(err), code: "APPLY_ERROR" }],
    };
  }

  if (errors.length > 0) {
    return {
      ok: false,
      yaml: null,
      errors,
    };
  }

  // 9. Stringify (catch exceptions)
  let result: string;
  try {
    result = cloned.toString({ lineWidth: 0 });
  } catch (err) {
    return {
      ok: false,
      yaml: null,
      errors: [{ message: `Stringify error: ${String(err)}`, code: "STRINGIFY_ERROR" }],
    };
  }
  return { ok: true, yaml: result, errors: [] };
}

// ─── Raw item enforcement ────────────────────────────────────────────────────

interface RawEnforcementResult {
  ok: boolean;
  errors: VisualError[];
}

function checkRawItemIntegrity(baseDraft: VisualDraft, draft: VisualDraft): RawEnforcementResult {
  const errors: VisualError[] = [];

  // Groups: each raw item's array position must equal its originalIndex
  for (let i = 0; i < draft.groups.length; i++) {
    const item = draft.groups[i];
    if (item.kind === "raw") {
      if (i !== item.originalIndex) {
        errors.push({
          message: `Raw group "${item.label}" moved from position ${item.originalIndex} to ${i}. Raw items cannot be reordered.`,
          code: "RAW_GROUP_MUTATION",
        });
      }
    }
  }
  // Also check that original set of raw items hasn't changed (deletion/addition)
  if (!rawSetUnchanged(baseDraft.groups, draft.groups)) {
    errors.push({
      message: "Raw groups have been added or deleted.",
      code: "RAW_GROUP_MUTATION",
    });
  }

  // Providers: same positional check
  for (let i = 0; i < draft.providers.length; i++) {
    const item = draft.providers[i];
    if (item.kind === "raw") {
      if (i !== item.originalIndex) {
        errors.push({
          message: `Raw provider "${item.label}" moved from position ${item.originalIndex} to ${i}. Raw providers cannot be reordered.`,
          code: "RAW_PROVIDER_MUTATION",
        });
      }
    }
  }
  if (!rawSetUnchanged(baseDraft.providers, draft.providers)) {
    errors.push({
      message: "Raw providers have been added or deleted.",
      code: "RAW_PROVIDER_MUTATION",
    });
  }

  // Rules: same positional check
  for (let i = 0; i < draft.rules.length; i++) {
    const item = draft.rules[i];
    if (item.kind === "raw") {
      if (i !== item.originalIndex) {
        errors.push({
          message: `Raw rule "${item.label}" moved from position ${item.originalIndex} to ${i}. Raw rules cannot be reordered.`,
          code: "RAW_RULE_MUTATION",
        });
      }
    }
  }
  if (!rawSetUnchanged(baseDraft.rules, draft.rules)) {
    errors.push({
      message: "Raw rules have been added or deleted.",
      code: "RAW_RULE_MUTATION",
    });
  }

  return { ok: errors.length === 0, errors };
}

/** Check that the set of raw originalIndex values is identical between base and draft. */
function rawSetUnchanged(
  base: Array<{ kind: string; originalIndex: number }>,
  draft: Array<{ kind: string; originalIndex: number }>,
): boolean {
  const baseSet = new Set(base.filter((x) => x.kind === "raw").map((x) => x.originalIndex));
  const draftSet = new Set(draft.filter((x) => x.kind === "raw").map((x) => x.originalIndex));
  if (baseSet.size !== draftSet.size) return false;
  for (const idx of baseSet) {
    if (!draftSet.has(idx)) return false;
  }
  return true;
}

// ─── Groups apply ────────────────────────────────────────────────────────────

function applyGroups(
  doc: Document,
  groups: GroupItem[],
): void {
  const rootMap = doc.contents as YAMLMap;
  let seq = rootMap.get("proxy-groups", true) as YAMLSeq | undefined;

  // Create section if missing (must have items, otherwise skip)
  if (!isSeq(seq) && groups.length > 0) {
    seq = doc.createNode([], { aliasDuplicateObjects: false }) as YAMLSeq;
    const pair = doc.createPair("proxy-groups", seq);
    // Insert after "proxies" if present, else at beginning
    insertPair(rootMap, pair, "proxies");
  }

  if (!isSeq(seq)) return; // nothing to do

  const allPairs = [...(seq as YAMLSeq).items] as YAMLMap[];
  const newItems: unknown[] = [];

  for (const item of groups) {
    if (item.kind === "raw") {
      if (item.originalIndex >= 0 && item.originalIndex < allPairs.length) {
        newItems.push(allPairs[item.originalIndex]);
      }
    } else {
      const existing = item.originalIndex >= 0 && item.originalIndex < allPairs.length
        ? allPairs[item.originalIndex]
        : undefined;
      if (existing) {
        applyGroupEdit(existing, item);
        newItems.push(existing);
      } else {
        // Create new map node
        const obj: Record<string, unknown> = {
          name: item.name,
          type: item.type,
        };
        if (item.proxies != null) obj.proxies = item.proxies;
        if (item.includeAllProxies) obj["include-all-proxies"] = true;
        if (item.filter != null) obj.filter = item.filter;
        if (item.type === "url-test") {
          if (item.url != null) obj.url = item.url;
          if (item.interval != null) obj.interval = item.interval;
          if (item.timeout != null) obj.timeout = item.timeout;
          if (item.tolerance != null) obj.tolerance = item.tolerance;
        }
        const node = doc.createNode(obj, { aliasDuplicateObjects: false });
        newItems.push(node);
      }
    }
  }

  (seq as YAMLSeq).items = newItems;
}

function applyGroupEdit(map: YAMLMap, model: ModeledGroup): void {
  // Update known fields independently; leave unknown pairs untouched
  setScalar(map, "name", model.name);
  setScalar(map, "type", model.type);

  // proxies and include-all-proxies are independent
  if (model.proxies != null) {
    map.set("proxies", model.proxies);
  } else if (map.get("proxies", true) != null) {
    // Only delete if the key exists
    map.delete("proxies");
  }

  if (model.includeAllProxies) {
    map.set("include-all-proxies", true);
  } else if (map.get("include-all-proxies", true) != null) {
    map.delete("include-all-proxies");
  }

  if (model.filter != null) {
    setScalar(map, "filter", model.filter);
  } else if (map.get("filter", true) != null) {
    map.delete("filter");
  }

  if (model.type === "url-test") {
    if (model.url != null) setScalar(map, "url", model.url);
    else if (map.get("url", true) != null) map.delete("url");
    if (model.interval != null) setScalar(map, "interval", model.interval);
    else if (map.get("interval", true) != null) map.delete("interval");
    if (model.timeout != null) setScalar(map, "timeout", model.timeout);
    else if (map.get("timeout", true) != null) map.delete("timeout");
    if (model.tolerance != null) setScalar(map, "tolerance", model.tolerance);
    else if (map.get("tolerance", true) != null) map.delete("tolerance");
  } else {
    // Remove url-test specific fields if type changed to select
    if (map.get("url", true) != null) map.delete("url");
    if (map.get("interval", true) != null) map.delete("interval");
    if (map.get("timeout", true) != null) map.delete("timeout");
    if (map.get("tolerance", true) != null) map.delete("tolerance");
  }
}

// ─── Providers apply ─────────────────────────────────────────────────────────

function applyProviders(
  doc: Document,
  providers: ProviderItem[],
): void {
  const rootMap = doc.contents as YAMLMap;
  let map = rootMap.get("rule-providers", true) as YAMLMap | undefined;

  // Create section if missing
  if (!isMap(map) && providers.length > 0) {
    map = doc.createNode({}, { aliasDuplicateObjects: false }) as YAMLMap;
    const pair = doc.createPair("rule-providers", map);
    insertPair(rootMap, pair, "proxy-groups");
  }

  if (!isMap(map)) return;

  const allPairs = [...(map as YAMLMap).items] as Pair<unknown, unknown>[];
  const newPairs: Pair<unknown, unknown>[] = [];

  for (const item of providers) {
    if (item.kind === "raw") {
      if (item.originalIndex >= 0 && item.originalIndex < allPairs.length) {
        newPairs.push(allPairs[item.originalIndex]);
      }
    } else {
      const existingPair = item.originalIndex >= 0 && item.originalIndex < allPairs.length
        ? allPairs[item.originalIndex]
        : undefined;
      if (existingPair && isMap(existingPair.value)) {
        // Handle key rename: update the key on the pair while preserving position
        const valueMap = existingPair.value as YAMLMap;
        applyProviderEdit(valueMap, item, existingPair, item.originalKey);
        newPairs.push(existingPair);
      } else {
        // Create new pair
        const value: Record<string, unknown> = {
          type: "http",
          behavior: "classical",
          format: "text",
          url: item.url,
          interval: item.interval,
        };
        const pair = doc.createPair(item.key, value);
        newPairs.push(pair);
      }
    }
  }

  (map as YAMLMap).items = newPairs;
}

function applyProviderEdit(
  valueMap: YAMLMap,
  model: ModeledProvider,
  pair: { key: unknown },
  originalKey: string | null,
): void {
  // Handle key rename — update pair key preserving position and metadata
  if (originalKey != null && model.key !== originalKey) {
    if (isScalar(pair.key)) {
      (pair.key as Scalar).value = model.key;
    }
  }

  // Update known fields in value map
  setScalar(valueMap, "url", model.url);
  setScalar(valueMap, "interval", model.interval);

  // Ensure required fields are present but don't delete unknown pairs
  if (valueMap.get("type", true) == null) setScalar(valueMap, "type", "http");
  if (valueMap.get("behavior", true) == null) setScalar(valueMap, "behavior", "classical");
  if (valueMap.get("format", true) == null) setScalar(valueMap, "format", "text");
}

// ─── Rules apply ─────────────────────────────────────────────────────────────

function applyRules(
  doc: Document,
  rules: RuleItem[],
): void {
  const rootMap = doc.contents as YAMLMap;
  let seq = rootMap.get("rules", true) as YAMLSeq | undefined;

  // Create section if missing
  if (!isSeq(seq) && rules.length > 0) {
    seq = doc.createNode([], { aliasDuplicateObjects: false }) as YAMLSeq;
    const pair = doc.createPair("rules", seq);
    insertPair(rootMap, pair, "rule-providers");
  }

  if (!isSeq(seq)) return;

  const oldItems = [...(seq as YAMLSeq).items];
  const newItems: unknown[] = [];

  for (const item of rules) {
    if (item.kind === "raw") {
      if (item.originalIndex >= 0 && item.originalIndex < oldItems.length) {
        newItems.push(oldItems[item.originalIndex]);
      }
    } else {
      const text = buildRuleText(item);
      const existing = item.originalIndex >= 0 && item.originalIndex < oldItems.length
        ? oldItems[item.originalIndex]
        : undefined;
      if (existing && isScalar(existing)) {
        // Update scalar in-place
        (existing as Scalar).value = text;
        newItems.push(existing);
      } else {
        // Create new rule
        const node = doc.createNode(text, { aliasDuplicateObjects: false });
        newItems.push(node);
      }
    }
  }

  (seq as YAMLSeq).items = newItems;
}

function buildRuleText(rule: ModeledRule): string {
  switch (rule.ruleType) {
    case "RULE-SET":
      return rule.noResolve
        ? `RULE-SET,${rule.provider},${rule.policy},no-resolve`
        : `RULE-SET,${rule.provider},${rule.policy}`;
    case "GEOIP":
      return rule.noResolve
        ? `GEOIP,${rule.geoipCode},${rule.policy},no-resolve`
        : `GEOIP,${rule.geoipCode},${rule.policy}`;
    case "MATCH":
      return `MATCH,${rule.policy}`;
    default:
      return rule.rawText;
  }
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function setScalar(map: YAMLMap, key: string, value: string | number | boolean): void {
  // Find existing pair to preserve position and scalar metadata (comments, anchor)
  const existingPair = map.items.find((p) => {
    const k = isScalar(p.key) ? String(p.key) : null;
    return k === key;
  });

  if (existingPair) {
    if (isScalar(existingPair.value)) {
      // Mutate existing Scalar in place — preserves position, comments, anchor
      (existingPair.value as Scalar).value = value;
    } else {
      // Replace non-scalar value — keep the pair at its current position
      existingPair.value = value;
    }
  } else {
    // Key doesn't exist — append at end
    map.set(key, value);
  }
}

/**
 * Insert a new pair into rootMap after the reference key, or at the beginning.
 */
function insertPair(rootMap: YAMLMap, newPair: Pair<unknown, unknown>, afterKey: string): void {
  const insertAfter = rootMap.items.findIndex((p) => {
    const k = isScalar(p.key) ? String(p.key) : null;
    return k === afterKey;
  });

  if (insertAfter >= 0) {
    rootMap.items.splice(insertAfter + 1, 0, newPair);
  } else {
    // Insert at beginning
    rootMap.items.unshift(newPair);
  }
}
