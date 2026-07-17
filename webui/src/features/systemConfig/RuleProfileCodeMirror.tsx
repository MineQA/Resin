import { yaml } from "@codemirror/lang-yaml";
import { linter, type Diagnostic } from "@codemirror/lint";
import { EditorView } from "@codemirror/view";
import CodeMirror from "@uiw/react-codemirror";
import { useMemo } from "react";

export type RuleProfileCodeMirrorProps = {
  id?: string;
  value: string;
  invalid?: boolean;
  readOnly?: boolean;
  /** Accessible name for the editor content (contenteditable is not labelable via htmlFor). */
  "aria-label"?: string;
  "aria-labelledby"?: string;
  /** External diagnostics (e.g. YAML parse error line hints). */
  diagnostics?: Diagnostic[];
  onChange: (value: string) => void;
};

const resinEditorTheme = EditorView.theme({
  "&": {
    backgroundColor: "var(--surface-sunken, rgba(0, 0, 0, 0.02))",
    color: "var(--text)",
    fontSize: "12px",
    borderRadius: "10px",
  },
  ".cm-content": {
    fontFamily:
      'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
    lineHeight: "1.55",
    caretColor: "var(--primary)",
    padding: "10px 0",
  },
  ".cm-gutters": {
    backgroundColor: "transparent",
    color: "var(--text-muted)",
    border: "none",
    borderRight: "1px solid var(--border)",
  },
  ".cm-activeLineGutter": {
    backgroundColor: "rgba(20, 112, 255, 0.06)",
  },
  ".cm-activeLine": {
    backgroundColor: "rgba(20, 112, 255, 0.04)",
  },
  "&.cm-focused": {
    outline: "none",
  },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "rgba(20, 112, 255, 0.18) !important",
  },
  ".cm-cursor": {
    borderLeftColor: "var(--primary)",
  },
  ".cm-scroller": {
    overflow: "auto",
  },
});

/**
 * CodeMirror 6 YAML editor for Rule Profile templates.
 * Loaded via React.lazy so it stays in a separate chunk (no Vite worker setup).
 */
export default function RuleProfileCodeMirror({
  id,
  value,
  invalid,
  readOnly,
  "aria-label": ariaLabel,
  "aria-labelledby": ariaLabelledBy,
  diagnostics = [],
  onChange,
}: RuleProfileCodeMirrorProps) {
  const extensions = useMemo(() => {
    const externalLinter = linter(() => diagnostics, { delay: 200 });
    const a11yAttrs: Record<string, string> = {};
    if (ariaLabel) {
      a11yAttrs["aria-label"] = ariaLabel;
    }
    if (ariaLabelledBy) {
      a11yAttrs["aria-labelledby"] = ariaLabelledBy;
    }
    return [
      yaml(),
      resinEditorTheme,
      EditorView.lineWrapping,
      externalLinter,
      EditorView.editable.of(!readOnly),
      ...(Object.keys(a11yAttrs).length > 0
        ? [EditorView.contentAttributes.of(a11yAttrs)]
        : []),
    ];
  }, [ariaLabel, ariaLabelledBy, diagnostics, readOnly]);

  return (
    <div
      id={id}
      className={`rule-profile-cm-shell${invalid ? " rule-profile-cm-shell-invalid" : ""}`}
      data-invalid={invalid ? "true" : undefined}
    >
      <CodeMirror
        value={value}
        height="420px"
        minHeight="360px"
        theme="light"
        basicSetup={{
          lineNumbers: true,
          foldGutter: true,
          highlightActiveLine: true,
          highlightActiveLineGutter: true,
          bracketMatching: true,
          autocompletion: false,
          searchKeymap: true,
        }}
        extensions={extensions}
        editable={!readOnly}
        readOnly={readOnly}
        onChange={onChange}
      />
    </div>
  );
}
