import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { Switch } from "../../components/ui/Switch";
import { useI18n } from "../../i18n";
import type { ModeledRule, RuleItem, VisualDraft } from "./ruleProfileVisualModel";
import {
  canDeleteModeledItem,
  canMoveRule,
  createGeoipRule,
  createMatchRule,
  createRuleSetRule,
  insertRuleBeforeMatch,
  moveItemInList,
  policyOptionsFromDraft,
  providerKeyOptionsFromDraft,
  removeModeledById,
  terminalMatchIndex,
  updateRuleAt,
} from "./ruleProfileVisualUi";

type RuleProfileRulesPanelProps = {
  draft: VisualDraft;
  disabled?: boolean;
  onChange: (next: VisualDraft) => void;
};

export function RuleProfileRulesPanel({ draft, disabled, onChange }: RuleProfileRulesPanelProps) {
  const { t } = useI18n();
  const blocked = draft.blockedSections.includes("rules");
  const matchIdx = terminalMatchIndex(draft.rules);
  const hasTerminalMatch = matchIdx >= 0;
  const matchCount = draft.rules.filter((r) => r.kind === "modeled" && r.ruleType === "MATCH").length;
  const policyOptions = policyOptionsFromDraft(draft);
  const providerOptions = providerKeyOptionsFromDraft(draft);

  const setRules = (rules: RuleItem[]) => {
    onChange({ ...draft, rules });
  };

  const addRuleSet = () => {
    if (disabled || blocked) {
      return;
    }
    setRules(insertRuleBeforeMatch(draft.rules, createRuleSetRule(policyOptions[0] || "DIRECT")));
  };

  const addGeoip = () => {
    if (disabled || blocked) {
      return;
    }
    setRules(insertRuleBeforeMatch(draft.rules, createGeoipRule(policyOptions[0] || "DIRECT")));
  };

  const addMatch = () => {
    if (disabled || blocked || hasTerminalMatch) {
      return;
    }
    setRules([...draft.rules, createMatchRule(policyOptions[0] || "DIRECT")]);
  };

  const move = (index: number, dir: -1 | 1) => {
    if (disabled || blocked || !canMoveRule(draft.rules, index, dir)) {
      return;
    }
    setRules(moveItemInList(draft.rules, index, dir));
  };

  const remove = (id: string) => {
    if (disabled || blocked || !canDeleteModeledItem(draft.rules, id)) {
      return;
    }
    setRules(removeModeledById(draft.rules, id));
  };

  const patch = (id: string, next: Partial<ModeledRule>) => {
    if (disabled || blocked) {
      return;
    }
    setRules(updateRuleAt(draft.rules, id, next));
  };

  return (
    <div className="rule-profile-visual-panel" role="tabpanel" id="rule-profile-tabpanel-rules" aria-labelledby="rule-profile-tab-rules">
      {blocked ? (
        <div className="callout callout-warning" role="status">
          <span>
            {t("rules 区段因错误类型或锚点/别名被阻断，仅可在 YAML 中编辑。请修复后从 YAML 重新加载。")}
          </span>
        </div>
      ) : null}

      {!hasTerminalMatch ? (
        <div className="callout callout-error" role="alert">
          <span>{t("需要恰好一条末尾 MATCH 规则。可添加 MATCH，或在 YAML 中修复。")}</span>
        </div>
      ) : null}
      {matchCount > 1 ? (
        <div className="callout callout-error" role="alert">
          <span>{t("发现多条 MATCH 规则（{{count}}）。请删除多余项或在 YAML 中整理。", { count: matchCount })}</span>
        </div>
      ) : null}

      <div className="callout callout-info rule-profile-visual-note" role="note">
        <span>
          {t("仅建模 RULE-SET / GEOIP / MATCH。其他规则类型为原始项，不可在可视化中重排或删除。新规则插入在末尾 MATCH 之前。")}
        </span>
      </div>

      <div className="rule-profile-visual-panel-actions">
        <Button variant="secondary" size="sm" onClick={addRuleSet} disabled={disabled || blocked}>
          <Plus size={14} />
          {t("添加 RULE-SET")}
        </Button>
        <Button variant="secondary" size="sm" onClick={addGeoip} disabled={disabled || blocked}>
          <Plus size={14} />
          {t("添加 GEOIP")}
        </Button>
        <Button variant="secondary" size="sm" onClick={addMatch} disabled={disabled || blocked || hasTerminalMatch}>
          <Plus size={14} />
          {t("添加 MATCH")}
        </Button>
        <span className="muted rule-profile-visual-count">
          {t("规则 {{count}}", { count: draft.rules.length })}
        </span>
      </div>

      {draft.rules.length === 0 ? (
        <div className="empty-box rule-profile-visual-empty">
          <p>{t("暂无 rules。请添加 MATCH 作为末条，或从 YAML / ACL4SSR 导入。")}</p>
        </div>
      ) : (
        <div className="rule-profile-visual-card-list">
          {draft.rules.map((rule, index) => (
            <RuleCard
              key={rule.id}
              rule={rule}
              index={index}
              total={draft.rules.length}
              isTerminalMatch={hasTerminalMatch && index === matchIdx}
              disabled={disabled || blocked}
              canUp={canMoveRule(draft.rules, index, -1)}
              canDown={canMoveRule(draft.rules, index, 1)}
              canDelete={rule.kind === "modeled" && canDeleteModeledItem(draft.rules, rule.id)}
              policyOptions={policyOptions}
              providerOptions={providerOptions}
              onMove={move}
              onRemove={remove}
              onPatch={patch}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function RuleCard({
  rule,
  index,
  total,
  isTerminalMatch,
  disabled,
  canUp,
  canDown,
  canDelete,
  policyOptions,
  providerOptions,
  onMove,
  onRemove,
  onPatch,
}: {
  rule: RuleItem;
  index: number;
  total: number;
  isTerminalMatch: boolean;
  disabled: boolean;
  canUp: boolean;
  canDown: boolean;
  canDelete: boolean;
  policyOptions: string[];
  providerOptions: string[];
  onMove: (index: number, dir: -1 | 1) => void;
  onRemove: (id: string) => void;
  onPatch: (id: string, patch: Partial<ModeledRule>) => void;
}) {
  const { t } = useI18n();

  if (rule.kind === "raw") {
    return (
      <article className="rule-profile-visual-card rule-profile-visual-card-raw">
        <div className="rule-profile-visual-card-head">
          <div className="rule-profile-visual-card-title">
            <span className="rule-profile-raw-badge">{t("仅 YAML")}</span>
            <strong>{rule.label || t("原始规则")}</strong>
            <span className="muted">{rule.sourceType || "raw"}</span>
          </div>
          <span className="muted">#{index + 1}/{total}</span>
        </div>
        <p className="muted rule-profile-raw-reason">
          {rule.reason || t("不支持的规则类型；请在 YAML 中精确编辑。")}
        </p>
        {rule.text ? <pre className="rule-profile-raw-pre">{rule.text}</pre> : null}
      </article>
    );
  }

  const isMatch = rule.ruleType === "MATCH";

  return (
    <article className={`rule-profile-visual-card${isTerminalMatch ? " rule-profile-visual-card-match" : ""}`}>
      <div className="rule-profile-visual-card-head">
        <div className="rule-profile-visual-card-title">
          <span className="rule-profile-type-badge">{rule.ruleType}</span>
          {isTerminalMatch ? <span className="rule-profile-match-pin">{t("末条固定")}</span> : null}
          <strong>
            {rule.ruleType === "RULE-SET"
              ? `${rule.provider || "…"} → ${rule.policy || "…"}`
              : rule.ruleType === "GEOIP"
                ? `${rule.geoipCode || "…"} → ${rule.policy || "…"}`
                : `MATCH → ${rule.policy || "…"}`}
          </strong>
        </div>
        <div className="rule-profile-order-btns">
          {!isMatch ? (
            <>
              <Button
                variant="ghost"
                size="sm"
                className="rule-profile-icon-btn"
                disabled={disabled || !canUp}
                onClick={() => onMove(index, -1)}
                aria-label={t("上移规则 {{index}}", { index: index + 1 })}
              >
                <ArrowUp size={15} />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="rule-profile-icon-btn"
                disabled={disabled || !canDown}
                onClick={() => onMove(index, 1)}
                aria-label={t("下移规则 {{index}}", { index: index + 1 })}
              >
                <ArrowDown size={15} />
              </Button>
            </>
          ) : null}
          <Button
            variant="danger"
            size="sm"
            className="rule-profile-icon-btn"
            disabled={disabled || !canDelete}
            onClick={() => onRemove(rule.id)}
            title={!canDelete ? t("删除会移动后面的原始项位置，请先在 YAML 中处理原始项") : undefined}
            aria-label={
              !canDelete
                ? t("无法删除规则 {{index}}：其后有原始项锚点", { index: index + 1 })
                : t("删除规则 {{index}}", { index: index + 1 })
            }
          >
            <Trash2 size={14} />
          </Button>
        </div>
      </div>

      {rule.ruleType === "RULE-SET" ? (
        <div className="rule-profile-visual-form-grid">
          <div className="field-group">
            <label className="field-label" htmlFor={`${rule.id}-provider`}>{t("Provider key")}</label>
            <Input
              id={`${rule.id}-provider`}
              list={`${rule.id}-provider-list`}
              value={rule.provider ?? ""}
              disabled={disabled}
              spellCheck={false}
              onChange={(event) => onPatch(rule.id, { provider: event.target.value })}
            />
            <datalist id={`${rule.id}-provider-list`}>
              {providerOptions.map((key) => (
                <option key={key} value={key} />
              ))}
            </datalist>
          </div>
          <PolicyField
            id={`${rule.id}-policy`}
            value={rule.policy}
            options={policyOptions}
            disabled={disabled}
            onChange={(policy) => onPatch(rule.id, { policy })}
          />
        </div>
      ) : null}

      {rule.ruleType === "GEOIP" ? (
        <div className="rule-profile-visual-form-grid">
          <div className="field-group">
            <label className="field-label" htmlFor={`${rule.id}-geo`}>{t("国家/地区代码")}</label>
            <Input
              id={`${rule.id}-geo`}
              value={rule.geoipCode ?? ""}
              disabled={disabled}
              spellCheck={false}
              onChange={(event) => onPatch(rule.id, { geoipCode: event.target.value })}
            />
          </div>
          <PolicyField
            id={`${rule.id}-policy`}
            value={rule.policy}
            options={policyOptions}
            disabled={disabled}
            onChange={(policy) => onPatch(rule.id, { policy })}
          />
        </div>
      ) : null}

      {rule.ruleType === "MATCH" ? (
        <PolicyField
          id={`${rule.id}-policy`}
          value={rule.policy}
          options={policyOptions}
          disabled={disabled}
          onChange={(policy) => onPatch(rule.id, { policy })}
        />
      ) : null}

      {rule.ruleType !== "MATCH" ? (
        <div className="rule-profile-enabled-field rule-profile-visual-switch-row">
          <div>
            <span className="field-label">no-resolve</span>
            <small>{t("GEOIP 等场景常用；RULE-SET 按需开启。")}</small>
          </div>
          <Switch
            checked={rule.noResolve}
            disabled={disabled}
            onChange={(event) => onPatch(rule.id, { noResolve: event.target.checked })}
            aria-label="no-resolve"
          />
        </div>
      ) : null}
    </article>
  );
}

function PolicyField({
  id,
  value,
  options,
  disabled,
  onChange,
}: {
  id: string;
  value: string;
  options: string[];
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="field-group">
      <label className="field-label" htmlFor={id}>{t("策略目标")}</label>
      <Input
        id={id}
        list={`${id}-list`}
        value={value}
        disabled={disabled}
        spellCheck={false}
        onChange={(event) => onChange(event.target.value)}
      />
      <datalist id={`${id}-list`}>
        {options.map((opt) => (
          <option key={opt} value={opt} />
        ))}
      </datalist>
    </div>
  );
}
