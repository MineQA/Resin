import { AlertTriangle, ChevronDown, ChevronRight, Info, RotateCcw, Sparkles } from "lucide-react";
import { useMemo, useState } from "react";
import { Badge } from "./ui/Badge";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";
import { Select } from "./ui/Select";
import { useI18n } from "../i18n";
import {
  BALANCED_SCORING_POLICY,
  CF_POLICY_MODES,
  CF_STATUS_TOKENS,
  cloneScoringPolicy,
  deriveEffectivePolicy,
  isBalancedPreset,
  isValidCustomTargetURL,
  validateScoringPolicy,
  type CloudflareStatusToken,
  type DimensionKey,
  type Grade,
  type LegacyNormalizationInput,
  type ScoringPolicy,
  type ScoringPolicyMode,
} from "../lib/cloudflareStatus";
import {
  cfPolicyModeDescription,
  cfPolicyModeLabel,
  cfStatusDescription,
  cfStatusLabel,
  dimensionLabel,
} from "../lib/cloudflareStatus";

// ---------------------------------------------------------------------------
// Preset state
// ---------------------------------------------------------------------------

type PresetKey = "balanced" | "custom";

function detectPreset(policy: ScoringPolicy | null): PresetKey {
  if (!policy) {
    return "balanced";
  }
  return isBalancedPreset(policy) ? "balanced" : "custom";
}

// ---------------------------------------------------------------------------
// Editor props
// ---------------------------------------------------------------------------

export type ScoringPolicyEditorProps = {
  /** Current canonical policy (may be null when backend uses legacy flat fields). */
  policy: ScoringPolicy | null;
  /** Legacy flat flags used to derive an effective policy when policy is null. */
  legacy: LegacyNormalizationInput;
  /** Called whenever the draft policy changes. */
  onChange: (policy: ScoringPolicy | null) => void;
};

// ---------------------------------------------------------------------------
// Editor component
// ---------------------------------------------------------------------------

export function ScoringPolicyEditor({ policy, legacy, onChange }: ScoringPolicyEditorProps) {
  const { t } = useI18n();
  const [expertOpen, setExpertOpen] = useState(false);

  // The effective policy is either the canonical policy or a derived one.
  const effective: ScoringPolicy = useMemo(() => {
    if (policy) {
      return policy;
    }
    return deriveEffectivePolicy(legacy);
  }, [policy, legacy]);

  const preset = detectPreset(effective);
  const derived = !policy; // true when policy was derived from legacy flags
  const validation = useMemo(() => validateScoringPolicy(effective), [effective]);
  const customUrlValid = isValidCustomTargetURL(effective.cloudflare.target_url);

  const update = (next: ScoringPolicy) => {
    onChange(next);
  };

  const applyPreset = (key: PresetKey) => {
    if (key === "balanced") {
      onChange(cloneScoringPolicy(BALANCED_SCORING_POLICY));
    } else {
      // Switch to custom from current effective state (so user keeps their edits).
      onChange(cloneScoringPolicy(effective));
    }
  };

  const setWeight = (key: DimensionKey, value: number) => {
    const next = cloneScoringPolicy(effective);
    next.weights[key] = Math.max(0, Math.min(100, Math.round(value) || 0));
    update(next);
  };

  const setThreshold = (grade: "A" | "B" | "C" | "D", value: number) => {
    const next = cloneScoringPolicy(effective);
    next.grade_thresholds[grade] = Math.max(0, Math.min(100, Math.round(value) || 0));
    update(next);
  };

  const setCFPolicy = (mode: ScoringPolicyMode) => {
    const next = cloneScoringPolicy(effective);
    next.cloudflare.policy = mode;
    update(next);
  };

  const setCFTargetURL = (url: string) => {
    const next = cloneScoringPolicy(effective);
    next.cloudflare.target_url = url;
    update(next);
  };

  const setCFStatusScore = (token: CloudflareStatusToken, value: number | null) => {
    const next = cloneScoringPolicy(effective);
    next.cloudflare.status_scores[token] = value;
    update(next);
  };

  const setCFGradeCap = (token: CloudflareStatusToken, grade: Grade | "") => {
    const next = cloneScoringPolicy(effective);
    if (grade === "") {
      delete next.cloudflare.grade_caps[token];
    } else {
      next.cloudflare.grade_caps[token] = grade;
    }
    update(next);
  };

  const setLatencyBandMax = (index: number, maxMs: number | null) => {
    const next = cloneScoringPolicy(effective);
    next.latency.bands[index].max_ms = maxMs;
    update(next);
  };

  const setLatencyBandScore = (index: number, score: number) => {
    const next = cloneScoringPolicy(effective);
    next.latency.bands[index].score = Math.max(0, Math.min(100, Math.round(score) || 0));
    update(next);
  };

  const addLatencyBand = () => {
    const next = cloneScoringPolicy(effective);
    const bands = next.latency.bands;
    // Insert before the open-end band.
    const lastFinite = bands[bands.length - 2]?.max_ms ?? 0;
    bands.splice(bands.length - 1, 0, { max_ms: lastFinite + 500, score: 50 });
    update(next);
  };

  const removeLatencyBand = (index: number) => {
    const next = cloneScoringPolicy(effective);
    if (next.latency.bands.length <= 2) {
      return;
    }
    next.latency.bands.splice(index, 1);
    update(next);
  };

  const setCVBandMax = (index: number, maxPercent: number | null) => {
    const next = cloneScoringPolicy(effective);
    next.stability.cv_bands[index].max_percent = maxPercent;
    update(next);
  };

  const setCVBandScore = (index: number, score: number) => {
    const next = cloneScoringPolicy(effective);
    next.stability.cv_bands[index].score = Math.max(0, Math.min(100, Math.round(score) || 0));
    update(next);
  };

  const addCVBand = () => {
    const next = cloneScoringPolicy(effective);
    const bands = next.stability.cv_bands;
    const lastFinite = bands[bands.length - 2]?.max_percent ?? 0;
    bands.splice(bands.length - 1, 0, { max_percent: lastFinite + 10, score: 50 });
    update(next);
  };

  const removeCVBand = (index: number) => {
    const next = cloneScoringPolicy(effective);
    if (next.stability.cv_bands.length <= 2) {
      return;
    }
    next.stability.cv_bands.splice(index, 1);
    update(next);
  };

  const setDimensionCapEnabled = (key: DimensionKey, enabled: boolean) => {
    const next = cloneScoringPolicy(effective);
    if (enabled) {
      next.dimension_caps[key] = { below_score: 50, grade_cap: "D" };
    } else {
      next.dimension_caps[key] = null;
    }
    update(next);
  };

  const setDimensionCap = (key: DimensionKey, field: "below_score" | "grade_cap", value: number | Grade) => {
    const next = cloneScoringPolicy(effective);
    const cap = next.dimension_caps[key];
    if (!cap) {
      return;
    }
    if (field === "below_score") {
      cap.below_score = Math.max(0, Math.min(100, Math.round(value as number) || 0));
    } else {
      cap.grade_cap = value as Grade;
    }
    update(next);
  };

  const resetToBalanced = () => {
    onChange(cloneScoringPolicy(BALANCED_SCORING_POLICY));
  };

  return (
    <div className="scoring-policy-editor" style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
      {/* Observation always-on notice */}
      <div className="callout callout-info" style={{ alignItems: "flex-start", fontSize: "0.8rem" }}>
        <Info size={14} />
        <span>
          {t(
            "Cloudflare 观测始终执行并记录状态，策略仅控制观测结果是否影响分数或等级。即使关闭服务可达性，观测仍会向目标服务发出额外请求。",
          )}
        </span>
      </div>

      {/* Preset selector */}
      <div className="field-group" style={{ margin: 0 }}>
        <label className="field-label" style={{ margin: 0 }}>
          {t("评分预设")}
        </label>
        <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap", alignItems: "center", marginTop: "6px" }}>
          <Button
            variant={preset === "balanced" ? "primary" : "secondary"}
            size="sm"
            onClick={() => applyPreset("balanced")}
          >
            <Sparkles size={14} />
            {t("平衡（推荐）")}
          </Button>
          <Button
            variant={preset === "custom" ? "primary" : "secondary"}
            size="sm"
            onClick={() => applyPreset("custom")}
          >
            {t("自定义")}
          </Button>
          {preset === "balanced" ? (
            <Badge variant="success">{t("当前为平衡预设")}</Badge>
          ) : (
            <Badge variant="warning">{t("已偏离平衡预设")}</Badge>
          )}
          {derived ? (
            <Badge variant="muted" title={t("后端未返回规范策略，已从旧字段推导展示")}>
              {t("推导")}
            </Badge>
          ) : null}
        </div>
        {derived ? (
          <p className="muted" style={{ marginTop: 6, fontSize: 12 }}>
            {t(
              "后端尚未保存规范评分策略，当前展示由旧字段推导而来。保存后将写入规范 v1 策略。",
            )}
          </p>
        ) : null}
      </div>

      {/* CF policy mode + target URL */}
      <div className="form-grid" style={{ marginTop: 0 }}>
        <div className="field-group">
          <label className="field-label" htmlFor="scoring-cf-policy" style={{ margin: 0 }}>
            {t("Cloudflare 策略")}
          </label>
          <Select
            id="scoring-cf-policy"
            value={effective.cloudflare.policy}
            onChange={(e) => setCFPolicy(e.target.value as ScoringPolicyMode)}
          >
            {CF_POLICY_MODES.map((mode) => (
              <option key={mode} value={mode}>
                {t(cfPolicyModeLabel(mode))}
              </option>
            ))}
          </Select>
          <small style={{ color: "var(--text-muted)", fontSize: 11 }}>
            {t(cfPolicyModeDescription(effective.cloudflare.policy))}
          </small>
        </div>

        <div className="field-group">
          <label className="field-label" htmlFor="scoring-cf-target" style={{ margin: 0 }}>
            {t("自定义 CF 目标 URL")}
          </label>
          <Input
            id="scoring-cf-target"
            value={effective.cloudflare.target_url}
            onChange={(e) => setCFTargetURL(e.target.value)}
            placeholder={t("留空使用 Profile 服务地址")}
          />
          <small style={{ color: customUrlValid ? "var(--text-muted)" : "var(--danger)", fontSize: 11 }}>
            {customUrlValid
              ? t(
                  "仅接受 HTTPS，拒绝本地/私有地址与凭据。此为尽力而为的 URL 形状校验，远端 DNS 重绑定风险无法完全防范。",
                )
              : t("URL 不符合要求：仅 HTTPS、不含凭据/片段、非本地/私有地址。")}
          </small>
        </div>
      </div>

      {/* Validation errors */}
      {!validation.ok ? (
        <div className="callout callout-error" style={{ alignItems: "flex-start", fontSize: "0.8rem" }}>
          <AlertTriangle size={14} />
          <div style={{ flex: 1, minWidth: 0 }}>
            <strong>{t("校验问题")}：</strong>
            <ul style={{ margin: "4px 0 0", paddingLeft: "20px" }}>
              {validation.errors.map((err) => (
                <li key={err}>{err}</li>
              ))}
            </ul>
            <small style={{ color: "var(--text-muted)" }}>
              {t("服务端仍为最终权威；客户端校验仅用于防止明显无效的提交。")}
            </small>
          </div>
        </div>
      ) : null}

      {/* Expert editor toggle */}
      <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
        <button
          type="button"
          onClick={() => setExpertOpen((open) => !open)}
          style={{
            background: "transparent",
            border: "none",
            cursor: "pointer",
            display: "inline-flex",
            alignItems: "center",
            gap: "4px",
            color: "var(--primary)",
            fontWeight: 600,
            fontSize: "0.85rem",
            padding: 0,
          }}
        >
          {expertOpen ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
          {t("高级编辑")}
        </button>
        <Button variant="ghost" size="sm" onClick={resetToBalanced} title={t("恢复为平衡预设")}>
          <RotateCcw size={14} />
          {t("重置为平衡")}
        </Button>
      </div>

      {expertOpen ? (
        <div
          className="scoring-expert"
          style={{
            border: "1px solid var(--border)",
            borderRadius: "8px",
            padding: "12px",
            background: "var(--surface-sunken, rgba(0,0,0,0.02))",
            display: "flex",
            flexDirection: "column",
            gap: "14px",
          }}
        >
          {/* Weights */}
          <ExpertSection title={t("维度权重（0-100，自动归一化）")}>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(140px, 1fr))", gap: "8px" }}>
              {(["service", "api", "cloudflare", "stability", "latency"] as DimensionKey[]).map((key) => (
                <div key={key}>
                  <label style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>{t(dimensionLabel(key))}</label>
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    value={effective.weights[key]}
                    onChange={(e) => setWeight(key, Number(e.target.value))}
                  />
                </div>
              ))}
            </div>
            <p className="muted" style={{ fontSize: 12, marginTop: 6 }}>
              {t("权重无需合计 100。可用维度（权重大于 0 且有结果）参与加权，不可用维度从分子和分母同时排除。")}
            </p>
          </ExpertSection>

          {/* Grade thresholds */}
          <ExpertSection title={t("等级阈值（严格递减 A>B>C>D）")}>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(100px, 1fr))", gap: "8px" }}>
              {(["A", "B", "C", "D"] as const).map((g) => (
                <div key={g}>
                  <label style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>{g}</label>
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    value={effective.grade_thresholds[g]}
                    onChange={(e) => setThreshold(g, Number(e.target.value))}
                  />
                </div>
              ))}
            </div>
            <p className="muted" style={{ fontSize: 12, marginTop: 6 }}>
              {t("F 为低于 D 阈值的默认等级。")}
            </p>
          </ExpertSection>

          {/* CF status scores */}
          <ExpertSection title={t("Cloudflare 状态分（0-100 或不可用）")}>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(160px, 1fr))", gap: "8px" }}>
              {CF_STATUS_TOKENS.map((token) => {
                const score = effective.cloudflare.status_scores[token];
                const nullable = token === "ng" || token === "unchecked";
                return (
                  <div key={token}>
                    <label
                      style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}
                      title={t(cfStatusDescription(token))}
                    >
                      {t(cfStatusLabel(token))}
                    </label>
                    <div style={{ display: "flex", gap: "4px", alignItems: "center" }}>
                      <Input
                        type="number"
                        min={0}
                        max={100}
                        value={score === null ? "" : score}
                        disabled={score === null}
                        onChange={(e) => {
                          const raw = e.target.value;
                          if (raw === "") {
                            setCFStatusScore(token, nullable ? null : 0);
                          } else {
                            setCFStatusScore(token, Number(raw));
                          }
                        }}
                        style={{ flex: 1 }}
                      />
                      {nullable ? (
                        <label style={{ display: "flex", alignItems: "center", gap: "2px", fontSize: "0.7rem", color: "var(--text-muted)" }}>
                          <input
                            type="checkbox"
                            checked={score === null}
                            onChange={(e) => setCFStatusScore(token, e.target.checked ? null : 0)}
                          />
                          {t("不可用")}
                        </label>
                      ) : null}
                    </div>
                  </div>
                );
              })}
            </div>
            <p className="muted" style={{ fontSize: 12, marginTop: 6 }}>
              {t("ng 与 unchecked 可设为不可用（不参与加权）；其他状态必须为 0-100 整数。")}
            </p>
          </ExpertSection>

          {/* CF grade caps */}
          <ExpertSection title={t("Cloudflare 挑战封顶等级")}>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(140px, 1fr))", gap: "8px" }}>
              {(["js_challenge", "captcha_challenge", "challenge", "block"] as CloudflareStatusToken[]).map((token) => (
                <div key={token}>
                  <label style={{ fontSize: "0.75rem", color: "var(--text-secondary)" }}>{t(cfStatusLabel(token))}</label>
                  <Select
                    value={effective.cloudflare.grade_caps[token] ?? ""}
                    onChange={(e) => setCFGradeCap(token, e.target.value as Grade | "")}
                  >
                    <option value="">{t("不封顶")}</option>
                    {(["A", "B", "C", "D", "F"] as Grade[]).map((g) => (
                      <option key={g} value={g}>{g}</option>
                    ))}
                  </Select>
                </div>
              ))}
            </div>
          </ExpertSection>

          {/* Latency bands */}
          <ExpertSection title={t("延迟分档（毫秒，严格递增，末档上限留空）")}>
            <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
              {effective.latency.bands.map((band, i) => {
                const isLast = i === effective.latency.bands.length - 1;
                return (
                  <div key={i} style={{ display: "flex", gap: "6px", alignItems: "center", flexWrap: "wrap" }}>
                    <Input
                      type="number"
                      min={0}
                      placeholder={isLast ? t("开放") : "max_ms"}
                      value={band.max_ms === null ? "" : band.max_ms}
                      disabled={isLast}
                      onChange={(e) => {
                        const raw = e.target.value;
                        setLatencyBandMax(i, raw === "" ? null : Number(raw));
                      }}
                      style={{ maxWidth: 120 }}
                    />
                    <span style={{ color: "var(--text-muted)", fontSize: "0.75rem" }}>→</span>
                    <Input
                      type="number"
                      min={0}
                      max={100}
                      value={band.score}
                      onChange={(e) => setLatencyBandScore(i, Number(e.target.value))}
                      style={{ maxWidth: 100 }}
                    />
                    {!isLast ? (
                      <Button variant="ghost" size="sm" onClick={() => removeLatencyBand(i)} title={t("删除此档")}>
                        ×
                      </Button>
                    ) : null}
                  </div>
                );
              })}
              <Button variant="secondary" size="sm" onClick={addLatencyBand}>
                {t("新增延迟档")}
              </Button>
            </div>
          </ExpertSection>

          {/* CV bands */}
          <ExpertSection title={t("稳定性变异系数分档（%，严格递增，末档上限留空）")}>
            <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
              {effective.stability.cv_bands.map((band, i) => {
                const isLast = i === effective.stability.cv_bands.length - 1;
                return (
                  <div key={i} style={{ display: "flex", gap: "6px", alignItems: "center", flexWrap: "wrap" }}>
                    <Input
                      type="number"
                      min={0}
                      placeholder={isLast ? t("开放") : "max_percent"}
                      value={band.max_percent === null ? "" : band.max_percent}
                      disabled={isLast}
                      onChange={(e) => {
                        const raw = e.target.value;
                        setCVBandMax(i, raw === "" ? null : Number(raw));
                      }}
                      style={{ maxWidth: 120 }}
                    />
                    <span style={{ color: "var(--text-muted)", fontSize: "0.75rem" }}>→</span>
                    <Input
                      type="number"
                      min={0}
                      max={100}
                      value={band.score}
                      onChange={(e) => setCVBandScore(i, Number(e.target.value))}
                      style={{ maxWidth: 100 }}
                    />
                    {!isLast ? (
                      <Button variant="ghost" size="sm" onClick={() => removeCVBand(i)} title={t("删除此档")}>
                        ×
                      </Button>
                    ) : null}
                  </div>
                );
              })}
              <Button variant="secondary" size="sm" onClick={addCVBand}>
                {t("新增 CV 档")}
              </Button>
            </div>
            <p className="muted" style={{ fontSize: 12, marginTop: 6 }}>
              {t("稳定性仅在多轮检测且至少两轮有可比结果时可用。")}
            </p>
          </ExpertSection>

          {/* Dimension caps */}
          <ExpertSection title={t("维度封顶（可选）")}>
            <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
              {(["service", "api", "cloudflare", "stability", "latency"] as DimensionKey[]).map((key) => {
                const cap = effective.dimension_caps[key];
                const enabled = cap !== null;
                return (
                  <div
                    key={key}
                    style={{
                      display: "flex",
                      gap: "8px",
                      alignItems: "center",
                      flexWrap: "wrap",
                      padding: "6px 8px",
                      border: "1px solid var(--border)",
                      borderRadius: "6px",
                      background: "rgba(255,255,255,0.6)",
                    }}
                  >
                    <label style={{ display: "flex", alignItems: "center", gap: "4px", fontSize: "0.8rem", fontWeight: 600 }}>
                      <input
                        type="checkbox"
                        checked={enabled}
                        onChange={(e) => setDimensionCapEnabled(key, e.target.checked)}
                      />
                      {t(dimensionLabel(key))}
                    </label>
                    {enabled && cap ? (
                      <>
                        <span style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>{t("低于分")}</span>
                        <Input
                          type="number"
                          min={0}
                          max={100}
                          value={cap.below_score}
                          onChange={(e) => setDimensionCap(key, "below_score", Number(e.target.value))}
                          style={{ maxWidth: 80 }}
                        />
                        <span style={{ fontSize: "0.75rem", color: "var(--text-muted)" }}>{t("封顶等级")}</span>
                        <Select
                          value={cap.grade_cap}
                          onChange={(e) => setDimensionCap(key, "grade_cap", e.target.value as Grade)}
                          style={{ maxWidth: 80 }}
                        >
                          {(["A", "B", "C", "D", "F"] as Grade[]).map((g) => (
                            <option key={g} value={g}>{g}</option>
                          ))}
                        </Select>
                      </>
                    ) : null}
                  </div>
                );
              })}
            </div>
            <p className="muted" style={{ fontSize: 12, marginTop: 6 }}>
              {t("维度分项低于阈值时封顶等级；不可用维度不触发封顶。")}
            </p>
          </ExpertSection>
        </div>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Expert section wrapper
// ---------------------------------------------------------------------------

function ExpertSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h5 style={{ margin: "0 0 6px", fontSize: "0.8rem", fontWeight: 700 }}>{title}</h5>
      {children}
    </div>
  );
}
