/**
 * ruleProfileVisualParse.ts — Parse & classify YAML into a shared VisualDraft.
 *
 * Uses yaml@2.9 parseDocument for full AST access.
 * Classifies proxy-groups / rule-providers / rules items as modeled or raw.
 * Detects anchor/alias in any visual section and blocks that section.
 * Returns blockers for present sections with wrong YAML types.
 */

import {
  isAlias,
  isMap,
  isScalar,
  isSeq,
  parseDocument,
  type Scalar,
  type YAMLMap,
  type YAMLSeq,
} from "yaml";
import {
  computeFingerprint,
  itemId,
  type GroupItem,
  type ModeledGroup,
  type ModeledProvider,
  type ModeledRule,
  type ProviderItem,
  type RawGroup,
  type RawProvider,
  type RawRule,
  type RuleItem,
  type VisualDraft,
  type VisualParseResult,
} from "./ruleProfileVisualModel";

// ─── Public API ──────────────────────────────────────────────────────────────

/**
 * Parse a YAML string into a VisualDraft with all items classified.
 *
 * Returns errors for parse failures or missing root mapping.
 * A non-empty YAML with no modeled sections still produces a valid draft
 * with all items marked as raw.
 */
export function parseRuleProfileVisualDraft(source: string): VisualParseResult {
  const trimmed = source.trim();
  if (!trimmed) {
    return {
      ok: true,
      draft: emptyDraft(source),
      errors: [],
    };
  }

  let doc;
  try {
    doc = parseDocument(source, {
      prettyErrors: true,
      uniqueKeys: false,
    });
  } catch (err) {
    return {
      ok: false,
      draft: null,
      errors: [{ message: String(err), code: "PARSE_ERROR" }],
    };
  }

  if (doc.errors.length > 0) {
    return {
      ok: false,
      draft: null,
      errors: doc.errors.map((e) => ({
        message: e.message,
        code: "YAML_PARSE_ERROR",
      })),
    };
  }

  const root = doc.contents;
  if (root == null) {
    // empty document → valid empty draft
    return {
      ok: true,
      draft: emptyDraft(source),
      errors: [],
    };
  }
  if (!isMap(root)) {
    return {
      ok: false,
      draft: null,
      errors: [{ message: "Root document must be a YAML mapping", code: "ROOT_NOT_MAP" }],
    };
  }

  const rootMap = root as YAMLMap;
  const blockedSections: Array<"groups" | "providers" | "rules"> = [];

  // ── Check section node presence & type ─────────────────────────────────────
  const groupsNode = rootMap.get("proxy-groups", true);
  const providersNode = rootMap.get("rule-providers", true);
  const rulesNode = rootMap.get("rules", true);

  // proxy-groups: must be sequence if present
  if (groupsNode != null && !isSeq(groupsNode)) {
    blockedSections.push("groups");
  }
  // rule-providers: must be mapping if present
  if (providersNode != null && !isMap(providersNode)) {
    blockedSections.push("providers");
  }
  // rules: must be sequence if present
  if (rulesNode != null && !isSeq(rulesNode)) {
    blockedSections.push("rules");
  }

  // ── Detect anchors/aliases in each section ─────────────────────────────────
  if (!blockedSections.includes("groups") && seqHasAnchorOrAlias(groupsNode as YAMLSeq | undefined)) {
    blockedSections.push("groups");
  }
  if (!blockedSections.includes("providers") && mapHasAnchorOrAlias(providersNode as YAMLMap | undefined)) {
    blockedSections.push("providers");
  }
  if (!blockedSections.includes("rules") && seqHasAnchorOrAlias(rulesNode as YAMLSeq | undefined)) {
    blockedSections.push("rules");
  }

  // ── Parse groups ───────────────────────────────────────────────────────────
  const groups: GroupItem[] = [];
  if (isSeq(groupsNode)) {
    const seq = groupsNode as YAMLSeq;
    for (let i = 0; i < seq.items.length; i++) {
      const item = seq.items[i];
      if (isMap(item)) {
        const modeled = classifyGroupMap(item as YAMLMap, i);
        if (modeled) {
          groups.push(modeled);
        } else {
          groups.push(rawGroupFromNode(item, i));
        }
      } else {
        groups.push(rawGroupFromNode(item, i));
      }
    }
  }

  // ── Parse providers ────────────────────────────────────────────────────────
  const providers: ProviderItem[] = [];
  if (isMap(providersNode)) {
    const map = providersNode as YAMLMap;
    let idx = 0;
    for (const pair of map.items) {
      if (isScalar(pair.key) && isMap(pair.value)) {
        const key = String((pair.key as Scalar).value);
        const modeled = classifyProviderMap(key, pair.value as YAMLMap, idx);
        if (modeled) {
          providers.push(modeled);
        } else {
          providers.push(rawProviderFromPair(pair, idx));
        }
      } else {
        providers.push(rawProviderFromPair(pair, idx));
      }
      idx++;
    }
  }

  // ── Parse rules ────────────────────────────────────────────────────────────
  const rules: RuleItem[] = [];
  if (isSeq(rulesNode)) {
    const seq = rulesNode as YAMLSeq;
    for (let i = 0; i < seq.items.length; i++) {
      const item = seq.items[i];
      const text = scalarText(item);
      if (text != null) {
        const modeled = classifyRuleText(text, i);
        if (modeled) {
          rules.push(modeled);
        } else {
          rules.push(rawRuleFromText(text, i));
        }
      } else {
        rules.push(rawRuleFromNode(item, i));
      }
    }
  }

  const fingerprint = computeFingerprint(source);

  const draft: VisualDraft = {
    sourceFingerprint: fingerprint,
    groups,
    providers,
    rules,
    blockedSections,
  };

  return { ok: true, draft, errors: [] };
}

// ─── Classification helpers ───────────────────────────────────────────────────

/** Known group type strings we can model. */
const MODELED_GROUP_TYPES = new Set(["select", "url-test"]);

/** Known provider fields for modeled providers. */
const MODELED_PROVIDER_FIELDS = new Set(["type", "behavior", "format", "url", "interval"]);

function classifyGroupMap(map: YAMLMap, index: number): ModeledGroup | null {
  const typeScalar = map.get("type", true) as Scalar | undefined;
  const typeVal = scalarText(typeScalar);
  if (!typeVal || !MODELED_GROUP_TYPES.has(typeVal)) {
    return null;
  }

  const nameScalar = map.get("name", true) as Scalar | undefined;
  const name = scalarText(nameScalar) ?? "";

  // Count unknown keys (everything except the fields we know about for this type)
  const knownKeys = new Set([
    "name",
    "type",
    "proxies",
    "include-all-proxies",
    "filter",
  ]);
  if (typeVal === "url-test") {
    knownKeys.add("url");
    knownKeys.add("interval");
    knownKeys.add("timeout");
    knownKeys.add("tolerance");
  }
  let unknownCount = 0;
  for (const pair of map.items) {
    const k = scalarText(pair.key);
    if (k != null && !knownKeys.has(k)) {
      unknownCount++;
    }
  }

  const proxiesRaw = map.get("proxies", true) as YAMLSeq | undefined;
  const proxies: string[] | null = isSeq(proxiesRaw)
    ? proxiesRaw.items.map((p) => scalarText(p) ?? String(p)).filter(Boolean)
    : null;

  const includeAllProxies = !!map.get("include-all-proxies");
  const filter = scalarText(map.get("filter", true) as Scalar | undefined) ?? null;

  let url: string | null = null;
  let interval: number | null = null;
  let timeout: number | null = null;
  let tolerance: number | null = null;

  if (typeVal === "url-test") {
    url = scalarText(map.get("url", true) as Scalar | undefined) ?? null;
    interval = numberOrNull(map.get("interval", true));
    timeout = numberOrNull(map.get("timeout", true));
    tolerance = numberOrNull(map.get("tolerance", true));
  }

  return {
    kind: "modeled",
    id: itemId("g", index),
    originalIndex: index,
    originalName: name,
    name,
    type: typeVal as "select" | "url-test",
    proxies,
    includeAllProxies,
    filter,
    url,
    interval,
    timeout,
    tolerance,
    unknownKeyCount: unknownCount,
  };
}

function classifyProviderMap(key: string, map: YAMLMap, index: number): ModeledProvider | null {
  const typeVal = scalarText(map.get("type", true) as Scalar | undefined);
  const behaviorVal = scalarText(map.get("behavior", true) as Scalar | undefined);
  const formatVal = scalarText(map.get("format", true) as Scalar | undefined);

  if (typeVal !== "http" || behaviorVal !== "classical" || formatVal !== "text") {
    return null;
  }

  // Count unknown keys
  let unknownCount = 0;
  for (const pair of map.items) {
    const k = scalarText(pair.key);
    if (k != null && !MODELED_PROVIDER_FIELDS.has(k)) {
      unknownCount++;
    }
  }

  const url = scalarText(map.get("url", true) as Scalar | undefined) ?? "";
  const interval = numberOrNull(map.get("interval", true)) ?? 86400;

  return {
    kind: "modeled",
    id: itemId("p", index),
    originalIndex: index,
    originalKey: key,
    key,
    url,
    interval,
    unknownKeyCount: unknownCount,
  };
}

function classifyRuleText(text: string, index: number): ModeledRule | null {
  const trimmed = text.trim();
  const parts = trimmed.split(",");

  // RULE-SET: exactly 3 or 4 parts, 4th must be "no-resolve"
  if (parts[0] === "RULE-SET") {
    if (parts.length === 3) {
      return {
        kind: "modeled",
        id: itemId("r", index),
        originalIndex: index,
        ruleType: "RULE-SET",
        provider: parts[1],
        policy: parts[2],
        noResolve: false,
        rawText: trimmed,
      };
    }
    if (parts.length === 4 && parts[3].toLowerCase() === "no-resolve") {
      return {
        kind: "modeled",
        id: itemId("r", index),
        originalIndex: index,
        ruleType: "RULE-SET",
        provider: parts[1],
        policy: parts[2],
        noResolve: true,
        rawText: trimmed,
      };
    }
    return null;
  }

  // GEOIP: exactly 3 or 4 parts, 4th must be "no-resolve"
  if (parts[0] === "GEOIP") {
    if (parts.length === 3) {
      return {
        kind: "modeled",
        id: itemId("r", index),
        originalIndex: index,
        ruleType: "GEOIP",
        geoipCode: parts[1],
        policy: parts[2],
        noResolve: false,
        rawText: trimmed,
      };
    }
    if (parts.length === 4 && parts[3].toLowerCase() === "no-resolve") {
      return {
        kind: "modeled",
        id: itemId("r", index),
        originalIndex: index,
        ruleType: "GEOIP",
        geoipCode: parts[1],
        policy: parts[2],
        noResolve: true,
        rawText: trimmed,
      };
    }
    return null;
  }

  // MATCH: exactly 2 parts
  if (parts[0] === "MATCH") {
    if (parts.length === 2) {
      return {
        kind: "modeled",
        id: itemId("r", index),
        originalIndex: index,
        ruleType: "MATCH",
        policy: parts[1],
        noResolve: false,
        rawText: trimmed,
      };
    }
    return null;
  }

  return null;
}

// ─── Raw item factories with metadata ─────────────────────────────────────────

function rawGroupFromNode(node: unknown, index: number): RawGroup {
  let label = "";
  let sourceType = "";
  const text: string | null = null;

  if (isMap(node)) {
    const map = node as YAMLMap;
    const name = scalarText(map.get("name", true) as Scalar | undefined) ?? "";
    const typeV = scalarText(map.get("type", true) as Scalar | undefined) ?? "unknown";
    label = name || `unnamed-${index}`;
    sourceType = typeV;
  } else if (isScalar(node)) {
    label = String((node as Scalar).value ?? "");
    sourceType = "scalar";
  } else {
    label = `complex-${index}`;
    sourceType = "complex";
  }

  return {
    kind: "raw",
    id: itemId("g", index),
    originalIndex: index,
    label,
    sourceType,
    reason: `Unsupported group type "${sourceType}"`,
    text,
  };
}

function rawProviderFromPair(pair: { key: unknown; value: unknown }, index: number): RawProvider {
  let label = "";
  let sourceType = "";
  const text: string | null = null;

  const key = isScalar(pair.key) ? String((pair.key as Scalar).value) : "";
  label = key || `unnamed-${index}`;

  if (isMap(pair.value)) {
    const valMap = pair.value as YAMLMap;
    const typeV = scalarText(valMap.get("type", true) as Scalar | undefined) ?? "unknown";
    const behaviorV = scalarText(valMap.get("behavior", true) as Scalar | undefined) ?? "";
    const formatV = scalarText(valMap.get("format", true) as Scalar | undefined) ?? "";
    sourceType = behaviorV || formatV ? `${typeV}+${behaviorV}+${formatV}` : typeV;
  } else {
    sourceType = "non-map";
  }

  return {
    kind: "raw",
    id: itemId("p", index),
    originalIndex: index,
    label,
    sourceType,
    reason: `Unsupported provider type "${sourceType}"`,
    text,
  };
}

function rawRuleFromText(text: string, index: number): RawRule {
  const parts = text.split(",");
  const typeLabel = parts[0] ?? "UNKNOWN";
  const truncated = text.length > 60 ? text.slice(0, 57) + "..." : text;
  return {
    kind: "raw",
    id: itemId("r", index),
    originalIndex: index,
    label: truncated,
    sourceType: typeLabel,
    reason: `Unsupported rule type "${typeLabel}"`,
    text,
  };
}

function rawRuleFromNode(node: unknown, index: number): RawRule {
  const text = isScalar(node) ? String((node as Scalar).value ?? "") : null;
  const label = text
    ? (text.length > 60 ? text.slice(0, 57) + "..." : text)
    : `complex-${index}`;
  return {
    kind: "raw",
    id: itemId("r", index),
    originalIndex: index,
    label,
    sourceType: text ? (text.split(",")[0] ?? "UNKNOWN") : "complex",
    reason: text ? `Unsupported rule type "${text.split(",")[0] ?? "UNKNOWN"}"` : "Complex non-scalar rule node",
    text,
  };
}

// ─── Anchor/alias detection ───────────────────────────────────────────────────

/** Check if a YAMLSeq or its items contain any anchor or alias. */
function seqHasAnchorOrAlias(node: YAMLSeq | undefined): boolean {
  if (!isSeq(node)) return false;
  const seq = node as YAMLSeq;
  for (const item of seq.items) {
    if (hasAnchorOrAliasDeep(item)) return true;
  }
  return false;
}

/** Check if a YAMLMap or its values contain any anchor or alias. */
function mapHasAnchorOrAlias(node: YAMLMap | undefined): boolean {
  if (!isMap(node)) return false;
  const map = node as YAMLMap;
  for (const pair of map.items) {
    if (nodeHasAnchor(pair.key)) return true;
    if (pair.value != null && hasAnchorOrAliasDeep(pair.value)) return true;
  }
  return false;
}

/** Recursively check a node for anchor or alias. */
function hasAnchorOrAliasDeep(node: unknown): boolean {
  if (!node || typeof node !== "object") return false;
  if (nodeHasAnchor(node)) return true;
  if (isAlias(node)) return true;
  if (isMap(node)) {
    const map = node as YAMLMap;
    for (const pair of map.items) {
      if (hasAnchorOrAliasDeep(pair.key)) return true;
      if (pair.value != null && hasAnchorOrAliasDeep(pair.value)) return true;
    }
  }
  if (isSeq(node)) {
    const seq = node as YAMLSeq;
    for (const item of seq.items) {
      if (hasAnchorOrAliasDeep(item)) return true;
    }
  }
  return false;
}

function nodeHasAnchor(node: unknown): boolean {
  if (!node || typeof node !== "object") return false;
  const n = node as Record<string, unknown>;
  if (n.anchor != null && n.anchor !== "") return true;
  return false;
}

// ─── Scalar helpers ───────────────────────────────────────────────────────────

function scalarText(node: Scalar | unknown): string | null {
  if (isScalar(node)) {
    const s = node as Scalar;
    if (s.value == null) return null;
    return String(s.value);
  }
  return null;
}

function numberOrNull(node: Scalar | unknown): number | null {
  if (isScalar(node)) {
    const s = node as Scalar;
    if (typeof s.value === "number") return s.value;
    if (typeof s.value === "string") {
      const n = Number(s.value);
      if (!Number.isNaN(n)) return n;
    }
  }
  return null;
}

// ─── Empty draft ─────────────────────────────────────────────────────────────

function emptyDraft(source: string): VisualDraft {
  return {
    sourceFingerprint: computeFingerprint(source),
    groups: [],
    providers: [],
    rules: [],
    blockedSections: [],
  };
}
