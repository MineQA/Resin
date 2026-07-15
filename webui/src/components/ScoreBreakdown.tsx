import { AlertTriangle, Info } from "lucide-react";
import { Badge } from "./ui/Badge";
import { useI18n } from "../i18n";
import {
  CF_STATUS_TOKENS,
  DIMENSION_KEYS,
  cfStatusBadgeVariant,
  cfStatusDescription,
  cfStatusLabel,
  dimensionLabel,
  gradeBadgeVariant,
  isCFContradiction,
  type CloudflareStatusToken,
  type ScoreBreakdown,
} from "../lib/cloudflareStatus";

// ---------------------------------------------------------------------------
// Status badge with grounded copy
// ---------------------------------------------------------------------------

export function CloudflareStatusBadge({
  status,
  compact = false,
}: {
  status: CloudflareStatusToken;
  compact?: boolean;
}) {
  const { t } = useI18n();
  const variant = cfStatusBadgeVariant(status);
  const label = t(cfStatusLabel(status));
  const title = compact ? t(cfStatusDescription(status)) : undefined;
  return (
    <Badge variant={variant} title={title} style={compact ? { fontSize: "0.65rem", padding: "2px 5px" } : undefined}>
      {label}
    </Badge>
  );
}

// ---------------------------------------------------------------------------
// Score breakdown explanation (node drawer / proxy check result)
// ---------------------------------------------------------------------------

export function ScoreBreakdownExplanation({
  breakdown,
  policyVersion,
}: {
  breakdown: ScoreBreakdown | null | undefined;
  policyVersion?: number | null;
}) {
  const { t } = useI18n();

  if (!breakdown) {
    // Legacy score — no breakdown persisted.
    return (
      <div className="callout callout-warning" style={{ alignItems: "flex-start", fontSize: "0.8rem" }}>
        <Info size={14} />
        <span>
          {t(
            "该分数由旧评分引擎产生，未保留分项明细。重新检测后将按当前评分策略生成分项解释。",
          )}
        </span>
      </div>
    );
  }

  const version = breakdown.version ?? policyVersion ?? 0;
  if (!version) {
    return (
      <div className="callout callout-warning" style={{ alignItems: "flex-start", fontSize: "0.8rem" }}>
        <Info size={14} />
        <span>{t("该分数由旧评分引擎产生，未保留分项明细。重新检测后将按当前评分策略生成分项解释。")}</span>
      </div>
    );
  }

  const effectiveWeights = breakdown.effective_weights ?? {};
  const subScores = breakdown.sub_scores ?? {};
  const unavailable = new Set<string>(breakdown.unavailable_dimensions ?? []);
  const appliedCaps = breakdown.applied_caps ?? [];
  const gradeFromScore = breakdown.grade_from_score;
  const finalGrade = breakdown.final_grade;
  const terminalReason = breakdown.terminal_reason;

  return (
    <div className="score-breakdown" style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
      <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap", alignItems: "center" }}>
        <span style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>{t("评分策略版本")}</span>
        <Badge variant="neutral">v{version}</Badge>
      </div>

      <div className="score-breakdown-weights" style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
        <span style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>{t("有效权重与分项")}</span>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(140px, 1fr))", gap: "6px" }}>
          {DIMENSION_KEYS.map((key) => {
            const weight = effectiveWeights[key];
            const entry = subScores[key];
            const isUnavailable = unavailable.has(key) || (entry?.unavailable ?? false);
            const subValue = entry?.value;
            return (
              <div
                key={key}
                className="score-breakdown-dim"
                style={{
                  border: "1px solid var(--border)",
                  borderRadius: "6px",
                  padding: "6px 8px",
                  background: "var(--surface-sunken, rgba(0,0,0,0.02))",
                  fontSize: "0.75rem",
                }}
              >
                <div style={{ fontWeight: 600 }}>{t(dimensionLabel(key))}</div>
                {isUnavailable ? (
                  <div style={{ color: "var(--text-muted)" }}>{t("不可用")}</div>
                ) : (
                  <>
                    <div style={{ color: "var(--text-muted)" }}>
                      {t("权重")} {typeof weight === "number" ? weight : "-"}
                    </div>
                    <div>
                      {t("分项")} {typeof subValue === "number" ? Math.round(subValue) : "-"}
                    </div>
                  </>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {unavailable.size > 0 ? (
        <div style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>
          {t(
            "不可用维度不参与加权计算，既不计入分子也不计入分母。常见原因：该维度被关闭、单轮检测无法评估稳定性、或观测请求失败。",
          )}
        </div>
      ) : null}

      <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap", alignItems: "center" }}>
        <span style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>{t("分数对应等级")}</span>
        {gradeFromScore ? <Badge variant={gradeBadgeVariant(gradeFromScore)}>{gradeFromScore}</Badge> : <span>-</span>}
        <span style={{ color: "var(--text-muted)" }}>→</span>
        <span style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>{t("最终等级")}</span>
        {finalGrade ? <Badge variant={gradeBadgeVariant(finalGrade)}>{finalGrade}</Badge> : <span>-</span>}
      </div>

      {appliedCaps.length > 0 ? (
        <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
          <span style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>{t("已应用封顶")}</span>
          <div style={{ display: "flex", gap: "0.25rem", flexWrap: "wrap" }}>
            {appliedCaps.map((cap, i) => {
              const label = typeof cap === "string" ? cap : `${cap.dimension}: ${cap.reason} → ${cap.cap}`;
              const badgeText = typeof cap === "string" ? cap : `${cap.cap}`;
              return (
                <Badge key={i} variant="warning" style={{ fontSize: "0.65rem", padding: "2px 5px" }} title={label}>
                  {badgeText}
                </Badge>
              );
            })}
          </div>
        </div>
      ) : null}

      {terminalReason ? (
        <div className="callout callout-error" style={{ alignItems: "flex-start", fontSize: "0.8rem" }}>
          <AlertTriangle size={14} />
          <span>
            {t("终止原因")}：{terminalReason}
          </span>
        </div>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Reusable multi-select for CF detailed status (platform forms + node filters)
// ---------------------------------------------------------------------------

export function CloudflareStatusMultiSelect({
  selected,
  onToggle,
  showContradictionHint = false,
  legacyChallenged = "any",
  layout = "wrap",
}: {
  selected: CloudflareStatusToken[];
  onToggle: (token: CloudflareStatusToken) => void;
  /** When true, show a contradiction warning if legacy challenged filter conflicts. */
  showContradictionHint?: boolean;
  /** "true"/"challenged" / "false"/"clean" / "any" — legacy challenged filter state. */
  legacyChallenged?: string;
  layout?: "wrap" | "stack";
}) {
  const { t } = useI18n();

  const isContradiction = showContradictionHint ? isCFContradiction(selected, legacyChallenged) : false;

  return (
    <div className="cf-status-multiselect" style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <div style={{
        display: layout === "stack" ? "flex" : "flex",
        flexWrap: "wrap",
        gap: "4px",
        padding: "4px 6px",
        minHeight: "32px",
        border: "1px solid var(--border)",
        borderRadius: "6px",
        background: "var(--surface-sunken, rgba(0,0,0,0.02))",
      }}>
        {CF_STATUS_TOKENS.map((token) => {
          const isSelected = selected.includes(token);
          return (
            <button
              key={token}
              type="button"
              onClick={() => onToggle(token)}
              style={{
                padding: "2px 6px",
                borderRadius: "4px",
                fontSize: "0.7rem",
                fontWeight: 600,
                border: isSelected ? "1px solid var(--primary)" : "1px solid var(--border)",
                background: isSelected ? "var(--primary)" : "transparent",
                color: isSelected ? "#fff" : "var(--text-secondary)",
                cursor: "pointer",
              }}
              title={t(cfStatusDescription(token))}
              aria-pressed={isSelected}
            >
              {t(cfStatusLabel(token))}
            </button>
          );
        })}
      </div>
      {showContradictionHint ? (
        isContradiction ? (
          <small style={{ color: "var(--danger)", fontSize: 11, marginTop: 2, display: "block" }}>
            {t("与“Cloudflare 拦截”筛选矛盾，结果为空。")}
          </small>
        ) : (
          <small style={{ color: "var(--text-muted)", fontSize: 11, marginTop: 2, display: "block" }}>
            {t("多选 OR，与“Cloudflare 拦截”取交集。留空表示不限制。")}
          </small>
        )
      ) : null}
    </div>
  );
}


