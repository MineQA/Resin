/**
 * ruleProfileVisualModel.ts — Phase 4 pure types for Rule Profile visual editing.
 *
 * Public API types consumed by React and other pure modules.
 * No YAML parser or React imports.
 */

// ─── Source fingerprint ───────────────────────────────────────────────────────

/** Deterministic lightweight content fingerprint (DJB2 hash, base-36). */
export type SourceFingerprint = string;

// ─── Visual draft (shared across Groups/Providers/Rules tabs) ─────────────────

/**
 * Shared visual draft derived from a single YAML snapshot.
 * All three section arrays are always populated together from one parse.
 */
export interface VisualDraft {
  sourceFingerprint: SourceFingerprint;
  groups: GroupItem[];
  providers: ProviderItem[];
  rules: RuleItem[];
  /** Section names that are blocked from visual editing (anchor/alias or wrong shape). */
  blockedSections: Array<'groups' | 'providers' | 'rules'>;
}

// ─── Group items ─────────────────────────────────────────────────────────────

export type GroupItem = ModeledGroup | RawGroup;

export interface ModeledGroup {
  kind: 'modeled';
  /** Stable identity for React keys (e.g. `"g-{originalIndex}"`). */
  id: string;
  /** Index in the original base document's proxy-groups sequence.
   *  Immutable provenance — reordering moves item objects without changing this. */
  originalIndex: number;
  /** The group name as parsed from the base, or null if this is a new item. */
  originalName: string | null;
  name: string;
  type: 'select' | 'url-test';
  /** Explicit member list ([] if none, null if absent). */
  proxies: string[] | null;
  includeAllProxies: boolean;
  /** null = absent */
  filter: string | null;
  /** url-test only. null when group type is select or absent. */
  url: string | null;
  interval: number | null;
  timeout: number | null;
  tolerance: number | null;
  /** Number of unknown/extra keys on this group that will be preserved. */
  unknownKeyCount: number;
}

export interface RawGroup {
  kind: 'raw';
  id: string;
  originalIndex: number;
  /** Display label (group name or fallback). */
  label: string;
  /** The raw Clash type string (e.g. "fallback", "load-balance"). */
  sourceType: string;
  /** Human-readable reason this item is raw. */
  reason: string;
  /** Original YAML source text if available as a string. */
  text: string | null;
}

// ─── Provider items ──────────────────────────────────────────────────────────

export type ProviderItem = ModeledProvider | RawProvider;

export interface ModeledProvider {
  kind: 'modeled';
  id: string;
  originalIndex: number;
  /** The provider key as parsed from the base mapping, or null if new. */
  originalKey: string | null;
  key: string;
  url: string;
  interval: number;
  /** Number of unknown keys preserved. */
  unknownKeyCount: number;
}

export interface RawProvider {
  kind: 'raw';
  id: string;
  originalIndex: number;
  /** Display key (the YAML map key for this provider). */
  label: string;
  /** The Source type string (e.g. "file", "http+classical+text"). */
  sourceType: string;
  /** Human-readable reason this item is raw. */
  reason: string;
  /** Original YAML source text if available as a string. */
  text: string | null;
}

// ─── Rule items ──────────────────────────────────────────────────────────────

export type RuleItem = ModeledRule | RawRule;

export interface ModeledRule {
  kind: 'modeled';
  id: string;
  originalIndex: number;
  ruleType: 'RULE-SET' | 'GEOIP' | 'MATCH';
  /** For RULE-SET: provider key. */
  provider?: string;
  /** For GEOIP: country code. */
  geoipCode?: string;
  /** Target policy (group name / DIRECT / REJECT). */
  policy: string;
  noResolve: boolean;
  /** Original text representation for display. */
  rawText: string;
}

export interface RawRule {
  kind: 'raw';
  id: string;
  originalIndex: number;
  /** Short display label (truncated to ~40 chars). */
  label: string;
  /** Rule type description. */
  sourceType: string;
  /** Human-readable reason this rule is raw. */
  reason: string;
  /** Original text if scalar, null if complex node. */
  text: string | null;
}

// ─── Parse result ────────────────────────────────────────────────────────────

export interface VisualParseResult {
  ok: boolean;
  draft: VisualDraft | null;
  /** Fatal errors that prevented parsing. */
  errors: VisualError[];
}

export interface VisualError {
  message: string;
  code?: string;
}

// ─── Validation ──────────────────────────────────────────────────────────────

export interface VisualValidationResult {
  valid: boolean;
  errors: VisualValidationError[];
  warnings: VisualValidationWarning[];
}

export interface VisualValidationError {
  section: 'groups' | 'providers' | 'rules' | 'general';
  itemId?: string;
  field?: string;
  message: string;
  code: string;
}

export interface VisualValidationWarning {
  section: 'groups' | 'providers' | 'rules' | 'general';
  itemId?: string;
  field?: string;
  message: string;
  code: string;
}

// ─── Fidelity report ─────────────────────────────────────────────────────────

export type FidelitySeverity = 'blocker' | 'warning' | 'info';

export interface FidelityIssue {
  severity: FidelitySeverity;
  section: 'groups' | 'providers' | 'rules' | 'general';
  itemId?: string;
  message: string;
  code: string;
}

export interface FidelityReport {
  issues: FidelityIssue[];
  hasBlocker: boolean;
}

// ─── Apply plan & result ─────────────────────────────────────────────────────

export interface VisualApplyPlan {
  canApply: boolean;
  fidelity: FidelityReport;
  validation: VisualValidationResult;
  /** Estimated changes for display. */
  stats: {
    groupsChanged: number;
    providersChanged: number;
    rulesChanged: number;
    /** Set of operation codes present: 'rename', 'delete', 'reorder', 'add', 'edit' */
    operations: string[];
  };
}

export interface VisualApplyResult {
  ok: boolean;
  yaml: string | null;
  errors: VisualError[];
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

/** Generate a stable item id from section prefix and original index. */
export function itemId(section: string, originalIndex: number): string {
  return `${section}-${originalIndex}`;
}

/** Simple content fingerprint (non-cryptographic). Uses DJB2 hash. */
export function computeFingerprint(source: string): SourceFingerprint {
  let hash = 5381;
  for (let i = 0; i < source.length; i++) {
    hash = ((hash << 5) + hash + source.charCodeAt(i)) | 0;
  }
  return (hash >>> 0).toString(36);
}

/** Compare two draft sections for semantic equality (used by no-op detection). */
export function draftsEqual(a: VisualDraft, b: VisualDraft): boolean {
  if (a === b) return true;
  if (a.groups.length !== b.groups.length) return false;
  if (a.providers.length !== b.providers.length) return false;
  if (a.rules.length !== b.rules.length) return false;
  for (let i = 0; i < a.groups.length; i++) {
    if (!groupItemsEqual(a.groups[i], b.groups[i])) return false;
  }
  for (let i = 0; i < a.providers.length; i++) {
    if (!providerItemsEqual(a.providers[i], b.providers[i])) return false;
  }
  for (let i = 0; i < a.rules.length; i++) {
    if (!ruleItemsEqual(a.rules[i], b.rules[i])) return false;
  }
  return true;
}

function groupItemsEqual(a: GroupItem, b: GroupItem): boolean {
  if (a.kind !== b.kind) return false;
  if (a.originalIndex !== b.originalIndex) return false;
  if (a.id !== b.id) return false;
  if (a.kind === 'raw' && b.kind === 'raw') return true; // raw = opaque, identity-mapped
  if (a.kind === 'modeled' && b.kind === 'modeled') {
    return (
      a.name === b.name &&
      a.type === b.type &&
      a.includeAllProxies === b.includeAllProxies &&
      a.filter === b.filter &&
      a.url === b.url &&
      a.interval === b.interval &&
      a.timeout === b.timeout &&
      a.tolerance === b.tolerance &&
      arraysEqual(a.proxies, b.proxies)
    );
  }
  return false;
}

function providerItemsEqual(a: ProviderItem, b: ProviderItem): boolean {
  if (a.kind !== b.kind) return false;
  if (a.originalIndex !== b.originalIndex) return false;
  if (a.id !== b.id) return false;
  if (a.kind === 'raw' && b.kind === 'raw') return true;
  if (a.kind === 'modeled' && b.kind === 'modeled') {
    return a.key === b.key && a.url === b.url && a.interval === b.interval;
  }
  return false;
}

function ruleItemsEqual(a: RuleItem, b: RuleItem): boolean {
  if (a.kind !== b.kind) return false;
  if (a.originalIndex !== b.originalIndex) return false;
  if (a.id !== b.id) return false;
  if (a.kind === 'raw' && b.kind === 'raw') return true;
  if (a.kind === 'modeled' && b.kind === 'modeled') {
    return a.ruleType === b.ruleType &&
      a.provider === b.provider &&
      a.geoipCode === b.geoipCode &&
      a.policy === b.policy &&
      a.noResolve === b.noResolve;
  }
  return false;
}

function arraysEqual<T>(a: T[] | null, b: T[] | null): boolean {
  if (a === b) return true;
  if (a == null || b == null) return false;
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}
