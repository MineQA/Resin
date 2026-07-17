import { isMap, isSeq, parseDocument, type YAMLMap, type YAMLSeq } from "yaml";

/**
 * Bounded live summary of a Mihomo Rule Profile template.
 * Counts top-level groups/providers/rules and checks MATCH placement only.
 * Does not replace server-side save validation.
 */
export type YamlTemplateWarning = {
  /** Chinese source key for i18n (`t(key, params)`). */
  key: string;
  params?: Record<string, string | number>;
};

export type YamlTemplateSummary = {
  /** True when the document parsed as a mapping (even if sparse). */
  ok: boolean;
  groupCount: number | null;
  providerCount: number | null;
  ruleCount: number | null;
  /** Policy target of the last MATCH rule, if any. */
  matchTarget: string | null;
  /** True when a MATCH rule exists and is the final rules entry. */
  matchIsLast: boolean | null;
  /** Non-fatal structural notes for the editor (not server validation). */
  warnings: YamlTemplateWarning[];
  /** Fatal YAML parse/structure error detail (may be library message or i18n key). */
  parseError: string | null;
  /** `yaml` = library parse error (verbatim detail); `structure` = i18n key. */
  parseErrorKind: "yaml" | "structure" | null;
  /** Inclusive start offset of the parse error in source, if known. */
  parseErrorFrom: number | null;
  /** Exclusive end offset of the parse error in source, if known. */
  parseErrorTo: number | null;
  /** 1-based line number of the parse error, if known. */
  parseErrorLine: number | null;
};

const EMPTY_SUMMARY: YamlTemplateSummary = {
  ok: false,
  groupCount: null,
  providerCount: null,
  ruleCount: null,
  matchTarget: null,
  matchIsLast: null,
  warnings: [],
  parseError: null,
  parseErrorKind: null,
  parseErrorFrom: null,
  parseErrorTo: null,
  parseErrorLine: null,
};

/** Expected: sequence (list of groups). Missing → 0; wrong type → null. */
function countSequence(node: unknown): number | null {
  if (node == null) {
    return 0;
  }
  if (isSeq(node)) {
    return (node as YAMLSeq).items.length;
  }
  return null;
}

/** Expected: mapping. Missing → 0; wrong type → null. */
function countMapping(node: unknown): number | null {
  if (node == null) {
    return 0;
  }
  if (isMap(node)) {
    return (node as YAMLMap).items.length;
  }
  return null;
}

function ruleText(item: unknown): string {
  if (item == null) {
    return "";
  }
  if (typeof item === "string" || typeof item === "number" || typeof item === "boolean") {
    return String(item);
  }
  if (typeof item === "object" && item !== null && "toString" in item) {
    try {
      return String(item);
    } catch {
      return "";
    }
  }
  return "";
}

function parseMatchTarget(rule: string): string | null {
  const parts = rule.split(",").map((part) => part.trim());
  if (parts.length < 2) {
    return null;
  }
  if (parts[0]?.toUpperCase() !== "MATCH") {
    return null;
  }
  return parts[1] || null;
}

type YamlErrorLike = {
  message?: string;
  pos?: [number, number] | number[];
  linePos?: Array<{ line: number; col: number }>;
};

/**
 * Extract a bounded, clamped source range from a yaml package error.
 * Offsets are clamped to [0, sourceLength]; empty range expands by 1 when possible.
 */
export function extractYamlErrorRange(
  error: YamlErrorLike | null | undefined,
  sourceLength: number,
): { from: number; to: number; line: number | null } {
  const len = Math.max(0, sourceLength);
  let from = 0;
  let to = 0;
  let line: number | null = null;

  const pos = error?.pos;
  if (Array.isArray(pos) && pos.length >= 1) {
    const rawFrom = Number(pos[0]);
    const rawTo = pos.length >= 2 ? Number(pos[1]) : rawFrom;
    if (Number.isFinite(rawFrom)) {
      from = Math.min(Math.max(0, Math.floor(rawFrom)), len);
    }
    if (Number.isFinite(rawTo)) {
      to = Math.min(Math.max(0, Math.floor(rawTo)), len);
    } else {
      to = from;
    }
  }

  if (to < from) {
    to = from;
  }
  if (to === from && len > 0) {
    to = Math.min(from + 1, len);
  }
  if (from === 0 && to === 0 && len > 0) {
    to = 1;
  }

  const linePos = error?.linePos;
  if (Array.isArray(linePos) && linePos[0] && Number.isFinite(linePos[0].line)) {
    line = Math.max(1, Math.floor(linePos[0].line));
  }

  return { from, to, line };
}

/**
 * Summarize template YAML for the Rule Profile editor.
 * Uses a real YAML parser; never relies on fragile line-only heuristics for correctness.
 */
export function summarizeYamlTemplate(source: string): YamlTemplateSummary {
  const trimmed = source.trim();
  if (!trimmed) {
    return {
      ...EMPTY_SUMMARY,
      ok: true,
      groupCount: 0,
      providerCount: 0,
      ruleCount: 0,
      matchTarget: null,
      matchIsLast: null,
      warnings: [],
      parseError: null,
      parseErrorKind: null,
      parseErrorFrom: null,
      parseErrorTo: null,
      parseErrorLine: null,
    };
  }

  try {
    const doc = parseDocument(source, { prettyErrors: true, uniqueKeys: false });
    const warnings: YamlTemplateWarning[] = [];

    if (doc.errors.length > 0) {
      const first = doc.errors[0] as YamlErrorLike | undefined;
      const range = extractYamlErrorRange(first, source.length);
      return {
        ...EMPTY_SUMMARY,
        parseError: first?.message ?? "YAML parse error",
        parseErrorKind: "yaml",
        parseErrorFrom: range.from,
        parseErrorTo: range.to,
        parseErrorLine: range.line,
      };
    }

    for (const warning of doc.warnings) {
      if (warnings.length >= 8) {
        break;
      }
      // Library warning text stays verbatim; UI prefixes with a localized label.
      warnings.push({ key: "YAML 警告：{{detail}}", params: { detail: warning.message } });
    }

    const root = doc.contents;
    if (root == null) {
      return {
        ok: true,
        groupCount: 0,
        providerCount: 0,
        ruleCount: 0,
        matchTarget: null,
        matchIsLast: null,
        warnings,
        parseError: null,
        parseErrorKind: null,
        parseErrorFrom: null,
        parseErrorTo: null,
        parseErrorLine: null,
      };
    }

    if (!isMap(root)) {
      return {
        ...EMPTY_SUMMARY,
        parseError: "根文档必须是 YAML 映射",
        parseErrorKind: "structure",
        warnings,
      };
    }

    const map = root as YAMLMap;
    const groupsNode = map.get("proxy-groups");
    const providersNode = map.get("rule-providers");
    const rulesNode = map.get("rules");

    // Enforce Mihomo top-level shapes: sequence / mapping / sequence.
    // Wrong map↔sequence interchange is not counted as valid.
    const groupCount = countSequence(groupsNode);
    const providerCount = countMapping(providersNode);
    const ruleCount = countSequence(rulesNode);

    if (groupsNode != null && groupCount === null) {
      warnings.push({ key: "proxy-groups 应为序列（sequence），不能是映射或其他类型" });
    }
    if (providersNode != null && providerCount === null) {
      warnings.push({ key: "rule-providers 应为映射（mapping），不能是序列或其他类型" });
    }
    if (rulesNode != null && ruleCount === null) {
      warnings.push({ key: "rules 应为序列（sequence），不能是映射或其他类型" });
    }

    let matchTarget: string | null = null;
    let matchIsLast: boolean | null = null;
    let matchIndex = -1;
    let matchCount = 0;

    if (isSeq(rulesNode)) {
      const rules = rulesNode as YAMLSeq;
      for (let i = 0; i < rules.items.length; i += 1) {
        const text = ruleText(rules.get(i));
        const target = parseMatchTarget(text);
        if (target != null) {
          matchCount += 1;
          matchIndex = i;
          matchTarget = target;
        }
      }
      if (matchCount === 0) {
        matchIsLast = null;
        if (rules.items.length > 0) {
          warnings.push({ key: "未找到 MATCH 规则" });
        }
      } else {
        matchIsLast = matchIndex === rules.items.length - 1;
        if (!matchIsLast) {
          warnings.push({ key: "MATCH 不是最后一条规则" });
        }
        if (matchCount > 1) {
          warnings.push({ key: "发现多条 MATCH 规则（{{count}}）", params: { count: matchCount } });
        }
      }
    }

    return {
      ok: true,
      // Keep null when shape is wrong so callers do not treat interchange as valid counts.
      groupCount,
      providerCount,
      ruleCount,
      matchTarget,
      matchIsLast,
      warnings: warnings.slice(0, 8),
      parseError: null,
      parseErrorKind: null,
      parseErrorFrom: null,
      parseErrorTo: null,
      parseErrorLine: null,
    };
  } catch (error) {
    return {
      ...EMPTY_SUMMARY,
      parseError: error instanceof Error ? error.message : "YAML parse error",
      parseErrorKind: "yaml",
      parseErrorFrom: 0,
      parseErrorTo: Math.min(source.length, 1),
      parseErrorLine: null,
    };
  }
}
