/**
 * Visual editor state helpers (non-component module for react-refresh).
 */

import type { VisualDraft } from "./ruleProfileVisualModel";
import { parseRuleProfileVisualDraft } from "./ruleProfileVisualParse";

export type EditorTab = "yaml" | "groups" | "providers" | "rules";

export type VisualEditorState = {
  draft: VisualDraft | null;
  /** Fingerprint of YAML when draft was last loaded/applied. */
  loadedFingerprint: string | null;
  parseErrors: string[];
  /** True when draft differs from the snapshot at load/apply time. */
  dirty: boolean;
  /** True when templateYAML no longer matches loadedFingerprint. */
  stale: boolean;
};

export const EMPTY_VISUAL_STATE: VisualEditorState = {
  draft: null,
  loadedFingerprint: null,
  parseErrors: [],
  dirty: false,
  stale: false,
};

export function loadVisualStateFromYaml(yaml: string): VisualEditorState {
  const result = parseRuleProfileVisualDraft(yaml);
  if (!result.ok || !result.draft) {
    return {
      draft: null,
      loadedFingerprint: null,
      parseErrors: result.errors.map((e) => e.message),
      dirty: false,
      stale: false,
    };
  }
  return {
    draft: result.draft,
    loadedFingerprint: result.draft.sourceFingerprint,
    parseErrors: [],
    dirty: false,
    stale: false,
  };
}
