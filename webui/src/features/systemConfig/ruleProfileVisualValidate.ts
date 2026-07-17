/**
 * ruleProfileVisualValidate.ts — Client-side validation/advisories for VisualDraft.
 *
 * Does NOT replace server-side validation.
 * Checks are designed to catch common mistakes before Apply.
 * Missing MATCH is a blocker error (not a warning).
 */

import {
  type VisualDraft,
  type VisualValidationResult,
  type VisualValidationError,
  type VisualValidationWarning,
} from "./ruleProfileVisualModel";

// ─── Public API ──────────────────────────────────────────────────────────────

export function validateRuleProfileVisualDraft(draft: VisualDraft): VisualValidationResult {
  const errors: VisualValidationError[] = [];
  const warnings: VisualValidationWarning[] = [];

  validateGroupNames(draft, errors);
  validateGroupFields(draft, errors, warnings);
  validateProviderKeys(draft, errors);
  validateProviderFields(draft, errors);
  validateModeledRules(draft, errors);
  validateStaleReferences(draft, errors, warnings);

  return {
    valid: errors.length === 0,
    errors,
    warnings,
  };
}

// ─── Group validation ────────────────────────────────────────────────────────

function validateGroupNames(draft: VisualDraft, errors: VisualValidationError[]): void {
  const names = new Map<string, string>(); // name → first itemId

  // Include raw group labels for collision detection
  for (const item of draft.groups) {
    if (item.kind === "modeled") {
      const name = item.name.trim();
      if (!name) {
        errors.push({
          section: "groups",
          itemId: item.id,
          field: "name",
          message: "Group name must not be empty",
          code: "EMPTY_GROUP_NAME",
        });
        continue;
      }
      if (names.has(name)) {
        errors.push({
          section: "groups",
          itemId: item.id,
          field: "name",
          message: `Duplicate group name: "${name}"`,
          code: "DUPLICATE_GROUP_NAME",
        });
      } else {
        names.set(name, item.id);
      }
    } else {
      // Raw group — use its label for duplicate detection
      const label = item.label.trim();
      if (label && !names.has(label)) {
        names.set(label, item.id);
      }
    }
  }
}

function validateGroupFields(
  draft: VisualDraft,
  errors: VisualValidationError[],
  warnings: VisualValidationWarning[],
): void {
  for (const item of draft.groups) {
    if (item.kind !== "modeled") continue;

    // url-test requires url
    if (item.type === "url-test") {
      if (!item.url || item.url.trim() === "") {
        errors.push({
          section: "groups",
          itemId: item.id,
          field: "url",
          message: `url-test group "${item.name}" requires a URL`,
          code: "MISSING_URL_TEST_URL",
        });
      } else {
        // Validate URL: must be absolute http/https with host
        try {
          const parsed = new URL(item.url);
          if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
            errors.push({
              section: "groups",
              itemId: item.id,
              field: "url",
              message: `url-test group "${item.name}" URL must use http or https`,
              code: "URL_TEST_URL_INVALID_PROTOCOL",
            });
          }
          if (!parsed.hostname) {
            errors.push({
              section: "groups",
              itemId: item.id,
              field: "url",
              message: `url-test group "${item.name}" URL must have a host`,
              code: "URL_TEST_URL_NO_HOST",
            });
          }
        } catch {
          errors.push({
            section: "groups",
            itemId: item.id,
            field: "url",
            message: `url-test group "${item.name}" URL is not valid`,
            code: "INVALID_URL_TEST_URL",
          });
        }
      }
    }

    // url-test: interval must be positive and finite
    if (item.type === "url-test" && item.interval != null && (item.interval <= 0 || !Number.isFinite(item.interval))) {
      errors.push({
        section: "groups",
        itemId: item.id,
        field: "interval",
        message: `url-test group "${item.name}" interval must be a positive finite number`,
        code: "INVALID_URL_TEST_INTERVAL",
      });
    }

    // url-test: timeout must be positive and finite
    if (item.type === "url-test" && item.timeout != null && (item.timeout <= 0 || !Number.isFinite(item.timeout))) {
      errors.push({
        section: "groups",
        itemId: item.id,
        field: "timeout",
        message: `url-test group "${item.name}" timeout must be a positive finite number`,
        code: "INVALID_URL_TEST_TIMEOUT",
      });
    }

    // url-test: tolerance must be non-negative and finite
    if (item.type === "url-test" && item.tolerance != null && (item.tolerance < 0 || !Number.isFinite(item.tolerance))) {
      errors.push({
        section: "groups",
        itemId: item.id,
        field: "tolerance",
        message: `url-test group "${item.name}" tolerance must be a non-negative finite number`,
        code: "INVALID_URL_TEST_TOLERANCE",
      });
    }

    // filter without include-all-proxies: unusual but not an error
    if (item.filter && !item.includeAllProxies) {
      warnings.push({
        section: "groups",
        itemId: item.id,
        field: "filter",
        message: `Group "${item.name}" has a filter without include-all-proxies`,
        code: "FILTER_WITHOUT_INCLUDE_ALL",
      });
    }

    // Empty proxies and no include-all-proxies
    if (item.proxies != null && item.proxies.length === 0 && !item.includeAllProxies) {
      warnings.push({
        section: "groups",
        itemId: item.id,
        field: "proxies",
        message: `Group "${item.name}" has no proxies and no include-all-proxies`,
        code: "EMPTY_GROUP_NO_INCLUDE_ALL",
      });
    }
  }
}

// ─── Provider validation ─────────────────────────────────────────────────────

function validateProviderKeys(draft: VisualDraft, errors: VisualValidationError[]): void {
  const keys = new Map<string, string>();

  // Include raw provider labels for collision detection
  for (const item of draft.providers) {
    if (item.kind === "modeled") {
      const key = item.key.trim();
      if (!key) {
        errors.push({
          section: "providers",
          itemId: item.id,
          field: "key",
          message: "Provider key must not be empty",
          code: "EMPTY_PROVIDER_KEY",
        });
        continue;
      }
      if (keys.has(key)) {
        errors.push({
          section: "providers",
          itemId: item.id,
          field: "key",
          message: `Duplicate provider key: "${key}"`,
          code: "DUPLICATE_PROVIDER_KEY",
        });
      } else {
        keys.set(key, item.id);
      }
    } else {
      const label = item.label.trim();
      if (label && !keys.has(label)) {
        keys.set(label, item.id);
      }
    }
  }
}

function validateProviderFields(
  draft: VisualDraft,
  errors: VisualValidationError[],
): void {
  for (const item of draft.providers) {
    if (item.kind !== "modeled") continue;

    const url = item.url.trim();

    if (!url) {
      errors.push({
        section: "providers",
        itemId: item.id,
        field: "url",
        message: `Provider "${item.key}" URL must not be empty`,
        code: "EMPTY_PROVIDER_URL",
      });
      continue;
    }

    // Must be absolute HTTPS URL with host
    try {
      const parsed = new URL(url);
      if (parsed.protocol !== "https:") {
        errors.push({
          section: "providers",
          itemId: item.id,
          field: "url",
          message: `Provider "${item.key}" URL must use HTTPS`,
          code: "PROVIDER_URL_NOT_HTTPS",
        });
      }
      if (!parsed.hostname) {
        errors.push({
          section: "providers",
          itemId: item.id,
          field: "url",
          message: `Provider "${item.key}" URL must have a host`,
          code: "PROVIDER_URL_NO_HOST",
        });
      }
      if (parsed.username || parsed.password) {
        errors.push({
          section: "providers",
          itemId: item.id,
          field: "url",
          message: `Provider "${item.key}" URL must not contain userinfo`,
          code: "PROVIDER_URL_HAS_USERINFO",
        });
      }
      if (parsed.hash) {
        errors.push({
          section: "providers",
          itemId: item.id,
          field: "url",
          message: `Provider "${item.key}" URL must not contain a fragment`,
          code: "PROVIDER_URL_HAS_FRAGMENT",
        });
      }
    } catch {
      errors.push({
        section: "providers",
        itemId: item.id,
        field: "url",
        message: `Provider "${item.key}" URL is not valid`,
        code: "INVALID_PROVIDER_URL",
      });
    }

    if (item.interval <= 0 || !Number.isFinite(item.interval)) {
      errors.push({
        section: "providers",
        itemId: item.id,
        field: "interval",
        message: `Provider "${item.key}" interval must be a positive finite number`,
        code: "INVALID_PROVIDER_INTERVAL",
      });
    }
  }
}

// ─── Rule validation ─────────────────────────────────────────────────────────

function validateModeledRules(
  draft: VisualDraft,
  errors: VisualValidationError[],
): void {
  let matchCount = 0;
  let matchIndex = -1;
  const totalRules = draft.rules.length;

  for (let i = 0; i < draft.rules.length; i++) {
    const item = draft.rules[i];
    if (item.kind !== "modeled") continue;

    if (item.ruleType === "MATCH") {
      matchCount++;
      matchIndex = i;

      // MATCH policy must be non-empty
      if (!item.policy || item.policy.trim() === "") {
        errors.push({
          section: "rules",
          itemId: item.id,
          field: "policy",
          message: "MATCH rule must have a non-empty policy",
          code: "EMPTY_MATCH_POLICY",
        });
      }
    }

    // RULE-SET: provider must be non-empty
    if (item.ruleType === "RULE-SET") {
      if (!item.provider || item.provider.trim() === "") {
        errors.push({
          section: "rules",
          itemId: item.id,
          field: "provider",
          message: "RULE-SET must have a non-empty provider",
          code: "EMPTY_RULE_SET_PROVIDER",
        });
      }
      if (!item.policy || item.policy.trim() === "") {
        errors.push({
          section: "rules",
          itemId: item.id,
          field: "policy",
          message: "RULE-SET must have a non-empty policy",
          code: "EMPTY_RULE_SET_POLICY",
        });
      }
    }

    // GEOIP: code must be non-empty
    if (item.ruleType === "GEOIP") {
      if (!item.geoipCode || item.geoipCode.trim() === "") {
        errors.push({
          section: "rules",
          itemId: item.id,
          field: "geoipCode",
          message: "GEOIP must have a non-empty country code",
          code: "EMPTY_GEOIP_CODE",
        });
      }
      if (!item.policy || item.policy.trim() === "") {
        errors.push({
          section: "rules",
          itemId: item.id,
          field: "policy",
          message: "GEOIP must have a non-empty policy",
          code: "EMPTY_GEOIP_POLICY",
        });
      }
    }
  }

  // Check for duplicate MATCH
  if (matchCount > 1) {
    errors.push({
      section: "rules",
      message: `Found ${matchCount} MATCH rules; only one is allowed`,
      code: "DUPLICATE_MATCH",
    });
  }

  // Check MATCH is last (before any raw rules too — raw rules after MATCH would make it non-terminal)
  if (matchCount === 1 && matchIndex < totalRules - 1) {
    errors.push({
      section: "rules",
      itemId: draft.rules[matchIndex]?.id,
      message: "MATCH must be the last rule",
      code: "MATCH_NOT_LAST",
    });
  }

  // Missing MATCH is a BLOCKER error (not a warning)
  if (matchCount === 0 && totalRules > 0) {
    errors.push({
      section: "rules",
      message: "No MATCH rule found; a MATCH rule is required for valid configuration",
      code: "MISSING_MATCH",
    });
  }
}

// ─── Stale reference checks ───────────────────────────────────────────────────

function validateStaleReferences(
  draft: VisualDraft,
  _errors: VisualValidationError[],
  warnings: VisualValidationWarning[],
): void {
  const modeledGroupNames = new Set<string>();
  const allGroupNames = new Set<string>(); // includes raw labels
  const modeledProviderKeys = new Set<string>();
  const allProviderKeys = new Set<string>(); // includes raw labels

  for (const g of draft.groups) {
    if (g.kind === "modeled") {
      modeledGroupNames.add(g.name);
      allGroupNames.add(g.name);
    } else {
      allGroupNames.add(g.label);
    }
  }
  for (const p of draft.providers) {
    if (p.kind === "modeled") {
      modeledProviderKeys.add(p.key);
      allProviderKeys.add(p.key);
    } else {
      allProviderKeys.add(p.label);
    }
  }

  // Check rule RULE-SET providers against ALL providers
  for (const r of draft.rules) {
    if (r.kind === "modeled" && r.ruleType === "RULE-SET" && r.provider) {
      if (!allProviderKeys.has(r.provider)) {
        if (modeledProviderKeys.size > 0) {
          warnings.push({
            section: "rules",
            itemId: r.id,
            field: "provider",
            message: `RULE-SET references provider "${r.provider}" which is not defined`,
            code: "RULE_SET_STALE_PROVIDER",
          });
        }
      }
    }
  }

  // Check modeled group proxies' member list for stale group names
  for (const g of draft.groups) {
    if (g.kind !== "modeled") continue;
    if (g.proxies == null) continue;
    for (const member of g.proxies) {
      if (
        member !== "DIRECT" &&
        member !== "REJECT" &&
        member !== "REJECT-DROP" &&
        member !== "PASS" &&
        !allGroupNames.has(member) &&
        modeledGroupNames.size > 0
      ) {
        warnings.push({
          section: "groups",
          itemId: g.id,
          field: "proxies",
          message: `Group "${g.name}" proxies list references "${member}" which is not a known group`,
          code: "GROUP_MEMBER_STALE_REFERENCE",
        });
      }
    }
  }

  // Check rule policy references known group names
  for (const r of draft.rules) {
    if (r.kind === "modeled" && r.policy) {
      const policy = r.policy.trim();
      if (
        policy !== "DIRECT" &&
        policy !== "REJECT" &&
        policy !== "REJECT-DROP" &&
        policy !== "PASS" &&
        !allGroupNames.has(policy) &&
        modeledGroupNames.size > 0
      ) {
        warnings.push({
          section: "rules",
          itemId: r.id,
          field: "policy",
          message: `Rule policy "${policy}" does not match any known group name`,
          code: "RULE_POLICY_STALE_REFERENCE",
        });
      }
    }
  }
}
