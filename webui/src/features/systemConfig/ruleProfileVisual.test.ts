/**
 * ruleProfileVisual.test.ts — Phase 4 pure-core tests.
 *
 * Covers parse, validate, apply, fidelity, and plan API.
 * Golden fixture loaded from canonical repo path (not a local copy).
 */

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  computeFingerprint,
  type VisualDraft,
  type ModeledGroup,
  type ModeledProvider,
  type ModeledRule,
} from "./ruleProfileVisualModel";
import { parseRuleProfileVisualDraft } from "./ruleProfileVisualParse";
import { validateRuleProfileVisualDraft } from "./ruleProfileVisualValidate";
import { applyRuleProfileVisualDraft } from "./ruleProfileVisualApply";
import { buildFidelityReport, planRuleProfileVisualApply } from "./ruleProfileVisualFidelity";

// ─── Helpers ─────────────────────────────────────────────────────────────────

/** Load the golden ACL4SSR Full fixture from its canonical repo location. */
function loadGoldenYaml(): string {
  const p = resolve(
    __dirname,
    "../../../../internal/service/testdata/acl4ssr/ACL4SSR_Online_Full.golden.yaml",
  );
  return readFileSync(p, "utf-8");
}

/** Count modeled/raw items in a draft section. */
function countItems(items: Array<{ kind: string }>): {
  total: number;
  modeled: number;
  raw: number;
} {
  const modeled = items.filter((i) => i.kind === "modeled").length;
  const raw = items.filter((i) => i.kind === "raw").length;
  return { total: items.length, modeled, raw };
}

/** Helper to find a modeled group by name. */
function findModeledGroup(
  draft: VisualDraft,
  name: string,
): ModeledGroup | undefined {
  return draft.groups.find(
    (g): g is ModeledGroup => g.kind === "modeled" && g.name === name,
  );
}

/** Helper to find a modeled provider by key. */
function findModeledProvider(
  draft: VisualDraft,
  key: string,
): ModeledProvider | undefined {
  return draft.providers.find(
    (p): p is ModeledProvider => p.kind === "modeled" && p.key === key,
  );
}

/** Helper to find last modeled rule (usually MATCH). */
function lastModeledRule(draft: VisualDraft): ModeledRule | undefined {
  for (let i = draft.rules.length - 1; i >= 0; i--) {
    const r = draft.rules[i];
    if (r.kind === "modeled") return r;
  }
  return undefined;
}

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 1: ACL4SSR Full golden — baseline parse
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 1: Full golden", () => {
  const yaml = loadGoldenYaml();
  const result = parseRuleProfileVisualDraft(yaml);

  it("parses successfully", () => {
    expect(result.ok).toBe(true);
    expect(result.errors).toHaveLength(0);
    expect(result.draft).not.toBeNull();
  });

  const draft = result.draft!;

  it("has 29 groups, 31 providers, 33 rules", () => {
    expect(draft.groups).toHaveLength(29);
    expect(draft.providers).toHaveLength(31);
    expect(draft.rules).toHaveLength(33);
  });

  it("all items are modeled (0 raw)", () => {
    expect(countItems(draft.groups).raw).toBe(0);
    expect(countItems(draft.providers).raw).toBe(0);
    expect(countItems(draft.rules).raw).toBe(0);
    expect(countItems(draft.groups).modeled).toBe(29);
    expect(countItems(draft.providers).modeled).toBe(31);
    expect(countItems(draft.rules).modeled).toBe(33);
  });

  it("no blocked sections", () => {
    expect(draft.blockedSections).toHaveLength(0);
  });

  it("no validation errors", () => {
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.valid).toBe(true);
    expect(val.errors).toHaveLength(0);
  });

  it("last rule is MATCH", () => {
    const last = lastModeledRule(draft);
    expect(last).toBeDefined();
    expect(last!.ruleType).toBe("MATCH");
    expect(last!.policy).toBe("🐟 漏网之鱼");
  });

  it("has GEOIP,CN rule with no-resolve", () => {
    const geoip = draft.rules.find(
      (r) => r.kind === "modeled" && r.ruleType === "GEOIP" && r.geoipCode === "CN",
    ) as ModeledRule | undefined;
    expect(geoip).toBeDefined();
    expect(geoip!.noResolve).toBe(true);
  });

  it("fidelity report has no blockers", () => {
    const report = buildFidelityReport(yaml, draft);
    expect(report.hasBlocker).toBe(false);
  });

  it("plan indicates canApply", () => {
    const plan = planRuleProfileVisualApply(yaml, draft);
    expect(plan.canApply).toBe(true);
    expect(plan.validation.valid).toBe(true);
    expect(plan.fidelity.hasBlocker).toBe(false);
    // No-op should have zero changes
    expect(plan.stats.groupsChanged).toBe(0);
    expect(plan.stats.providersChanged).toBe(0);
    expect(plan.stats.rulesChanged).toBe(0);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 2: No-op round trip — byte-for-byte exact
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 2: No-op round trip", () => {
  const yaml = loadGoldenYaml();
  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("apply returns original YAML byte-for-byte", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    expect(applyResult.yaml).toBe(yaml); // byte-for-byte exact
  });

  it("deterministic: repeated apply returns same result", () => {
    const r1 = applyRuleProfileVisualDraft(yaml, draft);
    expect(r1.ok).toBe(true);
    const r2 = applyRuleProfileVisualDraft(yaml, draft);
    expect(r2.ok).toBe(true);
    expect(r2.yaml).toBe(r1.yaml);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 3: Group rename + stale reference detection
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 3: Group rename", () => {
  const yaml = loadGoldenYaml();
  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  // Rename the first modeled group
  const targetGroup = findModeledGroup(draft, "🚀 节点选择");
  expect(targetGroup).toBeDefined();
  targetGroup!.name = "我的节点";

  it("plan detects rename operation and stale refs", () => {
    const plan = planRuleProfileVisualApply(yaml, draft);
    expect(plan.canApply).toBe(true); // no blockers, valid
    expect(plan.stats.operations).toContain("rename");
    expect(plan.stats.groupsChanged).toBeGreaterThan(0);
    // Should have stale reference warnings
    const staleRefs = plan.fidelity.issues.filter(
      (i) => i.code === "GROUP_RENAMED_STALE_REFS" || i.code === "GROUP_MEMBER_STALE_REFERENCE" || i.code === "RULE_POLICY_STALE_REFERENCE",
    );
    expect(staleRefs.length).toBeGreaterThanOrEqual(1);
  });

  const applyResult = applyRuleProfileVisualDraft(yaml, draft);

  it("applies successfully", () => {
    expect(applyResult.ok).toBe(true);
  });

  const output = applyResult.yaml!;

  it("renamed group appears with new name", () => {
    expect(output).toContain("我的节点");
  });

  it("other groups still present and same count", () => {
    const reParsed = parseRuleProfileVisualDraft(output);
    expect(reParsed.ok).toBe(true);
    expect(reParsed.draft!.groups).toHaveLength(29);
    const oldNamed = findModeledGroup(reParsed.draft!, "🚀 节点选择");
    expect(oldNamed).toBeUndefined();
  });

  it("rename does NOT auto-update rule policies (stale reference)", () => {
    // Renaming a group does NOT update rules that reference the old name
    const reParsed = parseRuleProfileVisualDraft(output);
    const proxyGfw = reParsed.draft!.rules.find(
      (r) => r.kind === "modeled" && r.ruleType === "RULE-SET" && r.provider === "ProxyGFWlist",
    ) as ModeledRule | undefined;
    expect(proxyGfw).toBeDefined();
    expect(proxyGfw!.policy).toBe("🚀 节点选择"); // Not auto-updated
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 4: Rule reorder — originalIndex immutable
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 4: Rule reorder", () => {
  const yaml = loadGoldenYaml();
  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  // Record original indexes before swap
  const originalIdx0 = draft.rules[0].originalIndex;
  const originalIdx1 = draft.rules[1].originalIndex;

  // Swap first two items: originalIndex must NOT change
  [draft.rules[0], draft.rules[1]] = [draft.rules[1], draft.rules[0]];

  it("originalIndex unchanged after swap", () => {
    expect(draft.rules[0].originalIndex).toBe(originalIdx1);
    expect(draft.rules[1].originalIndex).toBe(originalIdx0);
    expect(draft.rules[0].originalIndex).not.toBe(0); // was swapped
  });

  const applyResult = applyRuleProfileVisualDraft(yaml, draft);

  it("applies successfully", () => {
    expect(applyResult.ok).toBe(true);
  });

  const output = applyResult.yaml!;

  it("rules reordered: UnBan before LocalAreaNetwork", () => {
    const unBanIdx = output.indexOf("RULE-SET,UnBan");
    const localIdx = output.indexOf("RULE-SET,LocalAreaNetwork");
    expect(unBanIdx).toBeGreaterThan(0);
    expect(localIdx).toBeGreaterThan(0);
    expect(unBanIdx).toBeLessThan(localIdx);
  });

  it("MATCH remains last", () => {
    const reParsed = parseRuleProfileVisualDraft(output);
    const last = lastModeledRule(reParsed.draft!);
    expect(last).toBeDefined();
    expect(last!.ruleType).toBe("MATCH");
  });

  it("rule count unchanged", () => {
    const reParsed = parseRuleProfileVisualDraft(output);
    expect(reParsed.draft!.rules).toHaveLength(33);
  });

  it("plan detects reorder operation", () => {
    const plan = planRuleProfileVisualApply(yaml, draft);
    expect(plan.stats.operations).toContain("reorder");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 5: Add rule before MATCH
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 5: Add rule before MATCH", () => {
  const yaml = loadGoldenYaml();
  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  const matchIdx = draft.rules.findIndex(
    (r) => r.kind === "modeled" && r.ruleType === "MATCH",
  );
  expect(matchIdx).toBeGreaterThanOrEqual(0);

  const newRule: ModeledRule = {
    kind: "modeled",
    id: "r-new",
    originalIndex: -1, // new item
    ruleType: "RULE-SET",
    provider: "CustomProvider",
    policy: "🚀 节点选择",
    noResolve: false,
    rawText: "RULE-SET,CustomProvider,🚀 节点选择",
  };

  draft.rules.splice(matchIdx, 0, newRule);

  const applyResult = applyRuleProfileVisualDraft(yaml, draft);

  it("applies successfully", () => {
    expect(applyResult.ok).toBe(true);
  });

  const output = applyResult.yaml!;

  it("contains new rule", () => {
    expect(output).toContain("RULE-SET,CustomProvider,🚀 节点选择");
  });

  it("MATCH is still last rule", () => {
    const reParsed = parseRuleProfileVisualDraft(output);
    const last = lastModeledRule(reParsed.draft!);
    expect(last).toBeDefined();
    expect(last!.ruleType).toBe("MATCH");
  });

  it("rule count incremented by 1", () => {
    const reParsed = parseRuleProfileVisualDraft(output);
    expect(reParsed.draft!.rules).toHaveLength(34);
  });

  it("plan detects add operation", () => {
    const plan = planRuleProfileVisualApply(yaml, draft);
    expect(plan.stats.operations).toContain("add");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 6: Delete filter — preserves proxies + include-all independently
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 6: Delete filter, preserve proxies+include-all", () => {
  const yaml = loadGoldenYaml();
  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  // 🎶 网易音乐 has BOTH explicit proxies and include-all-proxies
  const netease = findModeledGroup(draft, "🎶 网易音乐");
  expect(netease).toBeDefined();
  expect(netease!.proxies).not.toBeNull();
  expect(netease!.includeAllProxies).toBe(true);
  expect(netease!.filter).not.toBeNull();

  // Delete filter only
  netease!.filter = null;

  const applyResult = applyRuleProfileVisualDraft(yaml, draft);
  const output = applyResult.yaml!;

  it("applies successfully", () => {
    expect(applyResult.ok).toBe(true);
  });

  it("filter removed from group", () => {
    const reParsed = parseRuleProfileVisualDraft(output);
    const netease2 = findModeledGroup(reParsed.draft!, "🎶 网易音乐");
    expect(netease2).toBeDefined();
    expect(netease2!.filter).toBeNull();
  });

  it("explicit proxies still present after filter delete", () => {
    const reParsed = parseRuleProfileVisualDraft(output);
    const netease2 = findModeledGroup(reParsed.draft!, "🎶 网易音乐");
    expect(netease2!.proxies).not.toBeNull();
    expect(netease2!.proxies!.length).toBeGreaterThan(0);
  });

  it("include-all-proxies still true after filter delete", () => {
    const reParsed = parseRuleProfileVisualDraft(output);
    const netease2 = findModeledGroup(reParsed.draft!, "🎶 网易音乐");
    expect(netease2!.includeAllProxies).toBe(true);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 7: Raw fallback group metadata
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 7: Fallback group raw", () => {
  const yaml = `proxies: []
proxy-groups:
  - name: TestFallback
    type: fallback
    url: http://www.gstatic.com/generate_204
    interval: 300
  - name: SelectGroup
    type: select
    proxies:
      - DIRECT
rules:
  - MATCH,SelectGroup
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("fallback group is raw with metadata", () => {
    const fallback = draft.groups[0];
    expect(fallback.kind).toBe("raw");
    if (fallback.kind === "raw") {
      expect(fallback.label).toBe("TestFallback");
      expect(fallback.sourceType).toBe("fallback");
      expect(fallback.reason).toContain("Unsupported");
    }
  });

  it("select group is modeled", () => {
    const select = draft.groups[1];
    expect(select.kind).toBe("modeled");
  });

  it("no-op apply preserves fallback group", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    expect(applyResult.yaml).toContain("type: fallback");
    expect(applyResult.yaml).toContain("TestFallback");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 8: Raw file provider metadata
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 8: File provider raw", () => {
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rule-providers:
  MyList:
    type: file
    behavior: classical
    format: text
    path: "./rules.txt"
  HttpProvider:
    type: http
    behavior: classical
    format: text
    url: https://example.com/rules.txt
    interval: 86400
rules:
  - RULE-SET,HttpProvider,Proxy
  - MATCH,DIRECT
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("file provider is raw with metadata", () => {
    expect(draft.providers[0].kind).toBe("raw");
    if (draft.providers[0].kind === "raw") {
      expect(draft.providers[0].label).toBe("MyList");
      expect(draft.providers[0].sourceType).toContain("file");
      expect(draft.providers[0].reason).toContain("Unsupported");
    }
  });

  it("http provider is modeled", () => {
    expect(draft.providers[1].kind).toBe("modeled");
  });

  it("no-op apply preserves file provider", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    expect(applyResult.yaml).toContain("type: file");
    expect(applyResult.yaml).toContain("./rules.txt");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 9: Complex/non-string rules
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 9: Complex/non-string rule raw", () => {
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rules:
  - DOMAIN-SUFFIX,example.com,Proxy
  - "RULE-SET,MyProvider,Proxy"
  - MATCH,DIRECT
  - 42
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("DOMAIN-SUFFIX rule is raw with metadata", () => {
    const rule = draft.rules[0];
    expect(rule.kind).toBe("raw");
    if (rule.kind === "raw") {
      expect(rule.sourceType).toBe("DOMAIN-SUFFIX");
      expect(rule.reason).toContain("Unsupported");
      expect(rule.text).toBe("DOMAIN-SUFFIX,example.com,Proxy");
    }
  });

  it("RULE-SET rule is modeled", () => {
    expect(draft.rules[1].kind).toBe("modeled");
  });

  it("MATCH rule is modeled", () => {
    const match = draft.rules[2];
    expect(match.kind).toBe("modeled");
    if (match.kind === "modeled") {
      expect(match.ruleType).toBe("MATCH");
    }
  });

  it("non-string numeric rule is raw with metadata", () => {
    expect(draft.rules[3].kind).toBe("raw");
    if (draft.rules[3].kind === "raw") {
      expect(draft.rules[3].text).toBe("42");
    }
  });

  it("no-op apply preserves raw rules in order", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;
    expect(output).toContain("DOMAIN-SUFFIX,example.com,Proxy");
    expect(output).toContain("RULE-SET,MyProvider,Proxy");
    expect(output).toContain("MATCH,DIRECT");
    expect(output).toContain("42");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 10: Comments retained — exact assertions
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 10: Comments retained", () => {
  const yaml = `# Top-level comment
proxy-groups:
  # Group section comment
  - name: Proxy
    type: select
    proxies:
      - DIRECT
    # Group inline comment
  - name: Auto
    # Before url-test
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
rule-providers:
  # Provider comment
  MyRules:
    type: http
    behavior: classical
    format: text
    url: https://example.com/rules.txt
    interval: 86400
rules:
  # Rule section comment
  - "RULE-SET,MyRules,Proxy"
  - MATCH,DIRECT
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("no-block apply succeeds", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
  });

  it("top-level comment preserved in output", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    const output = applyResult.yaml!;
    expect(output).toContain("Top-level comment");
  });

  it("section comment preserved in output", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    const output = applyResult.yaml!;
    expect(output).toContain("Group section comment");
  });

  it("inline group comment preserved", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    const output = applyResult.yaml!;
    expect(output).toContain("Group inline comment");
  });

  it("url-test comment preserved", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    const output = applyResult.yaml!;
    expect(output).toContain("Before url-test");
  });

  it("provider comment preserved", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    const output = applyResult.yaml!;
    expect(output).toContain("Provider comment");
  });

  it("rule section comment preserved", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    const output = applyResult.yaml!;
    expect(output).toContain("Rule section comment");
  });

  it("fidelity report includes comment info", () => {
    const report = buildFidelityReport(yaml, draft);
    const commentIssues = report.issues.filter(
      (i) => i.code === "ITEM_COMMENT" || i.code === "SECTION_KEY_COMMENT",
    );
    // There should be at least comment info entries
    expect(commentIssues.length).toBeGreaterThan(0);
    // NOT using >= 0 — we expect actual comments to be detected
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 11: Anchor blocks + alias-only detection
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 11: Anchor and alias blocking", () => {
  const yaml = `proxy-groups:
  - &anchor1
    name: Template
    type: select
    proxies:
      - DIRECT
  - <<: *anchor1
    name: Proxy
rules:
  - MATCH,DIRECT
`;

  const result = parseRuleProfileVisualDraft(yaml);

  it("groups section is blocked", () => {
    expect(result.draft!.blockedSections).toContain("groups");
  });

  it("fidelity report has blocker for blocked section", () => {
    const report = buildFidelityReport(yaml, result.draft!);
    expect(report.hasBlocker).toBe(true);
    const blockers = report.issues.filter(
      (i) => i.code === "SECTION_BLOCKED",
    );
    expect(blockers.length).toBeGreaterThanOrEqual(1);
  });

  it("plan blocks apply for anchor section", () => {
    const plan = planRuleProfileVisualApply(yaml, result.draft!);
    expect(plan.canApply).toBe(false);
    expect(plan.fidelity.hasBlocker).toBe(true);
  });

  it("apply refuses with blocked sections", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, result.draft!);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("BLOCKED_SECTIONS");
  });
});

describe("Fixture 11b: Alias-only (no anchor in section)", () => {
  // rules uses an alias referencing an anchor defined OUTSIDE the section
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rules:
  - *someExternalAnchor
`;

  const result = parseRuleProfileVisualDraft(yaml);

  it("rules section is blocked due to alias", () => {
    expect(result.draft!.blockedSections).toContain("rules");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 12: Unknown top-level keys preserved
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 12: Unknown top-level pair preserved", () => {
  const yaml = `dns:
  enable: true
  listen: 0.0.0.0:53
proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
tun:
  enable: false
rules:
  - MATCH,DIRECT
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("unknown top-level keys noted in fidelity info", () => {
    const report = buildFidelityReport(yaml, draft);
    const unknownKeys = report.issues.filter(
      (i) => i.code === "UNKNOWN_TOP_LEVEL_KEY",
    );
    expect(unknownKeys.length).toBe(2);
    const keyNames = unknownKeys.map((i) => i.message);
    expect(keyNames.some((m) => m.includes("dns"))).toBe(true);
    expect(keyNames.some((m) => m.includes("tun"))).toBe(true);
  });

  it("apply preserves dns and tun in order", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;
    const dnsIdx = output.indexOf("dns:");
    const pgIdx = output.indexOf("proxy-groups:");
    expect(dnsIdx).toBeLessThan(pgIdx);
    const tunIdx = output.indexOf("tun:");
    const rulesIdx = output.indexOf("rules:");
    expect(tunIdx).toBeGreaterThan(pgIdx);
    expect(tunIdx).toBeLessThan(rulesIdx);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 13: Unknown modeled fields preserved on edit
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 13: Unknown modeled fields preserved on edit", () => {
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
    extra-field: some-value
    another-extra: 42
    include-all-proxies: false
rule-providers:
  Custom:
    type: http
    behavior: classical
    format: text
    url: https://example.com/list.txt
    interval: 86400
    custom-meta: true
rules:
  - MATCH,DIRECT
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("modeled group has 2 unknown keys", () => {
    const proxy = findModeledGroup(draft, "Proxy");
    expect(proxy).toBeDefined();
    expect(proxy!.unknownKeyCount).toBe(2);
  });

  it("modeled provider has 1 unknown key", () => {
    const custom = findModeledProvider(draft, "Custom");
    expect(custom).toBeDefined();
    expect(custom!.unknownKeyCount).toBe(1);
  });

  it("fidelity report lists unknown modeled fields", () => {
    const report = buildFidelityReport(yaml, draft);
    const unknownFieldIssues = report.issues.filter(
      (i) => i.code === "GROUP_UNKNOWN_FIELDS" || i.code === "PROVIDER_UNKNOWN_FIELDS",
    );
    expect(unknownFieldIssues.length).toBe(2);
  });

  it("apply preserves unknown fields after no-op", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;
    expect(output).toContain("extra-field");
    expect(output).toContain("another-extra");
    expect(output).toContain("custom-meta");
  });

  it("edit (change name) preserves unknown fields", () => {
    const proxy = findModeledGroup(draft, "Proxy");
    proxy!.name = "ChangedProxy";

    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;
    expect(output).toContain("extra-field");
    expect(output).toContain("another-extra");
    expect(output).toContain("custom-meta");
    expect(output).toContain("ChangedProxy");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 14: Stale fingerprint rejection
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 14: Stale fingerprint rejection", () => {
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rules:
  - MATCH,DIRECT
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("apply succeeds with matching fingerprint", () => {
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
  });

  it("apply rejects after source change", () => {
    const modifiedYaml = yaml + "\n# extra comment\n";
    const applyResult = applyRuleProfileVisualDraft(modifiedYaml, draft);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("STALE_DRAFT");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 15: Duplicate / non-terminal MATCH validation
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 15: Duplicate MATCH", () => {
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rules:
  - MATCH,Proxy
  - MATCH,DIRECT
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("validation catches duplicate MATCH", () => {
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.valid).toBe(false);
    const dupMatch = val.errors.find((e) => e.code === "DUPLICATE_MATCH");
    expect(dupMatch).toBeDefined();
  });
});

describe("Fixture 16: Non-terminal MATCH", () => {
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rules:
  - MATCH,Proxy
  - DOMAIN-SUFFIX,example.com,Proxy
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("validation catches non-terminal MATCH", () => {
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.valid).toBe(false);
    const notLast = val.errors.find((e) => e.code === "MATCH_NOT_LAST");
    expect(notLast).toBeDefined();
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 17: Missing MATCH is error
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 17: Missing MATCH error", () => {
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rules:
  - DOMAIN-SUFFIX,example.com,Proxy
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  it("validation reports MISSING_MATCH error (not warning)", () => {
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.valid).toBe(false);
    const missing = val.errors.find((e) => e.code === "MISSING_MATCH");
    expect(missing).toBeDefined();
    const warning = val.warnings.find((w) => w.code === "MISSING_MATCH");
    expect(warning).toBeUndefined();
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 18: Empty rule fields validation
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 18: Empty rule field validation", () => {
  it("empty MATCH policy is error", () => {
    const draft: VisualDraft = createMinimalDraft();
    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "MATCH",
      policy: "",
      noResolve: false,
      rawText: "MATCH,",
    });
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "EMPTY_MATCH_POLICY")).toBe(true);
  });

  it("empty RULE-SET provider is error", () => {
    const draft = createMinimalDraft();
    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "RULE-SET",
      provider: "",
      policy: "DIRECT",
      noResolve: false,
      rawText: "RULE-SET,,DIRECT",
    });
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "EMPTY_RULE_SET_PROVIDER")).toBe(true);
  });

  it("empty GEOIP code is error", () => {
    const draft = createMinimalDraft();
    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "GEOIP",
      geoipCode: "",
      policy: "DIRECT",
      noResolve: false,
      rawText: "GEOIP,,DIRECT",
    });
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "EMPTY_GEOIP_CODE")).toBe(true);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 19: Provider URL validation boundaries
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 19: Provider URL validation", () => {
  it("rejects HTTP provider URL", () => {
    const draft = createMinimalDraft();
    draft.providers.push(makeProvider("http://example.com/list.txt", 86400));
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.valid).toBe(false);
    expect(val.errors.some((e) => e.code === "PROVIDER_URL_NOT_HTTPS")).toBe(true);
  });

  it("rejects provider URL with userinfo", () => {
    const draft = createMinimalDraft();
    draft.providers.push(makeProvider("https://user:pass@example.com/list.txt", 86400));
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "PROVIDER_URL_HAS_USERINFO")).toBe(true);
  });

  it("rejects provider URL with fragment", () => {
    const draft = createMinimalDraft();
    draft.providers.push(makeProvider("https://example.com/list.txt#section", 86400));
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "PROVIDER_URL_HAS_FRAGMENT")).toBe(true);
  });

  it("rejects provider URL without host", () => {
    const draft = createMinimalDraft();
    // The URL constructor rejects bare hosts for https: in some environments
    draft.providers.push(makeProvider("https:///", 86400));
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.valid).toBe(false);
    const hasHostError = val.errors.some(
      (e) => e.code === "PROVIDER_URL_NO_HOST" || e.code === "INVALID_PROVIDER_URL",
    );
    expect(hasHostError).toBe(true);
  });

  it("rejects provider interval zero", () => {
    const draft = createMinimalDraft();
    draft.providers.push(makeProvider("https://example.com/list.txt", 0));
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_PROVIDER_INTERVAL")).toBe(true);
  });

  it("rejects provider interval negative", () => {
    const draft = createMinimalDraft();
    draft.providers.push(makeProvider("https://example.com/list.txt", -1));
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_PROVIDER_INTERVAL")).toBe(true);
  });

  it("accepts valid HTTPS provider URL", () => {
    const draft = createMinimalDraft();
    draft.providers.push(makeProvider("https://example.com/list.txt", 86400));
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.valid).toBe(true);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 20: url-test URL validation boundaries
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 20: url-test URL validation", () => {
  const baseDraft = () => {
    const d = createMinimalDraft();
    d.groups.push({
      kind: "modeled",
      id: "g-0",
      originalIndex: 0,
      originalName: "Auto",
      name: "Auto",
      type: "url-test",
      proxies: null,
      includeAllProxies: true,
      filter: null,
      url: "http://www.gstatic.com/generate_204",
      interval: 300,
      timeout: null,
      tolerance: null,
      unknownKeyCount: 0,
    });
    d.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "MATCH",
      policy: "Proxy",
      noResolve: false,
      rawText: "MATCH,Proxy",
    });
    d.rules.push({
      kind: "modeled",
      id: "r-1",
      originalIndex: 1,
      ruleType: "MATCH",
      policy: "DIRECT",
      noResolve: false,
      rawText: "MATCH,DIRECT",
    });
    return d;
  };

  it("missing url is error", () => {
    const draft = baseDraft();
    const group = draft.groups[0];
    if (group.kind === "modeled") group.url = null;
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "MISSING_URL_TEST_URL")).toBe(true);
  });

  it("empty url is error", () => {
    const draft = baseDraft();
    const group = draft.groups[0];
    if (group.kind === "modeled") group.url = "";
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "MISSING_URL_TEST_URL")).toBe(true);
  });

  it("rejects ftp url", () => {
    const draft = baseDraft();
    const group = draft.groups[0];
    if (group.kind === "modeled") group.url = "ftp://example.com/";
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "URL_TEST_URL_INVALID_PROTOCOL")).toBe(true);
  });

  it("accepts http url-test URL", () => {
    const draft = baseDraft();
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code.startsWith("URL_TEST_URL"))).toBe(false);
  });

  it("negative interval is error", () => {
    const draft = baseDraft();
    const group = draft.groups[0];
    if (group.kind === "modeled") group.interval = -1;
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_URL_TEST_INTERVAL")).toBe(true);
  });

  it("negative tolerance is error", () => {
    const draft = baseDraft();
    const group = draft.groups[0];
    if (group.kind === "modeled") group.tolerance = -1;
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_URL_TEST_TOLERANCE")).toBe(true);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 21: Group URL may be HTTP (health check)
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 21: Group health-check URL may be HTTP", () => {
  const yaml = `proxy-groups:
  - name: Auto
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
rules:
  - MATCH,DIRECT
`;

  it("parse and apply preserves HTTP url-test URL", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    expect(result.ok).toBe(true);
    const draft = result.draft!;
    const auto = findModeledGroup(draft, "Auto");
    expect(auto).toBeDefined();
    expect(auto!.url).toBe("http://www.gstatic.com/generate_204");

    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    expect(applyResult.yaml).toContain("http://www.gstatic.com/generate_204");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 22: Provider key rename — preserves position, warns stale RULE-SET
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 22: Provider key rename", () => {
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rule-providers:
  ###############################################################################
  # Notice: Keep or replace this provider (old key: OldProviderName) accordingly.
  ###############################################################################
  OldProviderName:
    type: http
    behavior: classical
    format: text
    url: https://example.com/rules.txt
    interval: 86400
    custom-field: keep-me
rules:
  - RULE-SET,OldProviderName,Proxy
  - MATCH,DIRECT
`;

  const result = parseRuleProfileVisualDraft(yaml);
  const draft = result.draft!;

  const provider = findModeledProvider(draft, "OldProviderName");
  expect(provider).toBeDefined();
  expect(provider!.originalKey).toBe("OldProviderName");
  provider!.key = "NewProviderName";

  it("plan detects rename and stale RULE-SET warning", () => {
    const plan = planRuleProfileVisualApply(yaml, draft);
    expect(plan.stats.operations).toContain("rename");
    const staleWarnings = plan.fidelity.issues.filter(
      (i) => i.code === "PROVIDER_RENAMED_STALE_REFS",
    );
    expect(staleWarnings.length).toBeGreaterThanOrEqual(1);
    // RULE-SET stale reference also present (from validation)
    const ruleWarnings = plan.validation.warnings.filter(
      (w) => w.code === "RULE_SET_STALE_PROVIDER",
    );
    expect(ruleWarnings.length).toBeGreaterThanOrEqual(1);
  });

  const applyResult = applyRuleProfileVisualDraft(yaml, draft);

  it("applies successfully", () => {
    expect(applyResult.ok).toBe(true);
  });

  const output = applyResult.yaml!;

  it("new key name appears instead of old", () => {
    expect(output).toContain("NewProviderName:");
    // Old key may still appear in RULE-SET rule text (not auto-updated)
  });

  it("pair position preserved (comment before key)", () => {
    // The comment block should still appear before the renamed key
    expect(output).toContain("###############################################################################");
    const commentIdx = output.indexOf("# Notice:");
    const keyIdx = output.indexOf("NewProviderName:");
    expect(commentIdx).toBeGreaterThan(0);
    expect(keyIdx).toBeGreaterThan(commentIdx);
    // And the comment should be close to the key (within reasonable distance)
    expect(keyIdx - commentIdx).toBeLessThan(500);
  });

  it("unknown field preserved in value map", () => {
    expect(output).toContain("custom-field: keep-me");
  });

  it("RULE-SET rule still references old provider key (not auto-updated)", () => {
    expect(output).toContain("RULE-SET,OldProviderName,Proxy");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 23: Wrong section shapes block
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 23: Wrong section shapes blocked", () => {
  it("proxy-groups as mapping is blocked", () => {
    const yaml = `proxy-groups:
  name: Proxy
rules:
  - MATCH,DIRECT
`;
    const result = parseRuleProfileVisualDraft(yaml);
    expect(result.draft!.blockedSections).toContain("groups");
  });

  it("rule-providers as sequence is blocked", () => {
    const yaml = `proxy-groups:
  - name: Proxy
    type: select
rule-providers:
  - type: http
rules:
  - MATCH,DIRECT
`;
    const result = parseRuleProfileVisualDraft(yaml);
    expect(result.draft!.blockedSections).toContain("providers");
  });

  it("rules as mapping is blocked", () => {
    const yaml = `proxy-groups:
  - name: Proxy
    type: select
rules:
  name: value
`;
    const result = parseRuleProfileVisualDraft(yaml);
    expect(result.draft!.blockedSections).toContain("rules");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 24: Missing section creation
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 24: Missing section creation", () => {
  it("creates proxy-groups when absent", () => {
    const yaml = `rules:
  - MATCH,DIRECT
`;
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    expect(draft.groups).toHaveLength(0);

    // Add a group
    draft.groups.push({
      kind: "modeled",
      id: "g-new",
      originalIndex: -1,
      originalName: null,
      name: "Proxy",
      type: "select",
      proxies: ["DIRECT"],
      includeAllProxies: false,
      filter: null,
      url: null,
      interval: null,
      timeout: null,
      tolerance: null,
      unknownKeyCount: 0,
    });

    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;
    expect(output).toContain("proxy-groups:");
    expect(output).toContain("Proxy");
  });

  it("creates rule-providers when absent", () => {
    const yaml = `proxy-groups:
  - name: Proxy
    type: select
rules:
  - MATCH,DIRECT
`;
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;

    draft.providers.push({
      kind: "modeled",
      id: "p-new",
      originalIndex: -1,
      originalKey: null,
      key: "MyProvider",
      url: "https://example.com/list.txt",
      interval: 86400,
      unknownKeyCount: 0,
    });

    // Add a corresponding RULE-SET
    draft.rules.unshift({
      kind: "modeled",
      id: "r-new",
      originalIndex: -1,
      ruleType: "RULE-SET",
      provider: "MyProvider",
      policy: "Proxy",
      noResolve: false,
      rawText: "RULE-SET,MyProvider,Proxy",
    });

    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;
    expect(output).toContain("rule-providers:");
    expect(output).toContain("MyProvider:");
  });

  it("creates rules when absent", () => {
    const yaml = `proxy-groups:
  - name: Proxy
    type: select
`;
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;

    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "MATCH",
      policy: "DIRECT",
      noResolve: false,
      rawText: "MATCH,DIRECT",
    });

    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;
    expect(output).toContain("rules:");
    expect(output).toContain("MATCH,DIRECT");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 25: Raw item mutation blocked
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 25: Raw item mutation blocked", () => {
  const yaml = `proxy-groups:
  - name: Group1
    type: fallback
    url: http://example.com/
    interval: 300
  - name: Group2
    type: select
    proxies:
      - DIRECT
rules:
  - DOMAIN-SUFFIX,example.com,Proxy
  - MATCH,DIRECT
`;

  it("cannot delete raw group", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    // Remove the raw group
    draft.groups.splice(0, 1);
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("RAW_GROUP_MUTATION");
  });

  it("cannot reorder raw groups", () => {
    // Only possible if there are 2+ raw groups
    const yaml2 = `proxy-groups:
  - name: R1
    type: fallback
    url: http://example.com/
    interval: 300
  - name: R2
    type: fallback
    url: http://example.com/
    interval: 600
  - name: M1
    type: select
    proxies:
      - DIRECT
rules:
  - MATCH,DIRECT
`;
    const result = parseRuleProfileVisualDraft(yaml2);
    const draft = result.draft!;
    // Reorder the two raw groups in the array
    // By swapping positions 0 and 1 (both raw), we change the order
    [draft.groups[0], draft.groups[1]] = [draft.groups[1], draft.groups[0]];
    const applyResult = applyRuleProfileVisualDraft(yaml2, draft);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("RAW_GROUP_MUTATION");
  });

  it("cannot delete raw rule", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    // Remove the raw rule (DOMAIN-SUFFIX)
    draft.rules.splice(0, 1);
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("RAW_RULE_MUTATION");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 26: Deleting commented item warning
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 26: Deleting commented item warning", () => {
  const yaml = `proxy-groups:
  # This is a commented group
  - name: OldGroup
    type: select
    proxies:
      - DIRECT
rules:
  - MATCH,DIRECT
`;

  it("plan warns when deleting commented modeled group", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    // Delete the group
    draft.groups.splice(0, 1);
    const plan = planRuleProfileVisualApply(yaml, draft);
    const deleteWarnings = plan.fidelity.issues.filter(
      (i) => i.code === "DELETE_COMMENTED_ITEM",
    );
    expect(deleteWarnings.length).toBeGreaterThanOrEqual(1);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 27: Emoji/regex preservation roundtrip
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 27: Emoji/regex preservation", () => {
  const yaml = loadGoldenYaml();

  it("parsed emoji group names survive roundtrip", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;

    const checkNames = [
      "🚀 节点选择",
      "🎶 网易音乐",
      "Ⓜ️ 微软Bing",
      "🍎 苹果服务",
    ];
    for (const name of checkNames) {
      const group = findModeledGroup(draft, name);
      expect(group).toBeDefined();
    }

    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);

    // Re-parse to verify names survive (yaml library may escape unicode in output)
    const reParsed = parseRuleProfileVisualDraft(applyResult.yaml!);
    expect(reParsed.ok).toBe(true);
    for (const name of checkNames) {
      const group = findModeledGroup(reParsed.draft!, name);
      expect(group).toBeDefined();
    }
  });

  it("regex filter strings preserved", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;

    const hk = findModeledGroup(draft, "🇭🇰 香港节点");
    expect(hk).toBeDefined();
    expect(hk!.filter).toContain("HK");
    expect(hk!.filter).toContain("(?:^|/)");

    const netease = findModeledGroup(draft, "🎶 网易音乐");
    expect(netease).toBeDefined();
    expect(netease!.filter).toContain("网易");

    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;
    expect(output).toContain("(网易|音乐|解锁|Music|NetEase)");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 28: Empty YAML handling
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 28: Empty YAML", () => {
  it("empty string produces an empty draft", () => {
    const result = parseRuleProfileVisualDraft("");
    expect(result.ok).toBe(true);
    expect(result.draft!.groups).toHaveLength(0);
    expect(result.draft!.providers).toHaveLength(0);
    expect(result.draft!.rules).toHaveLength(0);
  });

  it("whitespace-only produces an empty draft", () => {
    const result = parseRuleProfileVisualDraft("  \n  \n  ");
    expect(result.ok).toBe(true);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 29: computeFingerprint determinism
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 29: computeFingerprint determinism", () => {
  it("same input produces same fingerprint", () => {
    const input = "proxy-groups: []\nrules: []\n";
    const fp1 = computeFingerprint(input);
    const fp2 = computeFingerprint(input);
    expect(fp1).toBe(fp2);
  });

  it("different input produces different fingerprint", () => {
    const fp1 = computeFingerprint("hello");
    const fp2 = computeFingerprint("world");
    expect(fp1).not.toBe(fp2);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Helpers
// ═════════════════════════════════════════════════════════════════════════════

/** Create a minimal draft with MATCH rule to avoid validation errors. */
function createMinimalDraft(): VisualDraft {
  return {
    sourceFingerprint: "test",
    groups: [],
    providers: [],
    rules: [
      {
        kind: "modeled",
        id: "r-match",
        originalIndex: 0,
        ruleType: "MATCH",
        policy: "DIRECT",
        noResolve: false,
        rawText: "MATCH,DIRECT",
      },
    ],
    blockedSections: [],
  };
}

function makeProvider(url: string, interval: number): ModeledProvider {
  return {
    kind: "modeled",
    id: "p-0",
    originalIndex: 0,
    originalKey: "Test",
    key: "Test",
    url,
    interval,
    unknownKeyCount: 0,
  };
}

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 30: Fingerprint always enforced (even with precomputed plan)
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 30: Fingerprint enforced even with plan", () => {
  const yaml = `proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rules:
  - MATCH,DIRECT
`;

  it("apply rejects stale YAML even when valid plan is provided", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;

    // Create a valid plan
    const plan = planRuleProfileVisualApply(yaml, draft);
    expect(plan.canApply).toBe(true);

    // Modify the YAML to make it stale
    const modifiedYaml = yaml + "\n# extra comment\n";

    // Apply with stale YAML but valid plan — should reject with STALE_DRAFT
    const applyResult = applyRuleProfileVisualDraft(modifiedYaml, draft, plan);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("STALE_DRAFT");
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 31: setScalar preserves key order and inline comments
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 31: setScalar preserves key order and comments", () => {
  const yaml = `proxy-groups:
  - name: Auto
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    tolerance: 50
rules:
  - MATCH,DIRECT
`;

  it("edit preserves key order (name, type, url, interval)", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    const auto = findModeledGroup(draft, "Auto");
    expect(auto).toBeDefined();
    auto!.interval = 600;

    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;

    // Keys should remain in original order: name, type, url, interval
    const nameIdx = output.indexOf("name: Auto");
    const typeIdx = output.indexOf("type: url-test");
    const urlIdx = output.indexOf("url: http://www.gstatic.com/generate_204");
    const intervalIdx = output.indexOf("interval: 600");
    expect(nameIdx).toBeGreaterThan(0);
    expect(typeIdx).toBeGreaterThan(nameIdx);
    expect(urlIdx).toBeGreaterThan(typeIdx);
    expect(intervalIdx).toBeGreaterThan(urlIdx);
  });

  it("edit multiple fields preserves order", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    const auto = findModeledGroup(draft, "Auto");
    expect(auto).toBeDefined();
    auto!.interval = 600;
    auto!.tolerance = 100;
    auto!.url = "http://example.com/health";

    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(true);
    const output = applyResult.yaml!;

    const urlIdx = output.indexOf("url: http://example.com/health");
    const intervalIdx = output.indexOf("interval: 600");
    const toleranceIdx = output.indexOf("tolerance: 100");
    expect(urlIdx).toBeGreaterThan(0);
    expect(intervalIdx).toBeGreaterThan(urlIdx);
    expect(toleranceIdx).toBeGreaterThan(intervalIdx);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 32: Raw item movement (positional signature) blocked
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 32: Raw item positional enforcement", () => {
  const yaml = `proxy-groups:
  - name: Raw1
    type: fallback
    url: http://example.com/
    interval: 300
  - name: M1
    type: select
    proxies:
      - DIRECT
  - name: Raw2
    type: fallback
    url: http://example.com/
    interval: 600
rule-providers:
  RawProv:
    type: file
    behavior: classical
    format: text
    path: ./rules.txt
  GoodProv:
    type: http
    behavior: classical
    format: text
    url: https://example.com/rules.txt
    interval: 86400
rules:
  - DOMAIN-SUFFIX,example.com,Direct
  - MATCH,DIRECT
`;

  it("cannot move raw group one slot earlier", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    // Raw2 is at index 2, move it to index 1 (swap with M1 at index 1)
    [draft.groups[1], draft.groups[2]] = [draft.groups[2], draft.groups[1]];
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("RAW_GROUP_MUTATION");
  });

  it("cannot move raw group one slot later", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    // M1 is at index 1, Raw1 is at index 0.
    // Move Raw1 to index 1 (swap with M1)
    [draft.groups[0], draft.groups[1]] = [draft.groups[1], draft.groups[0]];
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("RAW_GROUP_MUTATION");
  });

  it("cannot move raw provider one slot earlier", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    // RawProv is at index 0 in providers, GoodProv is at index 1.
    // Move GoodProv to index 0 (swap)
    [draft.providers[0], draft.providers[1]] = [draft.providers[1], draft.providers[0]];
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("RAW_PROVIDER_MUTATION");
  });

  it("cannot move raw rule one slot earlier", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    // DOMAIN-SUFFIX is at index 0 (raw), MATCH at index 1 (modeled).
    // Swap them — raw rule moves to index 1
    [draft.rules[0], draft.rules[1]] = [draft.rules[1], draft.rules[0]];
    const applyResult = applyRuleProfileVisualDraft(yaml, draft);
    expect(applyResult.ok).toBe(false);
    expect(applyResult.errors[0]?.code).toBe("RAW_RULE_MUTATION");
  });

  it("modeled reorder within raw anchors still works", () => {
    const yaml3 = `proxy-groups:
  - name: Raw1
    type: fallback
    url: http://example.com/
    interval: 300
  - name: M1
    type: select
    proxies:
      - A
  - name: M2
    type: select
    proxies:
      - B
  - name: Raw2
    type: fallback
    url: http://example.com/
    interval: 600
rules:
  - MATCH,DIRECT
`;
    const result = parseRuleProfileVisualDraft(yaml3);
    const draft = result.draft!;
    // Swap M1 and M2 (both modeled, between Raw1 and Raw2) — should work
    [draft.groups[1], draft.groups[2]] = [draft.groups[2], draft.groups[1]];
    const applyResult = applyRuleProfileVisualDraft(yaml3, draft);
    expect(applyResult.ok).toBe(true);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 33: Stringify error caught
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 33: Stringify error handling", () => {
  const yaml = `proxy-groups:
  - name: Auto
    type: url-test
    url: http://example.com/
    interval: 300
rules:
  - MATCH,DIRECT
`;

  it("apply does not throw on unusual edits", () => {
    // Normal edits should not cause stringify errors
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;
    const auto = findModeledGroup(draft, "Auto");
    expect(auto).toBeDefined();
    auto!.interval = 600;

    // Should not throw — returns ok or error
    expect(() => applyRuleProfileVisualDraft(yaml, draft)).not.toThrow();
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 34: REORDER_COMMENT_RISK is warning severity
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 34: REORDER_COMMENT_RISK severity", () => {
  const yaml = `proxy-groups:
  # Group1 comment
  - name: Group1
    type: select
    proxies:
      - DIRECT
  # Group2 comment
  - name: Group2
    type: select
    proxies:
      - REJECT
rules:
  - MATCH,DIRECT
`;

  it("reorder comment risk is warning, not info", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;

    // Reorder groups
    [draft.groups[0], draft.groups[1]] = [draft.groups[1], draft.groups[0]];

    const plan = planRuleProfileVisualApply(yaml, draft);
    const reorderRisk = plan.fidelity.issues.filter(
      (i) => i.code === "REORDER_COMMENT_RISK",
    );
    expect(reorderRisk.length).toBeGreaterThanOrEqual(1);
    for (const issue of reorderRisk) {
      expect(issue.severity).toBe("warning");
    }
  });

  it("node-attached comment preservation info remains info", () => {
    const result = parseRuleProfileVisualDraft(yaml);
    const draft = result.draft!;

    [draft.groups[0], draft.groups[1]] = [draft.groups[1], draft.groups[0]];

    const plan = planRuleProfileVisualApply(yaml, draft);
    const commentInfo = plan.fidelity.issues.filter(
      (i) => i.code === "RULES_REORDERED_COMMENTS",
    );
    for (const issue of commentInfo) {
      expect(issue.severity).toBe("info");
    }
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 35: Numeric finite validation (NaN/Infinity reject)
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 35: Numeric finite validation", () => {
  it("rejects NaN url-test interval", () => {
    const draft = createMinimalDraft();
    draft.groups.push({
      kind: "modeled",
      id: "g-0",
      originalIndex: 0,
      originalName: "Auto",
      name: "Auto",
      type: "url-test",
      proxies: null,
      includeAllProxies: true,
      filter: null,
      url: "http://example.com/",
      interval: NaN,
      timeout: null,
      tolerance: null,
      unknownKeyCount: 0,
    });
    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "MATCH",
      policy: "DIRECT",
      noResolve: false,
      rawText: "MATCH,DIRECT",
    });
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_URL_TEST_INTERVAL")).toBe(true);
  });

  it("rejects Infinity url-test interval", () => {
    const draft = createMinimalDraft();
    draft.groups.push({
      kind: "modeled",
      id: "g-0",
      originalIndex: 0,
      originalName: "Auto",
      name: "Auto",
      type: "url-test",
      proxies: null,
      includeAllProxies: true,
      filter: null,
      url: "http://example.com/",
      interval: Infinity,
      timeout: null,
      tolerance: null,
      unknownKeyCount: 0,
    });
    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "MATCH",
      policy: "DIRECT",
      noResolve: false,
      rawText: "MATCH,DIRECT",
    });
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_URL_TEST_INTERVAL")).toBe(true);
  });

  it("rejects NaN url-test tolerance", () => {
    const draft = createMinimalDraft();
    draft.groups.push({
      kind: "modeled",
      id: "g-0",
      originalIndex: 0,
      originalName: "Auto",
      name: "Auto",
      type: "url-test",
      proxies: null,
      includeAllProxies: true,
      filter: null,
      url: "http://example.com/",
      interval: 300,
      timeout: null,
      tolerance: NaN,
      unknownKeyCount: 0,
    });
    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "MATCH",
      policy: "DIRECT",
      noResolve: false,
      rawText: "MATCH,DIRECT",
    });
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_URL_TEST_TOLERANCE")).toBe(true);
  });

  it("rejects negative Infinity url-test timeout", () => {
    const draft = createMinimalDraft();
    draft.groups.push({
      kind: "modeled",
      id: "g-0",
      originalIndex: 0,
      originalName: "Auto",
      name: "Auto",
      type: "url-test",
      proxies: null,
      includeAllProxies: true,
      filter: null,
      url: "http://example.com/",
      interval: 300,
      timeout: -Infinity,
      tolerance: null,
      unknownKeyCount: 0,
    });
    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "MATCH",
      policy: "DIRECT",
      noResolve: false,
      rawText: "MATCH,DIRECT",
    });
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_URL_TEST_TIMEOUT")).toBe(true);
  });

  it("rejects NaN provider interval", () => {
    const draft = createMinimalDraft();
    draft.providers.push(makeProvider("https://example.com/list.txt", NaN));
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_PROVIDER_INTERVAL")).toBe(true);
  });

  it("rejects Infinity provider interval", () => {
    const draft = createMinimalDraft();
    draft.providers.push(makeProvider("https://example.com/list.txt", Infinity));
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.errors.some((e) => e.code === "INVALID_PROVIDER_INTERVAL")).toBe(true);
  });
});

// ═════════════════════════════════════════════════════════════════════════════
// Fixture 36: Group URL missing hostname validation
// ═════════════════════════════════════════════════════════════════════════════

describe("Fixture 36: Group URL missing hostname", () => {
  it("rejects url-test URL without host (https://)", () => {
    const draft = createMinimalDraft();
    draft.groups.push({
      kind: "modeled",
      id: "g-0",
      originalIndex: 0,
      originalName: "Auto",
      name: "Auto",
      type: "url-test",
      proxies: null,
      includeAllProxies: true,
      filter: null,
      url: "https://",
      interval: 300,
      timeout: null,
      tolerance: null,
      unknownKeyCount: 0,
    });
    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "MATCH",
      policy: "DIRECT",
      noResolve: false,
      rawText: "MATCH,DIRECT",
    });
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.valid).toBe(false);
    const hasHostError = val.errors.some(
      (e) => e.code === "URL_TEST_URL_NO_HOST" || e.code === "INVALID_URL_TEST_URL",
    );
    expect(hasHostError).toBe(true);
  });

  it("rejects url-test URL that is just protocol (http://)", () => {
    const draft = createMinimalDraft();
    draft.groups.push({
      kind: "modeled",
      id: "g-0",
      originalIndex: 0,
      originalName: "Auto",
      name: "Auto",
      type: "url-test",
      proxies: null,
      includeAllProxies: true,
      filter: null,
      url: "http://",
      interval: 300,
      timeout: null,
      tolerance: null,
      unknownKeyCount: 0,
    });
    draft.rules.push({
      kind: "modeled",
      id: "r-0",
      originalIndex: 0,
      ruleType: "MATCH",
      policy: "DIRECT",
      noResolve: false,
      rawText: "MATCH,DIRECT",
    });
    const val = validateRuleProfileVisualDraft(draft);
    expect(val.valid).toBe(false);
    const hasHostError = val.errors.some(
      (e) => e.code === "URL_TEST_URL_NO_HOST" || e.code === "INVALID_URL_TEST_URL",
    );
    expect(hasHostError).toBe(true);
  });
});
