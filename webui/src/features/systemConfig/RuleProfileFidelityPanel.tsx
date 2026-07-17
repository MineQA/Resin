import { AlertTriangle, Info, ShieldAlert } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { useI18n } from "../../i18n";
import { localizeVisualIssue } from "./ruleProfileVisualI18n";
import type {
  FidelityIssue,
  VisualApplyPlan,
  VisualValidationError,
  VisualValidationWarning,
} from "./ruleProfileVisualModel";

type RuleProfileFidelityPanelProps = {
  plan: VisualApplyPlan;
  onCancel: () => void;
  onConfirm: () => void;
  applyPending?: boolean;
};

export function RuleProfileFidelityPanel({
  plan,
  onCancel,
  onConfirm,
  applyPending,
}: RuleProfileFidelityPanelProps) {
  const { t } = useI18n();
  const blockers = plan.fidelity.issues.filter((i) => i.severity === "blocker");
  const warnings = plan.fidelity.issues.filter((i) => i.severity === "warning");
  const infos = plan.fidelity.issues.filter((i) => i.severity === "info");
  const validationErrors = plan.validation.errors;
  const validationWarnings = plan.validation.warnings;
  const canConfirm = plan.canApply && !applyPending;

  const loc = (issue: { code?: string; message: string }) => localizeVisualIssue(t, issue);

  return (
    <div className="rule-profile-fidelity-panel" role="region" aria-labelledby="rule-profile-fidelity-heading">
      <div className="rule-profile-fidelity-panel-head">
        <strong id="rule-profile-fidelity-heading">{t("应用可视化到 YAML — 保真报告")}</strong>
        <span className="muted">{t("尚未保存到服务器")}</span>
      </div>

      <p className="muted rule-profile-fidelity-lead">
        {t(
          "将用当前可视化草稿覆盖 YAML 中的 proxy-groups、rule-providers、rules。其他顶层键尽量保留。注释/锚点/格式可能变化；原始/不支持项仅 YAML 可精确编辑。确认后只更新本地 YAML，仍需创建/保存才会写入服务器。",
        )}
      </p>

      <div className="rule-profile-stat-row">
        <span className="rule-profile-stat-chip">
          {t("组变更 {{count}}", { count: plan.stats.groupsChanged })}
        </span>
        <span className="rule-profile-stat-chip">
          {t("Provider 变更 {{count}}", { count: plan.stats.providersChanged })}
        </span>
        <span className="rule-profile-stat-chip">
          {t("规则变更 {{count}}", { count: plan.stats.rulesChanged })}
        </span>
        {plan.stats.operations.length > 0 ? (
          <span className="rule-profile-stat-chip">
            {t("操作：{{ops}}", { ops: plan.stats.operations.join(", ") })}
          </span>
        ) : (
          <span className="rule-profile-stat-chip">{t("无语义变更（可原样回写）")}</span>
        )}
      </div>

      {validationErrors.length > 0 ? (
        <IssueList
          title={t("校验错误（{{count}}）— 阻止应用", { count: validationErrors.length })}
          tone="blocker"
          items={validationErrors.map((e: VisualValidationError) => loc(e))}
        />
      ) : null}

      {blockers.length > 0 ? (
        <IssueList
          title={t("保真阻断（{{count}}）", { count: blockers.length })}
          tone="blocker"
          items={blockers.map((i: FidelityIssue) => loc(i))}
          icon
        />
      ) : null}

      {validationWarnings.length > 0 ? (
        <IssueList
          title={t("校验警告（{{count}}）", { count: validationWarnings.length })}
          tone="warning"
          items={validationWarnings.map((w: VisualValidationWarning) => loc(w))}
        />
      ) : null}

      {warnings.length > 0 ? (
        <IssueList
          title={t("保真警告（{{count}}）— 请审阅后确认", { count: warnings.length })}
          tone="warning"
          items={warnings.map((i: FidelityIssue) => loc(i))}
          icon
        />
      ) : null}

      {infos.length > 0 ? (
        <IssueList
          title={t("保真信息（{{count}}）", { count: infos.length })}
          tone="info"
          items={infos.map((i: FidelityIssue) => loc(i))}
          icon
        />
      ) : null}

      {blockers.length === 0
        && warnings.length === 0
        && infos.length === 0
        && validationErrors.length === 0
        && validationWarnings.length === 0 ? (
        <p className="muted">{t("未发现额外保真提示；仍请确认再应用。")}</p>
      ) : null}

      <div className="rule-profile-fidelity-actions">
        <Button variant="ghost" size="sm" onClick={onCancel} disabled={applyPending}>
          {t("取消")}
        </Button>
        <Button variant="primary" size="sm" onClick={onConfirm} disabled={!canConfirm}>
          {applyPending ? t("应用中...") : t("确认应用（尚未保存）")}
        </Button>
      </div>
      {!plan.canApply ? (
        <p className="field-error rule-profile-fidelity-blocked">
          {t("存在阻断项，无法应用。请修复校验错误或回到 YAML 处理不支持结构。")}
        </p>
      ) : null}
    </div>
  );
}

function IssueList({
  title,
  tone,
  items,
  icon,
}: {
  title: string;
  tone: "blocker" | "warning" | "info";
  items: string[];
  icon?: boolean;
}) {
  return (
    <div className={`rule-profile-fidelity-list rule-profile-fidelity-list-${tone}`}>
      <strong>{title}</strong>
      <ul>
        {items.map((message) => (
          <li key={message}>
            {icon ? (
              <span className="rule-profile-fidelity-icon" aria-hidden>
                {severityIcon(tone)}
              </span>
            ) : null}
            <span>{message}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function severityIcon(tone: "blocker" | "warning" | "info") {
  if (tone === "blocker") {
    return <ShieldAlert size={12} />;
  }
  if (tone === "warning") {
    return <AlertTriangle size={12} />;
  }
  return <Info size={12} />;
}
