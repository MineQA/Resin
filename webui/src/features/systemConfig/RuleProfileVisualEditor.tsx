import {
  AlertTriangle,
  FileCode2,
  Layers,
  ListOrdered,
  RefreshCw,
  Save,
  Boxes,
} from "lucide-react";
import {
  useEffect,
  useMemo,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { Button } from "../../components/ui/Button";
import { useI18n } from "../../i18n";
import { applyRuleProfileVisualDraft } from "./ruleProfileVisualApply";
import { planRuleProfileVisualApply } from "./ruleProfileVisualFidelity";
import { localizeVisualIssue } from "./ruleProfileVisualI18n";
import type { VisualApplyPlan, VisualDraft } from "./ruleProfileVisualModel";
import { draftsEqual } from "./ruleProfileVisualModel";
import type { EditorTab, VisualEditorState } from "./ruleProfileVisualState";
import {
  applyPlanContextMatches,
  captureApplyPlanContext,
  countRawItems,
  type ApplyPlanContext,
} from "./ruleProfileVisualUi";
import { RuleProfileFidelityPanel } from "./RuleProfileFidelityPanel";
import { RuleProfileGroupsPanel } from "./RuleProfileGroupsPanel";
import { RuleProfileProvidersPanel } from "./RuleProfileProvidersPanel";
import { RuleProfileRulesPanel } from "./RuleProfileRulesPanel";

const TAB_ORDER: EditorTab[] = ["yaml", "groups", "providers", "rules"];

type RuleProfileVisualEditorProps = {
  activeTab: EditorTab;
  onTabChange: (tab: EditorTab) => void;
  yamlTab: ReactNode;
  visual: VisualEditorState;
  baselineDraft: VisualDraft | null;
  onVisualChange: (next: VisualEditorState) => void;
  onReloadFromYaml: () => void;
  onAppliedYaml: (yaml: string) => void;
  showToast: (tone: "success" | "error", text: string) => void;
  disabled?: boolean;
  templateYAML: string;
};

export function RuleProfileVisualEditor({
  activeTab,
  onTabChange,
  yamlTab,
  visual,
  baselineDraft,
  onVisualChange,
  onReloadFromYaml,
  onAppliedYaml,
  showToast,
  disabled,
  templateYAML,
}: RuleProfileVisualEditorProps) {
  const { t } = useI18n();
  const [applyPlan, setApplyPlan] = useState<VisualApplyPlan | null>(null);
  const [planContext, setPlanContext] = useState<ApplyPlanContext | null>(null);
  const [applyPending, setApplyPending] = useState(false);

  const clearApplyPlan = () => {
    setApplyPlan(null);
    setPlanContext(null);
  };

  // Drop obsolete plans when live context no longer matches the snapshot recorded at open.
  // On the render immediately after openApply, contexts match so the plan stays visible.
  useEffect(() => {
    if (!applyPlan || !planContext) {
      return;
    }
    if (!applyPlanContextMatches(planContext, visual.draft, templateYAML, visual.stale)) {
      clearApplyPlan();
    }
  }, [applyPlan, planContext, visual.draft, templateYAML, visual.stale]);

  const rawCount = visual.draft ? countRawItems(visual.draft) : 0;
  const fidelityBlockers = applyPlan?.fidelity.issues.filter((i) => i.severity === "blocker").length ?? 0;
  const fidelityWarnings = applyPlan?.fidelity.issues.filter((i) => i.severity === "warning").length ?? 0;

  const liveValidation = useMemo(() => {
    if (!visual.draft || visual.stale || activeTab === "yaml") {
      return null;
    }
    return planRuleProfileVisualApply(templateYAML, visual.draft);
  }, [visual.draft, visual.stale, activeTab, templateYAML]);

  const tabs: Array<{ id: EditorTab; label: string; icon: ReactNode }> = [
    { id: "yaml", label: t("YAML"), icon: <FileCode2 size={14} /> },
    { id: "groups", label: t("分组"), icon: <Layers size={14} /> },
    { id: "providers", label: t("Provider"), icon: <Boxes size={14} /> },
    { id: "rules", label: t("规则"), icon: <ListOrdered size={14} /> },
  ];

  const openApply = () => {
    if (!visual.draft) {
      showToast("error", t("请先从 YAML 加载可视化草稿。"));
      return;
    }
    if (visual.stale) {
      showToast("error", t("可视化草稿已过期。请先从 YAML 重新加载。"));
      return;
    }
    const plan = planRuleProfileVisualApply(templateYAML, visual.draft);
    setPlanContext(captureApplyPlanContext(visual.draft, templateYAML, false));
    setApplyPlan(plan);
  };

  const confirmApply = () => {
    if (!visual.draft || !applyPlan) {
      return;
    }
    if (!applyPlanContextMatches(planContext, visual.draft, templateYAML, visual.stale)) {
      showToast("error", t("可视化草稿已过期。请先从 YAML 重新加载。"));
      clearApplyPlan();
      return;
    }
    if (!applyPlan.canApply) {
      return;
    }
    setApplyPending(true);
    try {
      const result = applyRuleProfileVisualDraft(templateYAML, visual.draft, applyPlan);
      if (!result.ok || result.yaml == null) {
        const first = result.errors[0];
        const message = first
          ? localizeVisualIssue(t, { code: first.code, message: first.message })
          : t("应用可视化失败");
        showToast("error", message);
        return;
      }
      onAppliedYaml(result.yaml);
      clearApplyPlan();
      showToast("success", t("已应用到本地 YAML（尚未保存）"));
    } finally {
      setApplyPending(false);
    }
  };

  const onDraftEdit = (nextDraft: VisualDraft) => {
    const dirty = baselineDraft ? !draftsEqual(nextDraft, baselineDraft) : true;
    onVisualChange({
      ...visual,
      draft: nextDraft,
      dirty,
      parseErrors: [],
    });
    // Immediate clear for snappy UX; effect also guards against external context drift.
    clearApplyPlan();
  };

  const onTabKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const key = event.key;
    if (key !== "ArrowLeft" && key !== "ArrowRight" && key !== "Home" && key !== "End") {
      return;
    }
    event.preventDefault();
    const current = TAB_ORDER.indexOf(activeTab);
    let nextIndex = current;
    if (key === "ArrowLeft") {
      nextIndex = current <= 0 ? TAB_ORDER.length - 1 : current - 1;
    } else if (key === "ArrowRight") {
      nextIndex = current >= TAB_ORDER.length - 1 ? 0 : current + 1;
    } else if (key === "Home") {
      nextIndex = 0;
    } else if (key === "End") {
      nextIndex = TAB_ORDER.length - 1;
    }
    const nextTab = TAB_ORDER[nextIndex];
    if (!nextTab) {
      return;
    }
    onTabChange(nextTab);
    window.requestAnimationFrame(() => {
      const el = document.getElementById(`rule-profile-tab-${nextTab}`);
      el?.focus();
    });
  };

  const visualDisabled = Boolean(disabled || visual.stale || !visual.draft);

  return (
    <div className="rule-profile-visual-editor">
      <div
        className="rule-profile-editor-tabs"
        role="tablist"
        aria-label={t("Rule Profile 编辑模式")}
        onKeyDown={onTabKeyDown}
      >
        {tabs.map((tab) => {
          const selected = activeTab === tab.id;
          return (
            <button
              key={tab.id}
              type="button"
              role="tab"
              id={`rule-profile-tab-${tab.id}`}
              aria-selected={selected}
              aria-controls={`rule-profile-tabpanel-${tab.id}`}
              tabIndex={selected ? 0 : -1}
              className={`rule-profile-editor-tab${selected ? " is-active" : ""}`}
              onClick={() => onTabChange(tab.id)}
            >
              {tab.icon}
              {tab.label}
            </button>
          );
        })}
      </div>

      {activeTab !== "yaml" ? (
        <div className="rule-profile-visual-toolbar">
          <div className="rule-profile-stat-row rule-profile-visual-status">
            {visual.dirty ? (
              <span className="rule-profile-stat-chip rule-profile-chip-dirty">{t("可视化未应用")}</span>
            ) : null}
            {visual.stale ? (
              <span className="rule-profile-stat-chip rule-profile-chip-stale">{t("YAML 已变 · 草稿过期")}</span>
            ) : null}
            {!visual.dirty && !visual.stale && visual.draft ? (
              <span className="rule-profile-stat-chip">{t("可视化已同步")}</span>
            ) : null}
            {rawCount > 0 ? (
              <span className="rule-profile-stat-chip">{t("原始项 {{count}}", { count: rawCount })}</span>
            ) : null}
            {liveValidation && !liveValidation.validation.valid ? (
              <span className="rule-profile-stat-chip rule-profile-chip-stale">
                {t("校验错误 {{count}}", { count: liveValidation.validation.errors.length })}
              </span>
            ) : null}
            {applyPlan && fidelityBlockers > 0 ? (
              <span className="rule-profile-stat-chip rule-profile-chip-stale">
                {t("阻断 {{count}}", { count: fidelityBlockers })}
              </span>
            ) : null}
            {applyPlan && fidelityWarnings > 0 ? (
              <span className="rule-profile-stat-chip">
                {t("警告 {{count}}", { count: fidelityWarnings })}
              </span>
            ) : null}
          </div>
          <div className="rule-profile-visual-toolbar-actions">
            <Button variant="secondary" size="sm" onClick={onReloadFromYaml} disabled={disabled}>
              <RefreshCw size={14} />
              {t("从 YAML 重新加载")}
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={openApply}
              disabled={disabled || !visual.draft || visual.stale}
            >
              <Save size={14} />
              {t("应用可视化到 YAML（尚未保存）")}
            </Button>
          </div>
        </div>
      ) : null}

      {activeTab !== "yaml" && visual.stale ? (
        <div className="callout callout-warning" role="status">
          <AlertTriangle size={14} />
          <span>
            {t("YAML 已在可视化之后被修改。草稿已过期，不能应用。请从 YAML 重新加载可视化。")}
          </span>
        </div>
      ) : null}

      {activeTab !== "yaml" && visual.parseErrors.length > 0 ? (
        <div className="callout callout-error" role="alert">
          <AlertTriangle size={14} />
          <div>
            <strong>{t("无法解析可视化草稿")}</strong>
            <ul className="rule-profile-parse-error-list">
              {visual.parseErrors.map((err) => (
                <li key={err}>{err}</li>
              ))}
            </ul>
            <p className="muted">{t("请先在 YAML 页签修复解析错误，再重新加载。")}</p>
          </div>
        </div>
      ) : null}

      {applyPlan && activeTab !== "yaml" ? (
        <RuleProfileFidelityPanel
          plan={applyPlan}
          applyPending={applyPending}
          onCancel={clearApplyPlan}
          onConfirm={confirmApply}
        />
      ) : null}

      {activeTab === "yaml" ? (
        <div role="tabpanel" id="rule-profile-tabpanel-yaml" aria-labelledby="rule-profile-tab-yaml">
          {yamlTab}
        </div>
      ) : null}

      {activeTab === "groups" && visual.draft ? (
        <RuleProfileGroupsPanel
          draft={visual.draft}
          disabled={visualDisabled}
          onChange={onDraftEdit}
        />
      ) : null}

      {activeTab === "providers" && visual.draft ? (
        <RuleProfileProvidersPanel
          draft={visual.draft}
          disabled={visualDisabled}
          onChange={onDraftEdit}
        />
      ) : null}

      {activeTab === "rules" && visual.draft ? (
        <RuleProfileRulesPanel
          draft={visual.draft}
          disabled={visualDisabled}
          onChange={onDraftEdit}
        />
      ) : null}

      {activeTab !== "yaml" && !visual.draft && visual.parseErrors.length === 0 ? (
        <div className="empty-box rule-profile-visual-empty">
          <p>{t("尚未加载可视化草稿。点击「从 YAML 重新加载」。")}</p>
          <Button variant="secondary" size="sm" onClick={onReloadFromYaml} disabled={disabled}>
            <RefreshCw size={14} />
            {t("从 YAML 重新加载")}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
