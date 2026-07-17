import type { Diagnostic } from "@codemirror/lint";
import { Component, lazy, Suspense, type ErrorInfo, type ReactNode } from "react";
import { Textarea } from "../../components/ui/Textarea";
import { useI18n } from "../../i18n";

export type RuleProfileYamlEditorProps = {
  id?: string;
  value: string;
  invalid?: boolean;
  readOnly?: boolean;
  /** Accessible name for both CodeMirror content and plain textarea fallback. */
  "aria-label"?: string;
  "aria-labelledby"?: string;
  diagnostics?: Diagnostic[];
  onChange: (value: string) => void;
};

const LazyCodeMirror = lazy(() => import("./RuleProfileCodeMirror"));

type BoundaryProps = {
  fallback: ReactNode;
  children: ReactNode;
};

type BoundaryState = {
  hasError: boolean;
};

/** Catches chunk load / runtime failures and falls back to plain textarea. */
class EditorErrorBoundary extends Component<BoundaryProps, BoundaryState> {
  state: BoundaryState = { hasError: false };

  static getDerivedStateFromError(): BoundaryState {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.warn("Rule Profile YAML editor failed; using plain-text fallback.", error, info.componentStack);
  }

  render() {
    if (this.state.hasError) {
      return this.props.fallback;
    }
    return this.props.children;
  }
}

function PlainYamlFallback({
  id,
  value,
  invalid,
  readOnly,
  "aria-label": ariaLabel,
  "aria-labelledby": ariaLabelledBy,
  onChange,
  notice,
}: RuleProfileYamlEditorProps & { notice?: string }) {
  return (
    <div className="rule-profile-yaml-fallback">
      {notice ? <p className="muted rule-profile-yaml-fallback-notice">{notice}</p> : null}
      <Textarea
        id={id}
        className="rule-profile-template-editor"
        rows={24}
        value={value}
        invalid={invalid}
        readOnly={readOnly}
        spellCheck={false}
        aria-label={ariaLabel}
        aria-labelledby={ariaLabelledBy}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}

/**
 * Lazy YAML editor with a robust plain-text fallback.
 * CodeMirror ships in a separate chunk; no Monaco / Vite worker setup.
 */
export function RuleProfileYamlEditor(props: RuleProfileYamlEditorProps) {
  const { t } = useI18n();
  const loadingFallback = (
    <PlainYamlFallback
      {...props}
      notice={t("正在加载 YAML 编辑器…")}
    />
  );
  const errorFallback = (
    <PlainYamlFallback
      {...props}
      notice={t("高级编辑器不可用，已切换为纯文本编辑。")}
    />
  );

  return (
    <EditorErrorBoundary fallback={errorFallback}>
      <Suspense fallback={loadingFallback}>
        <LazyCodeMirror {...props} />
      </Suspense>
    </EditorErrorBoundary>
  );
}
